package search

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	catalogapi "github.com/gliedabrennung/go-marketplace-backend/internal/catalog/api"
	inventoryapi "github.com/gliedabrennung/go-marketplace-backend/internal/inventory/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/search/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/search/application/command"
	"github.com/gliedabrennung/go-marketplace-backend/internal/search/infrastructure/postgres"
	sellerapi "github.com/gliedabrennung/go-marketplace-backend/internal/seller/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/cqrs"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/outbox"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/scheduler"
)

const (
	productConsumer = "search.index_products"
	offerConsumer   = "search.index_offers"
	sellerConsumer  = "search.index_sellers"
	stockConsumer   = "search.index_stock"
	reindexJob      = "search.run_reindex"
	lexiconJob      = "search.refresh_lexicon"
)

type WorkerDependencies struct {
	Pool    *pgxpool.Pool
	Clock   application.Clock
	Policy  application.Policy
	Feed    catalogapi.Feed
	Logger  *slog.Logger
	Metrics cqrs.Metrics
}

type Worker struct {
	pool           *pgxpool.Pool
	log            *slog.Logger
	indexProduct   cqrs.Handler[command.IndexProduct, struct{}]
	setCover       cqrs.Handler[command.SetProductCover, struct{}]
	indexOffer     cqrs.Handler[command.IndexOffer, struct{}]
	indexSeller    cqrs.Handler[command.IndexSeller, struct{}]
	indexStock     cqrs.Handler[command.IndexStock, struct{}]
	runReindex     cqrs.Handler[command.RunReindex, command.RunReindexResult]
	refreshLexicon cqrs.Handler[command.RefreshLexicon, int]
}

func NewWorker(d WorkerDependencies) *Worker {
	base := command.NewBase(postgres.NewIndex(d.Pool), d.Clock, d.Policy)
	jobs := postgres.NewReindexJobs(d.Pool)
	return &Worker{
		pool:           d.Pool,
		log:            d.Logger,
		indexProduct:   cqrs.Decorate(moduleName, command.NewIndexProductHandler(base), d.Logger, d.Metrics),
		setCover:       cqrs.Decorate(moduleName, command.NewSetProductCoverHandler(base), d.Logger, d.Metrics),
		indexOffer:     cqrs.Decorate(moduleName, command.NewIndexOfferHandler(base), d.Logger, d.Metrics),
		indexSeller:    cqrs.Decorate(moduleName, command.NewIndexSellerHandler(base), d.Logger, d.Metrics),
		indexStock:     cqrs.Decorate(moduleName, command.NewIndexStockHandler(base), d.Logger, d.Metrics),
		runReindex:     cqrs.Decorate(moduleName, command.NewRunReindexHandler(base, jobs, d.Feed), d.Logger, d.Metrics),
		refreshLexicon: cqrs.Decorate(moduleName, command.NewRefreshLexiconHandler(base), d.Logger, d.Metrics),
	}
}

func (w *Worker) Subscriptions() []outbox.Subscription {
	return []outbox.Subscription{
		{
			Consumer:   productConsumer,
			EventNames: []string{catalogapi.EventProductPublished, catalogapi.EventProductImageProcessed},
			Handle:     w.indexProducts,
		},
		{
			Consumer: offerConsumer,
			EventNames: []string{
				catalogapi.EventOfferCreated, catalogapi.EventOfferUpdated, catalogapi.EventOfferStatusChanged,
			},
			Handle: w.indexOffers,
		},
		{
			Consumer: sellerConsumer,
			EventNames: []string{
				sellerapi.EventApplicationApproved, sellerapi.EventSuspended,
				sellerapi.EventReinstated, sellerapi.EventTerminated,
			},
			Handle: w.indexSellers,
		},
		{
			Consumer:   stockConsumer,
			EventNames: []string{inventoryapi.EventStockChanged},
			Handle:     w.indexStockLevels,
		},
	}
}

func (w *Worker) indexStockLevels(ctx context.Context, msg outbox.Message) error {
	var event inventoryapi.StockChangedV1
	if !w.decode(ctx, msg, &event) || event.SKU == "" {
		return nil
	}
	_, err := w.indexStock.Handle(ctx, command.IndexStock{SKU: event.SKU, Available: event.Available})
	return err
}

func (w *Worker) indexProducts(ctx context.Context, msg outbox.Message) error {
	switch msg.EventName {
	case catalogapi.EventProductPublished:
		var event catalogapi.ProductPublishedV1
		if !w.decode(ctx, msg, &event) || event.ProductID == "" {
			return nil
		}
		_, err := w.indexProduct.Handle(ctx, command.IndexProduct{Document: catalogapi.ProductDocument{
			ProductID: event.ProductID, SellerID: event.SellerID, CategoryID: leaf(event.CategoryPath),
			CategoryPath: event.CategoryPath, Title: event.Title, Description: event.Description, Brand: event.Brand,
			CoverKey: event.CoverKey, Attributes: event.Attributes, PublishedAt: event.OccurredAt,
		}})
		return err
	default:
		var event catalogapi.ProductImageProcessedV1
		if !w.decode(ctx, msg, &event) || event.ProductID == "" || !event.Published {
			return nil
		}
		_, err := w.setCover.Handle(ctx, command.SetProductCover{ProductID: event.ProductID, CoverKey: event.CoverKey})
		return err
	}
}

func (w *Worker) indexOffers(ctx context.Context, msg outbox.Message) error {
	var event catalogapi.OfferV1
	if !w.decode(ctx, msg, &event) || event.OfferID == "" {
		return nil
	}
	_, err := w.indexOffer.Handle(ctx, command.IndexOffer{Offer: application.OfferState{
		OfferID: event.OfferID, ProductID: event.ProductID, SellerID: event.SellerID, Price: event.PriceAmount,
		Currency: event.Currency, Condition: event.Condition, Status: event.Status, UpdatedAt: event.OccurredAt,
	}})
	return err
}

func (w *Worker) indexSellers(ctx context.Context, msg outbox.Message) error {
	var event struct {
		SellerID string `json:"seller_id"`
	}
	if !w.decode(ctx, msg, &event) || event.SellerID == "" {
		return nil
	}
	canSell := msg.EventName == sellerapi.EventApplicationApproved || msg.EventName == sellerapi.EventReinstated
	_, err := w.indexSeller.Handle(ctx, command.IndexSeller{SellerID: event.SellerID, CanSell: canSell})
	return err
}

func (w *Worker) decode(ctx context.Context, msg outbox.Message, target any) bool {
	if err := json.Unmarshal(msg.Payload, target); err != nil {
		w.log.ErrorContext(ctx, "skipping malformed event", "event_name", msg.EventName, "outbox_id", msg.ID, "err", err)
		return false
	}
	return true
}

func (w *Worker) Jobs() []scheduler.Job {
	return []scheduler.Job{
		{
			Name:     reindexJob,
			Interval: 30 * time.Second,
			Run: scheduler.Exclusive(w.pool, reindexJob, func(ctx context.Context) error {
				_, err := w.runReindex.Handle(ctx, command.RunReindex{})
				return err
			}),
		},
		{
			Name:     lexiconJob,
			Interval: time.Hour,
			Run: scheduler.Exclusive(w.pool, lexiconJob, func(ctx context.Context) error {
				_, err := w.refreshLexicon.Handle(ctx, command.RefreshLexicon{})
				return err
			}),
		},
	}
}

func leaf(path []string) string {
	if len(path) == 0 {
		return ""
	}
	return path[len(path)-1]
}
