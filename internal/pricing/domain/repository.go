package domain

import (
	"context"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type PromotionRepository interface {
	FindByID(ctx context.Context, id PromotionID) (*Promotion, error)
	Save(ctx context.Context, promotion *Promotion) error
}

type PromoCodeRepository interface {
	FindByCode(ctx context.Context, code Code) (*PromoCode, error)
	CustomerUsage(ctx context.Context, code Code, customer kernel.UserID) (int, error)
	Redeemed(ctx context.Context, code Code, order OrderID) (bool, error)
	Save(ctx context.Context, promo *PromoCode) error
}

type OfferPriceRepository interface {
	FindBySKU(ctx context.Context, sku SKU) (*OfferPrice, error)
	Save(ctx context.Context, price *OfferPrice) error
}
