package command

import (
	"context"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

type ChangeBankAccount struct {
	Actor       auth.Principal
	SellerID    string
	IBAN        string
	BIC         string
	BankName    string
	Beneficiary string
}

type ChangeBankAccountHandler struct {
	unit Unit
}

func NewChangeBankAccountHandler(unit Unit) *ChangeBankAccountHandler {
	return &ChangeBankAccountHandler{unit: unit}
}

func (h *ChangeBankAccountHandler) Handle(ctx context.Context, cmd ChangeBankAccount) (struct{}, error) {
	account, err := domain.NewBankAccount(cmd.IBAN, cmd.BIC, cmd.BankName, cmd.Beneficiary)
	if err != nil {
		return struct{}{}, err
	}
	return struct{}{}, h.unit.memberAction(ctx, cmd.Actor, cmd.SellerID, func(s *domain.Seller, actor kernel.UserID, now time.Time) error {
		return s.ChangeBankAccount(actor, account, now)
	})
}
