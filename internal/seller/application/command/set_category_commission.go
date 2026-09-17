package command

import (
	"context"
	"errors"
	"strconv"

	identity "github.com/gliedabrennung/go-marketplace-backend/internal/identity/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

type SetCategoryCommission struct {
	Actor       auth.Principal
	CategoryID  string
	BasisPoints int
}

type SetCategoryCommissionHandler struct {
	unit Unit
}

func NewSetCategoryCommissionHandler(unit Unit) *SetCategoryCommissionHandler {
	return &SetCategoryCommissionHandler{unit: unit}
}

func (h *SetCategoryCommissionHandler) Handle(ctx context.Context, cmd SetCategoryCommission) (struct{}, error) {
	if err := identity.Authorize(cmd.Actor, identity.PermCommissionsManage); err != nil {
		return struct{}{}, err
	}
	actor, err := actorID(cmd.Actor)
	if err != nil {
		return struct{}{}, err
	}
	category, err := domain.ParseCategoryID(cmd.CategoryID)
	if err != nil {
		return struct{}{}, err
	}
	rate, err := kernel.NewBasisPoints(cmd.BasisPoints)
	if err != nil {
		return struct{}{}, err
	}
	now := h.unit.clock.Now()

	return struct{}{}, h.unit.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		commission, err := repos.CategoryCommissions().FindByCategory(ctx, category)
		switch {
		case errors.Is(err, domain.ErrCategoryCommissionNotFound):
			if commission, err = domain.SetCategoryCommission(category, rate, actor, now); err != nil {
				return err
			}
		case err != nil:
			return err
		default:
			commission.Change(rate, actor, now)
		}
		if err := repos.CategoryCommissions().Save(ctx, commission); err != nil {
			return err
		}
		details := map[string]string{"basis_points": strconv.Itoa(rate.Value())}
		return repos.Audit().Record(ctx, auditEntry(cmd.Actor, "seller.category_commission.set", "category", category.String(), details, now))
	})
}
