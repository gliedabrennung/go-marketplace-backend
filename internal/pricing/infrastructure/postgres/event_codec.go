package postgres

import (
	"github.com/gliedabrennung/go-marketplace-backend/internal/pricing/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/pricing/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/outbox"
)

func NewEventCodec() *outbox.Codec {
	c := outbox.NewCodec()
	outbox.Register(c, func(e domain.PriceChanged) any {
		return api.PriceChangedV1{
			SKU: e.SKU, SellerID: e.SellerID, ProductID: e.ProductID, Amount: e.Amount,
			Currency: e.Currency, Active: e.Active, OccurredAt: e.At.UTC(),
		}
	})
	outbox.Register(c, func(e domain.PromotionChanged) any {
		return api.PromotionChangedV1{PromotionID: e.PromotionID.String(), Status: string(e.Status), OccurredAt: e.At.UTC()}
	})
	outbox.Register(c, func(e domain.PromoCodeChanged) any {
		return api.PromoCodeChangedV1{Code: e.Code, Status: string(e.Status), OccurredAt: e.At.UTC()}
	})
	outbox.Register(c, func(e domain.PromoCodeRedeemed) any {
		return api.PromoCodeRedemptionV1{
			Code: e.Code, OrderID: e.OrderID, CustomerID: e.CustomerID, Amount: e.Amount,
			Currency: e.Currency, OccurredAt: e.At.UTC(),
		}
	})
	outbox.Register(c, func(e domain.PromoCodeReleased) any {
		return api.PromoCodeRedemptionV1{Code: e.Code, OrderID: e.OrderID, OccurredAt: e.At.UTC()}
	})
	return c
}
