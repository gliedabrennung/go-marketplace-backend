package query

import (
	"maps"

	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/domain"
)

func NewSellerView(snap domain.SellerSnapshot) SellerView {
	view := SellerView{
		ID:                  snap.ID,
		OwnerID:             snap.OwnerID,
		Status:              snap.Status,
		LegalForm:           snap.LegalForm,
		LegalName:           snap.LegalName,
		TaxID:               snap.TaxID,
		LegalAddress:        snap.LegalAddress,
		BankBIC:             snap.BankBIC,
		BankName:            snap.BankName,
		BankVerified:        snap.BankVerified,
		CommissionOverrides: maps.Clone(snap.CommissionOverrides),
		RejectionReason:     snap.RejectionReason,
		SuspensionReason:    snap.SuspensionReason,
		CreatedAt:           snap.CreatedAt,
		UpdatedAt:           snap.UpdatedAt,
		Documents:           make([]DocumentView, 0, len(snap.Documents)),
		Members:             make([]MemberView, 0, len(snap.Members)),
	}
	if iban, err := domain.NewIBAN(snap.BankIBAN); err == nil {
		view.BankIBANMasked = iban.Masked()
	}
	for _, d := range snap.Documents {
		view.Documents = append(view.Documents, DocumentView(d))
	}
	for _, m := range snap.Members {
		view.Members = append(view.Members, MemberView(m))
	}
	if r := snap.Rating; r != nil {
		view.Rating = &RatingView{Score: r.Score, Provisional: r.Provisional, Orders: r.Orders, CalculatedAt: r.CalculatedAt}
	}
	return view
}
