package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/gliedabrennung/go-marketplace-backend/internal/inventory/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/inventory/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/outbox"
	platform "github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/postgres"
)

const stockColumns = `sku, seller_id::text, available, reserved, created_at, updated_at, version`

type repositories struct {
	q      platform.Querier
	events *outbox.Writer
}

func NewOutboxWriter() *outbox.Writer {
	return outbox.NewWriter("platform", "outbox", NewEventCodec())
}

func NewRepositories(q platform.Querier, events *outbox.Writer) application.Repositories {
	return repositories{q: q, events: events}
}

func NewUnitOfWork(pool *pgxpool.Pool, events *outbox.Writer) *platform.UnitOfWork[application.Repositories] {
	return platform.NewUnitOfWork(pool, func(tx pgx.Tx) application.Repositories {
		return repositories{q: tx, events: events}
	})
}

func (r repositories) Stock() domain.StockRepository { return stockRepository(r) }

type stockRepository struct {
	q      platform.Querier
	events *outbox.Writer
}

func (r stockRepository) FindBySKU(ctx context.Context, sku domain.SKU) (*domain.StockItem, error) {
	snap, err := scanStock(r.q.QueryRow(ctx, `SELECT `+stockColumns+` FROM inventory.stock_items WHERE sku = $1`, sku.String()))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrStockNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("select stock item: %w", err)
	}
	items, err := withHolds(ctx, r.q, []domain.StockItemSnapshot{snap})
	if err != nil {
		return nil, err
	}
	return items[0], nil
}

func (r stockRepository) Lock(ctx context.Context, skus []domain.SKU) ([]*domain.StockItem, error) {
	keys := make([]string, 0, len(skus))
	for _, sku := range skus {
		keys = append(keys, sku.String())
	}
	return r.lock(ctx, `SELECT `+stockColumns+` FROM inventory.stock_items
		WHERE sku = ANY ($1) ORDER BY sku FOR UPDATE`, keys)
}

func (r stockRepository) LockByReservation(ctx context.Context, id domain.ReservationID) ([]*domain.StockItem, error) {
	return r.lock(ctx, `SELECT `+prefixed("s")+` FROM inventory.stock_items s
		WHERE EXISTS (SELECT 1 FROM inventory.reservations r WHERE r.sku = s.sku AND r.id = $1)
		ORDER BY s.sku FOR UPDATE`, id.String())
}

func (r stockRepository) LockExpired(ctx context.Context, before time.Time, limit int) ([]*domain.StockItem, error) {
	return r.lock(ctx, `SELECT `+prefixed("s")+` FROM inventory.stock_items s
		WHERE EXISTS (
			SELECT 1 FROM inventory.reservations r
			WHERE r.sku = s.sku AND r.status = 'held' AND r.expires_at <= $1
		)
		ORDER BY s.sku LIMIT $2 FOR UPDATE`, before.UTC(), limit)
}

func (r stockRepository) lock(ctx context.Context, sql string, args ...any) ([]*domain.StockItem, error) {
	rows, err := r.q.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("lock stock items: %w", err)
	}
	snaps, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.StockItemSnapshot, error) {
		return scanStock(row)
	})
	if err != nil {
		return nil, fmt.Errorf("scan stock items: %w", err)
	}
	return withHolds(ctx, r.q, snaps)
}

func (r stockRepository) Save(ctx context.Context, item *domain.StockItem) error {
	snap := item.Snapshot()
	if snap.Version == 0 {
		_, err := r.q.Exec(ctx, `
			INSERT INTO inventory.stock_items (sku, seller_id, available, reserved, created_at, updated_at, version)
			VALUES ($1, $2, $3, $4, $5, $6, 1)`,
			snap.SKU, snap.SellerID, snap.Available, snap.Reserved, snap.CreatedAt.UTC(), snap.UpdatedAt.UTC())
		if err != nil {
			if name, ok := platform.UniqueViolation(err); ok && name == "stock_items_pkey" {
				return domain.ErrStockExists
			}
			return fmt.Errorf("insert stock item: %w", err)
		}
	} else {
		tag, err := r.q.Exec(ctx, `
			UPDATE inventory.stock_items
			SET available = $2, reserved = $3, updated_at = $4, version = version + 1
			WHERE sku = $1 AND version = $5`,
			snap.SKU, snap.Available, snap.Reserved, snap.UpdatedAt.UTC(), snap.Version)
		if err != nil {
			return fmt.Errorf("update stock item: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return kernel.ErrConcurrentModification
		}
	}
	if err := saveHolds(ctx, r.q, snap); err != nil {
		return err
	}
	if err := saveMovements(ctx, r.q, item.PullMovements()); err != nil {
		return err
	}
	item.AdvanceVersion()
	return r.events.Write(ctx, r.q, item.PullEvents())
}

func saveHolds(ctx context.Context, q platform.Querier, snap domain.StockItemSnapshot) error {
	for _, hold := range snap.Holds {
		_, err := q.Exec(ctx, `
			INSERT INTO inventory.reservations (id, sku, order_id, quantity, status, expires_at, created_at, resolved_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (id, sku) DO UPDATE SET
				quantity = EXCLUDED.quantity, status = EXCLUDED.status,
				expires_at = EXCLUDED.expires_at, resolved_at = EXCLUDED.resolved_at`,
			hold.ReservationID, snap.SKU, nullable(hold.OrderID), hold.Quantity, hold.Status,
			hold.ExpiresAt.UTC(), hold.CreatedAt.UTC(), optionalTime(hold.ResolvedAt))
		if err != nil {
			return fmt.Errorf("save reservation: %w", err)
		}
	}
	return nil
}

func saveMovements(ctx context.Context, q platform.Querier, movements []domain.Movement) error {
	for _, movement := range movements {
		_, err := q.Exec(ctx, `
			INSERT INTO inventory.stock_movements (sku, delta, reason, reference_id, occurred_at)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (reason, reference_id, sku) DO NOTHING`,
			movement.SKU, movement.Delta, movement.Reason, movement.ReferenceID, movement.OccurredAt.UTC())
		if err != nil {
			return fmt.Errorf("record stock movement: %w", err)
		}
	}
	return nil
}

func withHolds(ctx context.Context, q platform.Querier, snaps []domain.StockItemSnapshot) ([]*domain.StockItem, error) {
	if len(snaps) == 0 {
		return nil, nil
	}
	keys := make([]string, 0, len(snaps))
	for _, snap := range snaps {
		keys = append(keys, snap.SKU)
	}
	rows, err := q.Query(ctx, `
		SELECT sku, id::text, COALESCE(order_id::text, ''), quantity, status, expires_at, created_at, resolved_at
		FROM inventory.reservations WHERE sku = ANY ($1) ORDER BY sku, created_at`, keys)
	if err != nil {
		return nil, fmt.Errorf("select reservations: %w", err)
	}
	defer rows.Close()

	holds := map[string][]domain.HoldSnapshot{}
	for rows.Next() {
		var (
			sku      string
			hold     domain.HoldSnapshot
			resolved *time.Time
		)
		if err := rows.Scan(&sku, &hold.ReservationID, &hold.OrderID, &hold.Quantity, &hold.Status,
			&hold.ExpiresAt, &hold.CreatedAt, &resolved); err != nil {
			return nil, fmt.Errorf("scan reservation: %w", err)
		}
		hold.ExpiresAt, hold.CreatedAt = hold.ExpiresAt.UTC(), hold.CreatedAt.UTC()
		if resolved != nil {
			hold.ResolvedAt = resolved.UTC()
		}
		holds[sku] = append(holds[sku], hold)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read reservations: %w", err)
	}

	items := make([]*domain.StockItem, 0, len(snaps))
	for _, snap := range snaps {
		snap.Holds = holds[snap.SKU]
		item, err := domain.RehydrateStockItem(snap)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func scanStock(row pgx.Row) (domain.StockItemSnapshot, error) {
	var snap domain.StockItemSnapshot
	err := row.Scan(&snap.SKU, &snap.SellerID, &snap.Available, &snap.Reserved, &snap.CreatedAt, &snap.UpdatedAt, &snap.Version)
	snap.CreatedAt, snap.UpdatedAt = snap.CreatedAt.UTC(), snap.UpdatedAt.UTC()
	return snap, err
}

func prefixed(alias string) string {
	return alias + `.sku, ` + alias + `.seller_id::text, ` + alias + `.available, ` + alias + `.reserved, ` +
		alias + `.created_at, ` + alias + `.updated_at, ` + alias + `.version`
}

func nullable(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func optionalTime(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	utc := value.UTC()
	return &utc
}
