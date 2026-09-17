package domain

import (
	"context"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type SellerRepository interface {
	FindByID(ctx context.Context, id kernel.SellerID) (*Seller, error)
	Save(ctx context.Context, s *Seller) error
}

type CategoryCommissionRepository interface {
	FindByCategory(ctx context.Context, id CategoryID) (*CategoryCommission, error)
	Save(ctx context.Context, c *CategoryCommission) error
}
