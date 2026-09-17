package payment

import (
	"context"
	"log/slog"
	"net/netip"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/gliedabrennung/go-marketplace-backend/internal/payment/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/payment/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/payment/application/command"
	"github.com/gliedabrennung/go-marketplace-backend/internal/payment/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/payment/infrastructure/httpapi"
	"github.com/gliedabrennung/go-marketplace-backend/internal/payment/infrastructure/postgres"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/cqrs"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/httpx"
)

const moduleName = "payment"

type Dependencies struct {
	Pool      *pgxpool.Pool
	Clock     application.Clock
	Providers application.Providers
	Allowlist []netip.Prefix
	Outcomes  func(provider, outcome string)
	Responder *httpx.Responder
	Logger    *slog.Logger
	Metrics   cqrs.Metrics
}

type Module struct {
	api      *httpapi.API
	payments *payments
}

func NewModule(d Dependencies) *Module {
	if d.Outcomes == nil {
		d.Outcomes = func(string, string) {}
	}
	base := command.NewBase(postgres.NewUnitOfWork(d.Pool, postgres.NewOutboxWriter()), d.Clock, d.Providers)
	reads := postgres.NewReadModel(d.Pool)
	webhook := decorate(d, command.NewHandleWebhookHandler(base))

	handlers := httpapi.Handlers{
		Webhook:           observed{next: webhook, outcomes: d.Outcomes},
		RemoveMethod:      decorate(d, command.NewRemoveSavedMethodHandler(base)),
		GetPayment:        decorate(d, query.NewGetPaymentHandler(reads)),
		ListMethods:       decorate(d, query.NewListSavedMethodsHandler(reads)),
		GetReconciliation: decorate(d, query.NewGetReconciliationHandler(reads)),
	}
	return &Module{
		api: httpapi.NewAPI(handlers, d.Responder, d.Allowlist),
		payments: &payments{
			create:  decorate(d, command.NewCreatePaymentHandler(base)),
			capture: decorate(d, command.NewCapturePaymentHandler(base)),
			cancel:  decorate(d, command.NewCancelPaymentHandler(base)),
			refund:  decorate(d, command.NewRefundPaymentHandler(base)),
			reads:   reads,
		},
	}
}

func (m *Module) RegisterRoutes(rt *httpx.Router) {
	m.api.Register(rt)
}

func (m *Module) Payments() api.Payments { return m.payments }

type observed struct {
	next     cqrs.Handler[command.HandleWebhook, command.HandleWebhookResult]
	outcomes func(provider, outcome string)
}

func (o observed) Handle(ctx context.Context, cmd command.HandleWebhook) (command.HandleWebhookResult, error) {
	result, err := o.next.Handle(ctx, cmd)
	if err == nil && result.Applied {
		o.outcomes(cmd.Provider, result.Type)
	}
	return result, err
}

type payments struct {
	create  cqrs.Handler[command.CreatePayment, command.CreatePaymentResult]
	capture cqrs.Handler[command.CapturePayment, struct{}]
	cancel  cqrs.Handler[command.CancelPayment, struct{}]
	refund  cqrs.Handler[command.RefundPayment, command.RefundPaymentResult]
	reads   query.ReadModel
}

func (p *payments) Create(ctx context.Context, request api.CreateRequest) (api.Created, error) {
	result, err := p.create.Handle(ctx, command.CreatePayment(request))
	if err != nil {
		return api.Created{}, err
	}
	return api.Created(result), nil
}

func (p *payments) Capture(ctx context.Context, paymentID string, amount int64) error {
	_, err := p.capture.Handle(ctx, command.CapturePayment{PaymentID: paymentID, Amount: amount})
	return err
}

func (p *payments) Cancel(ctx context.Context, paymentID, reason string) error {
	_, err := p.cancel.Handle(ctx, command.CancelPayment{PaymentID: paymentID, Reason: reason})
	return err
}

func (p *payments) Refund(ctx context.Context, paymentID, refundID string, amount int64, reason string) error {
	_, err := p.refund.Handle(ctx, command.RefundPayment{PaymentID: paymentID, RefundID: refundID, Amount: amount, Reason: reason})
	return err
}

func (p *payments) Info(ctx context.Context, paymentID string) (api.Info, error) {
	view, err := p.reads.Payment(ctx, paymentID)
	if err != nil {
		return api.Info{}, err
	}
	return api.Info{
		PaymentID: view.ID, OrderID: view.OrderID, Status: view.Status, RedirectURL: view.RedirectURL,
		Amount: view.Amount, Authorized: view.Authorized, Captured: view.Captured, Refunded: view.Refunded,
		Currency: view.Currency,
	}, nil
}

func decorate[C any, R any](d Dependencies, h cqrs.Handler[C, R]) cqrs.Handler[C, R] {
	return cqrs.Decorate(moduleName, h, d.Logger, d.Metrics)
}
