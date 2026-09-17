package command

import (
	"context"
	"errors"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/pricing/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/pricing/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

var ErrInvalidPromoCodeStatus = kernel.Validation("PRICING_INVALID_PROMO_CODE_STATUS", "status must be active or disabled")

type CreatePromoCode struct {
	Actor            auth.Principal
	Code             string
	Discount         DiscountInput
	MinCartAmount    int64
	TotalLimit       int
	PerCustomerLimit int
	StartsAt         time.Time
	EndsAt           time.Time
}

type CreatePromoCodeResult struct {
	Code string
}

type CreatePromoCodeHandler struct {
	base Base
}

func NewCreatePromoCodeHandler(base Base) *CreatePromoCodeHandler {
	return &CreatePromoCodeHandler{base: base}
}

func (h *CreatePromoCodeHandler) Handle(ctx context.Context, cmd CreatePromoCode) (CreatePromoCodeResult, error) {
	if err := requireManager(cmd.Actor); err != nil {
		return CreatePromoCodeResult{}, err
	}
	code, err := domain.NewCode(cmd.Code)
	if err != nil {
		return CreatePromoCodeResult{}, err
	}
	discount := h.base.discount(cmd.Discount)
	promo, err := domain.CreatePromoCode(code, domain.PromoCodeSpec{
		Discount: discount, MinCartAmount: cmd.MinCartAmount, Currency: discount.Currency,
		TotalLimit: cmd.TotalLimit, PerCustomerLimit: cmd.PerCustomerLimit, StartsAt: cmd.StartsAt, EndsAt: cmd.EndsAt,
	}, h.base.clock.Now())
	if err != nil {
		return CreatePromoCodeResult{}, err
	}
	err = h.base.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		return repos.PromoCodes().Save(ctx, promo)
	})
	if err != nil {
		return CreatePromoCodeResult{}, err
	}
	return CreatePromoCodeResult{Code: code.String()}, nil
}

type SetPromoCodeStatus struct {
	Actor  auth.Principal
	Code   string
	Status string
}

type SetPromoCodeStatusHandler struct {
	base Base
}

func NewSetPromoCodeStatusHandler(base Base) *SetPromoCodeStatusHandler {
	return &SetPromoCodeStatusHandler{base: base}
}

func (h *SetPromoCodeStatusHandler) Handle(ctx context.Context, cmd SetPromoCodeStatus) (struct{}, error) {
	if err := requireManager(cmd.Actor); err != nil {
		return struct{}{}, err
	}
	var op promoCodeOperation
	switch domain.PromoCodeStatus(cmd.Status) {
	case domain.PromoCodeActive:
		op = func(p *domain.PromoCode, now time.Time) error { p.Enable(now); return nil }
	case domain.PromoCodeDisabled:
		op = func(p *domain.PromoCode, now time.Time) error { p.Disable(now); return nil }
	default:
		return struct{}{}, ErrInvalidPromoCodeStatus.WithDetail("%q", cmd.Status)
	}
	return struct{}{}, h.base.promoCodeAction(ctx, cmd.Code, op)
}

type RedeemPromoCode struct {
	Code       string
	OrderID    string
	CustomerID string
	Subtotal   int64
	Currency   string
}

type RedeemPromoCodeHandler struct {
	base Base
}

func NewRedeemPromoCodeHandler(base Base) *RedeemPromoCodeHandler {
	return &RedeemPromoCodeHandler{base: base}
}

func (h *RedeemPromoCodeHandler) Handle(ctx context.Context, cmd RedeemPromoCode) (struct{}, error) {
	code, err := domain.NewCode(cmd.Code)
	if err != nil {
		return struct{}{}, domain.ErrPromoCodeNotFound
	}
	order, err := domain.ParseOrderID(cmd.OrderID)
	if err != nil {
		return struct{}{}, kernel.ErrInvalidID
	}
	customer, err := kernel.ParseUserID(cmd.CustomerID)
	if err != nil {
		return struct{}{}, kernel.ErrInvalidID
	}
	subtotal, err := h.base.money(cmd.Subtotal, cmd.Currency)
	if err != nil {
		return struct{}{}, err
	}
	now := h.base.clock.Now()
	return struct{}{}, h.base.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		promo, err := repos.PromoCodes().FindByCode(ctx, code)
		if err != nil {
			return err
		}
		redeemed, err := repos.PromoCodes().Redeemed(ctx, code, order)
		if err != nil {
			return err
		}
		if redeemed {
			return domain.ErrPromoAlreadyUsed
		}
		usage, err := repos.PromoCodes().CustomerUsage(ctx, code, customer)
		if err != nil {
			return err
		}
		if err := promo.Redeem(order, customer, subtotal, usage, now); err != nil {
			return err
		}
		return repos.PromoCodes().Save(ctx, promo)
	})
}

type ReleasePromoCode struct {
	Code    string
	OrderID string
}

type ReleasePromoCodeHandler struct {
	base Base
}

func NewReleasePromoCodeHandler(base Base) *ReleasePromoCodeHandler {
	return &ReleasePromoCodeHandler{base: base}
}

func (h *ReleasePromoCodeHandler) Handle(ctx context.Context, cmd ReleasePromoCode) (struct{}, error) {
	order, err := domain.ParseOrderID(cmd.OrderID)
	if err != nil {
		return struct{}{}, kernel.ErrInvalidID
	}
	code, err := domain.NewCode(cmd.Code)
	if err != nil {
		return struct{}{}, nil
	}
	now := h.base.clock.Now()
	err = h.base.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		promo, err := repos.PromoCodes().FindByCode(ctx, code)
		if err != nil {
			return err
		}
		redeemed, err := repos.PromoCodes().Redeemed(ctx, code, order)
		if err != nil || !redeemed {
			return err
		}
		if err := promo.Release(order, redeemed, now); err != nil {
			return err
		}
		return repos.PromoCodes().Save(ctx, promo)
	})
	if errors.Is(err, domain.ErrPromoCodeNotFound) {
		return struct{}{}, nil
	}
	return struct{}{}, err
}
