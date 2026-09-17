package command

import (
	"context"
	"slices"

	"github.com/gliedabrennung/go-marketplace-backend/internal/inventory/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/inventory/domain"
	sellerapi "github.com/gliedabrennung/go-marketplace-backend/internal/seller/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

var ErrInvalidLines = kernel.Validation("INVENTORY_INVALID_LINES", "reservation must contain 1-100 lines with distinct positive quantities")

type Base struct {
	uow     application.UnitOfWork
	clock   application.Clock
	sellers application.SellerDirectory
	policy  application.Policy
}

func NewBase(uow application.UnitOfWork, clock application.Clock, sellers application.SellerDirectory, policy application.Policy) Base {
	return Base{uow: uow, clock: clock, sellers: sellers, policy: policy}
}

type Line struct {
	SKU      string
	Quantity int
}

type parsedLine struct {
	sku      domain.SKU
	quantity kernel.Quantity
}

func (b Base) parseLines(lines []Line) ([]parsedLine, error) {
	if len(lines) == 0 || len(lines) > b.policy.MaxLines {
		return nil, ErrInvalidLines
	}
	out := make([]parsedLine, 0, len(lines))
	for _, line := range lines {
		sku, err := domain.NewSKU(line.SKU)
		if err != nil {
			return nil, err
		}
		if slices.ContainsFunc(out, func(p parsedLine) bool { return p.sku == sku }) {
			return nil, ErrInvalidLines.WithDetail("duplicate sku %s", sku)
		}
		quantity, err := kernel.NewQuantity(line.Quantity)
		if err != nil {
			return nil, err
		}
		if quantity.IsZero() {
			return nil, domain.ErrInvalidQuantity
		}
		out = append(out, parsedLine{sku: sku, quantity: quantity})
	}
	slices.SortFunc(out, func(a, b parsedLine) int { return compareSKU(a.sku, b.sku) })
	return out, nil
}

func compareSKU(a, b domain.SKU) int {
	switch {
	case a.String() < b.String():
		return -1
	case a.String() > b.String():
		return 1
	default:
		return 0
	}
}

func (b Base) requireMember(ctx context.Context, p auth.Principal, sellerID kernel.SellerID) error {
	if p.UserID == "" {
		return auth.ErrUnauthenticated
	}
	_, member, err := b.sellers.MemberRole(ctx, sellerID.String(), p.UserID)
	if err != nil {
		return err
	}
	if !member {
		return sellerapi.ErrSellerNotFound
	}
	return nil
}

func (b Base) stockOf(ctx context.Context, repos application.Repositories, skus []domain.SKU) ([]*domain.StockItem, error) {
	items, err := repos.Stock().Lock(ctx, skus)
	if err != nil {
		return nil, err
	}
	if len(items) != len(skus) {
		return nil, domain.ErrStockNotFound.WithDetail("%s", missingSKU(skus, items))
	}
	return items, nil
}

func missingSKU(skus []domain.SKU, items []*domain.StockItem) string {
	for _, sku := range skus {
		if !slices.ContainsFunc(items, func(item *domain.StockItem) bool { return item.SKU() == sku }) {
			return sku.String()
		}
	}
	return ""
}

func reference(raw string) string {
	if raw != "" {
		return raw
	}
	return kernel.NewID[struct{}]().String()
}
