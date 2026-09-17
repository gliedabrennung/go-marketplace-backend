package command_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/cart/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/cart/application/command"
	"github.com/gliedabrennung/go-marketplace-backend/internal/cart/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/cart/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/cart/infrastructure/memory"
	catalogapi "github.com/gliedabrennung/go-marketplace-backend/internal/catalog/api"
	pricingapi "github.com/gliedabrennung/go-marketplace-backend/internal/pricing/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/clock"
	shippingapi "github.com/gliedabrennung/go-marketplace-backend/internal/shipping/api"
)

var ctx = context.Background()

type catalog map[string]catalogapi.OfferSummary

func (c catalog) Offers(_ context.Context, ids []string) (map[string]catalogapi.OfferSummary, error) {
	out := map[string]catalogapi.OfferSummary{}
	for _, id := range ids {
		if offer, ok := c[id]; ok {
			out[id] = offer
		}
	}
	return out, nil
}

type stock map[string]int

func (s stock) Available(_ context.Context, skus []string) (map[string]int, error) {
	out := map[string]int{}
	for _, sku := range skus {
		if value, ok := s[sku]; ok {
			out[sku] = value
		}
	}
	return out, nil
}

type price struct {
	seller string
	amount int64
	active bool
}

type pricing struct {
	prices map[string]price
	fail   error
}

func (p *pricing) Quote(_ context.Context, request pricingapi.QuoteRequest) (pricingapi.Quote, error) {
	if p.fail != nil {
		return pricingapi.Quote{}, p.fail
	}
	quote := pricingapi.Quote{Currency: "KZT"}
	for _, line := range request.Lines {
		entry, ok := p.prices[line.SKU]
		if !ok {
			return pricingapi.Quote{}, pricingapi.ErrOfferPriceNotFound.WithDetail("sku %s", line.SKU)
		}
		if !entry.active {
			return pricingapi.Quote{}, pricingapi.ErrOfferPriceInactive
		}
		base := entry.amount * int64(line.Quantity)
		quote.Lines = append(quote.Lines, pricingapi.PricedLine{
			SKU: line.SKU, SellerID: entry.seller, Quantity: line.Quantity, UnitPrice: entry.amount, Base: base, Final: base,
		})
		quote.Subtotal += base
	}
	quote.Total = quote.Subtotal
	switch request.PromoCode {
	case "":
	case "SALE10":
		if quote.Subtotal < 1000 {
			return pricingapi.Quote{}, fmt.Errorf("quote: %w", errBelowMinimum)
		}
		discount := quote.Lines[0].Final / 10
		quote.Lines[0].Final -= discount
		quote.Discount, quote.Total, quote.PromoCode = discount, quote.Total-discount, "SALE10"
	default:
		return pricingapi.Quote{}, errPromoNotFound
	}
	return quote, nil
}

var (
	errPromoNotFound = kernel.NotFound("PRICING_PROMO_CODE_NOT_FOUND", "promo code not found")
	errBelowMinimum  = kernel.BusinessRule("PRICING_CART_BELOW_MINIMUM", "cart total is below the promo code minimum")
)

type env struct {
	store   *memory.Store
	clock   *clock.Manual
	catalog catalog
	stock   stock
	pricing *pricing

	add         *command.AddItemHandler
	update      *command.UpdateItemHandler
	remove      *command.RemoveItemHandler
	applyPromo  *command.ApplyPromoCodeHandler
	removePromo *command.RemovePromoCodeHandler
	merge       *command.MergeCartsHandler
	ordered     *command.RemoveOrderedItemsHandler
	purge       *command.PurgeExpiredHandler
	get         *query.GetCartHandler
	checkout    *query.GetCheckoutHandler
}

const (
	sellerA = "0192a0b0-0000-7000-8000-00000000000a"
	sellerB = "0192a0b0-0000-7000-8000-00000000000b"
	device  = "device-0123456789abcdef"
)

func newEnv() *env {
	e := &env{
		store: memory.NewStore(), clock: clock.NewManual(time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)),
		catalog: catalog{}, stock: stock{}, pricing: &pricing{prices: map[string]price{}},
	}
	policy := application.DefaultPolicy()
	policy.Limits.MaxItems = 3
	assembler := application.NewAssembler(e.catalog, e.stock, e.pricing, tariffs{fee: 500, freeFrom: 10000})
	base := command.NewBase(memory.NewUnitOfWork(e.store), e.clock, policy, e.catalog, e.stock, assembler)
	e.add = command.NewAddItemHandler(base)
	e.update = command.NewUpdateItemHandler(base)
	e.remove = command.NewRemoveItemHandler(base)
	e.applyPromo = command.NewApplyPromoCodeHandler(base)
	e.removePromo = command.NewRemovePromoCodeHandler(base)
	e.merge = command.NewMergeCartsHandler(base)
	e.ordered = command.NewRemoveOrderedItemsHandler(base)
	e.purge = command.NewPurgeExpiredHandler(base)
	e.get = query.NewGetCartHandler(e.store, e.clock, policy, assembler)
	e.checkout = query.NewGetCheckoutHandler(e.store)
	return e
}

func (e *env) offer(sku, seller string, amount int64, available int) {
	e.catalog[sku] = catalogapi.OfferSummary{
		OfferID: sku, ProductID: "product-" + sku, SellerID: seller, Title: "Товар " + sku, Status: "active",
		PriceAmount: amount, Currency: "KZT", ProductPublished: true,
	}
	e.stock[sku] = available
	e.pricing.prices[sku] = price{seller: seller, amount: amount, active: true}
}

func user() application.OwnerRef {
	return application.OwnerRef{UserID: kernel.NewUserID().String()}
}

func (e *env) view(t *testing.T, owner application.OwnerRef) application.View {
	t.Helper()
	view, err := e.get.Handle(ctx, query.GetCart{Owner: owner})
	require.NoError(t, err)
	return view
}

func TestCartLifecycleWithPricingAndShipping(t *testing.T) {
	e := newEnv()
	e.offer("A", sellerA, 3000, 5)
	e.offer("B", sellerA, 1000, 5)
	e.offer("C", sellerB, 12000, 1)
	owner := user()

	empty := e.view(t, owner)
	assert.Empty(t, empty.CartID)
	assert.False(t, empty.Ready)
	_, err := e.checkout.Handle(ctx, query.GetCheckout{UserID: owner.UserID})
	require.ErrorIs(t, err, application.ErrCartEmpty)

	for _, add := range []command.AddItem{{Owner: owner, SKU: "A", Quantity: 2}, {Owner: owner, SKU: "B", Quantity: 1}, {Owner: owner, SKU: "C", Quantity: 1}} {
		_, err := e.add.Handle(ctx, add)
		require.NoError(t, err)
	}
	view := e.view(t, owner)
	require.True(t, view.Ready, "%+v", view.Issues)
	require.Len(t, view.Groups, 2)
	assert.Equal(t, sellerA, view.Groups[0].SellerID)
	assert.Equal(t, int64(7000), view.Groups[0].Subtotal)
	assert.Equal(t, int64(500), view.Groups[0].Shipping)
	assert.Equal(t, int64(0), view.Groups[1].Shipping)
	assert.Equal(t, int64(19000), view.Subtotal)
	assert.Equal(t, int64(500), view.Shipping)
	assert.Equal(t, int64(19500), view.Total)
	assert.Equal(t, 4, view.ItemsCount)

	_, err = e.applyPromo.Handle(ctx, command.ApplyPromoCode{Owner: owner, Code: "sale10"})
	require.NoError(t, err)
	view = e.view(t, owner)
	assert.Equal(t, "SALE10", view.PromoCode)
	assert.Equal(t, int64(600), view.Discount)
	assert.Equal(t, int64(18900), view.Total)

	_, err = e.applyPromo.Handle(ctx, command.ApplyPromoCode{Owner: owner, Code: "UNKNOWN"})
	require.ErrorIs(t, err, errPromoNotFound)
	assert.Equal(t, "SALE10", e.view(t, owner).PromoCode)

	_, err = e.update.Handle(ctx, command.UpdateItem{Owner: owner, SKU: "A", Quantity: 5})
	require.NoError(t, err)
	_, err = e.update.Handle(ctx, command.UpdateItem{Owner: owner, SKU: "A", Quantity: 6})
	require.ErrorIs(t, err, domain.ErrExceedsStock)
	_, err = e.update.Handle(ctx, command.UpdateItem{Owner: owner, SKU: "A", Quantity: -1})
	require.ErrorIs(t, err, domain.ErrInvalidQuantity)
	_, err = e.update.Handle(ctx, command.UpdateItem{Owner: owner, SKU: "B", Quantity: 0})
	require.NoError(t, err)
	_, err = e.remove.Handle(ctx, command.RemoveItem{Owner: owner, SKU: "B"})
	require.ErrorIs(t, err, domain.ErrItemNotFound)

	checkout, err := e.checkout.Handle(ctx, query.GetCheckout{UserID: owner.UserID})
	require.NoError(t, err)
	assert.Equal(t, "SALE10", checkout.PromoCode)
	assert.Len(t, checkout.Items, 2)

	_, err = e.removePromo.Handle(ctx, command.RemovePromoCode{Owner: owner})
	require.NoError(t, err)
	assert.Empty(t, e.view(t, owner).PromoCode)

	_, err = e.ordered.Handle(ctx, command.RemoveOrderedItems{UserID: owner.UserID, SKUs: []string{"A", "C"}})
	require.NoError(t, err)
	assert.Empty(t, e.view(t, owner).Groups)
	_, err = e.ordered.Handle(ctx, command.RemoveOrderedItems{UserID: owner.UserID, SKUs: []string{"A"}})
	require.NoError(t, err)
	_, err = e.ordered.Handle(ctx, command.RemoveOrderedItems{UserID: kernel.NewUserID().String(), SKUs: []string{"A"}})
	require.NoError(t, err)
}

func TestCartHighlightsProblems(t *testing.T) {
	e := newEnv()
	e.offer("A", sellerA, 3000, 5)
	e.offer("B", sellerA, 1000, 5)
	e.offer("C", sellerB, 2000, 5)
	owner := user()
	for _, sku := range []string{"A", "B", "C"} {
		_, err := e.add.Handle(ctx, command.AddItem{Owner: owner, SKU: sku, Quantity: 2})
		require.NoError(t, err)
	}
	_, err := e.applyPromo.Handle(ctx, command.ApplyPromoCode{Owner: owner, Code: "SALE10"})
	require.NoError(t, err)

	archived := e.catalog["A"]
	archived.Status = "archived"
	e.catalog["A"] = archived
	e.stock["B"] = 1
	e.pricing.prices["C"] = price{seller: sellerB, amount: 2500, active: true}

	view := e.view(t, owner)
	assert.False(t, view.Ready)
	assert.ElementsMatch(t, []application.Issue{
		{SKU: "A", Code: "CART_OFFER_UNAVAILABLE"}, {SKU: "B", Code: "CART_QUANTITY_EXCEEDS_STOCK"},
	}, view.Issues)
	line := view.Groups[1].Lines[0]
	assert.True(t, line.PriceChanged)
	assert.Equal(t, int64(2000), line.SavedPrice)
	assert.Equal(t, int64(2500), line.UnitPrice)
	assert.Equal(t, int64(5000), view.Subtotal)
	assert.Equal(t, int64(500), view.Discount)
	assert.Equal(t, application.LineUnavailable, view.Groups[0].Lines[0].Status)

	e.pricing.prices["C"] = price{seller: sellerB, amount: 400, active: true}
	view = e.view(t, owner)
	require.ErrorIs(t, view.PromoError, errBelowMinimum)
	assert.Equal(t, int64(0), view.Discount)

	e.pricing.prices["C"] = price{seller: sellerB, amount: 400, active: false}
	e.stock["B"] = 5
	view = e.view(t, owner)
	assert.Equal(t, application.LineUnavailable, view.Groups[1].Lines[0].Status)
	assert.Equal(t, int64(2000), view.Subtotal)
	delete(e.pricing.prices, "B")
	view = e.view(t, owner)
	assert.Zero(t, view.Total)
	assert.Len(t, view.Issues, 3)

	e.pricing.fail = errBelowMinimum
	_, err = e.get.Handle(ctx, query.GetCart{Owner: owner})
	require.Error(t, err)
	_, err = e.get.Handle(ctx, query.GetCart{Owner: owner, DeliveryMethod: "drone"})
	require.Error(t, err)
}

func TestAnonymousCartMergeAndPurge(t *testing.T) {
	e := newEnv()
	e.offer("A", sellerA, 1000, 10)
	e.offer("B", sellerA, 1000, 10)
	anonymous := application.OwnerRef{DeviceID: device}

	_, err := e.add.Handle(ctx, command.AddItem{Owner: anonymous, SKU: "A", Quantity: 3})
	require.NoError(t, err)
	_, err = e.add.Handle(ctx, command.AddItem{Owner: anonymous, SKU: "B", Quantity: 1})
	require.NoError(t, err)
	view := e.view(t, anonymous)
	assert.Equal(t, e.clock.Now().Add(30*24*time.Hour), view.ExpiresAt)

	owner := user()
	_, err = e.add.Handle(ctx, command.AddItem{Owner: owner, SKU: "A", Quantity: 2})
	require.NoError(t, err)
	_, err = e.merge.Handle(ctx, command.MergeCarts{UserID: owner.UserID, DeviceID: device})
	require.NoError(t, err)
	merged := e.view(t, owner)
	require.Len(t, merged.Groups, 1)
	assert.Equal(t, 6, merged.ItemsCount)
	assert.Empty(t, e.view(t, anonymous).CartID)

	_, err = e.merge.Handle(ctx, command.MergeCarts{UserID: owner.UserID, DeviceID: device})
	require.NoError(t, err)
	_, err = e.merge.Handle(ctx, command.MergeCarts{UserID: "bogus", DeviceID: device})
	require.ErrorIs(t, err, application.ErrOwnerRequired)
	_, err = e.merge.Handle(ctx, command.MergeCarts{UserID: owner.UserID, DeviceID: "x"})
	require.ErrorIs(t, err, domain.ErrInvalidDevice)

	fresh := user()
	_, err = e.add.Handle(ctx, command.AddItem{Owner: anonymous, SKU: "B", Quantity: 2})
	require.NoError(t, err)
	_, err = e.merge.Handle(ctx, command.MergeCarts{UserID: fresh.UserID, DeviceID: device})
	require.NoError(t, err)
	assert.Equal(t, 2, e.view(t, fresh).ItemsCount)

	_, err = e.add.Handle(ctx, command.AddItem{Owner: anonymous, SKU: "B", Quantity: 1})
	require.NoError(t, err)
	e.clock.Advance(31 * 24 * time.Hour)
	removed, err := e.purge.Handle(ctx, command.PurgeExpired{})
	require.NoError(t, err)
	assert.Equal(t, 1, removed)
	assert.Equal(t, 2, e.view(t, fresh).ItemsCount)
}

func TestCartValidation(t *testing.T) {
	e := newEnv()
	e.offer("A", sellerA, 1000, 10)
	hidden := catalogapi.OfferSummary{OfferID: "H", SellerID: sellerA, Status: "active", PriceAmount: 10, Currency: "KZT"}
	e.catalog["H"] = hidden

	_, err := e.add.Handle(ctx, command.AddItem{SKU: "A", Quantity: 1})
	require.ErrorIs(t, err, application.ErrOwnerRequired)
	_, err = e.add.Handle(ctx, command.AddItem{Owner: application.OwnerRef{UserID: "bogus"}, SKU: "A", Quantity: 1})
	require.ErrorIs(t, err, application.ErrOwnerRequired)
	_, err = e.add.Handle(ctx, command.AddItem{Owner: application.OwnerRef{DeviceID: "short"}, SKU: "A", Quantity: 1})
	require.ErrorIs(t, err, domain.ErrInvalidDevice)

	owner := user()
	_, err = e.add.Handle(ctx, command.AddItem{Owner: owner, SKU: "bad sku", Quantity: 1})
	require.ErrorIs(t, err, domain.ErrInvalidSKU)
	_, err = e.add.Handle(ctx, command.AddItem{Owner: owner, SKU: "missing", Quantity: 1})
	require.ErrorIs(t, err, domain.ErrOfferUnavailable)
	_, err = e.add.Handle(ctx, command.AddItem{Owner: owner, SKU: "H", Quantity: 1})
	require.ErrorIs(t, err, domain.ErrOfferUnavailable)
	_, err = e.add.Handle(ctx, command.AddItem{Owner: owner, SKU: "A", Quantity: 11})
	require.ErrorIs(t, err, domain.ErrExceedsStock)

	_, err = e.update.Handle(ctx, command.UpdateItem{Owner: owner, SKU: "A", Quantity: 1})
	require.ErrorIs(t, err, domain.ErrCartNotFound)
	_, err = e.remove.Handle(ctx, command.RemoveItem{Owner: owner, SKU: "A"})
	require.ErrorIs(t, err, domain.ErrItemNotFound)
	_, err = e.applyPromo.Handle(ctx, command.ApplyPromoCode{Owner: owner, Code: "SALE10"})
	require.ErrorIs(t, err, application.ErrCartEmpty)
	_, err = e.removePromo.Handle(ctx, command.RemovePromoCode{Owner: owner})
	require.NoError(t, err)

	_, err = e.add.Handle(ctx, command.AddItem{Owner: owner, SKU: "A", Quantity: 1})
	require.NoError(t, err)
	_, err = e.remove.Handle(ctx, command.RemoveItem{Owner: owner, SKU: "A"})
	require.NoError(t, err)
	_, err = e.applyPromo.Handle(ctx, command.ApplyPromoCode{Owner: owner, Code: "SALE10"})
	require.ErrorIs(t, err, application.ErrCartEmpty)
	_, err = e.applyPromo.Handle(ctx, command.ApplyPromoCode{Owner: owner, Code: "?"})
	require.ErrorIs(t, err, application.ErrCartEmpty)
	_, err = e.checkout.Handle(ctx, query.GetCheckout{UserID: owner.UserID})
	require.ErrorIs(t, err, application.ErrCartEmpty)
	_, err = e.checkout.Handle(ctx, query.GetCheckout{UserID: "bogus"})
	require.ErrorIs(t, err, application.ErrOwnerRequired)
	_, err = e.get.Handle(ctx, query.GetCart{})
	require.ErrorIs(t, err, application.ErrOwnerRequired)
}

type tariffs struct {
	fee, freeFrom int64
}

func (t tariffs) Quote(_ context.Context, request shippingapi.TariffRequest) (shippingapi.TariffQuote, error) {
	if request.Method != "" && request.Method != shippingapi.MethodStandard {
		return shippingapi.TariffQuote{}, shippingapi.ErrUnknownMethod
	}
	quote := shippingapi.TariffQuote{Method: shippingapi.MethodStandard, Currency: request.Currency}
	for _, parcel := range request.Parcels {
		cost := t.fee
		if parcel.Subtotal >= t.freeFrom {
			cost = 0
		}
		quote.Parcels = append(quote.Parcels, shippingapi.ParcelCost{SellerID: parcel.SellerID, Cost: cost})
		quote.Total += cost
	}
	return quote, nil
}
