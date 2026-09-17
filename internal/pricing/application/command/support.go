package command

import (
	"context"
	"time"

	identity "github.com/gliedabrennung/go-marketplace-backend/internal/identity/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/pricing/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/pricing/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

type Base struct {
	uow    application.UnitOfWork
	clock  application.Clock
	policy application.Policy
}

func NewBase(uow application.UnitOfWork, clock application.Clock, policy application.Policy) Base {
	return Base{uow: uow, clock: clock, policy: policy}
}

type DiscountInput struct {
	Kind        string
	BasisPoints int
	Amount      int64
	Currency    string
	BuyQuantity int
	FreeUnits   int
}

func (b Base) discount(input DiscountInput) domain.DiscountSpec {
	currency := input.Currency
	if currency == "" {
		currency = string(b.policy.DefaultCurrency)
	}
	return domain.DiscountSpec{
		Kind: domain.DiscountKind(input.Kind), BasisPoints: input.BasisPoints, Amount: input.Amount,
		Currency: currency, BuyQuantity: input.BuyQuantity, FreeUnits: input.FreeUnits,
	}
}

func (b Base) money(amount int64, raw string) (kernel.Money, error) {
	if raw == "" {
		raw = string(b.policy.DefaultCurrency)
	}
	currency, err := kernel.NewCurrency(raw)
	if err != nil {
		return kernel.Money{}, err
	}
	return kernel.NewMoney(amount, currency)
}

type TargetInput struct {
	SKUs       []string
	Sellers    []string
	Categories []string
}

func target(input TargetInput) (domain.Target, error) {
	out := domain.Target{}
	for _, raw := range input.SKUs {
		sku, err := domain.NewSKU(raw)
		if err != nil {
			return domain.Target{}, err
		}
		out.SKUs = append(out.SKUs, sku)
	}
	for _, raw := range input.Sellers {
		seller, err := kernel.ParseSellerID(raw)
		if err != nil {
			return domain.Target{}, domain.ErrInvalidTarget.WithDetail("seller %q", raw)
		}
		out.Sellers = append(out.Sellers, seller)
	}
	for _, raw := range input.Categories {
		category, err := domain.ParseCategoryID(raw)
		if err != nil {
			return domain.Target{}, domain.ErrInvalidTarget.WithDetail("category %q", raw)
		}
		out.Categories = append(out.Categories, category)
	}
	return out, nil
}

func requireManager(p auth.Principal) error {
	return identity.Authorize(p, identity.PermPromotionsManage)
}

type promotionOperation func(promotion *domain.Promotion, now time.Time) error

func (b Base) promotionAction(ctx context.Context, p auth.Principal, rawID string, op promotionOperation) error {
	if err := requireManager(p); err != nil {
		return err
	}
	id, err := domain.ParsePromotionID(rawID)
	if err != nil {
		return domain.ErrPromotionNotFound
	}
	now := b.clock.Now()
	return b.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		promotion, err := repos.Promotions().FindByID(ctx, id)
		if err != nil {
			return err
		}
		if err := op(promotion, now); err != nil {
			return err
		}
		return repos.Promotions().Save(ctx, promotion)
	})
}

type promoCodeOperation func(promo *domain.PromoCode, now time.Time) error

func (b Base) promoCodeAction(ctx context.Context, rawCode string, op promoCodeOperation) error {
	code, err := domain.NewCode(rawCode)
	if err != nil {
		return domain.ErrPromoCodeNotFound
	}
	now := b.clock.Now()
	return b.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		promo, err := repos.PromoCodes().FindByCode(ctx, code)
		if err != nil {
			return err
		}
		if err := op(promo, now); err != nil {
			return err
		}
		return repos.PromoCodes().Save(ctx, promo)
	})
}
