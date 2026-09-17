package command

import (
	"context"
	"time"

	identity "github.com/gliedabrennung/go-marketplace-backend/internal/identity/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

type TerminateSeller struct {
	Actor    auth.Principal
	SellerID string
	Reason   string
}

type TerminateSellerHandler struct {
	unit Unit
}

func NewTerminateSellerHandler(unit Unit) *TerminateSellerHandler {
	return &TerminateSellerHandler{unit: unit}
}

func (h *TerminateSellerHandler) Handle(ctx context.Context, cmd TerminateSeller) (struct{}, error) {
	action := platformAction{
		permission: identity.PermSellersManage,
		action:     "seller.terminate",
		details:    map[string]string{"reason": cmd.Reason},
	}
	return struct{}{}, h.unit.platformAction(ctx, cmd.Actor, cmd.SellerID, action, func(s *domain.Seller, actor kernel.UserID, now time.Time) error {
		return s.Terminate(actor, cmd.Reason, now)
	})
}
