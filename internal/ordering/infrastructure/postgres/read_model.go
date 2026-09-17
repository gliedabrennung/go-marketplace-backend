package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/gliedabrennung/go-marketplace-backend/internal/ordering/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/ordering/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/pagination"
	platform "github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/postgres"
)

type ReadModel struct {
	db platform.Querier
}

func NewReadModel(db platform.Querier) *ReadModel {
	return &ReadModel{db: db}
}

func (m *ReadModel) Order(ctx context.Context, id string) (domain.OrderSnapshot, query.SagaState, error) {
	snap, err := loadOrder(ctx, m.db, id, false)
	if err != nil {
		return domain.OrderSnapshot{}, query.SagaState{}, err
	}
	var state query.SagaState
	err = m.db.QueryRow(ctx, `SELECT status, step, payment_id, deadline FROM ordering.checkout_sagas WHERE order_id = $1`, id).
		Scan(&state.Status, &state.Step, &state.PaymentID, &state.Deadline)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return domain.OrderSnapshot{}, query.SagaState{}, fmt.Errorf("select saga state: %w", err)
	}
	state.Deadline = state.Deadline.UTC()
	return snap, state, nil
}

func keyset(sql string, args []any, after *pagination.Keyset, columns string) (string, []any) {
	if after == nil {
		return sql, args
	}
	args = append(args, after.At, after.ID)
	return sql + fmt.Sprintf(` AND (%s) < ($%d, $%d::uuid)`, columns, len(args)-1, len(args)), args
}

func (m *ReadModel) BuyerOrders(ctx context.Context, buyerID, status string, limit int, after *pagination.Keyset) (pagination.Page[query.OrderSummaryView], error) {
	sql, args := keyset(`SELECT o.id::text, o.status, o.currency, o.total,
			(SELECT COALESCE(SUM(i.quantity), 0)::int FROM ordering.order_items i WHERE i.order_id = o.id), o.created_at
		FROM ordering.orders o WHERE o.buyer_id = $1 AND ($2 = '' OR o.status = $2)`,
		[]any{buyerID, status, limit + 1}, after, "o.created_at, o.id")
	rows, err := m.db.Query(ctx, sql+` ORDER BY o.created_at DESC, o.id DESC LIMIT $3`, args...)
	if err != nil {
		return pagination.Page[query.OrderSummaryView]{}, fmt.Errorf("select buyer orders: %w", err)
	}
	views, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (query.OrderSummaryView, error) {
		var v query.OrderSummaryView
		err := row.Scan(&v.ID, &v.Status, &v.Currency, &v.Total, &v.ItemsCount, &v.CreatedAt)
		v.CreatedAt = v.CreatedAt.UTC()
		return v, err
	})
	if err != nil {
		return pagination.Page[query.OrderSummaryView]{}, fmt.Errorf("scan buyer orders: %w", err)
	}
	return pagination.Build(views, limit, func(v query.OrderSummaryView) pagination.Keyset {
		return pagination.Keyset{At: v.CreatedAt, ID: v.ID}
	}), nil
}

func (m *ReadModel) SellerOrders(ctx context.Context, sellerID, status string, limit int, after *pagination.Keyset) (pagination.Page[query.SellerOrderView], error) {
	sql, args := keyset(`SELECT o.id::text, o.status, o.currency, p.seller_id::text, p.subtotal, p.discount, p.shipping, p.total,
			p.order_created_at
		FROM ordering.order_parts p JOIN ordering.orders o ON o.id = p.order_id
		WHERE p.seller_id = $1 AND ($2 = '' OR o.status = $2)
			AND o.status IN ('paid', 'in_fulfilment', 'shipped', 'delivered', 'completed', 'cancelled', 'returning', 'returned')`,
		[]any{sellerID, status, limit + 1}, after, "p.order_created_at, p.order_id")
	rows, err := m.db.Query(ctx, sql+` ORDER BY p.order_created_at DESC, p.order_id DESC LIMIT $3`, args...)
	if err != nil {
		return pagination.Page[query.SellerOrderView]{}, fmt.Errorf("select seller orders: %w", err)
	}
	views, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (query.SellerOrderView, error) {
		var v query.SellerOrderView
		err := row.Scan(&v.ID, &v.Status, &v.Currency, &v.Part.SellerID, &v.Part.Subtotal, &v.Part.Discount, &v.Part.Shipping,
			&v.Part.Total, &v.CreatedAt)
		v.CreatedAt = v.CreatedAt.UTC()
		return v, err
	})
	if err != nil {
		return pagination.Page[query.SellerOrderView]{}, fmt.Errorf("scan seller orders: %w", err)
	}
	page := pagination.Build(views, limit, func(v query.SellerOrderView) pagination.Keyset {
		return pagination.Keyset{At: v.CreatedAt, ID: v.ID}
	})
	for i := range page.Items {
		snaps, err := items(ctx, m.db, page.Items[i].ID, sellerID)
		if err != nil {
			return pagination.Page[query.SellerOrderView]{}, err
		}
		for _, item := range snaps {
			page.Items[i].Items = append(page.Items[i].Items, query.NewItemView(item))
		}
	}
	return page, nil
}

func (m *ReadModel) Sagas(ctx context.Context, status string, limit int, after *pagination.Keyset) (pagination.Page[query.SagaView], error) {
	sql, args := keyset(`SELECT `+sagaColumns+` FROM ordering.checkout_sagas WHERE ($1 = '' OR status = $1)`,
		[]any{status, limit + 1}, after, "updated_at, order_id")
	rows, err := m.db.Query(ctx, sql+` ORDER BY updated_at DESC, order_id DESC LIMIT $2`, args...)
	if err != nil {
		return pagination.Page[query.SagaView]{}, fmt.Errorf("select checkout sagas: %w", err)
	}
	views, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (query.SagaView, error) {
		s, err := scanSaga(row)
		return query.SagaView{
			OrderID: s.OrderID, BuyerID: s.BuyerID, Status: s.Status, Step: s.Step, ReservationID: s.ReservationID,
			PaymentID: s.PaymentID, Reason: s.Reason, LastError: s.LastError, Attempts: s.Attempts, Compensated: s.Compensated,
			Deadline: s.Deadline, UpdatedAt: s.UpdatedAt,
		}, err
	})
	if err != nil {
		return pagination.Page[query.SagaView]{}, fmt.Errorf("scan checkout sagas: %w", err)
	}
	return pagination.Build(views, limit, func(v query.SagaView) pagination.Keyset {
		return pagination.Keyset{At: v.UpdatedAt, ID: v.OrderID}
	}), nil
}
