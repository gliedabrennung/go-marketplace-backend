package application

import (
	"context"
	"time"

	sellerapi "github.com/gliedabrennung/go-marketplace-backend/internal/seller/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/settlement/domain"
)

type Clock interface {
	Now() time.Time
}

type Repositories interface {
	Entries() domain.Repository
}

type UnitOfWork interface {
	Do(ctx context.Context, fn func(ctx context.Context, repos Repositories) error) error
}

type CommissionRates = sellerapi.Directory

type Policy struct {
	DefaultCommissionRate int
}

func DefaultPolicy() Policy {
	return Policy{DefaultCommissionRate: 1000}
}
