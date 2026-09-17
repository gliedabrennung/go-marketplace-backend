package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/gliedabrennung/go-marketplace-backend/internal/payment/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/payment/domain"
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

func (r repositories) Payments() domain.PaymentRepository { return paymentRepository(r) }

func (r repositories) Methods() domain.MethodRepository { return methodRepository{q: r.q} }

func (r repositories) Webhooks() domain.WebhookLog { return webhookLog{q: r.q} }

const paymentColumns = `id::text, order_id::text, buyer_id::text, provider, COALESCE(provider_payment_id, ''), redirect_url,
	COALESCE(method_id::text, ''), save_method, status, currency, amount, authorized, captured, refunded,
	failure_reason, created_at, updated_at, version`

type paymentRepository struct {
	q      platform.Querier
	events *outbox.Writer
}

func (r paymentRepository) FindByID(ctx context.Context, id domain.PaymentID) (*domain.Payment, error) {
	return r.find(ctx, `WHERE id = $1`, id.String())
}

func (r paymentRepository) FindByProviderID(ctx context.Context, provider, providerPaymentID string) (*domain.Payment, error) {
	return r.find(ctx, `WHERE provider = $1 AND provider_payment_id = $2`, provider, providerPaymentID)
}

func (r paymentRepository) find(ctx context.Context, where string, args ...any) (*domain.Payment, error) {
	snap, err := scanPayment(r.q.QueryRow(ctx, `SELECT `+paymentColumns+` FROM payment.payments `+where+` FOR UPDATE`, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrPaymentNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("select payment: %w", err)
	}
	if snap.Refunds, err = refunds(ctx, r.q, []string{snap.ID}); err != nil {
		return nil, err
	}
	return domain.RehydratePayment(snap)
}

func (r paymentRepository) Save(ctx context.Context, payment *domain.Payment) error {
	s := payment.Snapshot()
	var err error
	if s.Version == 0 {
		_, err = r.q.Exec(ctx, `
			INSERT INTO payment.payments (id, order_id, buyer_id, provider, provider_payment_id, redirect_url, method_id,
				save_method, status, currency, amount, authorized, captured, refunded, failure_reason, created_at,
				updated_at, version)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, 1)`,
			s.ID, s.OrderID, s.BuyerID, s.Provider, optional(s.ProviderPaymentID), s.RedirectURL, optional(s.MethodID),
			s.SaveMethod, s.Status, s.Currency, s.Amount, s.Authorized, s.Captured, s.Refunded, s.FailureReason,
			s.CreatedAt.UTC(), s.UpdatedAt.UTC())
	} else {
		err = r.update(ctx, s)
	}
	if err != nil {
		return wrap("save payment", err)
	}
	for _, refund := range s.Refunds {
		if _, err := r.q.Exec(ctx, `
			INSERT INTO payment.refunds (id, payment_id, amount, reason, status, provider_refund_id, created_at, completed_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (id) DO UPDATE SET reason = EXCLUDED.reason, status = EXCLUDED.status,
				provider_refund_id = EXCLUDED.provider_refund_id, completed_at = EXCLUDED.completed_at`,
			refund.ID, s.ID, refund.Amount, refund.Reason, refund.Status, refund.ProviderRefundID,
			refund.CreatedAt.UTC(), optionalTime(refund.CompletedAt)); err != nil {
			return fmt.Errorf("save refund: %w", err)
		}
	}
	payment.AdvanceVersion()
	return r.events.Write(ctx, r.q, payment.PullEvents())
}

func (r paymentRepository) update(ctx context.Context, s domain.PaymentSnapshot) error {
	tag, err := r.q.Exec(ctx, `
		UPDATE payment.payments SET provider_payment_id = $2, redirect_url = $3, status = $4, authorized = $5,
			captured = $6, refunded = $7, failure_reason = $8, updated_at = $9, version = version + 1
		WHERE id = $1 AND version = $10`,
		s.ID, optional(s.ProviderPaymentID), s.RedirectURL, s.Status, s.Authorized, s.Captured, s.Refunded,
		s.FailureReason, s.UpdatedAt.UTC(), s.Version)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return kernel.ErrConcurrentModification
	}
	return nil
}

func scanPayment(row pgx.Row) (domain.PaymentSnapshot, error) {
	var s domain.PaymentSnapshot
	err := row.Scan(&s.ID, &s.OrderID, &s.BuyerID, &s.Provider, &s.ProviderPaymentID, &s.RedirectURL, &s.MethodID,
		&s.SaveMethod, &s.Status, &s.Currency, &s.Amount, &s.Authorized, &s.Captured, &s.Refunded, &s.FailureReason,
		&s.CreatedAt, &s.UpdatedAt, &s.Version)
	s.CreatedAt, s.UpdatedAt = s.CreatedAt.UTC(), s.UpdatedAt.UTC()
	return s, err
}

func refunds(ctx context.Context, q platform.Querier, paymentIDs []string) ([]domain.RefundSnapshot, error) {
	all, err := refundsByPayment(ctx, q, paymentIDs)
	if err != nil {
		return nil, err
	}
	var out []domain.RefundSnapshot
	for _, id := range paymentIDs {
		out = append(out, all[id]...)
	}
	return out, nil
}

const methodColumns = `id::text, buyer_id::text, provider, token, label, created_at, removed_at, version`

type methodRepository struct {
	q platform.Querier
}

func (r methodRepository) FindByID(ctx context.Context, id domain.MethodID) (*domain.SavedMethod, error) {
	return r.find(ctx, `WHERE id = $1`, id.String())
}

func (r methodRepository) FindByToken(ctx context.Context, provider, token string) (*domain.SavedMethod, error) {
	return r.find(ctx, `WHERE provider = $1 AND token = $2`, provider, token)
}

func (r methodRepository) find(ctx context.Context, where string, args ...any) (*domain.SavedMethod, error) {
	var (
		s       domain.SavedMethodSnapshot
		removed *time.Time
	)
	err := r.q.QueryRow(ctx, `SELECT `+methodColumns+` FROM payment.saved_methods `+where, args...).
		Scan(&s.ID, &s.BuyerID, &s.Provider, &s.Token, &s.Label, &s.CreatedAt, &removed, &s.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrMethodNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("select saved method: %w", err)
	}
	s.CreatedAt, s.RemovedAt = s.CreatedAt.UTC(), moment(removed)
	return domain.RehydrateSavedMethod(s)
}

func (r methodRepository) Save(ctx context.Context, method *domain.SavedMethod) error {
	s := method.Snapshot()
	if s.Version == 0 {
		_, err := r.q.Exec(ctx, `
			INSERT INTO payment.saved_methods (id, buyer_id, provider, token, label, created_at, removed_at, version)
			VALUES ($1, $2, $3, $4, $5, $6, $7, 1)
			ON CONFLICT ON CONSTRAINT uq_saved_methods_token DO NOTHING`,
			s.ID, s.BuyerID, s.Provider, s.Token, s.Label, s.CreatedAt.UTC(), optionalTime(s.RemovedAt))
		if err != nil {
			return fmt.Errorf("insert saved method: %w", err)
		}
		method.AdvanceVersion()
		return nil
	}
	tag, err := r.q.Exec(ctx, `UPDATE payment.saved_methods SET label = $2, removed_at = $3, version = version + 1
		WHERE id = $1 AND version = $4`, s.ID, s.Label, optionalTime(s.RemovedAt), s.Version)
	if err != nil {
		return fmt.Errorf("update saved method: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return kernel.ErrConcurrentModification
	}
	method.AdvanceVersion()
	return nil
}

type webhookLog struct {
	q platform.Querier
}

func (w webhookLog) Record(ctx context.Context, provider, eventID, eventType string, receivedAt time.Time) (bool, error) {
	tag, err := w.q.Exec(ctx, `
		INSERT INTO payment.webhook_events (provider, event_id, event_type, received_at) VALUES ($1, $2, $3, $4)
		ON CONFLICT DO NOTHING`, provider, eventID, eventType, receivedAt.UTC())
	if err != nil {
		return false, fmt.Errorf("record webhook: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func wrap(op string, err error) error {
	if errors.Is(err, kernel.ErrConcurrentModification) {
		return err
	}
	if name, ok := platform.CheckViolation(err); ok && name == "chk_payments_amounts" {
		return fmt.Errorf("%s: %w", op, domain.ErrAmountMismatch)
	}
	if _, ok := platform.UniqueViolation(err); ok {
		return kernel.ErrConcurrentModification
	}
	return fmt.Errorf("%s: %w", op, err)
}

func optional(value string) *string {
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

func moment(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return value.UTC()
}
