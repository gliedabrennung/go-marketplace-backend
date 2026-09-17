package cart

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/gliedabrennung/go-marketplace-backend/internal/cart/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/cart/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/cart/application/command"
	"github.com/gliedabrennung/go-marketplace-backend/internal/cart/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/cart/infrastructure/httpapi"
	"github.com/gliedabrennung/go-marketplace-backend/internal/cart/infrastructure/postgres"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/cqrs"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/httpx"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/scheduler"
)

const (
	moduleName  = "cart"
	purgeJob    = "cart.purge_expired"
	purgePeriod = time.Hour
)

type Dependencies struct {
	Pool      *pgxpool.Pool
	Clock     application.Clock
	Policy    application.Policy
	Offers    application.Offers
	Stock     application.Stock
	Pricing   application.Pricing
	Tariffs   application.Tariffs
	Responder *httpx.Responder
	Logger    *slog.Logger
	Metrics   cqrs.Metrics
}

type Module struct {
	pool  *pgxpool.Pool
	api   *httpapi.API
	carts *carts
	purge cqrs.Handler[command.PurgeExpired, int]
}

func NewModule(d Dependencies) *Module {
	assembler := application.NewAssembler(d.Offers, d.Stock, d.Pricing, d.Tariffs)
	base := command.NewBase(postgres.NewUnitOfWork(d.Pool), d.Clock, d.Policy, d.Offers, d.Stock, assembler)
	reader := postgres.NewRepository(d.Pool)

	handlers := httpapi.Handlers{
		Add:         decorate(d, command.NewAddItemHandler(base)),
		Update:      decorate(d, command.NewUpdateItemHandler(base)),
		Remove:      decorate(d, command.NewRemoveItemHandler(base)),
		ApplyPromo:  decorate(d, command.NewApplyPromoCodeHandler(base)),
		RemovePromo: decorate(d, command.NewRemovePromoCodeHandler(base)),
		Merge:       decorate(d, command.NewMergeCartsHandler(base)),
		Get:         decorate(d, query.NewGetCartHandler(reader, d.Clock, d.Policy, assembler)),
	}
	return &Module{
		pool: d.Pool,
		api:  httpapi.NewAPI(handlers, d.Responder),
		carts: &carts{
			checkout: decorate(d, query.NewGetCheckoutHandler(reader)),
			remove:   decorate(d, command.NewRemoveOrderedItemsHandler(base)),
		},
		purge: decorate(d, command.NewPurgeExpiredHandler(base)),
	}
}

func (m *Module) RegisterRoutes(rt *httpx.Router) {
	m.api.Register(rt)
}

func (m *Module) Carts() api.Carts { return m.carts }

func (m *Module) Jobs() []scheduler.Job {
	return []scheduler.Job{{
		Name:     purgeJob,
		Interval: purgePeriod,
		Run: scheduler.Exclusive(m.pool, purgeJob, func(ctx context.Context) error {
			_, err := m.purge.Handle(ctx, command.PurgeExpired{})
			return err
		}),
	}}
}

type carts struct {
	checkout cqrs.Handler[query.GetCheckout, query.Checkout]
	remove   cqrs.Handler[command.RemoveOrderedItems, struct{}]
}

func (c *carts) Checkout(ctx context.Context, userID string) (api.Checkout, error) {
	view, err := c.checkout.Handle(ctx, query.GetCheckout{UserID: userID})
	if err != nil {
		return api.Checkout{}, err
	}
	out := api.Checkout{CartID: view.CartID, Currency: view.Currency, PromoCode: view.PromoCode, Items: make([]api.Item, 0, len(view.Items))}
	for _, item := range view.Items {
		out.Items = append(out.Items, api.Item(item))
	}
	return out, nil
}

func (c *carts) RemoveOrdered(ctx context.Context, userID string, skus []string) error {
	_, err := c.remove.Handle(ctx, command.RemoveOrderedItems{UserID: userID, SKUs: skus})
	return err
}

func decorate[C any, R any](d Dependencies, h cqrs.Handler[C, R]) cqrs.Handler[C, R] {
	return cqrs.Decorate(moduleName, h, d.Logger, d.Metrics)
}
