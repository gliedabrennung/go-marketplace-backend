package domain

import (
	"slices"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type AppliedDiscount struct {
	RuleID    string
	Kind      string
	Amount    kernel.Money
	PromoCode string
}

type LineBreakdown struct {
	SKU       SKU
	SellerID  kernel.SellerID
	ProductID ProductID
	Quantity  kernel.Quantity
	UnitPrice kernel.Money
	Base      kernel.Money
	Discounts []AppliedDiscount
	Final     kernel.Money
}

type Quote struct {
	Lines     []LineBreakdown
	Subtotal  kernel.Money
	Discount  kernel.Money
	Total     kernel.Money
	PromoCode string
	Currency  kernel.Currency
}

type PriceCalculator struct{}

func (c PriceCalculator) Calculate(lines []CartLine, rules []DiscountRule, promo *PromoCode) (Quote, error) {
	if len(lines) == 0 {
		return Quote{}, ErrEmptyQuote
	}
	currency := lines[0].UnitPrice.Currency()
	subtotal, err := kernel.ZeroMoney(currency)
	if err != nil {
		return Quote{}, err
	}
	base := subtotal

	quote := Quote{Currency: currency, Lines: make([]LineBreakdown, 0, len(lines))}
	for _, line := range lines {
		if line.UnitPrice.Currency() != currency {
			return Quote{}, ErrCurrencyMismatch
		}
		if line.Quantity.IsZero() {
			return Quote{}, ErrInvalidQuantity
		}
		breakdown, err := c.line(line, rules, PricingContext{Subtotal: subtotal, Items: len(lines)})
		if err != nil {
			return Quote{}, err
		}
		if subtotal, err = subtotal.Add(breakdown.Final); err != nil {
			return Quote{}, err
		}
		if base, err = base.Add(breakdown.Base); err != nil {
			return Quote{}, err
		}
		quote.Lines = append(quote.Lines, breakdown)
	}

	total := subtotal
	if promo != nil {
		discount, err := promo.Discount(subtotal)
		if err != nil {
			return Quote{}, err
		}
		if !discount.IsZero() {
			if total, err = subtotal.Sub(discount); err != nil {
				return Quote{}, err
			}
			quote.PromoCode = promo.Code().String()
			quote.Lines, err = distribute(quote.Lines, discount, promo.Code().String())
			if err != nil {
				return Quote{}, err
			}
		}
	}

	discount, err := base.Sub(total)
	if err != nil {
		return Quote{}, err
	}
	quote.Subtotal, quote.Discount, quote.Total = base, discount, total
	return quote, nil
}

func (c PriceCalculator) line(line CartLine, rules []DiscountRule, ctx PricingContext) (LineBreakdown, error) {
	base, err := line.UnitPrice.MulQuantity(line.Quantity)
	if err != nil {
		return LineBreakdown{}, err
	}
	breakdown := LineBreakdown{
		SKU: line.SKU, SellerID: line.SellerID, ProductID: line.ProductID, Quantity: line.Quantity,
		UnitPrice: line.UnitPrice, Base: base, Final: base, Discounts: []AppliedDiscount{},
	}
	for _, rule := range applicable(rules, line, ctx) {
		if breakdown.Final.IsZero() {
			break
		}
		amount := rule.Discount(breakdown.Final, line)
		if amount.IsZero() {
			continue
		}
		next, err := breakdown.Final.Sub(amount)
		if err != nil {
			amount, next = breakdown.Final, kernel.ZeroLike(breakdown.Final)
		}
		breakdown.Discounts = append(breakdown.Discounts, AppliedDiscount{
			RuleID: rule.ID().String(), Kind: kindOf(rule), Amount: amount,
		})
		breakdown.Final = next
		if rule.IsExclusive() {
			break
		}
	}
	return breakdown, nil
}

func applicable(rules []DiscountRule, line CartLine, ctx PricingContext) []DiscountRule {
	out := make([]DiscountRule, 0, len(rules))
	for _, rule := range rules {
		if rule.IsApplicable(line, ctx) {
			out = append(out, rule)
		}
	}
	slices.SortStableFunc(out, func(a, b DiscountRule) int { return b.Priority() - a.Priority() })
	return out
}

func distribute(lines []LineBreakdown, discount kernel.Money, code string) ([]LineBreakdown, error) {
	total := int64(0)
	for _, line := range lines {
		total += line.Final.Amount()
	}
	if total == 0 {
		return lines, nil
	}
	left := discount.Amount()
	for i := range lines {
		if left == 0 {
			break
		}
		share := discount.Amount() * lines[i].Final.Amount() / total
		if i == len(lines)-1 || share > left {
			share = left
		}
		if share == 0 {
			continue
		}
		amount, err := kernel.NewMoney(share, discount.Currency())
		if err != nil {
			return nil, err
		}
		final, err := lines[i].Final.Sub(amount)
		if err != nil {
			return nil, err
		}
		lines[i].Final = final
		lines[i].Discounts = append(lines[i].Discounts, AppliedDiscount{
			RuleID: code, Kind: "promo_code", Amount: amount, PromoCode: code,
		})
		left -= share
	}
	return lines, nil
}

func kindOf(rule DiscountRule) string {
	switch rule.(type) {
	case percentageRule:
		return string(DiscountPercentage)
	case fixedRule:
		return string(DiscountFixed)
	default:
		return string(DiscountBuyNGetM)
	}
}
