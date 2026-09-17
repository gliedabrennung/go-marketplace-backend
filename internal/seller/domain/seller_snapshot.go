package domain

import (
	"fmt"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type DocumentSnapshot struct {
	Kind       string
	ObjectKey  string
	UploadedAt time.Time
}

type MemberSnapshot struct {
	UserID  string
	Role    string
	AddedAt time.Time
}

type RatingSnapshot struct {
	Score            int
	CancellationRate int
	LateShipmentRate int
	AverageReview    int
	Orders           int
	Provisional      bool
	CalculatedAt     time.Time
}

type SellerSnapshot struct {
	ID                  string
	OwnerID             string
	Status              string
	LegalForm           string
	LegalName           string
	TaxID               string
	LegalAddress        string
	BankIBAN            string
	BankBIC             string
	BankName            string
	BankBeneficiary     string
	BankVerified        bool
	Documents           []DocumentSnapshot
	Members             []MemberSnapshot
	CommissionOverrides map[string]int
	Rating              *RatingSnapshot
	RejectionReason     string
	SuspensionReason    string
	SuspensionNote      string
	CreatedAt           time.Time
	UpdatedAt           time.Time
	Version             int
}

func (s *Seller) Snapshot() SellerSnapshot {
	snap := SellerSnapshot{
		ID:                  s.id.String(),
		OwnerID:             s.ownerID.String(),
		Status:              string(s.status),
		LegalForm:           string(s.legal.form),
		LegalName:           s.legal.name,
		TaxID:               s.legal.taxID.String(),
		LegalAddress:        s.legal.address,
		BankIBAN:            s.bank.iban.String(),
		BankBIC:             s.bank.bic.String(),
		BankName:            s.bank.bankName,
		BankBeneficiary:     s.bank.beneficiary,
		BankVerified:        s.bankVerified,
		Documents:           make([]DocumentSnapshot, 0, len(s.documents)),
		Members:             make([]MemberSnapshot, 0, len(s.members)),
		CommissionOverrides: make(map[string]int, len(s.commissionOverrides)),
		RejectionReason:     s.rejectionReason,
		SuspensionReason:    string(s.suspensionReason),
		SuspensionNote:      s.suspensionNote,
		CreatedAt:           s.createdAt,
		UpdatedAt:           s.updatedAt,
		Version:             s.version,
	}
	for _, d := range s.documents {
		snap.Documents = append(snap.Documents, DocumentSnapshot{Kind: string(d.kind), ObjectKey: d.objectKey, UploadedAt: d.uploadedAt})
	}
	for _, m := range s.members {
		snap.Members = append(snap.Members, MemberSnapshot{UserID: m.userID.String(), Role: string(m.role), AddedAt: m.addedAt})
	}
	for category, rate := range s.commissionOverrides {
		snap.CommissionOverrides[category.String()] = rate.Value()
	}
	if !s.rating.IsZero() {
		snap.Rating = &RatingSnapshot{
			Score:            s.rating.score,
			CancellationRate: s.rating.cancellationRate.Value(),
			LateShipmentRate: s.rating.lateShipmentRate.Value(),
			AverageReview:    s.rating.averageReview,
			Orders:           s.rating.orders,
			Provisional:      s.rating.provisional,
			CalculatedAt:     s.rating.calculatedAt,
		}
	}
	return snap
}

func RehydrateSeller(snap SellerSnapshot) (*Seller, error) {
	s, err := rehydrateSellerCore(snap)
	if err != nil {
		return nil, fmt.Errorf("rehydrate seller %s: %w", snap.ID, err)
	}
	if err := s.rehydrateCollections(snap); err != nil {
		return nil, fmt.Errorf("rehydrate seller %s: %w", snap.ID, err)
	}
	return s, nil
}

func rehydrateSellerCore(snap SellerSnapshot) (*Seller, error) {
	id, err := kernel.ParseSellerID(snap.ID)
	if err != nil {
		return nil, err
	}
	owner, err := kernel.ParseUserID(snap.OwnerID)
	if err != nil {
		return nil, err
	}
	legal, err := NewLegalDetails(snap.LegalForm, snap.LegalName, snap.TaxID, snap.LegalAddress)
	if err != nil {
		return nil, err
	}
	s := &Seller{
		id:                  id,
		ownerID:             owner,
		status:              SellerStatus(snap.Status),
		legal:               legal,
		bankVerified:        snap.BankVerified,
		commissionOverrides: make(map[CategoryID]kernel.BasisPoints, len(snap.CommissionOverrides)),
		rejectionReason:     snap.RejectionReason,
		suspensionReason:    SuspensionReason(snap.SuspensionReason),
		suspensionNote:      snap.SuspensionNote,
		createdAt:           snap.CreatedAt,
		updatedAt:           snap.UpdatedAt,
		version:             snap.Version,
	}
	if snap.BankIBAN != "" {
		if s.bank, err = NewBankAccount(snap.BankIBAN, snap.BankBIC, snap.BankName, snap.BankBeneficiary); err != nil {
			return nil, err
		}
	}
	if r := snap.Rating; r != nil {
		s.rating = Rating{
			score:            r.Score,
			cancellationRate: kernel.MustBasisPoints(clampBasis(r.CancellationRate)),
			lateShipmentRate: kernel.MustBasisPoints(clampBasis(r.LateShipmentRate)),
			averageReview:    r.AverageReview,
			orders:           r.Orders,
			provisional:      r.Provisional,
			calculatedAt:     r.CalculatedAt,
		}
	}
	return s, nil
}

func (s *Seller) rehydrateCollections(snap SellerSnapshot) error {
	for _, d := range snap.Documents {
		doc, err := NewDocument(d.Kind, d.ObjectKey, d.UploadedAt)
		if err != nil {
			return err
		}
		s.documents = append(s.documents, doc)
	}
	for _, m := range snap.Members {
		userID, err := kernel.ParseUserID(m.UserID)
		if err != nil {
			return err
		}
		role, err := ParseMemberRole(m.Role)
		if err != nil {
			return err
		}
		s.members = append(s.members, Member{userID: userID, role: role, addedAt: m.AddedAt})
	}
	for rawCategory, bp := range snap.CommissionOverrides {
		category, err := ParseCategoryID(rawCategory)
		if err != nil {
			return err
		}
		rate, err := kernel.NewBasisPoints(bp)
		if err != nil {
			return err
		}
		s.commissionOverrides[category] = rate
	}
	return nil
}

func clampBasis(v int) int {
	return min(max(v, 0), kernel.MaxBasisPoints)
}
