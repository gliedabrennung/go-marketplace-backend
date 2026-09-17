package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/gliedabrennung/go-marketplace-backend/internal/ordering/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/ordering/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/outbox"
	platform "github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/postgres"
)

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

func (r repositories) Orders() domain.OrderRepository { return orderRepository(r) }

func (r repositories) Sagas() domain.SagaRepository { return sagaRepository{q: r.q} }

type orderRepository struct {
	q      platform.Querier
	events *outbox.Writer
}

func (r orderRepository) FindByID(ctx context.Context, id domain.OrderID) (*domain.Order, error) {
	snap, err := loadOrder(ctx, r.q, id.String(), true)
	if err != nil {
		return nil, err
	}
	return domain.RehydrateOrder(snap)
}

const orderColumns = `id::text, buyer_id::text, status, address, delivery_method, promo_code, currency, subtotal, discount,
	shipping, total, payment_id, created_at, updated_at, delivered_at, version`

func loadOrder(ctx context.Context, q platform.Querier, id string, lock bool) (domain.OrderSnapshot, error) {
	sql := `SELECT ` + orderColumns + ` FROM ordering.orders WHERE id = $1`
	if lock {
		sql += ` FOR UPDATE`
	}
	var (
		s           domain.OrderSnapshot
		address     []byte
		deliveredAt *time.Time
	)
	err := q.QueryRow(ctx, sql, id).Scan(&s.ID, &s.BuyerID, &s.Status, &address, &s.DeliveryMethod, &s.PromoCode, &s.Currency,
		&s.Subtotal, &s.Discount, &s.Shipping, &s.Total, &s.PaymentID, &s.CreatedAt, &s.UpdatedAt, &deliveredAt, &s.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.OrderSnapshot{}, domain.ErrOrderNotFound
	}
	if err != nil {
		return domain.OrderSnapshot{}, fmt.Errorf("select order: %w", err)
	}
	if err := json.Unmarshal(address, &s.Address); err != nil {
		return domain.OrderSnapshot{}, fmt.Errorf("decode order address: %w", err)
	}
	s.CreatedAt, s.UpdatedAt = s.CreatedAt.UTC(), s.UpdatedAt.UTC()
	s.DeliveredAt = moment(deliveredAt)
	if s.Items, err = items(ctx, q, id, ""); err != nil {
		return domain.OrderSnapshot{}, err
	}
	rows, err := q.Query(ctx, `SELECT seller_id::text, subtotal, discount, shipping, total FROM ordering.order_parts
		WHERE order_id = $1 ORDER BY position`, id)
	if err != nil {
		return domain.OrderSnapshot{}, fmt.Errorf("select order parts: %w", err)
	}
	if s.Parts, err = pgx.CollectRows(rows, pgx.RowToStructByPos[domain.PartSnapshot]); err != nil {
		return domain.OrderSnapshot{}, fmt.Errorf("scan order parts: %w", err)
	}
	rows, err = q.Query(ctx, `SELECT from_status, to_status, actor_kind, actor_id, reason, changed_at
		FROM ordering.order_status_history WHERE order_id = $1 ORDER BY position`, id)
	if err != nil {
		return domain.OrderSnapshot{}, fmt.Errorf("select order history: %w", err)
	}
	s.History, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.ChangeSnapshot, error) {
		var change domain.ChangeSnapshot
		err := row.Scan(&change.From, &change.To, &change.ActorKind, &change.ActorID, &change.Reason, &change.At)
		change.At = change.At.UTC()
		return change, err
	})
	if err != nil {
		return domain.OrderSnapshot{}, fmt.Errorf("scan order history: %w", err)
	}
	return s, nil
}

func items(ctx context.Context, q platform.Querier, orderID, sellerID string) ([]domain.ItemSnapshot, error) {
	rows, err := q.Query(ctx, `SELECT sku, product_id, COALESCE(category_id::text, ''), seller_id::text, title, quantity,
			unit_price, base, final
		FROM ordering.order_items WHERE order_id = $1 AND ($2 = '' OR seller_id::text = $2) ORDER BY position`, orderID, sellerID)
	if err != nil {
		return nil, fmt.Errorf("select order items: %w", err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByPos[domain.ItemSnapshot])
	if err != nil {
		return nil, fmt.Errorf("scan order items: %w", err)
	}
	return out, nil
}

func (r orderRepository) Save(ctx context.Context, order *domain.Order) error {
	s := order.Snapshot()
	address, err := json.Marshal(s.Address)
	if err != nil {
		return fmt.Errorf("encode order address: %w", err)
	}
	if s.Version == 0 {
		err = r.insert(ctx, s, address)
	} else {
		err = r.update(ctx, s)
	}
	if err != nil {
		return err
	}
	for position, change := range s.History {
		if _, err := r.q.Exec(ctx, `
			INSERT INTO ordering.order_status_history (order_id, position, from_status, to_status, actor_kind, actor_id, reason, changed_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8) ON CONFLICT DO NOTHING`,
			s.ID, position, change.From, change.To, change.ActorKind, change.ActorID, change.Reason, change.At.UTC()); err != nil {
			return fmt.Errorf("insert order history: %w", err)
		}
	}
	order.AdvanceVersion()
	return r.events.Write(ctx, r.q, order.PullEvents())
}

func (r orderRepository) insert(ctx context.Context, s domain.OrderSnapshot, address []byte) error {
	_, err := r.q.Exec(ctx, `
		INSERT INTO ordering.orders (id, buyer_id, status, address, delivery_method, promo_code, currency, subtotal, discount,
			shipping, total, payment_id, created_at, updated_at, version)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, 1)`,
		s.ID, s.BuyerID, s.Status, address, s.DeliveryMethod, s.PromoCode, s.Currency, s.Subtotal, s.Discount, s.Shipping,
		s.Total, s.PaymentID, s.CreatedAt.UTC(), s.UpdatedAt.UTC())
	if _, ok := platform.UniqueViolation(err); ok {
		return kernel.ErrConcurrentModification
	}
	if err != nil {
		return fmt.Errorf("insert order: %w", err)
	}
	for position, item := range s.Items {
		if _, err := r.q.Exec(ctx, `
			INSERT INTO ordering.order_items (order_id, sku, product_id, category_id, seller_id, title, quantity, unit_price,
				base, final, position)
			VALUES ($1, $2, $3, NULLIF($4, '')::uuid, $5, $6, $7, $8, $9, $10, $11)`,
			s.ID, item.SKU, item.ProductID, item.CategoryID, item.SellerID, item.Title, item.Quantity, item.UnitPrice,
			item.Base, item.Final, position); err != nil {
			return fmt.Errorf("insert order item: %w", err)
		}
	}
	for position, part := range s.Parts {
		if _, err := r.q.Exec(ctx, `
			INSERT INTO ordering.order_parts (order_id, seller_id, subtotal, discount, shipping, total, position, order_created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			s.ID, part.SellerID, part.Subtotal, part.Discount, part.Shipping, part.Total, position, s.CreatedAt.UTC()); err != nil {
			return fmt.Errorf("insert order part: %w", err)
		}
	}
	return nil
}

func (r orderRepository) update(ctx context.Context, s domain.OrderSnapshot) error {
	tag, err := r.q.Exec(ctx, `UPDATE ordering.orders SET status = $2, payment_id = $3, updated_at = $4, delivered_at = $5,
		version = version + 1 WHERE id = $1 AND version = $6`,
		s.ID, s.Status, s.PaymentID, s.UpdatedAt.UTC(), optionalTime(s.DeliveredAt), s.Version)
	if err != nil {
		return fmt.Errorf("update order: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return kernel.ErrConcurrentModification
	}
	return nil
}

func (r orderRepository) DeliveredBefore(ctx context.Context, before time.Time, limit int) ([]domain.OrderID, error) {
	rows, err := r.q.Query(ctx, `SELECT id::text FROM ordering.orders WHERE status = 'delivered' AND delivered_at < $1
		ORDER BY delivered_at LIMIT $2`, before.UTC(), limit)
	if err != nil {
		return nil, fmt.Errorf("select delivered orders: %w", err)
	}
	raw, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("scan delivered orders: %w", err)
	}
	out := make([]domain.OrderID, 0, len(raw))
	for _, value := range raw {
		id, err := domain.ParseOrderID(value)
		if err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, nil
}

func optionalTime(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	utc := value.UTC()
	return &utc
}

func moment(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return value.UTC()
}

type sagaRepository struct {
	q platform.Querier
}

const sagaColumns = `order_id::text, buyer_id::text, reservation_id, payment_id, refund_id, promo_code, currency, amount,
	status, step, captured, compensated, reason, last_error, attempts, deadline, created_at, updated_at, version`

func scanSaga(row pgx.Row) (domain.SagaSnapshot, error) {
	var s domain.SagaSnapshot
	err := row.Scan(&s.OrderID, &s.BuyerID, &s.ReservationID, &s.PaymentID, &s.RefundID, &s.PromoCode, &s.Currency, &s.Amount,
		&s.Status, &s.Step, &s.Captured, &s.Compensated, &s.Reason, &s.LastError, &s.Attempts, &s.Deadline, &s.CreatedAt,
		&s.UpdatedAt, &s.Version)
	s.Deadline, s.CreatedAt, s.UpdatedAt = s.Deadline.UTC(), s.CreatedAt.UTC(), s.UpdatedAt.UTC()
	return s, err
}

func (r sagaRepository) FindByOrder(ctx context.Context, id domain.OrderID) (*domain.CheckoutSaga, error) {
	return r.find(ctx, `order_id = $1`, id.String())
}

func (r sagaRepository) FindByPayment(ctx context.Context, paymentID string) (*domain.CheckoutSaga, error) {
	return r.find(ctx, `payment_id = $1`, paymentID)
}

func (r sagaRepository) find(ctx context.Context, where string, arg string) (*domain.CheckoutSaga, error) {
	snap, err := scanSaga(r.q.QueryRow(ctx, `SELECT `+sagaColumns+` FROM ordering.checkout_sagas WHERE `+where+` FOR UPDATE`, arg))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrSagaNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("select checkout saga: %w", err)
	}
	return domain.RehydrateSaga(snap)
}

func (r sagaRepository) Expired(ctx context.Context, now time.Time, limit int) ([]domain.OrderID, error) {
	return r.ids(ctx, `status = 'running' AND step IN ('stock_reserved', 'promo_redeemed', 'awaiting_payment') AND deadline < $1
		ORDER BY deadline`, now.UTC(), limit)
}

func (r sagaRepository) Compensating(ctx context.Context, now time.Time, limit int) ([]domain.OrderID, error) {
	return r.ids(ctx, `status = 'compensating' AND (attempts = 0 OR
		updated_at + LEAST(interval '10 seconds' * power(2, LEAST(attempts - 1, 10)), interval '10 minutes') <= $1)
		ORDER BY updated_at`, now.UTC(), limit)
}

func (r sagaRepository) Stalled(ctx context.Context, before time.Time, limit int) ([]domain.OrderID, error) {
	return r.ids(ctx, `status = 'running' AND step IN ('committing_stock', 'stock_committed', 'payment_captured')
		AND updated_at < $1 ORDER BY updated_at`, before.UTC(), limit)
}

func (r sagaRepository) ids(ctx context.Context, where string, at time.Time, limit int) ([]domain.OrderID, error) {
	rows, err := r.q.Query(ctx, `SELECT order_id::text FROM ordering.checkout_sagas WHERE `+where+` LIMIT $2`, at, limit)
	if err != nil {
		return nil, fmt.Errorf("select checkout sagas: %w", err)
	}
	raw, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("scan checkout sagas: %w", err)
	}
	out := make([]domain.OrderID, 0, len(raw))
	for _, value := range raw {
		id, err := domain.ParseOrderID(value)
		if err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, nil
}

func (r sagaRepository) Save(ctx context.Context, saga *domain.CheckoutSaga) error {
	s := saga.Snapshot()
	if s.Version == 0 {
		_, err := r.q.Exec(ctx, `
			INSERT INTO ordering.checkout_sagas (order_id, buyer_id, reservation_id, payment_id, refund_id, promo_code, currency,
				amount, status, step, captured, compensated, reason, last_error, attempts, deadline, created_at, updated_at, version)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, 1)`,
			s.OrderID, s.BuyerID, s.ReservationID, s.PaymentID, s.RefundID, s.PromoCode, s.Currency, s.Amount, s.Status, s.Step,
			s.Captured, s.Compensated, s.Reason, s.LastError, s.Attempts, s.Deadline.UTC(), s.CreatedAt.UTC(), s.UpdatedAt.UTC())
		if _, ok := platform.UniqueViolation(err); ok {
			return kernel.ErrConcurrentModification
		}
		if err != nil {
			return fmt.Errorf("insert checkout saga: %w", err)
		}
		saga.AdvanceVersion()
		return nil
	}
	tag, err := r.q.Exec(ctx, `
		UPDATE ordering.checkout_sagas SET payment_id = $2, refund_id = $3, status = $4, step = $5, captured = $6,
			compensated = $7, reason = $8, last_error = $9, attempts = $10, updated_at = $11, version = version + 1
		WHERE order_id = $1 AND version = $12`,
		s.OrderID, s.PaymentID, s.RefundID, s.Status, s.Step, s.Captured, s.Compensated, s.Reason, s.LastError, s.Attempts,
		s.UpdatedAt.UTC(), s.Version)
	if _, ok := platform.UniqueViolation(err); ok {
		return kernel.ErrConcurrentModification
	}
	if err != nil {
		return fmt.Errorf("update checkout saga: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return kernel.ErrConcurrentModification
	}
	saga.AdvanceVersion()
	return nil
}
