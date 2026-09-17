package identity

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/application/command"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/infrastructure/postgres"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/cqrs"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/outbox"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/scheduler"
)

const revokeOnBlockConsumer = "identity.revoke_sessions_on_block"

type WorkerDependencies struct {
	Pool    *pgxpool.Pool
	Clock   application.Clock
	Logger  *slog.Logger
	Metrics cqrs.Metrics
}

type Worker struct {
	pool           *pgxpool.Pool
	log            *slog.Logger
	revokeSessions cqrs.Handler[command.RevokeUserSessions, command.RevokeUserSessionsResult]
}

func NewWorker(d WorkerDependencies) *Worker {
	uow := postgres.NewUnitOfWork(d.Pool, postgres.NewOutboxWriter())
	return &Worker{
		pool: d.Pool,
		log:  d.Logger,
		revokeSessions: cqrs.Decorate(moduleName,
			command.NewRevokeUserSessionsHandler(uow, postgres.NewReadModel(d.Pool), d.Clock), d.Logger, d.Metrics),
	}
}

func (w *Worker) Subscriptions() []outbox.Subscription {
	return []outbox.Subscription{{
		Consumer:   revokeOnBlockConsumer,
		EventNames: []string{api.EventUserBlocked},
		Handle:     w.revokeSessionsOnBlock,
	}}
}

func (w *Worker) revokeSessionsOnBlock(ctx context.Context, msg outbox.Message) error {
	var event api.UserBlockedV1
	if err := json.Unmarshal(msg.Payload, &event); err != nil || event.UserID == "" {
		w.log.ErrorContext(ctx, "skipping malformed event", "event_name", msg.EventName, "outbox_id", msg.ID, "err", err)
		return nil
	}
	_, err := w.revokeSessions.Handle(ctx, command.RevokeUserSessions{UserID: event.UserID})
	return err
}

func (w *Worker) Jobs() []scheduler.Job {
	return []scheduler.Job{{
		Name:     "identity.challenges_cleanup",
		Interval: time.Hour,
		Run: scheduler.Exclusive(w.pool, "identity.challenges_cleanup", func(ctx context.Context) error {
			_, err := postgres.DeleteExpiredChallenges(ctx, w.pool, time.Now().Add(-24*time.Hour))
			return err
		}),
	}}
}
