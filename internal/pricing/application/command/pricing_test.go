package command_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/pricing/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/pricing/application/command"
	"github.com/gliedabrennung/go-marketplace-backend/internal/pricing/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/pricing/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/pricing/infrastructure/memory"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/clock"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/pagination"
)

var ctx = context.Background()

type membership map[string]string

func (m membership) MemberRole(_ context.Context, sellerID, userID string) (string, bool, error) {
	return "seller_admin", m[sellerID] == userID, nil
}

type env struct {
	store *memory.Store
	reads *memory.ReadModel
	clock *clock.Manual
	team  membership

	createPromotion *command.CreatePromotionHandler
	updatePromotion *command.UpdatePromotionHandler
	promotionStatus *command.SetPromotionStatusHandler
	createCode      *command.CreatePromoCodeHandler
	codeStatus      *command.SetPromoCodeStatusHandler
	redeem          *command.RedeemPromoCodeHandler
	release         *command.ReleasePromoCodeHandler
	syncPrice       *command.SyncOfferPriceHandler
	compareAt       *command.SetCompareAtPriceHandler
	categories      *command.SetProductCategoriesHandler

	quote      *query.QuoteHandler
	promotion  *query.GetPromotionHandler
	promotions *query.ListPromotionsHandler
	promoCode  *query.GetPromoCodeHandler
}

func newEnv() *env {
	store := memory.NewStore()
	e := &env{
		store: store, reads: memory.NewReadModel(store), team: membership{},
		clock: clock.NewManual(time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)),
	}
	policy := application.DefaultPolicy()
	base := command.NewBase(memory.NewUnitOfWork(store), e.clock, policy)

	e.createPromotion = command.NewCreatePromotionHandler(base)
	e.updatePromotion = command.NewUpdatePromotionHandler(base)
	e.promotionStatus = command.NewSetPromotionStatusHandler(base)
	e.createCode = command.NewCreatePromoCodeHandler(base)
	e.codeStatus = command.NewSetPromoCodeStatusHandler(base)
	e.redeem = command.NewRedeemPromoCodeHandler(base)
	e.release = command.NewReleasePromoCodeHandler(base)
	e.syncPrice = command.NewSyncOfferPriceHandler(base)
	e.compareAt = command.NewSetCompareAtPriceHandler(base, e.team)
	e.categories = command.NewSetProductCategoriesHandler(base)

	e.quote = query.NewQuoteHandler(e.reads, e.clock, policy)
	e.promotion = query.NewGetPromotionHandler(e.reads)
	e.promotions = query.NewListPromotionsHandler(e.reads)
	e.promoCode = query.NewGetPromoCodeHandler(e.reads)
	return e
}

func admin() auth.Principal {
	return auth.Principal{UserID: kernel.NewUserID().String(), Roles: []string{"buyer", "platform_admin"}}
}

func buyer() auth.Principal {
	return auth.Principal{UserID: kernel.NewUserID().String(), Roles: []string{"buyer"}}
}

type catalog struct {
	seller   string
	category string
	phone    string
	cover    string
}

func (e *env) seed(t *testing.T) catalog {
	t.Helper()
	c := catalog{seller: kernel.NewSellerID().String(), category: domain.NewCategoryID().String()}
	phoneProduct := domain.NewProductID().String()
	caseProduct := domain.NewProductID().String()
	c.phone, c.cover = "OFFER-PHONE", "OFFER-CASE"

	_, err := e.syncPrice.Handle(ctx, command.SyncOfferPrice{SKU: c.phone, ProductID: phoneProduct, SellerID: c.seller, Amount: 200000, Active: true})
	require.NoError(t, err)
	_, err = e.syncPrice.Handle(ctx, command.SyncOfferPrice{SKU: c.cover, ProductID: caseProduct, SellerID: c.seller, Amount: 5000, Currency: "KZT", Active: true})
	require.NoError(t, err)
	_, err = e.categories.Handle(ctx, command.SetProductCategories{ProductID: phoneProduct, CategoryPath: []string{c.category}})
	require.NoError(t, err)
	return c
}

func (e *env) activePromotion(t *testing.T, input command.PromotionInput) string {
	t.Helper()
	manager := admin()
	created, err := e.createPromotion.Handle(ctx, command.CreatePromotion{Actor: manager, Promotion: input})
	require.NoError(t, err)
	_, err = e.promotionStatus.Handle(ctx, command.SetPromotionStatus{Actor: manager, PromotionID: created.PromotionID, Status: "active"})
	require.NoError(t, err)
	return created.PromotionID
}

func TestPromotionManagement(t *testing.T) {
	e := newEnv()
	manager := admin()
	input := command.PromotionInput{
		Name:     "Осенняя распродажа",
		Discount: command.DiscountInput{Kind: "percentage", BasisPoints: 1000},
		Target:   command.TargetInput{Categories: []string{domain.NewCategoryID().String()}},
		Priority: 5,
	}

	_, err := e.createPromotion.Handle(ctx, command.CreatePromotion{Actor: buyer(), Promotion: input})
	require.ErrorIs(t, err, auth.ErrForbidden)
	broken := input
	broken.Target = command.TargetInput{Sellers: []string{"bad"}}
	_, err = e.createPromotion.Handle(ctx, command.CreatePromotion{Actor: manager, Promotion: broken})
	require.ErrorIs(t, err, domain.ErrInvalidTarget)
	broken.Target = command.TargetInput{Categories: []string{"bad"}}
	_, err = e.createPromotion.Handle(ctx, command.CreatePromotion{Actor: manager, Promotion: broken})
	require.ErrorIs(t, err, domain.ErrInvalidTarget)
	broken.Target = command.TargetInput{SKUs: []string{"плохой"}}
	_, err = e.createPromotion.Handle(ctx, command.CreatePromotion{Actor: manager, Promotion: broken})
	require.ErrorIs(t, err, domain.ErrInvalidSKU)
	broken = input
	broken.Discount = command.DiscountInput{Kind: "percentage"}
	_, err = e.createPromotion.Handle(ctx, command.CreatePromotion{Actor: manager, Promotion: broken})
	require.ErrorIs(t, err, domain.ErrInvalidDiscount)

	first, err := e.createPromotion.Handle(ctx, command.CreatePromotion{Actor: manager, Promotion: input})
	require.NoError(t, err)
	e.clock.Advance(time.Minute)
	second, err := e.createPromotion.Handle(ctx, command.CreatePromotion{Actor: manager, Promotion: input})
	require.NoError(t, err)

	renamed := input
	renamed.Name = "Зимняя распродажа"
	_, err = e.updatePromotion.Handle(ctx, command.UpdatePromotion{Actor: manager, PromotionID: first.PromotionID, Promotion: renamed})
	require.NoError(t, err)
	_, err = e.updatePromotion.Handle(ctx, command.UpdatePromotion{Actor: manager, PromotionID: "bad", Promotion: renamed})
	require.ErrorIs(t, err, domain.ErrPromotionNotFound)
	_, err = e.updatePromotion.Handle(ctx, command.UpdatePromotion{Actor: manager, PromotionID: first.PromotionID, Promotion: broken})
	require.ErrorIs(t, err, domain.ErrInvalidDiscount)

	_, err = e.promotionStatus.Handle(ctx, command.SetPromotionStatus{Actor: manager, PromotionID: first.PromotionID, Status: "deleted"})
	require.ErrorIs(t, err, command.ErrInvalidPromotionStatus)
	for _, status := range []string{"active", "paused", "ended"} {
		_, err = e.promotionStatus.Handle(ctx, command.SetPromotionStatus{Actor: manager, PromotionID: first.PromotionID, Status: status})
		require.NoError(t, err)
	}
	_, err = e.promotionStatus.Handle(ctx, command.SetPromotionStatus{Actor: manager, PromotionID: first.PromotionID, Status: "active"})
	require.ErrorIs(t, err, domain.ErrPromotionEnded)
	_, err = e.promotionStatus.Handle(ctx, command.SetPromotionStatus{Actor: buyer(), PromotionID: first.PromotionID, Status: "active"})
	require.ErrorIs(t, err, auth.ErrForbidden)

	view, err := e.promotion.Handle(ctx, query.GetPromotion{Actor: manager, PromotionID: first.PromotionID})
	require.NoError(t, err)
	assert.Equal(t, "Зимняя распродажа", view.Name)
	assert.Equal(t, "ended", view.Status)
	assert.Equal(t, "KZT", view.Currency)
	_, err = e.promotion.Handle(ctx, query.GetPromotion{Actor: manager, PromotionID: "bad"})
	require.ErrorIs(t, err, domain.ErrPromotionNotFound)
	_, err = e.promotion.Handle(ctx, query.GetPromotion{Actor: buyer(), PromotionID: first.PromotionID})
	require.ErrorIs(t, err, auth.ErrForbidden)

	page, err := e.promotions.Handle(ctx, query.ListPromotions{Actor: manager, Limit: 1})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.Equal(t, second.PromotionID, page.Items[0].ID)
	page, err = e.promotions.Handle(ctx, query.ListPromotions{Actor: manager, Limit: 1, Cursor: page.NextCursor})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.Equal(t, first.PromotionID, page.Items[0].ID)

	page, err = e.promotions.Handle(ctx, query.ListPromotions{Actor: manager, Status: "draft"})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	_, err = e.promotions.Handle(ctx, query.ListPromotions{Actor: manager, Status: "unknown"})
	require.ErrorIs(t, err, query.ErrUnknownStatus)
	_, err = e.promotions.Handle(ctx, query.ListPromotions{Actor: manager, Cursor: "%%%"})
	require.ErrorIs(t, err, pagination.ErrInvalidCursor)
	_, err = e.promotions.Handle(ctx, query.ListPromotions{Actor: buyer()})
	require.ErrorIs(t, err, auth.ErrForbidden)
}

func TestQuoteAppliesPromotionsAndPromoCode(t *testing.T) {
	e := newEnv()
	c := e.seed(t)
	manager := admin()

	e.activePromotion(t, command.PromotionInput{
		Name: "Смартфоны -10%", Priority: 10,
		Discount: command.DiscountInput{Kind: "percentage", BasisPoints: 1000},
		Target:   command.TargetInput{Categories: []string{c.category}},
	})
	e.activePromotion(t, command.PromotionInput{
		Name: "Чехлы 3 по цене 2", Priority: 1,
		Discount: command.DiscountInput{Kind: "buy_n_get_m", BuyQuantity: 3, FreeUnits: 1},
		Target:   command.TargetInput{SKUs: []string{c.cover}},
	})

	plain, err := e.quote.Handle(ctx, query.Quote{Lines: []query.QuoteLine{{SKU: c.phone, Quantity: 1}, {SKU: c.cover, Quantity: 3}}})
	require.NoError(t, err)
	assert.Equal(t, int64(215000), plain.Subtotal)
	assert.Equal(t, int64(25000), plain.Discount)
	assert.Equal(t, int64(190000), plain.Total)
	assert.Equal(t, "KZT", plain.Currency)
	require.Len(t, plain.Lines, 2)
	assert.Equal(t, int64(180000), plain.Lines[0].Final)
	require.Len(t, plain.Lines[1].Discounts, 1)
	assert.Equal(t, "buy_n_get_m", plain.Lines[1].Discounts[0].Kind)

	_, err = e.createCode.Handle(ctx, command.CreatePromoCode{
		Actor: manager, Code: "welcome-2026", Discount: command.DiscountInput{Kind: "fixed", Amount: 10000},
		MinCartAmount: 150000, PerCustomerLimit: 1, TotalLimit: 10,
	})
	require.NoError(t, err)
	_, err = e.createCode.Handle(ctx, command.CreatePromoCode{Actor: manager, Code: "WELCOME-2026", Discount: command.DiscountInput{Kind: "fixed", Amount: 1}})
	require.ErrorIs(t, err, domain.ErrPromoCodeExists)
	_, err = e.createCode.Handle(ctx, command.CreatePromoCode{Actor: buyer(), Code: "OTHER-CODE", Discount: command.DiscountInput{Kind: "fixed", Amount: 1}})
	require.ErrorIs(t, err, auth.ErrForbidden)
	_, err = e.createCode.Handle(ctx, command.CreatePromoCode{Actor: manager, Code: "x", Discount: command.DiscountInput{Kind: "fixed", Amount: 1}})
	require.ErrorIs(t, err, domain.ErrInvalidPromoCode)
	_, err = e.createCode.Handle(ctx, command.CreatePromoCode{Actor: manager, Code: "BROKEN", Discount: command.DiscountInput{Kind: "fixed"}})
	require.ErrorIs(t, err, domain.ErrInvalidDiscount)

	customer := kernel.NewUserID().String()
	withCode, err := e.quote.Handle(ctx, query.Quote{
		Lines:     []query.QuoteLine{{SKU: c.phone, Quantity: 1}, {SKU: c.cover, Quantity: 3}},
		PromoCode: "welcome-2026", CustomerID: customer,
	})
	require.NoError(t, err)
	assert.Equal(t, "WELCOME-2026", withCode.PromoCode)
	assert.Equal(t, int64(180000), withCode.Total)
	assert.Equal(t, int64(35000), withCode.Discount)

	_, err = e.quote.Handle(ctx, query.Quote{Lines: []query.QuoteLine{{SKU: c.cover, Quantity: 1}}, PromoCode: "WELCOME-2026"})
	require.ErrorIs(t, err, domain.ErrCartBelowMinimum)
	_, err = e.quote.Handle(ctx, query.Quote{Lines: []query.QuoteLine{{SKU: c.phone, Quantity: 1}}, PromoCode: "MISSING-CODE"})
	require.ErrorIs(t, err, domain.ErrPromoCodeNotFound)
	_, err = e.quote.Handle(ctx, query.Quote{Lines: []query.QuoteLine{{SKU: c.phone, Quantity: 1}}, PromoCode: "!"})
	require.ErrorIs(t, err, domain.ErrPromoCodeNotFound)
	_, err = e.quote.Handle(ctx, query.Quote{Lines: []query.QuoteLine{{SKU: c.phone, Quantity: 1}}, PromoCode: "WELCOME-2026", CustomerID: "bad"})
	require.ErrorIs(t, err, kernel.ErrInvalidID)

	_, err = e.quote.Handle(ctx, query.Quote{})
	require.ErrorIs(t, err, query.ErrInvalidLines)
	_, err = e.quote.Handle(ctx, query.Quote{Lines: []query.QuoteLine{{SKU: c.phone, Quantity: 0}}})
	require.ErrorIs(t, err, query.ErrInvalidLines)
	_, err = e.quote.Handle(ctx, query.Quote{Lines: []query.QuoteLine{{SKU: c.phone, Quantity: 1}, {SKU: c.phone, Quantity: 2}}})
	require.ErrorIs(t, err, query.ErrInvalidLines)
	_, err = e.quote.Handle(ctx, query.Quote{Lines: []query.QuoteLine{{SKU: "плохой", Quantity: 1}}})
	require.ErrorIs(t, err, domain.ErrInvalidSKU)
	_, err = e.quote.Handle(ctx, query.Quote{Lines: []query.QuoteLine{{SKU: "UNKNOWN", Quantity: 1}}})
	require.ErrorIs(t, err, domain.ErrOfferPriceNotFound)

	_, err = e.syncPrice.Handle(ctx, command.SyncOfferPrice{SKU: c.cover, ProductID: domain.NewProductID().String(), SellerID: c.seller, Amount: 6000, Active: false})
	require.NoError(t, err)
	_, err = e.quote.Handle(ctx, query.Quote{Lines: []query.QuoteLine{{SKU: c.cover, Quantity: 1}}})
	require.ErrorIs(t, err, domain.ErrOfferPriceInactive)

	code, err := e.promoCode.Handle(ctx, query.GetPromoCode{Actor: manager, Code: "welcome-2026"})
	require.NoError(t, err)
	assert.Equal(t, int64(150000), code.MinCartAmount)
	assert.Equal(t, "active", code.Status)
	_, err = e.promoCode.Handle(ctx, query.GetPromoCode{Actor: manager, Code: "!"})
	require.ErrorIs(t, err, domain.ErrPromoCodeNotFound)
	_, err = e.promoCode.Handle(ctx, query.GetPromoCode{Actor: buyer(), Code: "WELCOME-2026"})
	require.ErrorIs(t, err, auth.ErrForbidden)
}

func TestPromoCodeRedemption(t *testing.T) {
	e := newEnv()
	manager := admin()
	_, err := e.createCode.Handle(ctx, command.CreatePromoCode{
		Actor: manager, Code: "SPRING", Discount: command.DiscountInput{Kind: "percentage", BasisPoints: 500},
		PerCustomerLimit: 1, TotalLimit: 2,
	})
	require.NoError(t, err)

	customer := kernel.NewUserID().String()
	order := domain.NewOrderID().String()
	redeem := command.RedeemPromoCode{Code: "SPRING", OrderID: order, CustomerID: customer, Subtotal: 100000}

	_, err = e.redeem.Handle(ctx, command.RedeemPromoCode{Code: "!", OrderID: order, CustomerID: customer, Subtotal: 1})
	require.ErrorIs(t, err, domain.ErrPromoCodeNotFound)
	_, err = e.redeem.Handle(ctx, command.RedeemPromoCode{Code: "SPRING", OrderID: "bad", CustomerID: customer, Subtotal: 1})
	require.ErrorIs(t, err, kernel.ErrInvalidID)
	_, err = e.redeem.Handle(ctx, command.RedeemPromoCode{Code: "SPRING", OrderID: order, CustomerID: "bad", Subtotal: 1})
	require.ErrorIs(t, err, kernel.ErrInvalidID)
	_, err = e.redeem.Handle(ctx, command.RedeemPromoCode{Code: "SPRING", OrderID: order, CustomerID: customer, Subtotal: 1, Currency: "bad"})
	require.Error(t, err)

	_, err = e.redeem.Handle(ctx, redeem)
	require.NoError(t, err)
	_, err = e.redeem.Handle(ctx, redeem)
	require.ErrorIs(t, err, domain.ErrPromoAlreadyUsed)

	other := redeem
	other.OrderID = domain.NewOrderID().String()
	_, err = e.redeem.Handle(ctx, other)
	require.ErrorIs(t, err, domain.ErrPromoCodePerBuyer)

	customerID, err := kernel.ParseUserID(customer)
	require.NoError(t, err)
	usage, err := e.reads.PromoCodeUsage(ctx, mustCode(t, "SPRING"), customerID)
	require.NoError(t, err)
	assert.Equal(t, 1, usage)

	_, err = e.release.Handle(ctx, command.ReleasePromoCode{Code: "SPRING", OrderID: order})
	require.NoError(t, err)
	_, err = e.release.Handle(ctx, command.ReleasePromoCode{Code: "SPRING", OrderID: order})
	require.NoError(t, err)
	_, err = e.release.Handle(ctx, command.ReleasePromoCode{Code: "UNKNOWN", OrderID: order})
	require.NoError(t, err)
	_, err = e.release.Handle(ctx, command.ReleasePromoCode{Code: "!", OrderID: order})
	require.NoError(t, err)
	_, err = e.release.Handle(ctx, command.ReleasePromoCode{Code: "SPRING", OrderID: "bad"})
	require.ErrorIs(t, err, kernel.ErrInvalidID)

	view, err := e.promoCode.Handle(ctx, query.GetPromoCode{Actor: manager, Code: "SPRING"})
	require.NoError(t, err)
	assert.Zero(t, view.Used)

	_, err = e.redeem.Handle(ctx, other)
	require.NoError(t, err)

	_, err = e.codeStatus.Handle(ctx, command.SetPromoCodeStatus{Actor: manager, Code: "SPRING", Status: "disabled"})
	require.NoError(t, err)
	third := redeem
	third.OrderID, third.CustomerID = domain.NewOrderID().String(), kernel.NewUserID().String()
	_, err = e.redeem.Handle(ctx, third)
	require.ErrorIs(t, err, domain.ErrPromoCodeInactive)
	_, err = e.codeStatus.Handle(ctx, command.SetPromoCodeStatus{Actor: manager, Code: "SPRING", Status: "active"})
	require.NoError(t, err)
	_, err = e.codeStatus.Handle(ctx, command.SetPromoCodeStatus{Actor: manager, Code: "SPRING", Status: "archived"})
	require.ErrorIs(t, err, command.ErrInvalidPromoCodeStatus)
	_, err = e.codeStatus.Handle(ctx, command.SetPromoCodeStatus{Actor: buyer(), Code: "SPRING", Status: "active"})
	require.ErrorIs(t, err, auth.ErrForbidden)
	_, err = e.codeStatus.Handle(ctx, command.SetPromoCodeStatus{Actor: manager, Code: "!", Status: "active"})
	require.ErrorIs(t, err, domain.ErrPromoCodeNotFound)
}

func TestOfferPriceSync(t *testing.T) {
	e := newEnv()
	c := e.seed(t)
	owner := kernel.NewUserID().String()
	e.team[c.seller] = owner

	_, err := e.syncPrice.Handle(ctx, command.SyncOfferPrice{SKU: "плохой", ProductID: domain.NewProductID().String(), SellerID: c.seller, Amount: 1})
	require.ErrorIs(t, err, domain.ErrInvalidSKU)
	_, err = e.syncPrice.Handle(ctx, command.SyncOfferPrice{SKU: "OFFER-X", ProductID: "bad", SellerID: c.seller, Amount: 1})
	require.ErrorIs(t, err, kernel.ErrInvalidID)
	_, err = e.syncPrice.Handle(ctx, command.SyncOfferPrice{SKU: "OFFER-X", ProductID: domain.NewProductID().String(), SellerID: "bad", Amount: 1})
	require.ErrorIs(t, err, kernel.ErrInvalidID)
	_, err = e.syncPrice.Handle(ctx, command.SyncOfferPrice{SKU: "OFFER-X", ProductID: domain.NewProductID().String(), SellerID: c.seller, Amount: 0, Active: true})
	require.ErrorIs(t, err, domain.ErrInvalidPrice)

	_, err = e.compareAt.Handle(ctx, command.SetCompareAtPrice{Actor: auth.Principal{UserID: owner}, SKU: c.phone, CompareAt: 250000})
	require.NoError(t, err)
	quote, err := e.quote.Handle(ctx, query.Quote{Lines: []query.QuoteLine{{SKU: c.phone, Quantity: 1}}})
	require.NoError(t, err)
	assert.Equal(t, int64(250000), quote.Lines[0].CompareAt)

	_, err = e.compareAt.Handle(ctx, command.SetCompareAtPrice{Actor: auth.Principal{UserID: owner}, SKU: c.phone, CompareAt: 100})
	require.ErrorIs(t, err, domain.ErrInvalidPrice)
	_, err = e.compareAt.Handle(ctx, command.SetCompareAtPrice{Actor: auth.Principal{UserID: owner}, SKU: c.phone})
	require.NoError(t, err)
	_, err = e.compareAt.Handle(ctx, command.SetCompareAtPrice{Actor: buyer(), SKU: c.phone, CompareAt: 250000})
	require.ErrorIs(t, err, domain.ErrOfferPriceNotFound)
	_, err = e.compareAt.Handle(ctx, command.SetCompareAtPrice{SKU: c.phone, CompareAt: 250000})
	require.ErrorIs(t, err, auth.ErrUnauthenticated)
	_, err = e.compareAt.Handle(ctx, command.SetCompareAtPrice{Actor: auth.Principal{UserID: owner}, SKU: "плохой", CompareAt: 1})
	require.ErrorIs(t, err, domain.ErrOfferPriceNotFound)

	_, err = e.categories.Handle(ctx, command.SetProductCategories{ProductID: "bad"})
	require.ErrorIs(t, err, kernel.ErrInvalidID)
	_, err = e.categories.Handle(ctx, command.SetProductCategories{ProductID: domain.NewProductID().String(), CategoryPath: []string{"bad"}})
	require.ErrorIs(t, err, kernel.ErrInvalidID)

	assert.NotEmpty(t, e.store.Events())
}

func mustCode(t *testing.T, raw string) domain.Code {
	t.Helper()
	code, err := domain.NewCode(raw)
	require.NoError(t, err)
	return code
}
