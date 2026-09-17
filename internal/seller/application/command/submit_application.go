package command

import (
	"context"

	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

type SubmitApplication struct {
	Actor    auth.Principal
	SellerID string
}

type SubmitApplicationHandler struct {
	unit Unit
}

func NewSubmitApplicationHandler(unit Unit) *SubmitApplicationHandler {
	return &SubmitApplicationHandler{unit: unit}
}

func (h *SubmitApplicationHandler) Handle(ctx context.Context, cmd SubmitApplication) (struct{}, error) {
	return struct{}{}, h.unit.memberAction(ctx, cmd.Actor, cmd.SellerID, (*domain.Seller).SubmitForReview)
}
