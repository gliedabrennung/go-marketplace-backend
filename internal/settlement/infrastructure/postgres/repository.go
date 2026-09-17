package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/gliedabrennung/go-marketplace-backend/internal/settlement/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/settlement/domain"
	platform "github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/postgres"
)

type repositories struct {
	q platform.Querier
}

func NewRepositories(q platform.Querier) application.Repositories {
	return repositories{q: q}
}

func NewUnitOfWork(pool *pgxpool.Pool) *platform.UnitOfWork[application.Repositories] {
	return platform.NewUnitOfWork(pool, func(tx pgx.Tx) application.Repositories {
		return repositories{q: tx}
	})
}

func (r repositories) Entries() domain.Repository { return Repository(r) }

type Repository struct {
	q platform.Querier
}

func NewRepository(q platform.Querier) Repository {
	return Repository{q: q}
}

const entryColumns = `id::text, order_id::text, seller_id::text, currency, gross, commission, net, accrued_at`

func (r Repository) FindByOrderAndSeller(ctx context.Context, orderID, sellerID string) (*domain.Entry, error) {
	snap, err := scanEntry(r.q.QueryRow(ctx, `SELECT `+entryColumns+` FROM settlement.entries WHERE order_id = $1 AND seller_id = $2`,
		orderID, sellerID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrEntryNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("select settlement entry: %w", err)
	}
	return domain.RehydrateEntry(snap)
}

func (r Repository) Save(ctx context.Context, entry *domain.Entry) error {
	s := entry.Snapshot()
	tag, err := r.q.Exec(ctx, `
		INSERT INTO settlement.entries (id, order_id, seller_id, currency, gross, commission, net, accrued_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) ON CONFLICT (order_id, seller_id) DO NOTHING`,
		s.ID, s.OrderID, s.SellerID, s.Currency, s.Gross, s.Commission, s.Net, s.AccruedAt.UTC())
	if err != nil {
		return fmt.Errorf("insert settlement entry: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrAlreadyAccrued
	}
	return nil
}

func (r Repository) ListBySeller(ctx context.Context, sellerID string, from, to time.Time) ([]*domain.Entry, error) {
	rows, err := r.q.Query(ctx, `SELECT `+entryColumns+` FROM settlement.entries
		WHERE seller_id = $1 AND accrued_at >= $2 AND accrued_at < $3 ORDER BY accrued_at`, sellerID, from.UTC(), to.UTC())
	if err != nil {
		return nil, fmt.Errorf("select settlement entries: %w", err)
	}
	snaps, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.EntrySnapshot, error) { return scanEntry(row) })
	if err != nil {
		return nil, fmt.Errorf("scan settlement entries: %w", err)
	}
	out := make([]*domain.Entry, 0, len(snaps))
	for _, snap := range snaps {
		entry, err := domain.RehydrateEntry(snap)
		if err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	return out, nil
}

func scanEntry(row pgx.Row) (domain.EntrySnapshot, error) {
	var s domain.EntrySnapshot
	err := row.Scan(&s.ID, &s.OrderID, &s.SellerID, &s.Currency, &s.Gross, &s.Commission, &s.Net, &s.AccruedAt)
	s.AccruedAt = s.AccruedAt.UTC()
	return s, err
}
