package command

import (
	"context"

	identity "github.com/gliedabrennung/go-marketplace-backend/internal/identity/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

type VerifyBankAccount struct {
	Actor    auth.Principal
	SellerID string
}

type VerifyBankAccountHandler struct {
	unit Unit
}

func NewVerifyBankAccountHandler(unit Unit) *VerifyBankAccountHandler {
	return &VerifyBankAccountHandler{unit: unit}
}

func (h *VerifyBankAccountHandler) Handle(ctx context.Context, cmd VerifyBankAccount) (struct{}, error) {
	action := platformAction{permission: identity.PermSellersModerate, action: "seller.bank_account.verify"}
	return struct{}{}, h.unit.platformAction(ctx, cmd.Actor, cmd.SellerID, action, (*domain.Seller).VerifyBankAccount)
}
