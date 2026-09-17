package application

import (
	"context"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/inventory/domain"
	sellerapi "github.com/gliedabrennung/go-marketplace-backend/internal/seller/api"
)

type Clock interface {
	Now() time.Time
}

type Repositories interface {
	Stock() domain.StockRepository
}

type UnitOfWork interface {
	Do(ctx context.Context, fn func(ctx context.Context, repos Repositories) error) error
}

type SellerDirectory interface {
	Seller(ctx context.Context, sellerID string) (sellerapi.SellerInfo, error)
	MemberRole(ctx context.Context, sellerID, userID string) (string, bool, error)
}

type Policy struct {
	ReservationTTL time.Duration
	ExpiryBatch    int
	MaxLines       int
}

func DefaultPolicy() Policy {
	return Policy{ReservationTTL: 20 * time.Minute, ExpiryBatch: 200, MaxLines: 100}
}
