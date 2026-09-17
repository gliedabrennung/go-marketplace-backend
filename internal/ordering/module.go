package ordering

import (
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/gliedabrennung/go-marketplace-backend/internal/ordering/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/ordering/application/command"
	"github.com/gliedabrennung/go-marketplace-backend/internal/ordering/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/ordering/infrastructure/httpapi"
	"github.com/gliedabrennung/go-marketplace-backend/internal/ordering/infrastructure/postgres"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/cqrs"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/httpx"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/observability"
)

const moduleName = "ordering"

type Dependencies struct {
	Pool        *pgxpool.Pool
	Clock       application.Clock
	Policy      application.Policy
	Carts       application.Carts
	Offers      application.Offers
	Pricing     application.Pricing
	Tariffs     application.Tariffs
	Inventory   application.Inventory
	Payments    application.Payments
	Sellers     application.SellerMembership
	Business    application.Metrics
	Responder   *httpx.Responder
	Idempotency httpx.Middleware
	Logger      *slog.Logger
	Metrics     cqrs.Metrics
}

func (d Dependencies) base() command.Base {
	return command.NewBase(command.Dependencies{
		UoW: postgres.NewUnitOfWork(d.Pool, postgres.NewOutboxWriter()), Clock: d.Clock, Policy: d.Policy, Carts: d.Carts,
		Offers: d.Offers, Pricing: d.Pricing, Tariffs: d.Tariffs, Inventory: d.Inventory, Payments: d.Payments,
		Metrics: d.Business, Logger: d.Logger,
	})
}

type Module struct {
	api *httpapi.API
}

func NewModule(d Dependencies) *Module {
	base := d.base()
	compensator := command.NewCompensator(base)
	reads := postgres.NewReadModel(d.Pool)
	place := command.NewPlaceOrderHandler(base, compensator)

	handlers := httpapi.Handlers{
		Place:        decorate(d, place),
		Cancel:       decorate(d, command.NewCancelOrderHandler(base, compensator)),
		RetryPayment: decorate(d, command.NewRetryPaymentHandler(base, place)),
		ResumeSaga:   decorate(d, command.NewResumeSagaHandler(base, compensator)),
		Ship:         decorate(d, command.NewMarkOrderShippedHandler(base, d.Sellers)),
		Deliver:      decorate(d, command.NewMarkOrderDeliveredHandler(base, d.Sellers)),
		Get:          decorate(d, query.NewGetOrderHandler(reads, d.Payments)),
		List:         decorate(d, query.NewListOrdersHandler(reads)),
		SellerOrders: decorate(d, query.NewListSellerOrdersHandler(reads, d.Sellers)),
		Sagas:        decorate(d, query.NewListSagasHandler(reads)),
	}
	return &Module{api: httpapi.NewAPI(handlers, d.Responder, d.Idempotency)}
}

func (m *Module) RegisterRoutes(rt *httpx.Router) {
	m.api.Register(rt)
}

func decorate[C any, R any](d Dependencies, h cqrs.Handler[C, R]) cqrs.Handler[C, R] {
	return cqrs.Decorate(moduleName, h, d.Logger, d.Metrics)
}

type Metrics struct {
	placed        *observability.Counter
	value         *observability.Counter
	steps         *observability.Counter
	compensations *observability.Counter
}

func NewMetrics(reg prometheus.Registerer) *Metrics {
	return &Metrics{
		placed:        observability.NewCounter(reg, "orders_placed_total", "Orders placed by resulting status.", "status"),
		value:         observability.NewCounter(reg, "order_value_total", "Total value of placed orders in minor units.", "currency"),
		steps:         observability.NewCounter(reg, "checkout_saga_step_total", "Checkout saga step outcomes.", "step", "outcome"),
		compensations: observability.NewCounter(reg, "checkout_saga_compensations_total", "Checkout saga compensation outcomes.", "step", "outcome"),
	}
}

func (m *Metrics) OrderPlaced(status, currency string, total int64) {
	m.placed.Inc(status)
	m.value.Add(float64(total), currency)
}

func (m *Metrics) SagaStep(step, outcome string) {
	m.steps.Inc(step, outcome)
}

func (m *Metrics) Compensation(step, outcome string) {
	m.compensations.Inc(step, outcome)
}
