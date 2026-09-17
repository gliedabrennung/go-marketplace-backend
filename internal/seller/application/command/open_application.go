package command

import (
	"context"

	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

type OpenApplication struct {
	Actor        auth.Principal
	LegalForm    string
	LegalName    string
	TaxID        string
	LegalAddress string
}

type OpenApplicationResult struct {
	SellerID string
}

type OpenApplicationHandler struct {
	unit Unit
}

func NewOpenApplicationHandler(unit Unit) *OpenApplicationHandler {
	return &OpenApplicationHandler{unit: unit}
}

func (h *OpenApplicationHandler) Handle(ctx context.Context, cmd OpenApplication) (OpenApplicationResult, error) {
	owner, err := actorID(cmd.Actor)
	if err != nil {
		return OpenApplicationResult{}, err
	}
	legal, err := domain.NewLegalDetails(cmd.LegalForm, cmd.LegalName, cmd.TaxID, cmd.LegalAddress)
	if err != nil {
		return OpenApplicationResult{}, err
	}
	seller, err := domain.OpenApplication(kernel.NewSellerID(), owner, legal, h.unit.clock.Now())
	if err != nil {
		return OpenApplicationResult{}, err
	}
	err = h.unit.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		return repos.Sellers().Save(ctx, seller)
	})
	if err != nil {
		return OpenApplicationResult{}, err
	}
	return OpenApplicationResult{SellerID: seller.ID().String()}, nil
}
