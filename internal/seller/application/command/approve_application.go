package command

import (
	"context"

	identity "github.com/gliedabrennung/go-marketplace-backend/internal/identity/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

type ApproveApplication struct {
	Actor    auth.Principal
	SellerID string
}

type ApproveApplicationHandler struct {
	unit Unit
}

func NewApproveApplicationHandler(unit Unit) *ApproveApplicationHandler {
	return &ApproveApplicationHandler{unit: unit}
}

func (h *ApproveApplicationHandler) Handle(ctx context.Context, cmd ApproveApplication) (struct{}, error) {
	action := platformAction{permission: identity.PermSellersModerate, action: "seller.application.approve"}
	return struct{}{}, h.unit.platformAction(ctx, cmd.Actor, cmd.SellerID, action, (*domain.Seller).Approve)
}
