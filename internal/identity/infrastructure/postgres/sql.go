package postgres

import (
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/outbox"
	platform "github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/postgres"
)

type repositories struct {
	q      platform.Querier
	events *outbox.Writer
}

func NewRepositories(q platform.Querier, events *outbox.Writer) application.Repositories {
	return repositories{q: q, events: events}
}

func NewUnitOfWork(pool *pgxpool.Pool, events *outbox.Writer) *platform.UnitOfWork[application.Repositories] {
	return platform.NewUnitOfWork(pool, func(tx pgx.Tx) application.Repositories {
		return repositories{q: tx, events: events}
	})
}

func NewOutboxWriter() *outbox.Writer {
	return outbox.NewWriter("platform", "outbox", NewEventCodec())
}

func (r repositories) Users() domain.UserRepository {
	return userRepository(r)
}

func (r repositories) Sessions() domain.SessionRepository {
	return sessionRepository(r)
}

func (r repositories) Challenges() domain.ChallengeRepository {
	return challengeRepository{q: r.q}
}

func (r repositories) Audit() application.AuditTrail {
	return auditTrail{q: r.q}
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func nullableTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func derefTime(p *time.Time) time.Time {
	if p == nil {
		return time.Time{}
	}
	return p.UTC()
}
