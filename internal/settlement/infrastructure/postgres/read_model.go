package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/gliedabrennung/go-marketplace-backend/internal/settlement/application/query"
	platform "github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/postgres"
)

type ReadModel struct {
	db platform.Querier
}

func NewReadModel(db platform.Querier) *ReadModel {
	return &ReadModel{db: db}
}

func (m *ReadModel) ListBySeller(ctx context.Context, sellerID string, from, to time.Time) ([]query.EntryView, error) {
	rows, err := m.db.Query(ctx, `SELECT id::text, order_id::text, currency, gross, commission, net, accrued_at
		FROM settlement.entries WHERE seller_id = $1 AND accrued_at >= $2 AND accrued_at < $3 ORDER BY accrued_at`,
		sellerID, from.UTC(), to.UTC())
	if err != nil {
		return nil, fmt.Errorf("select settlement report: %w", err)
	}
	views, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (query.EntryView, error) {
		var v query.EntryView
		err := row.Scan(&v.ID, &v.OrderID, &v.Currency, &v.Gross, &v.Commission, &v.Net, &v.AccruedAt)
		v.AccruedAt = v.AccruedAt.UTC()
		return v, err
	})
	if err != nil {
		return nil, fmt.Errorf("scan settlement report: %w", err)
	}
	return views, nil
}
