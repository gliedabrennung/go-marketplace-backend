package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/gliedabrennung/go-marketplace-backend/internal/inventory/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/inventory/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/pagination"
	platform "github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/postgres"
)

type ReadModel struct {
	db platform.Querier
}

func NewReadModel(db platform.Querier) *ReadModel {
	return &ReadModel{db: db}
}

func (m *ReadModel) Stock(ctx context.Context, sku string) (query.StockView, error) {
	var view query.StockView
	err := m.db.QueryRow(ctx, `
		SELECT sku, seller_id::text, available, reserved, updated_at
		FROM inventory.stock_items WHERE sku = $1`, sku).
		Scan(&view.SKU, &view.SellerID, &view.Available, &view.Reserved, &view.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return query.StockView{}, domain.ErrStockNotFound
	}
	if err != nil {
		return query.StockView{}, fmt.Errorf("select stock: %w", err)
	}
	view.UpdatedAt = view.UpdatedAt.UTC()
	return view, nil
}

func (m *ReadModel) SellerStock(ctx context.Context, sellerID string, limit int, after *pagination.Keyset) (pagination.Page[query.StockView], error) {
	sql := `SELECT sku, seller_id::text, available, reserved, updated_at
		FROM inventory.stock_items WHERE seller_id = $1`
	args := []any{sellerID, limit + 1}
	if after != nil {
		sql += ` AND sku > $3`
		args = append(args, after.ID)
	}
	sql += ` ORDER BY sku LIMIT $2`

	rows, err := m.db.Query(ctx, sql, args...)
	if err != nil {
		return pagination.Page[query.StockView]{}, fmt.Errorf("select seller stock: %w", err)
	}
	views, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (query.StockView, error) {
		var view query.StockView
		err := row.Scan(&view.SKU, &view.SellerID, &view.Available, &view.Reserved, &view.UpdatedAt)
		view.UpdatedAt = view.UpdatedAt.UTC()
		return view, err
	})
	if err != nil {
		return pagination.Page[query.StockView]{}, fmt.Errorf("scan seller stock: %w", err)
	}
	return pagination.Build(views, limit, func(v query.StockView) pagination.Keyset {
		return pagination.Keyset{At: v.UpdatedAt, ID: v.SKU}
	}), nil
}

func (m *ReadModel) Movements(ctx context.Context, sku string, limit int) ([]query.MovementView, error) {
	rows, err := m.db.Query(ctx, `
		SELECT sku, delta, reason, reference_id, occurred_at
		FROM inventory.stock_movements WHERE sku = $1 ORDER BY id DESC LIMIT $2`, sku, limit)
	if err != nil {
		return nil, fmt.Errorf("select stock movements: %w", err)
	}
	movements, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (query.MovementView, error) {
		var view query.MovementView
		err := row.Scan(&view.SKU, &view.Delta, &view.Reason, &view.ReferenceID, &view.OccurredAt)
		view.OccurredAt = view.OccurredAt.UTC()
		return view, err
	})
	if err != nil {
		return nil, fmt.Errorf("scan stock movements: %w", err)
	}
	return movements, nil
}

func (m *ReadModel) Reservation(ctx context.Context, reservationID string) (query.ReservationView, error) {
	rows, err := m.db.Query(ctx, `
		SELECT sku, COALESCE(order_id::text, ''), quantity, status, expires_at
		FROM inventory.reservations WHERE id = $1 ORDER BY sku`, reservationID)
	if err != nil {
		return query.ReservationView{}, fmt.Errorf("select reservation: %w", err)
	}
	defer rows.Close()

	view := query.ReservationView{ReservationID: reservationID}
	for rows.Next() {
		var line query.ReservationLineView
		if err := rows.Scan(&line.SKU, &view.OrderID, &line.Quantity, &line.Status, &view.ExpiresAt); err != nil {
			return query.ReservationView{}, fmt.Errorf("scan reservation: %w", err)
		}
		view.ExpiresAt = view.ExpiresAt.UTC()
		view.Status = line.Status
		view.Lines = append(view.Lines, line)
	}
	if err := rows.Err(); err != nil {
		return query.ReservationView{}, fmt.Errorf("read reservation: %w", err)
	}
	if len(view.Lines) == 0 {
		return query.ReservationView{}, domain.ErrReservationNotFound
	}
	return view, nil
}

func (m *ReadModel) Available(ctx context.Context, skus []string) (map[string]int, error) {
	rows, err := m.db.Query(ctx,
		`SELECT sku, available FROM inventory.stock_items WHERE sku = ANY ($1)`, skus)
	if err != nil {
		return nil, fmt.Errorf("select availability: %w", err)
	}
	defer rows.Close()

	out := make(map[string]int, len(skus))
	for rows.Next() {
		var (
			sku       string
			available int
		)
		if err := rows.Scan(&sku, &available); err != nil {
			return nil, fmt.Errorf("scan availability: %w", err)
		}
		out[sku] = available
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read availability: %w", err)
	}
	return out, nil
}
