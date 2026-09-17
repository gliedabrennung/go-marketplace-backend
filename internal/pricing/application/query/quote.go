package query

import (
	"context"
	"slices"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/pricing/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/pricing/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

var ErrInvalidLines = kernel.Validation("PRICING_INVALID_LINES", "quote must contain 1-100 lines with distinct skus and positive quantities")

type QuoteLine struct {
	SKU      string
	Quantity int
}

type Quote struct {
	Lines      []QuoteLine
	PromoCode  string
	CustomerID string
}

type DiscountView struct {
	RuleID    string
	Kind      string
	Amount    int64
	PromoCode string
}

type LineView struct {
	SKU       string
	SellerID  string
	ProductID string
	Quantity  int
	UnitPrice int64
	CompareAt int64
	Base      int64
	Discounts []DiscountView
	Final     int64
}

type QuoteView struct {
	Lines     []LineView
	Subtotal  int64
	Discount  int64
	Total     int64
	Currency  string
	PromoCode string
}

type QuoteHandler struct {
	data   application.PricingData
	clock  application.Clock
	policy application.Policy
}

func NewQuoteHandler(data application.PricingData, clock application.Clock, policy application.Policy) *QuoteHandler {
	return &QuoteHandler{data: data, clock: clock, policy: policy}
}

func (h *QuoteHandler) Handle(ctx context.Context, q Quote) (QuoteView, error) {
	skus, err := h.validate(q)
	if err != nil {
		return QuoteView{}, err
	}
	now := h.clock.Now()
	lines, prices, err := h.lines(ctx, q, skus)
	if err != nil {
		return QuoteView{}, err
	}
	rules, err := h.rules(ctx, now)
	if err != nil {
		return QuoteView{}, err
	}
	promo, err := h.promo(ctx, q, lines, rules, now)
	if err != nil {
		return QuoteView{}, err
	}
	quote, err := domain.PriceCalculator{}.Calculate(lines, rules, promo)
	if err != nil {
		return QuoteView{}, err
	}
	return view(quote, prices), nil
}

func (h *QuoteHandler) validate(q Quote) ([]string, error) {
	if len(q.Lines) == 0 || len(q.Lines) > h.policy.MaxQuoteLines {
		return nil, ErrInvalidLines
	}
	skus := make([]string, 0, len(q.Lines))
	for _, line := range q.Lines {
		if line.Quantity <= 0 || slices.Contains(skus, line.SKU) {
			return nil, ErrInvalidLines
		}
		if _, err := domain.NewSKU(line.SKU); err != nil {
			return nil, err
		}
		skus = append(skus, line.SKU)
	}
	return skus, nil
}

func (h *QuoteHandler) lines(ctx context.Context, q Quote, skus []string) ([]domain.CartLine, map[string]*domain.OfferPrice, error) {
	prices, err := h.data.Prices(ctx, skus)
	if err != nil {
		return nil, nil, err
	}
	bySKU := make(map[string]*domain.OfferPrice, len(prices))
	products := make([]string, 0, len(prices))
	for _, price := range prices {
		bySKU[price.SKU().String()] = price
		products = append(products, price.ProductID().String())
	}
	paths, err := h.data.CategoryPaths(ctx, products)
	if err != nil {
		return nil, nil, err
	}
	lines := make([]domain.CartLine, 0, len(q.Lines))
	for _, requested := range q.Lines {
		price, ok := bySKU[requested.SKU]
		if !ok {
			return nil, nil, domain.ErrOfferPriceNotFound.WithDetail("sku %s", requested.SKU)
		}
		quantity, err := kernel.NewQuantity(requested.Quantity)
		if err != nil {
			return nil, nil, err
		}
		line, err := price.Line(quantity, categoryIDs(paths[price.ProductID().String()]))
		if err != nil {
			return nil, nil, err
		}
		lines = append(lines, line)
	}
	return lines, bySKU, nil
}

func (h *QuoteHandler) rules(ctx context.Context, now time.Time) ([]domain.DiscountRule, error) {
	promotions, err := h.data.RunningPromotions(ctx, now)
	if err != nil {
		return nil, err
	}
	rules := make([]domain.DiscountRule, 0, len(promotions))
	for _, promotion := range promotions {
		if !promotion.IsRunning(now) {
			continue
		}
		rule, err := promotion.Rule()
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

func (h *QuoteHandler) promo(ctx context.Context, q Quote, lines []domain.CartLine, rules []domain.DiscountRule, now time.Time) (*domain.PromoCode, error) {
	if q.PromoCode == "" {
		return nil, nil
	}
	code, err := domain.NewCode(q.PromoCode)
	if err != nil {
		return nil, domain.ErrPromoCodeNotFound
	}
	promo, err := h.data.FindPromoCode(ctx, code)
	if err != nil {
		return nil, err
	}
	usage := 0
	if q.CustomerID != "" {
		customer, err := kernel.ParseUserID(q.CustomerID)
		if err != nil {
			return nil, kernel.ErrInvalidID
		}
		if usage, err = h.data.PromoCodeUsage(ctx, code, customer); err != nil {
			return nil, err
		}
	}
	discounted, err := domain.PriceCalculator{}.Calculate(lines, rules, nil)
	if err != nil {
		return nil, err
	}
	if err := promo.EnsureUsable(discounted.Total, usage, now); err != nil {
		return nil, err
	}
	return promo, nil
}

func categoryIDs(raw []string) []domain.CategoryID {
	out := make([]domain.CategoryID, 0, len(raw))
	for _, value := range raw {
		if id, err := domain.ParseCategoryID(value); err == nil {
			out = append(out, id)
		}
	}
	return out
}

func view(quote domain.Quote, prices map[string]*domain.OfferPrice) QuoteView {
	out := QuoteView{
		Subtotal: quote.Subtotal.Amount(), Discount: quote.Discount.Amount(), Total: quote.Total.Amount(),
		Currency: string(quote.Currency), PromoCode: quote.PromoCode, Lines: make([]LineView, 0, len(quote.Lines)),
	}
	for _, line := range quote.Lines {
		lineView := LineView{
			SKU: line.SKU.String(), SellerID: line.SellerID.String(), ProductID: line.ProductID.String(),
			Quantity: line.Quantity.Value(), UnitPrice: line.UnitPrice.Amount(), Base: line.Base.Amount(),
			Final: line.Final.Amount(), Discounts: make([]DiscountView, 0, len(line.Discounts)),
		}
		if price, ok := prices[line.SKU.String()]; ok {
			lineView.CompareAt = price.CompareAt().Amount()
		}
		for _, discount := range line.Discounts {
			lineView.Discounts = append(lineView.Discounts, DiscountView{
				RuleID: discount.RuleID, Kind: discount.Kind, Amount: discount.Amount.Amount(), PromoCode: discount.PromoCode,
			})
		}
		out.Lines = append(out.Lines, lineView)
	}
	return out
}
