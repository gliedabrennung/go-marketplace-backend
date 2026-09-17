package command

import (
	"context"
	"time"

	identity "github.com/gliedabrennung/go-marketplace-backend/internal/identity/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

type SuspendSeller struct {
	Actor    auth.Principal
	SellerID string
	Reason   string
}

type SuspendSellerHandler struct {
	unit Unit
}

func NewSuspendSellerHandler(unit Unit) *SuspendSellerHandler {
	return &SuspendSellerHandler{unit: unit}
}

func (h *SuspendSellerHandler) Handle(ctx context.Context, cmd SuspendSeller) (struct{}, error) {
	action := platformAction{
		permission: identity.PermSellersManage,
		action:     "seller.suspend",
		details:    map[string]string{"reason": cmd.Reason},
	}
	return struct{}{}, h.unit.platformAction(ctx, cmd.Actor, cmd.SellerID, action, func(s *domain.Seller, actor kernel.UserID, now time.Time) error {
		return s.Suspend(actor, domain.SuspendedManually, cmd.Reason, now)
	})
}
