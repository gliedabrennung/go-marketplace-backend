package catalog

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application/command"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/infrastructure/postgres"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/cqrs"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/outbox"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/scheduler"
)

const (
	thumbnailConsumer  = "catalog.image_thumbnails"
	offerImportRunner  = "catalog.offer_import_runner"
	requeueImportsJob  = "catalog.requeue_stale_imports"
	requeueImportsSize = 50
)

type WorkerDependencies struct {
	Pool       *pgxpool.Pool
	Clock      application.Clock
	Policy     application.Policy
	Sellers    application.SellerDirectory
	Thumbnails application.ThumbnailRenderer
	Source     application.ImportSource
	Reports    application.ImportReportWriter
	Logger     *slog.Logger
	Metrics    cqrs.Metrics
}

type Worker struct {
	pool           *pgxpool.Pool
	log            *slog.Logger
	processImage   cqrs.Handler[command.ProcessImage, struct{}]
	processImport  cqrs.Handler[command.ProcessImport, command.ProcessImportResult]
	requeueImports cqrs.Handler[command.RequeueStaleImports, int]
}

func NewWorker(d WorkerDependencies) *Worker {
	base := command.NewBase(postgres.NewUnitOfWork(d.Pool, postgres.NewOutboxWriter()), d.Clock, d.Sellers, d.Policy)
	reads := postgres.NewReadModel(d.Pool)
	return &Worker{
		pool:           d.Pool,
		log:            d.Logger,
		processImage:   cqrs.Decorate(moduleName, command.NewProcessImageHandler(base, d.Thumbnails), d.Logger, d.Metrics),
		processImport:  cqrs.Decorate(moduleName, command.NewProcessImportHandler(base, d.Source, d.Reports), d.Logger, d.Metrics),
		requeueImports: cqrs.Decorate(moduleName, command.NewRequeueStaleImportsHandler(base, reads), d.Logger, d.Metrics),
	}
}

func (w *Worker) Subscriptions() []outbox.Subscription {
	return []outbox.Subscription{
		{
			Consumer:   thumbnailConsumer,
			EventNames: []string{api.EventProductImageUploaded},
			Handle:     w.renderThumbnails,
		},
		{
			Consumer:   offerImportRunner,
			EventNames: []string{api.EventImportScheduled},
			Handle:     w.runImport,
		},
	}
}

func (w *Worker) renderThumbnails(ctx context.Context, msg outbox.Message) error {
	var event api.ProductImageUploadedV1
	if err := json.Unmarshal(msg.Payload, &event); err != nil || event.ProductID == "" || event.ImageID == "" {
		w.log.ErrorContext(ctx, "skipping malformed event", "event_name", msg.EventName, "outbox_id", msg.ID, "err", err)
		return nil
	}
	_, err := w.processImage.Handle(ctx, command.ProcessImage{ProductID: event.ProductID, ImageID: event.ImageID})
	return err
}

func (w *Worker) runImport(ctx context.Context, msg outbox.Message) error {
	var event api.ImportScheduledV1
	if err := json.Unmarshal(msg.Payload, &event); err != nil || event.JobID == "" {
		w.log.ErrorContext(ctx, "skipping malformed event", "event_name", msg.EventName, "outbox_id", msg.ID, "err", err)
		return nil
	}
	_, err := w.processImport.Handle(ctx, command.ProcessImport{JobID: event.JobID})
	return err
}

func (w *Worker) Jobs() []scheduler.Job {
	return []scheduler.Job{{
		Name:     requeueImportsJob,
		Interval: 5 * time.Minute,
		Run: scheduler.Exclusive(w.pool, requeueImportsJob, func(ctx context.Context) error {
			_, err := w.requeueImports.Handle(ctx, command.RequeueStaleImports{Limit: requeueImportsSize})
			return err
		}),
	}}
}
