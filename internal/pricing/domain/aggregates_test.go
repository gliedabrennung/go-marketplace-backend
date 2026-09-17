package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/pricing/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

func percentageSpec(name string) domain.PromotionSpec {
	return domain.PromotionSpec{
		Name:     name,
		Discount: domain.DiscountSpec{Kind: domain.DiscountPercentage, BasisPoints: 1500},
		Target:   domain.Target{Sellers: []kernel.SellerID{kernel.NewSellerID()}},
		Priority: 10,
	}
}

func TestPromotionLifecycle(t *testing.T) {
	_, err := domain.CreatePromotion(domain.PromotionID{}, percentageSpec("Скидка"), now)
	require.ErrorIs(t, err, kernel.ErrInvalidID)

	broken := percentageSpec("")
	_, err = domain.CreatePromotion(domain.NewPromotionID(), broken, now)
	require.ErrorIs(t, err, domain.ErrInvalidName)

	broken = percentageSpec("Скидка")
	broken.Target = domain.Target{}
	_, err = domain.CreatePromotion(domain.NewPromotionID(), broken, now)
	require.ErrorIs(t, err, domain.ErrInvalidTarget)

	broken = percentageSpec("Скидка")
	broken.Discount = domain.DiscountSpec{Kind: "mystery"}
	_, err = domain.CreatePromotion(domain.NewPromotionID(), broken, now)
	require.ErrorIs(t, err, domain.ErrInvalidDiscount)

	broken = percentageSpec("Скидка")
	broken.StartsAt, broken.EndsAt = now, now.Add(-time.Hour)
	_, err = domain.CreatePromotion(domain.NewPromotionID(), broken, now)
	require.ErrorIs(t, err, domain.ErrInvalidPeriod)

	spec := percentageSpec("Осенняя скидка")
	spec.StartsAt, spec.EndsAt = now, now.Add(48*time.Hour)
	promotion, err := domain.CreatePromotion(domain.NewPromotionID(), spec, now)
	require.NoError(t, err)
	assert.Equal(t, domain.PromotionDraft, promotion.Status())
	assert.Equal(t, "Осенняя скидка", promotion.Name())
	assert.Equal(t, 10, promotion.Priority())
	assert.False(t, promotion.IsRunning(now))

	require.NoError(t, promotion.Activate(now))
	assert.True(t, promotion.IsRunning(now))
	assert.False(t, promotion.IsRunning(now.Add(-time.Hour)))
	assert.False(t, promotion.IsRunning(now.Add(72*time.Hour)))
	require.NoError(t, promotion.Activate(now))

	require.NoError(t, promotion.Pause(now))
	assert.False(t, promotion.IsRunning(now))

	updated := percentageSpec("Зимняя скидка")
	require.NoError(t, promotion.Update(updated, now))
	assert.Equal(t, "Зимняя скидка", promotion.Name())

	require.NoError(t, promotion.End(now))
	require.ErrorIs(t, promotion.Activate(now), domain.ErrPromotionEnded)
	require.ErrorIs(t, promotion.Update(updated, now), domain.ErrPromotionEnded)

	rule, err := promotion.Rule()
	require.NoError(t, err)
	assert.Equal(t, promotion.ID(), rule.ID())

	promotion.AdvanceVersion()
	snap := promotion.Snapshot()
	restored, err := domain.RehydratePromotion(snap)
	require.NoError(t, err)
	assert.Equal(t, snap, restored.Snapshot())

	broken2 := snap
	broken2.Status = "unknown"
	_, err = domain.RehydratePromotion(broken2)
	require.ErrorContains(t, err, "unknown status")

	broken2 = snap
	broken2.SKUs = []string{"плохой"}
	_, err = domain.RehydratePromotion(broken2)
	require.ErrorIs(t, err, domain.ErrInvalidSKU)

	broken2 = snap
	broken2.ID = "bad"
	_, err = domain.RehydratePromotion(broken2)
	require.Error(t, err)
}

func TestPromoCodeLimits(t *testing.T) {
	code, err := domain.NewCode("welcome")
	require.NoError(t, err)
	_, err = domain.NewCode("no")
	require.ErrorIs(t, err, domain.ErrInvalidPromoCode)

	_, err = domain.CreatePromoCode(code, domain.PromoCodeSpec{
		Discount: domain.DiscountSpec{Kind: domain.DiscountBuyNGetM, BuyQuantity: 3, FreeUnits: 1},
	}, now)
	require.ErrorIs(t, err, domain.ErrInvalidDiscount)

	_, err = domain.CreatePromoCode(code, domain.PromoCodeSpec{
		Discount: domain.DiscountSpec{Kind: domain.DiscountPercentage, BasisPoints: 1000}, TotalLimit: -1,
	}, now)
	require.ErrorIs(t, err, domain.ErrInvalidLimits)

	promo, err := domain.CreatePromoCode(code, domain.PromoCodeSpec{
		Discount:      domain.DiscountSpec{Kind: domain.DiscountFixed, Amount: 5000, Currency: "KZT"},
		MinCartAmount: 50000, TotalLimit: 2, PerCustomerLimit: 1, StartsAt: now, EndsAt: now.Add(24 * time.Hour),
	}, now)
	require.NoError(t, err)
	assert.Equal(t, domain.PromoCodeActive, promo.Status())

	small := money(t, 10000)
	require.ErrorIs(t, promo.EnsureUsable(small, 0, now), domain.ErrCartBelowMinimum)
	require.ErrorIs(t, promo.EnsureUsable(money(t, 60000), 0, now.Add(-time.Hour)), domain.ErrPromoCodeExpired)
	require.ErrorIs(t, promo.EnsureUsable(money(t, 60000), 0, now.Add(48*time.Hour)), domain.ErrPromoCodeExpired)
	require.ErrorIs(t, promo.EnsureUsable(money(t, 60000), 1, now), domain.ErrPromoCodePerBuyer)
	require.NoError(t, promo.EnsureUsable(money(t, 60000), 0, now))

	discount, err := promo.Discount(money(t, 60000))
	require.NoError(t, err)
	assert.Equal(t, money(t, 5000), discount)
	discount, err = promo.Discount(money(t, 3000))
	require.NoError(t, err)
	assert.Equal(t, money(t, 3000), discount)

	order := domain.NewOrderID()
	customer := kernel.NewUserID()
	require.ErrorIs(t, promo.Redeem(domain.OrderID{}, customer, money(t, 60000), 0, now), kernel.ErrInvalidID)
	require.NoError(t, promo.Redeem(order, customer, money(t, 60000), 0, now))
	assert.Equal(t, 1, promo.Used())
	require.ErrorIs(t, promo.Redeem(order, customer, money(t, 60000), 0, now), domain.ErrPromoAlreadyUsed)

	require.NoError(t, promo.Redeem(domain.NewOrderID(), kernel.NewUserID(), money(t, 60000), 0, now))
	require.ErrorIs(t, promo.Redeem(domain.NewOrderID(), kernel.NewUserID(), money(t, 60000), 0, now), domain.ErrPromoCodeDepleted)

	redemptions := promo.PullRedemptions()
	require.Len(t, redemptions, 2)
	assert.Equal(t, order, redemptions[0].OrderID)
	assert.Equal(t, money(t, 5000), redemptions[0].Amount)
	assert.Empty(t, promo.PullRedemptions())

	require.ErrorIs(t, promo.Release(domain.OrderID{}, true, now), kernel.ErrInvalidID)
	require.NoError(t, promo.Release(order, false, now))
	assert.Equal(t, 2, promo.Used())
	require.NoError(t, promo.Release(order, true, now))
	assert.Equal(t, 1, promo.Used())
	require.NoError(t, promo.Release(order, true, now))
	assert.Equal(t, 1, promo.Used())
	assert.Equal(t, []domain.OrderID{order}, promo.PullReleases())
	assert.Empty(t, promo.PullReleases())

	promo.Disable(now)
	assert.Equal(t, domain.PromoCodeDisabled, promo.Status())
	require.ErrorIs(t, promo.EnsureUsable(money(t, 60000), 0, now), domain.ErrPromoCodeInactive)
	promo.Disable(now)
	promo.Enable(now)
	assert.Equal(t, domain.PromoCodeActive, promo.Status())

	promo.AdvanceVersion()
	snap := promo.Snapshot()
	restored, err := domain.RehydratePromoCode(snap)
	require.NoError(t, err)
	assert.Equal(t, snap, restored.Snapshot())
	assert.Equal(t, 1, restored.Used())

	broken := snap
	broken.Code = "no"
	_, err = domain.RehydratePromoCode(broken)
	require.ErrorIs(t, err, domain.ErrInvalidPromoCode)

	broken = snap
	broken.Status = "unknown"
	_, err = domain.RehydratePromoCode(broken)
	require.ErrorContains(t, err, "unknown status")

	broken = snap
	broken.Currency = "bad"
	_, err = domain.RehydratePromoCode(broken)
	require.Error(t, err)
}

func TestOfferPrice(t *testing.T) {
	sku := sku(t, "OFFER-1")
	product := domain.NewProductID()
	seller := kernel.NewSellerID()

	_, err := domain.NewOfferPrice(domain.SKU{}, product, seller, money(t, 1000), now)
	require.ErrorIs(t, err, kernel.ErrInvalidID)
	_, err = domain.NewOfferPrice(sku, product, seller, money(t, 0), now)
	require.ErrorIs(t, err, domain.ErrInvalidPrice)

	price, err := domain.NewOfferPrice(sku, product, seller, money(t, 100000), now)
	require.NoError(t, err)
	assert.True(t, price.IsActive())
	assert.Equal(t, seller, price.SellerID())
	assert.Equal(t, product, price.ProductID())
	assert.Equal(t, []string{"pricing.price_changed.v1"}, eventNames(price.PullEvents()))

	require.ErrorIs(t, price.SetCompareAt(money(t, 90000), now), domain.ErrInvalidPrice)
	require.NoError(t, price.SetCompareAt(money(t, 130000), now))
	assert.Equal(t, money(t, 130000), price.CompareAt())

	require.ErrorIs(t, price.ChangePrice(money(t, 0), now), domain.ErrInvalidPrice)
	require.NoError(t, price.ChangePrice(money(t, 100000), now))
	require.NoError(t, price.ChangePrice(money(t, 140000), now))
	assert.True(t, price.CompareAt().IsZero())

	line, err := price.Line(kernel.MustQuantity(2), []domain.CategoryID{domain.NewCategoryID()})
	require.NoError(t, err)
	assert.Equal(t, money(t, 140000), line.UnitPrice)
	_, err = price.Line(kernel.Quantity{}, nil)
	require.ErrorIs(t, err, domain.ErrInvalidQuantity)

	price.SetActive(false, now)
	price.SetActive(false, now)
	_, err = price.Line(kernel.MustQuantity(1), nil)
	require.ErrorIs(t, err, domain.ErrOfferPriceInactive)

	price.AdvanceVersion()
	snap := price.Snapshot()
	restored, err := domain.RehydrateOfferPrice(snap)
	require.NoError(t, err)
	assert.Equal(t, snap, restored.Snapshot())

	broken := snap
	broken.SKU = "плохой"
	_, err = domain.RehydrateOfferPrice(broken)
	require.ErrorIs(t, err, domain.ErrInvalidSKU)

	broken = snap
	broken.Currency = "XX"
	_, err = domain.RehydrateOfferPrice(broken)
	require.Error(t, err)

	broken = snap
	broken.CompareAt = 200000
	restored, err = domain.RehydrateOfferPrice(broken)
	require.NoError(t, err)
	assert.Equal(t, money(t, 200000), restored.CompareAt())
}

func eventNames(events []kernel.DomainEvent) []string {
	out := make([]string, len(events))
	for i, event := range events {
		out[i] = event.EventName()
	}
	return out
}
