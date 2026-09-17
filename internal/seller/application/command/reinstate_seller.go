package command

import (
	"context"

	identity "github.com/gliedabrennung/go-marketplace-backend/internal/identity/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

type ReinstateSeller struct {
	Actor    auth.Principal
	SellerID string
}

type ReinstateSellerHandler struct {
	unit Unit
}

func NewReinstateSellerHandler(unit Unit) *ReinstateSellerHandler {
	return &ReinstateSellerHandler{unit: unit}
}

func (h *ReinstateSellerHandler) Handle(ctx context.Context, cmd ReinstateSeller) (struct{}, error) {
	action := platformAction{permission: identity.PermSellersManage, action: "seller.reinstate"}
	return struct{}{}, h.unit.platformAction(ctx, cmd.Actor, cmd.SellerID, action, (*domain.Seller).Reinstate)
}
