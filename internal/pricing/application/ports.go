package application

import (
	"context"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/pricing/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type Clock interface {
	Now() time.Time
}

type Repositories interface {
	Promotions() domain.PromotionRepository
	PromoCodes() domain.PromoCodeRepository
	OfferPrices() domain.OfferPriceRepository
	Categories() CategoryIndex
}

type UnitOfWork interface {
	Do(ctx context.Context, fn func(ctx context.Context, repos Repositories) error) error
}

type CategoryIndex interface {
	Save(ctx context.Context, productID string, path []string, at time.Time) error
	Paths(ctx context.Context, productIDs []string) (map[string][]string, error)
}

type PricingData interface {
	Prices(ctx context.Context, skus []string) ([]*domain.OfferPrice, error)
	CategoryPaths(ctx context.Context, productIDs []string) (map[string][]string, error)
	RunningPromotions(ctx context.Context, at time.Time) ([]*domain.Promotion, error)
	FindPromoCode(ctx context.Context, code domain.Code) (*domain.PromoCode, error)
	PromoCodeUsage(ctx context.Context, code domain.Code, customer kernel.UserID) (int, error)
}

type Policy struct {
	DefaultCurrency kernel.Currency
	MaxQuoteLines   int
}

func DefaultPolicy() Policy {
	return Policy{DefaultCurrency: kernel.KZT, MaxQuoteLines: 100}
}
