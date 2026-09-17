//go:build integration

package pricing_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/pricing/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/pricing/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/pricing/infrastructure/memory"
	"github.com/gliedabrennung/go-marketplace-backend/internal/pricing/infrastructure/postgres"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/test/testdb"
)

var pricingTables = []string{
	"pricing.promo_redemptions", "pricing.promo_codes", "pricing.promotions",
	"pricing.product_categories", "pricing.offer_prices", "platform.outbox", "platform.idempotency_keys",
}

func implementations() map[string]func(t *testing.T) application.Repositories {
	return map[string]func(t *testing.T) application.Repositories{
		"memory": func(*testing.T) application.Repositories { return memory.NewStore() },
		"postgres": func(t *testing.T) application.Repositories {
			pool := testdb.Pool(t)
			testdb.Truncate(t, pool, pricingTables...)
			return postgres.NewRepositories(pool, postgres.NewOutboxWriter())
		},
	}
}

func now() time.Time {
	return time.Now().UTC().Truncate(time.Microsecond)
}

func money(t *testing.T, amount int64) kernel.Money {
	t.Helper()
	value, err := kernel.NewMoney(amount, kernel.KZT)
	require.NoError(t, err)
	return value
}

func TestPromotionRepositoryContract(t *testing.T) {
	ctx := context.Background()
	for name, factory := range implementations() {
		t.Run(name, func(t *testing.T) {
			repos := factory(t)
			_, err := repos.Promotions().FindByID(ctx, domain.NewPromotionID())
			require.ErrorIs(t, err, domain.ErrPromotionNotFound)

			sku, err := domain.NewSKU("OFFER-1")
			require.NoError(t, err)
			at := now()
			promotion, err := domain.CreatePromotion(domain.NewPromotionID(), domain.PromotionSpec{
				Name:     "Скидка",
				Discount: domain.DiscountSpec{Kind: domain.DiscountFixed, Amount: 1000, Currency: "KZT"},
				Target: domain.Target{
					SKUs: []domain.SKU{sku}, Sellers: []kernel.SellerID{kernel.NewSellerID()},
					Categories: []domain.CategoryID{domain.NewCategoryID()},
				},
				Priority: 3, Exclusive: true, StartsAt: at, EndsAt: at.Add(time.Hour),
			}, at)
			require.NoError(t, err)
			require.NoError(t, repos.Promotions().Save(ctx, promotion))

			reloaded, err := repos.Promotions().FindByID(ctx, promotion.ID())
			require.NoError(t, err)
			assert.Equal(t, promotion.Snapshot(), reloaded.Snapshot())

			require.NoError(t, reloaded.Activate(at))
			require.NoError(t, repos.Promotions().Save(ctx, reloaded))
			require.ErrorIs(t, repos.Promotions().Save(ctx, promotion), kernel.ErrConcurrentModification)
		})
	}
}

func TestPromoCodeRepositoryContract(t *testing.T) {
	ctx := context.Background()
	for name, factory := range implementations() {
		t.Run(name, func(t *testing.T) {
			repos := factory(t)
			code, err := domain.NewCode("HELLO-2026")
			require.NoError(t, err)
			_, err = repos.PromoCodes().FindByCode(ctx, code)
			require.ErrorIs(t, err, domain.ErrPromoCodeNotFound)

			at := now()
			promo, err := domain.CreatePromoCode(code, domain.PromoCodeSpec{
				Discount:   domain.DiscountSpec{Kind: domain.DiscountPercentage, BasisPoints: 1000},
				TotalLimit: 5, PerCustomerLimit: 2, MinCartAmount: 1000, StartsAt: at, EndsAt: at.Add(time.Hour),
			}, at)
			require.NoError(t, err)
			require.NoError(t, repos.PromoCodes().Save(ctx, promo))

			twin, err := domain.CreatePromoCode(code, domain.PromoCodeSpec{
				Discount: domain.DiscountSpec{Kind: domain.DiscountPercentage, BasisPoints: 500},
			}, at)
			require.NoError(t, err)
			require.ErrorIs(t, repos.PromoCodes().Save(ctx, twin), domain.ErrPromoCodeExists)

			reloaded, err := repos.PromoCodes().FindByCode(ctx, code)
			require.NoError(t, err)
			assert.Equal(t, promo.Snapshot(), reloaded.Snapshot())

			customer := kernel.NewUserID()
			order := domain.NewOrderID()
			require.NoError(t, reloaded.Redeem(order, customer, money(t, 5000), 0, at))
			require.NoError(t, repos.PromoCodes().Save(ctx, reloaded))

			usage, err := repos.PromoCodes().CustomerUsage(ctx, code, customer)
			require.NoError(t, err)
			assert.Equal(t, 1, usage)
			redeemed, err := repos.PromoCodes().Redeemed(ctx, code, order)
			require.NoError(t, err)
			assert.True(t, redeemed)

			duplicate, err := repos.PromoCodes().FindByCode(ctx, code)
			require.NoError(t, err)
			require.NoError(t, duplicate.Redeem(order, customer, money(t, 5000), 0, at))
			require.ErrorIs(t, repos.PromoCodes().Save(ctx, duplicate), domain.ErrPromoAlreadyUsed)

			current, err := repos.PromoCodes().FindByCode(ctx, code)
			require.NoError(t, err)
			assert.Equal(t, 1, current.Used())
			require.NoError(t, current.Release(order, true, at))
			require.NoError(t, repos.PromoCodes().Save(ctx, current))

			redeemed, err = repos.PromoCodes().Redeemed(ctx, code, order)
			require.NoError(t, err)
			assert.False(t, redeemed)
			released, err := repos.PromoCodes().FindByCode(ctx, code)
			require.NoError(t, err)
			assert.Zero(t, released.Used())
		})
	}
}

func TestOfferPriceAndCategoryContract(t *testing.T) {
	ctx := context.Background()
	for name, factory := range implementations() {
		t.Run(name, func(t *testing.T) {
			repos := factory(t)
			sku, err := domain.NewSKU("OFFER-1")
			require.NoError(t, err)
			_, err = repos.OfferPrices().FindBySKU(ctx, sku)
			require.ErrorIs(t, err, domain.ErrOfferPriceNotFound)

			at := now()
			product := domain.NewProductID()
			price, err := domain.NewOfferPrice(sku, product, kernel.NewSellerID(), money(t, 100000), at)
			require.NoError(t, err)
			require.NoError(t, price.SetCompareAt(money(t, 120000), at))
			require.NoError(t, repos.OfferPrices().Save(ctx, price))

			reloaded, err := repos.OfferPrices().FindBySKU(ctx, sku)
			require.NoError(t, err)
			assert.Equal(t, price.Snapshot(), reloaded.Snapshot())

			require.NoError(t, reloaded.ChangePrice(money(t, 130000), at))
			reloaded.SetActive(false, at)
			require.NoError(t, repos.OfferPrices().Save(ctx, reloaded))
			changed, err := repos.OfferPrices().FindBySKU(ctx, sku)
			require.NoError(t, err)
			assert.False(t, changed.IsActive())
			assert.True(t, changed.CompareAt().IsZero())
			require.ErrorIs(t, repos.OfferPrices().Save(ctx, price), kernel.ErrConcurrentModification)

			path := []string{domain.NewCategoryID().String(), domain.NewCategoryID().String()}
			require.NoError(t, repos.Categories().Save(ctx, product.String(), path, at))
			require.NoError(t, repos.Categories().Save(ctx, product.String(), path, at))
			paths, err := repos.Categories().Paths(ctx, []string{product.String(), domain.NewProductID().String()})
			require.NoError(t, err)
			assert.Equal(t, map[string][]string{product.String(): path}, paths)
		})
	}
}
