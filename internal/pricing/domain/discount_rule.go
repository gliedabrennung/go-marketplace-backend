package domain

import (
	"slices"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type DiscountKind string

const (
	DiscountPercentage DiscountKind = "percentage"
	DiscountFixed      DiscountKind = "fixed"
	DiscountBuyNGetM   DiscountKind = "buy_n_get_m"
)

type CartLine struct {
	SKU          SKU
	SellerID     kernel.SellerID
	ProductID    ProductID
	CategoryPath []CategoryID
	UnitPrice    kernel.Money
	Quantity     kernel.Quantity
}

type PricingContext struct {
	Subtotal kernel.Money
	Items    int
}

type DiscountRule interface {
	ID() PromotionID
	Priority() int
	IsExclusive() bool
	IsApplicable(line CartLine, ctx PricingContext) bool
	Discount(current kernel.Money, line CartLine) kernel.Money
}

type DiscountSpec struct {
	Kind        DiscountKind
	BasisPoints int
	Amount      int64
	Currency    string
	BuyQuantity int
	FreeUnits   int
}

func (s DiscountSpec) validate() error {
	switch s.Kind {
	case DiscountPercentage:
		if _, err := kernel.NewBasisPoints(s.BasisPoints); err != nil || s.BasisPoints == 0 {
			return ErrInvalidDiscount.WithDetail("basis points %d", s.BasisPoints)
		}
	case DiscountFixed:
		if s.Amount <= 0 {
			return ErrInvalidDiscount.WithDetail("amount %d", s.Amount)
		}
		if _, err := kernel.NewCurrency(s.Currency); err != nil {
			return ErrInvalidDiscount.WithDetail("currency %q", s.Currency)
		}
	case DiscountBuyNGetM:
		if s.BuyQuantity <= 0 || s.FreeUnits <= 0 || s.FreeUnits >= s.BuyQuantity {
			return ErrInvalidDiscount.WithDetail("buy %d get %d", s.BuyQuantity, s.FreeUnits)
		}
	default:
		return ErrInvalidDiscount.WithDetail("kind %q", s.Kind)
	}
	return nil
}

type Target struct {
	SKUs       []SKU
	Sellers    []kernel.SellerID
	Categories []CategoryID
}

func (t Target) isEmpty() bool {
	return len(t.SKUs) == 0 && len(t.Sellers) == 0 && len(t.Categories) == 0
}

func (t Target) matches(line CartLine) bool {
	if slices.Contains(t.SKUs, line.SKU) {
		return true
	}
	if slices.Contains(t.Sellers, line.SellerID) {
		return true
	}
	return slices.ContainsFunc(t.Categories, func(id CategoryID) bool { return slices.Contains(line.CategoryPath, id) })
}

type percentageRule struct {
	id          PromotionID
	target      Target
	priority    int
	exclusive   bool
	basisPoints kernel.BasisPoints
}

func (r percentageRule) ID() PromotionID { return r.id }

func (r percentageRule) Priority() int { return r.priority }

func (r percentageRule) IsExclusive() bool { return r.exclusive }

func (r percentageRule) IsApplicable(line CartLine, _ PricingContext) bool {
	return r.target.matches(line)
}

func (r percentageRule) Discount(current kernel.Money, _ CartLine) kernel.Money {
	return current.ApplyPercent(r.basisPoints)
}

type fixedRule struct {
	id        PromotionID
	target    Target
	priority  int
	exclusive bool
	amount    kernel.Money
}

func (r fixedRule) ID() PromotionID { return r.id }

func (r fixedRule) Priority() int { return r.priority }

func (r fixedRule) IsExclusive() bool { return r.exclusive }

func (r fixedRule) IsApplicable(line CartLine, _ PricingContext) bool {
	return r.target.matches(line) && r.amount.Currency() == line.UnitPrice.Currency()
}

func (r fixedRule) Discount(current kernel.Money, _ CartLine) kernel.Money {
	if compare, err := r.amount.Compare(current); err != nil || compare > 0 {
		return current
	}
	return r.amount
}

type buyNGetMRule struct {
	id        PromotionID
	target    Target
	priority  int
	exclusive bool
	buy       int
	free      int
}

func (r buyNGetMRule) ID() PromotionID { return r.id }

func (r buyNGetMRule) Priority() int { return r.priority }

func (r buyNGetMRule) IsExclusive() bool { return r.exclusive }

func (r buyNGetMRule) IsApplicable(line CartLine, _ PricingContext) bool {
	return r.target.matches(line) && line.Quantity.Value() >= r.buy
}

func (r buyNGetMRule) Discount(current kernel.Money, line CartLine) kernel.Money {
	sets := line.Quantity.Value() / r.buy
	free, err := kernel.NewQuantity(sets * r.free)
	if err != nil {
		return kernel.ZeroLike(current)
	}
	discount, err := line.UnitPrice.MulQuantity(free)
	if err != nil {
		return current
	}
	if compare, err := discount.Compare(current); err != nil || compare > 0 {
		return current
	}
	return discount
}

func newRule(id PromotionID, spec DiscountSpec, target Target, priority int, exclusive bool) (DiscountRule, error) {
	if err := spec.validate(); err != nil {
		return nil, err
	}
	switch spec.Kind {
	case DiscountPercentage:
		points, err := kernel.NewBasisPoints(spec.BasisPoints)
		if err != nil {
			return nil, ErrInvalidDiscount
		}
		return percentageRule{id: id, target: target, priority: priority, exclusive: exclusive, basisPoints: points}, nil
	case DiscountFixed:
		currency, err := kernel.NewCurrency(spec.Currency)
		if err != nil {
			return nil, ErrInvalidDiscount
		}
		amount, err := kernel.NewMoney(spec.Amount, currency)
		if err != nil {
			return nil, ErrInvalidDiscount
		}
		return fixedRule{id: id, target: target, priority: priority, exclusive: exclusive, amount: amount}, nil
	default:
		return buyNGetMRule{id: id, target: target, priority: priority, exclusive: exclusive, buy: spec.BuyQuantity, free: spec.FreeUnits}, nil
	}
}
