package command

import (
	"context"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

type UpdateLegalDetails struct {
	Actor        auth.Principal
	SellerID     string
	LegalForm    string
	LegalName    string
	TaxID        string
	LegalAddress string
}

type UpdateLegalDetailsHandler struct {
	unit Unit
}

func NewUpdateLegalDetailsHandler(unit Unit) *UpdateLegalDetailsHandler {
	return &UpdateLegalDetailsHandler{unit: unit}
}

func (h *UpdateLegalDetailsHandler) Handle(ctx context.Context, cmd UpdateLegalDetails) (struct{}, error) {
	legal, err := domain.NewLegalDetails(cmd.LegalForm, cmd.LegalName, cmd.TaxID, cmd.LegalAddress)
	if err != nil {
		return struct{}{}, err
	}
	return struct{}{}, h.unit.memberAction(ctx, cmd.Actor, cmd.SellerID, func(s *domain.Seller, actor kernel.UserID, now time.Time) error {
		return s.UpdateLegalDetails(actor, legal, now)
	})
}
