package command

import (
	"context"
	"errors"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/inventory/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/inventory/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

type EnsureStock struct {
	SKU      string
	SellerID string
}

type EnsureStockHandler struct {
	base Base
}

func NewEnsureStockHandler(base Base) *EnsureStockHandler {
	return &EnsureStockHandler{base: base}
}

func (h *EnsureStockHandler) Handle(ctx context.Context, cmd EnsureStock) (struct{}, error) {
	sku, err := domain.NewSKU(cmd.SKU)
	if err != nil {
		return struct{}{}, err
	}
	seller, err := kernel.ParseSellerID(cmd.SellerID)
	if err != nil {
		return struct{}{}, kernel.ErrInvalidID
	}
	now := h.base.clock.Now()
	err = h.base.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		_, err := repos.Stock().FindBySKU(ctx, sku)
		if err == nil {
			return nil
		}
		if !errors.Is(err, domain.ErrStockNotFound) {
			return err
		}
		item, err := domain.OpenStock(sku, seller, now)
		if err != nil {
			return err
		}
		return repos.Stock().Save(ctx, item)
	})
	if errors.Is(err, domain.ErrStockExists) {
		return struct{}{}, nil
	}
	return struct{}{}, err
}

type SetStock struct {
	Actor     auth.Principal
	SKU       string
	Quantity  int
	Reference string
}

type SetStockResult struct {
	SKU       string
	SellerID  string
	Available int
	Reserved  int
	UpdatedAt time.Time
}

type SetStockHandler struct {
	base Base
}

func NewSetStockHandler(base Base) *SetStockHandler {
	return &SetStockHandler{base: base}
}

func (h *SetStockHandler) Handle(ctx context.Context, cmd SetStock) (SetStockResult, error) {
	sku, err := domain.NewSKU(cmd.SKU)
	if err != nil {
		return SetStockResult{}, domain.ErrStockNotFound
	}
	quantity, err := kernel.NewQuantity(cmd.Quantity)
	if err != nil {
		return SetStockResult{}, err
	}
	owner, err := h.owner(ctx, sku)
	if err != nil {
		return SetStockResult{}, err
	}
	if err := h.base.requireMember(ctx, cmd.Actor, owner); err != nil {
		return SetStockResult{}, err
	}

	now := h.base.clock.Now()
	var result SetStockResult
	err = h.base.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		items, err := h.base.stockOf(ctx, repos, []domain.SKU{sku})
		if err != nil {
			return err
		}
		item := items[0]
		if err := item.SetAvailable(owner, quantity, reference(cmd.Reference), now); err != nil {
			return err
		}
		result = SetStockResult{
			SKU: item.SKU().String(), SellerID: item.SellerID().String(),
			Available: item.Available().Value(), Reserved: item.Reserved().Value(), UpdatedAt: item.UpdatedAt(),
		}
		return repos.Stock().Save(ctx, item)
	})
	if err != nil {
		return SetStockResult{}, err
	}
	return result, nil
}

func (h *SetStockHandler) owner(ctx context.Context, sku domain.SKU) (kernel.SellerID, error) {
	var owner kernel.SellerID
	err := h.base.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		item, err := repos.Stock().FindBySKU(ctx, sku)
		if err != nil {
			return err
		}
		owner = item.SellerID()
		return nil
	})
	return owner, err
}

type ReturnStock struct {
	Reference string
	Lines     []Line
}

type ReturnStockHandler struct {
	base Base
}

func NewReturnStockHandler(base Base) *ReturnStockHandler {
	return &ReturnStockHandler{base: base}
}

func (h *ReturnStockHandler) Handle(ctx context.Context, cmd ReturnStock) (struct{}, error) {
	lines, err := h.base.parseLines(cmd.Lines)
	if err != nil {
		return struct{}{}, err
	}
	if cmd.Reference == "" {
		return struct{}{}, domain.ErrInvalidReference
	}
	now := h.base.clock.Now()
	return struct{}{}, h.base.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		items, err := h.base.stockOf(ctx, repos, skus(lines))
		if err != nil {
			return err
		}
		for i, item := range items {
			if err := item.ReturnUnits(cmd.Reference, lines[i].quantity, now); err != nil {
				return err
			}
			if err := repos.Stock().Save(ctx, item); err != nil {
				return err
			}
		}
		return nil
	})
}

func skus(lines []parsedLine) []domain.SKU {
	out := make([]domain.SKU, 0, len(lines))
	for _, line := range lines {
		out = append(out, line.sku)
	}
	return out
}
