package inventory

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	catalogapi "github.com/gliedabrennung/go-marketplace-backend/internal/catalog/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/inventory/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/inventory/application/command"
	"github.com/gliedabrennung/go-marketplace-backend/internal/inventory/infrastructure/postgres"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/cqrs"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/outbox"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/scheduler"
)

const (
	offerConsumer  = "inventory.track_offers"
	expiryJob      = "inventory.expire_reservations"
	expiryInterval = 30 * time.Second
)

type WorkerDependencies struct {
	Pool    *pgxpool.Pool
	Clock   application.Clock
	Policy  application.Policy
	Sellers application.SellerDirectory
	Logger  *slog.Logger
	Metrics cqrs.Metrics
}

type Worker struct {
	pool   *pgxpool.Pool
	log    *slog.Logger
	ensure cqrs.Handler[command.EnsureStock, struct{}]
	expire cqrs.Handler[command.ExpireReservations, int]
	batch  int
}

func NewWorker(d WorkerDependencies) *Worker {
	base := command.NewBase(postgres.NewUnitOfWork(d.Pool, postgres.NewOutboxWriter()), d.Clock, d.Sellers, d.Policy)
	return &Worker{
		pool:   d.Pool,
		log:    d.Logger,
		ensure: cqrs.Decorate(moduleName, command.NewEnsureStockHandler(base), d.Logger, d.Metrics),
		expire: cqrs.Decorate(moduleName, command.NewExpireReservationsHandler(base), d.Logger, d.Metrics),
		batch:  d.Policy.ExpiryBatch,
	}
}

func (w *Worker) Subscriptions() []outbox.Subscription {
	return []outbox.Subscription{{
		Consumer:   offerConsumer,
		EventNames: []string{catalogapi.EventOfferCreated},
		Handle:     w.trackOffer,
	}}
}

func (w *Worker) trackOffer(ctx context.Context, msg outbox.Message) error {
	var event catalogapi.OfferV1
	if err := json.Unmarshal(msg.Payload, &event); err != nil || event.OfferID == "" || event.SellerID == "" {
		w.log.ErrorContext(ctx, "skipping malformed event", "event_name", msg.EventName, "outbox_id", msg.ID, "err", err)
		return nil
	}
	_, err := w.ensure.Handle(ctx, command.EnsureStock{SKU: event.OfferID, SellerID: event.SellerID})
	return err
}

func (w *Worker) Jobs() []scheduler.Job {
	return []scheduler.Job{{
		Name:     expiryJob,
		Interval: expiryInterval,
		Run: scheduler.Exclusive(w.pool, expiryJob, func(ctx context.Context) error {
			_, err := w.expire.Handle(ctx, command.ExpireReservations{Limit: w.batch})
			return err
		}),
	}}
}
