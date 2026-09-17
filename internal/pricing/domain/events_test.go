package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/pricing/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

func TestEventContract(t *testing.T) {
	promotionID := domain.NewPromotionID()
	events := []kernel.DomainEvent{
		domain.PriceChanged{SKU: "OFFER-1", At: now},
		domain.PromotionChanged{PromotionID: promotionID, Status: domain.PromotionActive, At: now},
		domain.PromoCodeChanged{Code: "WELCOME", Status: domain.PromoCodeActive, At: now},
		domain.PromoCodeRedeemed{Code: "WELCOME", OrderID: domain.NewOrderID().String(), At: now},
		domain.PromoCodeReleased{Code: "WELCOME", OrderID: domain.NewOrderID().String(), At: now},
	}

	assert.Equal(t, []string{
		"pricing.price_changed.v1", "pricing.promotion_changed.v1", "pricing.promo_code_changed.v1",
		"pricing.promo_code_redeemed.v1", "pricing.promo_code_released.v1",
	}, eventNames(events))

	for _, event := range events {
		assert.Equal(t, now, event.OccurredAt(), event.EventName())
		assert.NotEmpty(t, event.AggregateID(), event.EventName())
	}
	assert.Equal(t, promotionID.String(), events[1].AggregateID())
}

func TestPromotionEventsAreRecorded(t *testing.T) {
	promotion, err := domain.CreatePromotion(domain.NewPromotionID(), percentageSpec("Скидка"), now)
	require.NoError(t, err)
	require.NoError(t, promotion.Activate(now))
	require.NoError(t, promotion.End(now.Add(time.Hour)))
	assert.Equal(t, []string{
		"pricing.promotion_changed.v1", "pricing.promotion_changed.v1", "pricing.promotion_changed.v1",
	}, eventNames(promotion.PullEvents()))
	assert.Empty(t, promotion.PullEvents())

	code, err := domain.NewCode("SALE-2026")
	require.NoError(t, err)
	promo, err := domain.CreatePromoCode(code, domain.PromoCodeSpec{
		Discount: domain.DiscountSpec{Kind: domain.DiscountPercentage, BasisPoints: 500},
	}, now)
	require.NoError(t, err)
	order := domain.NewOrderID()
	require.NoError(t, promo.Redeem(order, kernel.NewUserID(), money(t, 10000), 0, now))
	require.NoError(t, promo.Release(order, true, now))
	assert.Equal(t, []string{
		"pricing.promo_code_changed.v1", "pricing.promo_code_redeemed.v1", "pricing.promo_code_released.v1",
	}, eventNames(promo.PullEvents()))
}

func TestDiscountSpecValidation(t *testing.T) {
	spec := percentageSpec("Скидка")

	for _, discount := range []domain.DiscountSpec{
		{Kind: domain.DiscountPercentage, BasisPoints: 0},
		{Kind: domain.DiscountPercentage, BasisPoints: 20000},
		{Kind: domain.DiscountFixed, Amount: -1, Currency: "KZT"},
		{Kind: domain.DiscountFixed, Amount: 100, Currency: "kzt"},
		{Kind: domain.DiscountBuyNGetM, BuyQuantity: 0, FreeUnits: 1},
		{Kind: domain.DiscountBuyNGetM, BuyQuantity: 2, FreeUnits: 2},
	} {
		spec.Discount = discount
		_, err := domain.CreatePromotion(domain.NewPromotionID(), spec, now)
		require.ErrorIs(t, err, domain.ErrInvalidDiscount, discount)
	}
}

func TestFixedRuleIgnoresOtherCurrency(t *testing.T) {
	item := line(t, "SKU-1", 100000, 1)
	usd := promotion(t, domain.PromotionSpec{
		Name: "Скидка в долларах", Priority: 1,
		Discount: domain.DiscountSpec{Kind: domain.DiscountFixed, Amount: 100, Currency: "USD"},
		Target:   domain.Target{SKUs: []domain.SKU{item.SKU}},
	})

	quote, err := domain.PriceCalculator{}.Calculate([]domain.CartLine{item}, []domain.DiscountRule{usd}, nil)
	require.NoError(t, err)
	assert.True(t, quote.Discount.IsZero())
	assert.Equal(t, 1, usd.Priority())
}
