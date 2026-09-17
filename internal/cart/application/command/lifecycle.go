package command

import (
	"context"
	"errors"

	"github.com/gliedabrennung/go-marketplace-backend/internal/cart/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/cart/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type MergeCarts struct {
	UserID   string
	DeviceID string
}

type MergeCartsHandler struct{ base Base }

func NewMergeCartsHandler(base Base) *MergeCartsHandler { return &MergeCartsHandler{base: base} }

func (h *MergeCartsHandler) Handle(ctx context.Context, cmd MergeCarts) (struct{}, error) {
	user, err := kernel.ParseUserID(cmd.UserID)
	if err != nil {
		return struct{}{}, application.ErrOwnerRequired
	}
	userOwner, err := domain.UserOwner(user)
	if err != nil {
		return struct{}{}, err
	}
	deviceOwner, err := domain.DeviceOwner(cmd.DeviceID)
	if err != nil {
		return struct{}{}, err
	}
	return struct{}{}, h.base.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		device, err := repos.Carts().FindByOwner(ctx, deviceOwner)
		if errors.Is(err, domain.ErrCartNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		now := h.base.clock.Now()
		target, err := repos.Carts().FindByOwner(ctx, userOwner)
		if errors.Is(err, domain.ErrCartNotFound) {
			target, err = domain.New(domain.NewCartID(), userOwner, h.base.policy.Limits, now)
		}
		if err != nil {
			return err
		}
		available, err := h.base.stock.Available(ctx, skus(device.Items()))
		if err != nil {
			return err
		}
		if err := target.Absorb(device, available, h.base.policy.Limits, now); err != nil {
			return err
		}
		if err := repos.Carts().Save(ctx, target); err != nil {
			return err
		}
		return repos.Carts().Delete(ctx, device)
	})
}

func skus(items []domain.Item) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.SKU())
	}
	return out
}

type RemoveOrderedItems struct {
	UserID string
	SKUs   []string
}

type RemoveOrderedItemsHandler struct{ base Base }

func NewRemoveOrderedItemsHandler(base Base) *RemoveOrderedItemsHandler {
	return &RemoveOrderedItemsHandler{base: base}
}

func (h *RemoveOrderedItemsHandler) Handle(ctx context.Context, cmd RemoveOrderedItems) (struct{}, error) {
	err := h.base.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		owner, err := application.OwnerRef{UserID: cmd.UserID}.Resolve()
		if err != nil {
			return err
		}
		cart, err := repos.Carts().FindByOwner(ctx, owner)
		if err != nil {
			return err
		}
		if !cart.RemoveSKUs(cmd.SKUs, h.base.policy.Limits, h.base.clock.Now()) {
			return nil
		}
		return repos.Carts().Save(ctx, cart)
	})
	if errors.Is(err, domain.ErrCartNotFound) {
		return struct{}{}, nil
	}
	return struct{}{}, err
}

type PurgeExpired struct{}

type PurgeExpiredHandler struct{ base Base }

func NewPurgeExpiredHandler(base Base) *PurgeExpiredHandler { return &PurgeExpiredHandler{base: base} }

func (h *PurgeExpiredHandler) Handle(ctx context.Context, _ PurgeExpired) (int, error) {
	var removed int
	err := h.base.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		var err error
		removed, err = repos.Carts().DeleteExpired(ctx, h.base.clock.Now(), h.base.policy.PurgeBatch)
		return err
	})
	return removed, err
}
