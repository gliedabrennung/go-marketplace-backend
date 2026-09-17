package application

import (
	"context"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/payment/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

var (
	ErrUnknownProvider  = kernel.NotFound("PAYMENT_UNKNOWN_PROVIDER", "payment provider is not configured")
	ErrInvalidSignature = kernel.Unauthenticated("PAYMENT_INVALID_WEBHOOK_SIGNATURE", "webhook signature is invalid or expired")
	ErrProviderRejected = kernel.BusinessRule("PAYMENT_PROVIDER_REJECTED", "payment provider rejected the operation")
	ErrInvalidWebhook   = kernel.Validation("PAYMENT_INVALID_WEBHOOK", "webhook payload is malformed")
)

const (
	WebhookAuthorized      = "payment.authorized"
	WebhookFailed          = "payment.failed"
	WebhookRefundSucceeded = "refund.succeeded"
	WebhookRefundFailed    = "refund.failed"
)

type Clock interface {
	Now() time.Time
}

type Repositories interface {
	Payments() domain.PaymentRepository
	Methods() domain.MethodRepository
	Webhooks() domain.WebhookLog
}

type UnitOfWork interface {
	Do(ctx context.Context, fn func(ctx context.Context, repos Repositories) error) error
}

type IntentRequest struct {
	PaymentID   string
	OrderID     string
	Amount      kernel.Money
	ReturnURL   string
	Description string
	SaveMethod  bool
	MethodToken string
}

type Intent struct {
	ProviderPaymentID string
	RedirectURL       string
}

type WebhookEvent struct {
	ID                string
	Type              string
	ProviderPaymentID string
	Amount            int64
	Currency          string
	Reason            string
	RefundReference   string
	ProviderRefundID  string
	MethodToken       string
	MethodLabel       string
}

type ProviderTransaction struct {
	ProviderPaymentID string
	Status            string
	Captured          int64
	Refunded          int64
	Currency          string
}

type Provider interface {
	Name() string
	CreateIntent(ctx context.Context, request IntentRequest) (Intent, error)
	Capture(ctx context.Context, providerPaymentID string, amount kernel.Money, idempotencyKey string) error
	Cancel(ctx context.Context, providerPaymentID, idempotencyKey string) error
	Refund(ctx context.Context, providerPaymentID string, amount kernel.Money, idempotencyKey string) (string, error)
	Transactions(ctx context.Context, day time.Time) ([]ProviderTransaction, error)
	Verify(headers map[string]string, body []byte, now time.Time) (WebhookEvent, error)
}

type Providers interface {
	Get(name string) (Provider, error)
	Default() Provider
}

type LedgerEntry struct {
	PaymentID         string
	ProviderPaymentID string
	Status            string
	Captured          int64
	Refunded          int64
	Currency          string
}

type Mismatch struct {
	PaymentID         string
	ProviderPaymentID string
	Field             string
	Internal          string
	Provider          string
}

type ReconciliationReport struct {
	Day        time.Time
	Provider   string
	Checked    int
	Mismatches []Mismatch
	CreatedAt  time.Time
}

type Reconciliations interface {
	Ledger(ctx context.Context, provider string, day time.Time) ([]LedgerEntry, error)
	Save(ctx context.Context, report ReconciliationReport) error
}
