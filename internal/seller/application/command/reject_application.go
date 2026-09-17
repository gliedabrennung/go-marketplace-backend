package command

import (
	"context"
	"time"

	identity "github.com/gliedabrennung/go-marketplace-backend/internal/identity/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

type RejectApplication struct {
	Actor    auth.Principal
	SellerID string
	Reason   string
}

type RejectApplicationHandler struct {
	unit Unit
}

func NewRejectApplicationHandler(unit Unit) *RejectApplicationHandler {
	return &RejectApplicationHandler{unit: unit}
}

func (h *RejectApplicationHandler) Handle(ctx context.Context, cmd RejectApplication) (struct{}, error) {
	action := platformAction{
		permission: identity.PermSellersModerate,
		action:     "seller.application.reject",
		details:    map[string]string{"reason": cmd.Reason},
	}
	return struct{}{}, h.unit.platformAction(ctx, cmd.Actor, cmd.SellerID, action, func(s *domain.Seller, actor kernel.UserID, now time.Time) error {
		return s.Reject(actor, cmd.Reason, now)
	})
}
