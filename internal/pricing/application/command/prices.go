package command

import (
	"context"
	"errors"

	"github.com/gliedabrennung/go-marketplace-backend/internal/pricing/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/pricing/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

type SyncOfferPrice struct {
	SKU       string
	ProductID string
	SellerID  string
	Amount    int64
	Currency  string
	Active    bool
}

type SyncOfferPriceHandler struct {
	base Base
}

func NewSyncOfferPriceHandler(base Base) *SyncOfferPriceHandler {
	return &SyncOfferPriceHandler{base: base}
}

func (h *SyncOfferPriceHandler) Handle(ctx context.Context, cmd SyncOfferPrice) (struct{}, error) {
	sku, err := domain.NewSKU(cmd.SKU)
	if err != nil {
		return struct{}{}, err
	}
	product, err := domain.ParseProductID(cmd.ProductID)
	if err != nil {
		return struct{}{}, kernel.ErrInvalidID
	}
	seller, err := kernel.ParseSellerID(cmd.SellerID)
	if err != nil {
		return struct{}{}, kernel.ErrInvalidID
	}
	price, err := h.base.money(cmd.Amount, cmd.Currency)
	if err != nil {
		return struct{}{}, err
	}
	now := h.base.clock.Now()
	return struct{}{}, h.base.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		offer, err := repos.OfferPrices().FindBySKU(ctx, sku)
		switch {
		case errors.Is(err, domain.ErrOfferPriceNotFound):
			if offer, err = domain.NewOfferPrice(sku, product, seller, price, now); err != nil {
				return err
			}
		case err != nil:
			return err
		default:
			if err := offer.ChangePrice(price, now); err != nil {
				return err
			}
		}
		offer.SetActive(cmd.Active, now)
		return repos.OfferPrices().Save(ctx, offer)
	})
}

type SetCompareAtPrice struct {
	Actor     auth.Principal
	SKU       string
	CompareAt int64
}

type SetCompareAtPriceHandler struct {
	base    Base
	sellers SellerMembership
}

type SellerMembership interface {
	MemberRole(ctx context.Context, sellerID, userID string) (string, bool, error)
}

func NewSetCompareAtPriceHandler(base Base, sellers SellerMembership) *SetCompareAtPriceHandler {
	return &SetCompareAtPriceHandler{base: base, sellers: sellers}
}

func (h *SetCompareAtPriceHandler) Handle(ctx context.Context, cmd SetCompareAtPrice) (struct{}, error) {
	if cmd.Actor.UserID == "" {
		return struct{}{}, auth.ErrUnauthenticated
	}
	sku, err := domain.NewSKU(cmd.SKU)
	if err != nil {
		return struct{}{}, domain.ErrOfferPriceNotFound
	}
	now := h.base.clock.Now()
	return struct{}{}, h.base.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		offer, err := repos.OfferPrices().FindBySKU(ctx, sku)
		if err != nil {
			return err
		}
		_, member, err := h.sellers.MemberRole(ctx, offer.SellerID().String(), cmd.Actor.UserID)
		if err != nil {
			return err
		}
		if !member {
			return domain.ErrOfferPriceNotFound
		}
		compareAt := kernel.Money{}
		if cmd.CompareAt > 0 {
			if compareAt, err = kernel.NewMoney(cmd.CompareAt, offer.Price().Currency()); err != nil {
				return err
			}
		}
		if err := offer.SetCompareAt(compareAt, now); err != nil {
			return err
		}
		return repos.OfferPrices().Save(ctx, offer)
	})
}

type SetProductCategories struct {
	ProductID    string
	CategoryPath []string
}

type SetProductCategoriesHandler struct {
	base Base
}

func NewSetProductCategoriesHandler(base Base) *SetProductCategoriesHandler {
	return &SetProductCategoriesHandler{base: base}
}

func (h *SetProductCategoriesHandler) Handle(ctx context.Context, cmd SetProductCategories) (struct{}, error) {
	if _, err := domain.ParseProductID(cmd.ProductID); err != nil {
		return struct{}{}, kernel.ErrInvalidID
	}
	for _, raw := range cmd.CategoryPath {
		if _, err := domain.ParseCategoryID(raw); err != nil {
			return struct{}{}, kernel.ErrInvalidID
		}
	}
	now := h.base.clock.Now()
	return struct{}{}, h.base.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		return repos.Categories().Save(ctx, cmd.ProductID, cmd.CategoryPath, now)
	})
}
