package pricing

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	catalogapi "github.com/gliedabrennung/go-marketplace-backend/internal/catalog/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/pricing/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/pricing/application/command"
	"github.com/gliedabrennung/go-marketplace-backend/internal/pricing/infrastructure/postgres"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/cqrs"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/outbox"
)

const (
	offerPriceConsumer = "pricing.sync_offer_prices"
	categoryConsumer   = "pricing.sync_product_categories"
)

type WorkerDependencies struct {
	Pool    *pgxpool.Pool
	Clock   application.Clock
	Policy  application.Policy
	Logger  *slog.Logger
	Metrics cqrs.Metrics
}

type Worker struct {
	log        *slog.Logger
	syncPrice  cqrs.Handler[command.SyncOfferPrice, struct{}]
	categories cqrs.Handler[command.SetProductCategories, struct{}]
}

func NewWorker(d WorkerDependencies) *Worker {
	base := command.NewBase(postgres.NewUnitOfWork(d.Pool, postgres.NewOutboxWriter()), d.Clock, d.Policy)
	return &Worker{
		log:        d.Logger,
		syncPrice:  cqrs.Decorate(moduleName, command.NewSyncOfferPriceHandler(base), d.Logger, d.Metrics),
		categories: cqrs.Decorate(moduleName, command.NewSetProductCategoriesHandler(base), d.Logger, d.Metrics),
	}
}

func (w *Worker) Subscriptions() []outbox.Subscription {
	return []outbox.Subscription{
		{
			Consumer: offerPriceConsumer,
			EventNames: []string{
				catalogapi.EventOfferCreated, catalogapi.EventOfferUpdated, catalogapi.EventOfferStatusChanged,
			},
			Handle: w.syncOffer,
		},
		{
			Consumer:   categoryConsumer,
			EventNames: []string{catalogapi.EventProductPublished},
			Handle:     w.syncCategories,
		},
	}
}

func (w *Worker) syncOffer(ctx context.Context, msg outbox.Message) error {
	var event catalogapi.OfferV1
	if !w.decode(ctx, msg, &event) || event.OfferID == "" {
		return nil
	}
	_, err := w.syncPrice.Handle(ctx, command.SyncOfferPrice{
		SKU: event.OfferID, ProductID: event.ProductID, SellerID: event.SellerID,
		Amount: event.PriceAmount, Currency: event.Currency, Active: event.Status == "active",
	})
	return err
}

func (w *Worker) syncCategories(ctx context.Context, msg outbox.Message) error {
	var event catalogapi.ProductPublishedV1
	if !w.decode(ctx, msg, &event) || event.ProductID == "" {
		return nil
	}
	_, err := w.categories.Handle(ctx, command.SetProductCategories{ProductID: event.ProductID, CategoryPath: event.CategoryPath})
	return err
}

func (w *Worker) decode(ctx context.Context, msg outbox.Message, target any) bool {
	if err := json.Unmarshal(msg.Payload, target); err != nil {
		w.log.ErrorContext(ctx, "skipping malformed event", "event_name", msg.EventName, "outbox_id", msg.ID, "err", err)
		return false
	}
	return true
}
