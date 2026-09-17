package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
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

func (r repositories) Categories() domain.CategoryRepository { return categoryRepository(r) }

func (r repositories) Products() domain.ProductRepository { return productRepository(r) }

func (r repositories) VariantGroups() domain.VariantGroupRepository { return variantGroupRepository(r) }

func (r repositories) Offers() domain.OfferRepository { return offerRepository(r) }

func (r repositories) Imports() domain.ImportJobRepository { return importRepository(r) }

func (r repositories) Audit() application.AuditTrail { return auditTrail{q: r.q} }

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

func constraintError(err error) error {
	name, ok := platform.UniqueViolation(err)
	if !ok {
		return nil
	}
	switch name {
	case "uq_categories_sibling_slug":
		return domain.ErrCategorySlugTaken
	case "uq_offers_seller_sku":
		return domain.ErrSellerSKUTaken
	case "uq_offers_seller_product":
		return domain.ErrOfferExists
	case "uq_variant_members_product":
		return domain.ErrProductAlreadyGrouped
	default:
		return nil
	}
}

func wrap(op string, err error) error {
	if err == nil {
		return nil
	}
	if mapped := constraintError(err); mapped != nil {
		return mapped
	}
	return fmt.Errorf("%s: %w", op, err)
}

func exec(ctx context.Context, q platform.Querier, op, sql string, args ...any) error {
	_, err := q.Exec(ctx, sql, args...)
	return wrap(op, err)
}

func update(ctx context.Context, q platform.Querier, op, sql string, args ...any) error {
	tag, err := q.Exec(ctx, sql, args...)
	if err != nil {
		return wrap(op, err)
	}
	if tag.RowsAffected() == 0 {
		return kernel.ErrConcurrentModification
	}
	return nil
}

func notFound(err error, missing error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return missing
	}
	return err
}

func list(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
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

func moment(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return t.UTC()
}

func optionalTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	utc := t.UTC()
	return &utc
}

func utc(t time.Time) time.Time {
	return t.UTC()
}
