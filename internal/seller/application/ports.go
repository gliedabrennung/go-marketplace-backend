package application

import (
	"context"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/domain"
)

type Clock interface {
	Now() time.Time
}

type AuditEntry struct {
	ActorID    string
	ActorRoles []string
	Action     string
	ObjectType string
	ObjectID   string
	Details    map[string]string
	OccurredAt time.Time
}

type AuditTrail interface {
	Record(ctx context.Context, e AuditEntry) error
}

type Repositories interface {
	Sellers() domain.SellerRepository
	CategoryCommissions() domain.CategoryCommissionRepository
	Audit() AuditTrail
}

type UnitOfWork interface {
	Do(ctx context.Context, fn func(ctx context.Context, repos Repositories) error) error
}
