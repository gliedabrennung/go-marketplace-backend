package command

import (
	"context"
	"time"

	identity "github.com/gliedabrennung/go-marketplace-backend/internal/identity/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

type ClearCommissionOverride struct {
	Actor      auth.Principal
	SellerID   string
	CategoryID string
}

type ClearCommissionOverrideHandler struct {
	unit Unit
}

func NewClearCommissionOverrideHandler(unit Unit) *ClearCommissionOverrideHandler {
	return &ClearCommissionOverrideHandler{unit: unit}
}

func (h *ClearCommissionOverrideHandler) Handle(ctx context.Context, cmd ClearCommissionOverride) (struct{}, error) {
	category, err := domain.ParseCategoryID(cmd.CategoryID)
	if err != nil {
		return struct{}{}, err
	}
	action := platformAction{
		permission: identity.PermCommissionsManage,
		action:     "seller.commission_override.clear",
		details:    map[string]string{"category_id": category.String()},
	}
	return struct{}{}, h.unit.platformAction(ctx, cmd.Actor, cmd.SellerID, action, func(s *domain.Seller, actor kernel.UserID, now time.Time) error {
		s.ClearCommissionOverride(category, actor, now)
		return nil
	})
}
