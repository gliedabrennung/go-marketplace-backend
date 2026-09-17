package command

import (
	"context"
	"strconv"
	"time"

	identity "github.com/gliedabrennung/go-marketplace-backend/internal/identity/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

type SetCommissionOverride struct {
	Actor       auth.Principal
	SellerID    string
	CategoryID  string
	BasisPoints int
}

type SetCommissionOverrideHandler struct {
	unit Unit
}

func NewSetCommissionOverrideHandler(unit Unit) *SetCommissionOverrideHandler {
	return &SetCommissionOverrideHandler{unit: unit}
}

func (h *SetCommissionOverrideHandler) Handle(ctx context.Context, cmd SetCommissionOverride) (struct{}, error) {
	category, err := domain.ParseCategoryID(cmd.CategoryID)
	if err != nil {
		return struct{}{}, err
	}
	rate, err := kernel.NewBasisPoints(cmd.BasisPoints)
	if err != nil {
		return struct{}{}, err
	}
	action := platformAction{
		permission: identity.PermCommissionsManage,
		action:     "seller.commission_override.set",
		details:    map[string]string{"category_id": category.String(), "basis_points": strconv.Itoa(rate.Value())},
	}
	return struct{}{}, h.unit.platformAction(ctx, cmd.Actor, cmd.SellerID, action, func(s *domain.Seller, actor kernel.UserID, now time.Time) error {
		return s.SetCommissionOverride(category, rate, actor, now)
	})
}
