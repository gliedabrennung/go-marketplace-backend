package command

import (
	"context"
	"errors"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/cart/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/cart/domain"
)

type Base struct {
	uow       application.UnitOfWork
	clock     application.Clock
	policy    application.Policy
	offers    application.Offers
	stock     application.Stock
	assembler *application.Assembler
}

func NewBase(uow application.UnitOfWork, clock application.Clock, policy application.Policy, offers application.Offers, stock application.Stock, assembler *application.Assembler) Base {
	return Base{uow: uow, clock: clock, policy: policy, offers: offers, stock: stock, assembler: assembler}
}

func (b Base) offer(ctx context.Context, rawSKU string) (domain.Offer, int, error) {
	sku, err := domain.NormalizeSKU(rawSKU)
	if err != nil {
		return domain.Offer{}, 0, err
	}
	offers, err := b.offers.Offers(ctx, []string{sku})
	if err != nil {
		return domain.Offer{}, 0, err
	}
	summary, found := offers[sku]
	if !application.Purchasable(summary, found) {
		return domain.Offer{}, 0, domain.ErrOfferUnavailable
	}
	stock, err := b.stock.Available(ctx, []string{sku})
	if err != nil {
		return domain.Offer{}, 0, err
	}
	return domain.Offer{SKU: sku, SellerID: summary.SellerID, Price: summary.PriceAmount, Currency: summary.Currency}, stock[sku], nil
}

func (b Base) change(ctx context.Context, ref application.OwnerRef, create bool, fn func(cart *domain.Cart, now time.Time) error) error {
	owner, err := ref.Resolve()
	if err != nil {
		return err
	}
	return b.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		now := b.clock.Now()
		cart, err := repos.Carts().FindByOwner(ctx, owner)
		if errors.Is(err, domain.ErrCartNotFound) && create {
			cart, err = domain.New(domain.NewCartID(), owner, b.policy.Limits, now)
		}
		if err != nil {
			return err
		}
		if err := fn(cart, now); err != nil {
			return err
		}
		return repos.Carts().Save(ctx, cart)
	})
}

type AddItem struct {
	Owner    application.OwnerRef
	SKU      string
	Quantity int
}

type AddItemHandler struct{ base Base }

func NewAddItemHandler(base Base) *AddItemHandler { return &AddItemHandler{base: base} }

func (h *AddItemHandler) Handle(ctx context.Context, cmd AddItem) (struct{}, error) {
	offer, available, err := h.base.offer(ctx, cmd.SKU)
	if err != nil {
		return struct{}{}, err
	}
	return struct{}{}, h.base.change(ctx, cmd.Owner, true, func(cart *domain.Cart, now time.Time) error {
		return cart.Add(offer, cmd.Quantity, available, h.base.policy.Limits, now)
	})
}

type UpdateItem struct {
	Owner    application.OwnerRef
	SKU      string
	Quantity int
}

type UpdateItemHandler struct{ base Base }

func NewUpdateItemHandler(base Base) *UpdateItemHandler { return &UpdateItemHandler{base: base} }

func (h *UpdateItemHandler) Handle(ctx context.Context, cmd UpdateItem) (struct{}, error) {
	if cmd.Quantity == 0 {
		return NewRemoveItemHandler(h.base).Handle(ctx, RemoveItem{Owner: cmd.Owner, SKU: cmd.SKU})
	}
	if cmd.Quantity < 0 {
		return struct{}{}, domain.ErrInvalidQuantity
	}
	offer, available, err := h.base.offer(ctx, cmd.SKU)
	if err != nil {
		return struct{}{}, err
	}
	return struct{}{}, h.base.change(ctx, cmd.Owner, false, func(cart *domain.Cart, now time.Time) error {
		return cart.SetQuantity(offer, cmd.Quantity, available, h.base.policy.Limits, now)
	})
}

type RemoveItem struct {
	Owner application.OwnerRef
	SKU   string
}

type RemoveItemHandler struct{ base Base }

func NewRemoveItemHandler(base Base) *RemoveItemHandler { return &RemoveItemHandler{base: base} }

func (h *RemoveItemHandler) Handle(ctx context.Context, cmd RemoveItem) (struct{}, error) {
	err := h.base.change(ctx, cmd.Owner, false, func(cart *domain.Cart, now time.Time) error {
		return cart.Remove(cmd.SKU, h.base.policy.Limits, now)
	})
	if errors.Is(err, domain.ErrCartNotFound) {
		return struct{}{}, domain.ErrItemNotFound
	}
	return struct{}{}, err
}

type ApplyPromoCode struct {
	Owner application.OwnerRef
	Code  string
}

type ApplyPromoCodeHandler struct{ base Base }

func NewApplyPromoCodeHandler(base Base) *ApplyPromoCodeHandler {
	return &ApplyPromoCodeHandler{base: base}
}

func (h *ApplyPromoCodeHandler) Handle(ctx context.Context, cmd ApplyPromoCode) (struct{}, error) {
	err := h.base.change(ctx, cmd.Owner, false, func(cart *domain.Cart, now time.Time) error {
		if cart.IsEmpty() {
			return application.ErrCartEmpty
		}
		if err := cart.ApplyPromoCode(cmd.Code, h.base.policy.Limits, now); err != nil {
			return err
		}
		view, err := h.base.assembler.Assemble(ctx, cart, cmd.Owner.UserID, "")
		if err != nil {
			return err
		}
		return view.PromoError
	})
	if errors.Is(err, domain.ErrCartNotFound) {
		return struct{}{}, application.ErrCartEmpty
	}
	return struct{}{}, err
}

type RemovePromoCode struct {
	Owner application.OwnerRef
}

type RemovePromoCodeHandler struct{ base Base }

func NewRemovePromoCodeHandler(base Base) *RemovePromoCodeHandler {
	return &RemovePromoCodeHandler{base: base}
}

func (h *RemovePromoCodeHandler) Handle(ctx context.Context, cmd RemovePromoCode) (struct{}, error) {
	err := h.base.change(ctx, cmd.Owner, false, func(cart *domain.Cart, now time.Time) error {
		cart.ClearPromoCode(h.base.policy.Limits, now)
		return nil
	})
	if errors.Is(err, domain.ErrCartNotFound) {
		return struct{}{}, nil
	}
	return struct{}{}, err
}
