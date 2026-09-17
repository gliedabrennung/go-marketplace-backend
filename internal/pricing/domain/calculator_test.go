package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/pricing/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

var now = time.Date(2026, 9, 16, 10, 0, 0, 0, time.UTC)

func money(t *testing.T, amount int64) kernel.Money {
	t.Helper()
	value, err := kernel.NewMoney(amount, kernel.KZT)
	require.NoError(t, err)
	return value
}

func sku(t *testing.T, raw string) domain.SKU {
	t.Helper()
	value, err := domain.NewSKU(raw)
	require.NoError(t, err)
	return value
}

func line(t *testing.T, raw string, price int64, quantity int, categories ...domain.CategoryID) domain.CartLine {
	t.Helper()
	return domain.CartLine{
		SKU: sku(t, raw), SellerID: kernel.NewSellerID(), ProductID: domain.NewProductID(),
		CategoryPath: categories, UnitPrice: money(t, price), Quantity: kernel.MustQuantity(quantity),
	}
}

func promotion(t *testing.T, spec domain.PromotionSpec) domain.DiscountRule {
	t.Helper()
	created, err := domain.CreatePromotion(domain.NewPromotionID(), spec, now)
	require.NoError(t, err)
	require.NoError(t, created.Activate(now))
	rule, err := created.Rule()
	require.NoError(t, err)
	return rule
}

func TestCalculator_AppliesRulesByPriority(t *testing.T) {
	first := line(t, "SKU-1", 100000, 2)
	category := domain.NewCategoryID()
	second := domain.CartLine{
		SKU: sku(t, "SKU-2"), SellerID: first.SellerID, ProductID: domain.NewProductID(),
		CategoryPath: []domain.CategoryID{category}, UnitPrice: money(t, 50000), Quantity: kernel.MustQuantity(1),
	}

	tenPercent := promotion(t, domain.PromotionSpec{
		Name: "Осенняя распродажа", Priority: 10,
		Discount: domain.DiscountSpec{Kind: domain.DiscountPercentage, BasisPoints: 1000},
		Target:   domain.Target{SKUs: []domain.SKU{first.SKU}},
	})
	fixed := promotion(t, domain.PromotionSpec{
		Name: "Скидка категории", Priority: 5,
		Discount: domain.DiscountSpec{Kind: domain.DiscountFixed, Amount: 5000, Currency: "KZT"},
		Target:   domain.Target{Categories: []domain.CategoryID{category}},
	})

	quote, err := domain.PriceCalculator{}.Calculate([]domain.CartLine{first, second}, []domain.DiscountRule{fixed, tenPercent}, nil)
	require.NoError(t, err)

	require.Len(t, quote.Lines, 2)
	assert.Equal(t, money(t, 200000), quote.Lines[0].Base)
	require.Len(t, quote.Lines[0].Discounts, 1)
	assert.Equal(t, money(t, 20000), quote.Lines[0].Discounts[0].Amount)
	assert.Equal(t, "percentage", quote.Lines[0].Discounts[0].Kind)
	assert.Equal(t, money(t, 180000), quote.Lines[0].Final)

	require.Len(t, quote.Lines[1].Discounts, 1)
	assert.Equal(t, money(t, 5000), quote.Lines[1].Discounts[0].Amount)
	assert.Equal(t, money(t, 45000), quote.Lines[1].Final)

	assert.Equal(t, money(t, 250000), quote.Subtotal)
	assert.Equal(t, money(t, 25000), quote.Discount)
	assert.Equal(t, money(t, 225000), quote.Total)
	assert.Equal(t, kernel.KZT, quote.Currency)
}

func TestCalculator_ExclusiveRuleStopsChain(t *testing.T) {
	item := line(t, "SKU-1", 100000, 1)
	exclusive := promotion(t, domain.PromotionSpec{
		Name: "Только эта скидка", Priority: 100, Exclusive: true,
		Discount: domain.DiscountSpec{Kind: domain.DiscountPercentage, BasisPoints: 2000},
		Target:   domain.Target{SKUs: []domain.SKU{item.SKU}},
	})
	other := promotion(t, domain.PromotionSpec{
		Name: "Дополнительная", Priority: 1,
		Discount: domain.DiscountSpec{Kind: domain.DiscountPercentage, BasisPoints: 1000},
		Target:   domain.Target{SKUs: []domain.SKU{item.SKU}},
	})

	quote, err := domain.PriceCalculator{}.Calculate([]domain.CartLine{item}, []domain.DiscountRule{other, exclusive}, nil)
	require.NoError(t, err)
	require.Len(t, quote.Lines[0].Discounts, 1)
	assert.Equal(t, money(t, 80000), quote.Total)
}

func TestCalculator_DiscountNeverExceedsBase(t *testing.T) {
	item := line(t, "SKU-1", 1000, 1)
	huge := promotion(t, domain.PromotionSpec{
		Name: "Большая скидка", Priority: 1,
		Discount: domain.DiscountSpec{Kind: domain.DiscountFixed, Amount: 900000, Currency: "KZT"},
		Target:   domain.Target{SKUs: []domain.SKU{item.SKU}},
	})
	extra := promotion(t, domain.PromotionSpec{
		Name: "Ещё скидка", Priority: 0,
		Discount: domain.DiscountSpec{Kind: domain.DiscountPercentage, BasisPoints: 5000},
		Target:   domain.Target{SKUs: []domain.SKU{item.SKU}},
	})

	quote, err := domain.PriceCalculator{}.Calculate([]domain.CartLine{item}, []domain.DiscountRule{huge, extra}, nil)
	require.NoError(t, err)
	assert.True(t, quote.Total.IsZero())
	assert.Equal(t, money(t, 1000), quote.Discount)
	require.Len(t, quote.Lines[0].Discounts, 1)
	assert.Equal(t, money(t, 1000), quote.Lines[0].Discounts[0].Amount)
}

func TestCalculator_BuyNGetM(t *testing.T) {
	item := line(t, "SKU-1", 30000, 7)
	rule := promotion(t, domain.PromotionSpec{
		Name: "3 по цене 2", Priority: 1,
		Discount: domain.DiscountSpec{Kind: domain.DiscountBuyNGetM, BuyQuantity: 3, FreeUnits: 1},
		Target:   domain.Target{SKUs: []domain.SKU{item.SKU}},
	})

	quote, err := domain.PriceCalculator{}.Calculate([]domain.CartLine{item}, []domain.DiscountRule{rule}, nil)
	require.NoError(t, err)
	assert.Equal(t, money(t, 60000), quote.Discount)
	assert.Equal(t, money(t, 150000), quote.Total)

	small := line(t, "SKU-1", 30000, 2)
	small.SKU = item.SKU
	quote, err = domain.PriceCalculator{}.Calculate([]domain.CartLine{small}, []domain.DiscountRule{rule}, nil)
	require.NoError(t, err)
	assert.True(t, quote.Discount.IsZero())
}

func TestCalculator_PromoCodeSpreadsAcrossLines(t *testing.T) {
	first := line(t, "SKU-1", 100000, 1)
	second := line(t, "SKU-2", 300000, 1)
	code, err := domain.NewCode("autumn-10")
	require.NoError(t, err)
	promo, err := domain.CreatePromoCode(code, domain.PromoCodeSpec{
		Discount: domain.DiscountSpec{Kind: domain.DiscountPercentage, BasisPoints: 1000},
	}, now)
	require.NoError(t, err)

	quote, err := domain.PriceCalculator{}.Calculate([]domain.CartLine{first, second}, nil, promo)
	require.NoError(t, err)
	assert.Equal(t, "AUTUMN-10", quote.PromoCode)
	assert.Equal(t, money(t, 40000), quote.Discount)
	assert.Equal(t, money(t, 360000), quote.Total)
	assert.Equal(t, money(t, 90000), quote.Lines[0].Final)
	assert.Equal(t, money(t, 270000), quote.Lines[1].Final)
	assert.Equal(t, "promo_code", quote.Lines[0].Discounts[0].Kind)
}

func TestCalculator_Validation(t *testing.T) {
	_, err := domain.PriceCalculator{}.Calculate(nil, nil, nil)
	require.ErrorIs(t, err, domain.ErrEmptyQuote)

	usd, err := kernel.NewMoney(100, kernel.USD)
	require.NoError(t, err)
	mixed := line(t, "SKU-2", 100, 1)
	mixed.UnitPrice = usd
	_, err = domain.PriceCalculator{}.Calculate([]domain.CartLine{line(t, "SKU-1", 100, 1), mixed}, nil, nil)
	require.ErrorIs(t, err, domain.ErrCurrencyMismatch)

	zero := line(t, "SKU-1", 100, 1)
	zero.Quantity = kernel.Quantity{}
	_, err = domain.PriceCalculator{}.Calculate([]domain.CartLine{zero}, nil, nil)
	require.ErrorIs(t, err, domain.ErrInvalidQuantity)
}
