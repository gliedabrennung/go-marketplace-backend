package domain

import (
	"fmt"
	"slices"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type PromoCodeStatus string

const (
	PromoCodeActive   PromoCodeStatus = "active"
	PromoCodeDisabled PromoCodeStatus = "disabled"
)

type Redemption struct {
	OrderID    OrderID
	CustomerID kernel.UserID
	Amount     kernel.Money
	At         time.Time
}

type PromoCodeSpec struct {
	Discount         DiscountSpec
	MinCartAmount    int64
	Currency         string
	TotalLimit       int
	PerCustomerLimit int
	StartsAt         time.Time
	EndsAt           time.Time
}

type PromoCode struct {
	code             Code
	spec             DiscountSpec
	minCartAmount    kernel.Money
	totalLimit       int
	perCustomerLimit int
	used             int
	status           PromoCodeStatus
	startsAt         time.Time
	endsAt           time.Time
	createdAt        time.Time
	updatedAt        time.Time
	version          int

	redemptions []Redemption
	releases    []OrderID
	events      kernel.EventBuffer
}

func CreatePromoCode(code Code, spec PromoCodeSpec, now time.Time) (*PromoCode, error) {
	if code.IsZero() {
		return nil, ErrInvalidPromoCode
	}
	if spec.Discount.Kind == DiscountBuyNGetM {
		return nil, ErrInvalidDiscount.WithDetail("promo code supports percentage and fixed discounts")
	}
	if err := spec.Discount.validate(); err != nil {
		return nil, err
	}
	if spec.TotalLimit < 0 || spec.PerCustomerLimit < 0 || spec.MinCartAmount < 0 {
		return nil, ErrInvalidLimits
	}
	if !spec.EndsAt.IsZero() && !spec.EndsAt.After(spec.StartsAt) {
		return nil, ErrInvalidPeriod
	}
	currency, err := kernel.NewCurrency(defaultCurrency(spec.Currency, spec.Discount))
	if err != nil {
		return nil, ErrInvalidDiscount.WithDetail("currency %q", spec.Currency)
	}
	minimum, err := kernel.NewMoney(spec.MinCartAmount, currency)
	if err != nil {
		return nil, ErrInvalidLimits
	}
	promo := &PromoCode{
		code: code, spec: spec.Discount, minCartAmount: minimum, totalLimit: spec.TotalLimit,
		perCustomerLimit: spec.PerCustomerLimit, status: PromoCodeActive,
		startsAt: spec.StartsAt, endsAt: spec.EndsAt, createdAt: now, updatedAt: now,
	}
	promo.events.Record(PromoCodeChanged{Code: code.String(), Status: PromoCodeActive, At: now})
	return promo, nil
}

func defaultCurrency(raw string, spec DiscountSpec) string {
	if raw != "" {
		return raw
	}
	if spec.Currency != "" {
		return spec.Currency
	}
	return string(kernel.KZT)
}

func (p *PromoCode) Discount(subtotal kernel.Money) (kernel.Money, error) {
	switch p.spec.Kind {
	case DiscountPercentage:
		points, err := kernel.NewBasisPoints(p.spec.BasisPoints)
		if err != nil {
			return kernel.Money{}, ErrInvalidDiscount
		}
		return subtotal.ApplyPercent(points), nil
	default:
		amount, err := kernel.NewMoney(p.spec.Amount, subtotal.Currency())
		if err != nil {
			return kernel.Money{}, ErrInvalidDiscount
		}
		if compare, err := amount.Compare(subtotal); err != nil || compare > 0 {
			return subtotal, nil
		}
		return amount, nil
	}
}

func (p *PromoCode) EnsureUsable(subtotal kernel.Money, customerUsage int, now time.Time) error {
	if p.status != PromoCodeActive {
		return ErrPromoCodeInactive
	}
	if !p.startsAt.IsZero() && now.Before(p.startsAt) {
		return ErrPromoCodeExpired
	}
	if !p.endsAt.IsZero() && !now.Before(p.endsAt) {
		return ErrPromoCodeExpired
	}
	if p.totalLimit > 0 && p.used >= p.totalLimit {
		return ErrPromoCodeDepleted
	}
	if p.perCustomerLimit > 0 && customerUsage >= p.perCustomerLimit {
		return ErrPromoCodePerBuyer
	}
	if !p.minCartAmount.IsZero() {
		compare, err := subtotal.Compare(p.minCartAmount)
		if err != nil {
			return ErrCurrencyMismatch
		}
		if compare < 0 {
			return ErrCartBelowMinimum.WithDetail("minimum %s", p.minCartAmount)
		}
	}
	return nil
}

func (p *PromoCode) Redeem(order OrderID, customer kernel.UserID, subtotal kernel.Money, customerUsage int, now time.Time) error {
	if order.IsZero() || customer.IsZero() {
		return kernel.ErrInvalidID
	}
	if slices.ContainsFunc(p.redemptions, func(r Redemption) bool { return r.OrderID == order }) {
		return ErrPromoAlreadyUsed
	}
	if err := p.EnsureUsable(subtotal, customerUsage, now); err != nil {
		return err
	}
	amount, err := p.Discount(subtotal)
	if err != nil {
		return err
	}
	p.used++
	p.updatedAt = now
	p.redemptions = append(p.redemptions, Redemption{OrderID: order, CustomerID: customer, Amount: amount, At: now})
	p.events.Record(PromoCodeRedeemed{
		Code: p.code.String(), OrderID: order.String(), CustomerID: customer.String(),
		Amount: amount.Amount(), Currency: string(amount.Currency()), At: now,
	})
	return nil
}

func (p *PromoCode) Release(order OrderID, redeemed bool, now time.Time) error {
	if order.IsZero() {
		return kernel.ErrInvalidID
	}
	if !redeemed || slices.Contains(p.releases, order) {
		return nil
	}
	if p.used > 0 {
		p.used--
	}
	p.updatedAt = now
	p.releases = append(p.releases, order)
	p.events.Record(PromoCodeReleased{Code: p.code.String(), OrderID: order.String(), At: now})
	return nil
}

func (p *PromoCode) PullRedemptions() []Redemption {
	out := p.redemptions
	p.redemptions = nil
	return out
}

func (p *PromoCode) PullReleases() []OrderID {
	out := p.releases
	p.releases = nil
	return out
}

func (p *PromoCode) Disable(now time.Time) {
	if p.status == PromoCodeDisabled {
		return
	}
	p.status, p.updatedAt = PromoCodeDisabled, now
	p.events.Record(PromoCodeChanged{Code: p.code.String(), Status: PromoCodeDisabled, At: now})
}

func (p *PromoCode) Enable(now time.Time) {
	if p.status == PromoCodeActive {
		return
	}
	p.status, p.updatedAt = PromoCodeActive, now
	p.events.Record(PromoCodeChanged{Code: p.code.String(), Status: PromoCodeActive, At: now})
}

func (p *PromoCode) Code() Code { return p.code }

func (p *PromoCode) Status() PromoCodeStatus { return p.status }

func (p *PromoCode) Used() int { return p.used }

func (p *PromoCode) Version() int { return p.version }

func (p *PromoCode) AdvanceVersion() { p.version++ }

func (p *PromoCode) PullEvents() []kernel.DomainEvent { return p.events.Pull() }

type PromoCodeSnapshot struct {
	Code             string
	Kind             string
	BasisPoints      int
	Amount           int64
	Currency         string
	MinCartAmount    int64
	TotalLimit       int
	PerCustomerLimit int
	Used             int
	Status           string
	StartsAt         time.Time
	EndsAt           time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
	Version          int
}

func (p *PromoCode) Snapshot() PromoCodeSnapshot {
	snap := PromoCodeSnapshot{
		Code: p.code.String(), Kind: string(p.spec.Kind), BasisPoints: p.spec.BasisPoints, Amount: p.spec.Amount,
		Currency: string(p.minCartAmount.Currency()), MinCartAmount: p.minCartAmount.Amount(),
		TotalLimit: p.totalLimit, PerCustomerLimit: p.perCustomerLimit, Used: p.used, Status: string(p.status),
		StartsAt: p.startsAt, EndsAt: p.endsAt, CreatedAt: p.createdAt, UpdatedAt: p.updatedAt, Version: p.version,
	}
	return snap
}

func RehydratePromoCode(s PromoCodeSnapshot) (*PromoCode, error) {
	code, err := NewCode(s.Code)
	if err != nil {
		return nil, fmt.Errorf("rehydrate promo code: %w", err)
	}
	currency, err := kernel.NewCurrency(s.Currency)
	if err != nil {
		return nil, fmt.Errorf("rehydrate promo code %s currency: %w", s.Code, err)
	}
	minimum, err := kernel.NewMoney(s.MinCartAmount, currency)
	if err != nil {
		return nil, fmt.Errorf("rehydrate promo code %s minimum: %w", s.Code, err)
	}
	promo := &PromoCode{
		code: code, spec: DiscountSpec{Kind: DiscountKind(s.Kind), BasisPoints: s.BasisPoints, Amount: s.Amount, Currency: s.Currency},
		minCartAmount: minimum, totalLimit: s.TotalLimit, perCustomerLimit: s.PerCustomerLimit, used: s.Used,
		status: PromoCodeStatus(s.Status), startsAt: s.StartsAt, endsAt: s.EndsAt,
		createdAt: s.CreatedAt, updatedAt: s.UpdatedAt, version: s.Version,
	}
	if !slices.Contains([]PromoCodeStatus{PromoCodeActive, PromoCodeDisabled}, promo.status) {
		return nil, fmt.Errorf("rehydrate promo code %s: unknown status %q", s.Code, s.Status)
	}
	return promo, nil
}
