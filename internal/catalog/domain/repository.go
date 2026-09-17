package domain

import (
	"context"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type CategoryRepository interface {
	FindByID(ctx context.Context, id CategoryID) (*Category, error)
	FindChain(ctx context.Context, id CategoryID) ([]*Category, error)
	DescendantAttributeCodes(ctx context.Context, id CategoryID) ([]string, error)
	Save(ctx context.Context, c *Category) error
}

type ProductRepository interface {
	FindByID(ctx context.Context, id ProductID) (*Product, error)
	Save(ctx context.Context, p *Product) error
}

type VariantGroupRepository interface {
	FindByID(ctx context.Context, id VariantGroupID) (*VariantGroup, error)
	Save(ctx context.Context, g *VariantGroup) error
}

type OfferRepository interface {
	FindByID(ctx context.Context, id OfferID) (*Offer, error)
	FindBySellerSKU(ctx context.Context, seller kernel.SellerID, sku SellerSKU) (*Offer, error)
	Save(ctx context.Context, o *Offer) error
}

type ImportJobRepository interface {
	FindByID(ctx context.Context, id ImportJobID) (*ImportJob, error)
	Save(ctx context.Context, j *ImportJob) error
}
