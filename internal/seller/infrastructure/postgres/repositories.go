package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/audit"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/outbox"
	platform "github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/postgres"
)

type repositories struct {
	q      platform.Querier
	events *outbox.Writer
}

func NewOutboxWriter() *outbox.Writer {
	return outbox.NewWriter("platform", "outbox", NewEventCodec())
}

func NewRepositories(q platform.Querier, events *outbox.Writer) application.Repositories {
	return repositories{q: q, events: events}
}

func NewUnitOfWork(pool *pgxpool.Pool, events *outbox.Writer) *platform.UnitOfWork[application.Repositories] {
	return platform.NewUnitOfWork(pool, func(tx pgx.Tx) application.Repositories {
		return repositories{q: tx, events: events}
	})
}

func (r repositories) Sellers() domain.SellerRepository {
	return sellerRepository(r)
}

func (r repositories) CategoryCommissions() domain.CategoryCommissionRepository {
	return commissionRepository(r)
}

func (r repositories) Audit() application.AuditTrail {
	return auditTrail{q: r.q}
}

type auditTrail struct {
	q platform.Querier
}

func (a auditTrail) Record(ctx context.Context, e application.AuditEntry) error {
	return audit.Writer{}.Write(ctx, a.q, audit.Entry{
		ActorID:    e.ActorID,
		ActorRoles: e.ActorRoles,
		Action:     e.Action,
		ObjectType: e.ObjectType,
		ObjectID:   e.ObjectID,
		Details:    e.Details,
		OccurredAt: e.OccurredAt,
	})
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

func utc(t time.Time) time.Time {
	return t.UTC()
}
