package payment

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/gliedabrennung/go-marketplace-backend/internal/payment/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/payment/application/command"
	"github.com/gliedabrennung/go-marketplace-backend/internal/payment/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/payment/infrastructure/postgres"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/cqrs"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/scheduler"
)

const (
	reconciliationJob = "payment.reconciliation"
	webhookCleanupJob = "payment.webhook_cleanup"
	webhookRetention  = 30 * 24 * time.Hour
)

type WorkerDependencies struct {
	Pool      *pgxpool.Pool
	Clock     application.Clock
	Providers application.Providers
	Logger    *slog.Logger
	Metrics   cqrs.Metrics
}

type Worker struct {
	pool      *pgxpool.Pool
	clock     application.Clock
	providers application.Providers
	reads     query.ReadModel
	reconcile cqrs.Handler[command.Reconcile, application.ReconciliationReport]
	log       *slog.Logger
}

func NewWorker(d WorkerDependencies) *Worker {
	base := command.NewBase(postgres.NewUnitOfWork(d.Pool, postgres.NewOutboxWriter()), d.Clock, d.Providers)
	return &Worker{
		pool: d.Pool, clock: d.Clock, providers: d.Providers, reads: postgres.NewReadModel(d.Pool), log: d.Logger,
		reconcile: cqrs.Decorate(moduleName, command.NewReconcileHandler(base, postgres.NewReconciliations(d.Pool)), d.Logger, d.Metrics),
	}
}

func (w *Worker) Jobs() []scheduler.Job {
	return []scheduler.Job{
		{
			Name:     reconciliationJob,
			Interval: time.Hour,
			Run:      scheduler.Exclusive(w.pool, reconciliationJob, w.reconcilePreviousDay),
		},
		{
			Name:     webhookCleanupJob,
			Interval: 24 * time.Hour,
			Run: scheduler.Exclusive(w.pool, webhookCleanupJob, func(ctx context.Context) error {
				_, err := postgres.DeleteWebhooksBefore(ctx, w.pool, w.clock.Now().Add(-webhookRetention))
				return err
			}),
		},
	}
}

func (w *Worker) reconcilePreviousDay(ctx context.Context) error {
	now := w.clock.Now().UTC()
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -1)
	_, err := w.reads.Reconciliation(ctx, w.providers.Default().Name(), day)
	if err == nil {
		return nil
	}
	if !errors.Is(err, query.ErrReportNotFound) {
		return err
	}
	report, err := w.reconcile.Handle(ctx, command.Reconcile{Day: day})
	if err != nil {
		return err
	}
	if len(report.Mismatches) > 0 {
		w.log.ErrorContext(ctx, "payment reconciliation found mismatches",
			"provider", report.Provider, "day", day.Format(time.DateOnly), "mismatches", len(report.Mismatches))
	}
	return nil
}
