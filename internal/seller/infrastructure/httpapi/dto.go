package httpapi

import (
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

var ErrBasisPointsRequired = kernel.Validation("SELLER_BASIS_POINTS_REQUIRED", "basis_points is required")

type legalDetailsRequest struct {
	LegalForm    string `json:"legal_form"`
	LegalName    string `json:"legal_name"`
	TaxID        string `json:"tax_id"`
	LegalAddress string `json:"legal_address"`
}

type sellerCreatedResponse struct {
	SellerID string `json:"seller_id"`
}

type bankAccountRequest struct {
	IBAN        string `json:"iban"`
	BIC         string `json:"bic"`
	BankName    string `json:"bank_name"`
	Beneficiary string `json:"beneficiary"`
}

type documentRequest struct {
	Kind      string `json:"kind"`
	ObjectKey string `json:"object_key"`
}

type memberRequest struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"`
}

type reasonRequest struct {
	Reason string `json:"reason"`
}

type rateRequest struct {
	BasisPoints *int `json:"basis_points"`
}

type documentResponse struct {
	Kind       string    `json:"kind"`
	ObjectKey  string    `json:"object_key"`
	UploadedAt time.Time `json:"uploaded_at"`
}

type memberResponse struct {
	UserID  string    `json:"user_id"`
	Role    string    `json:"role"`
	AddedAt time.Time `json:"added_at"`
}

type ratingResponse struct {
	Score        int       `json:"score"`
	Provisional  bool      `json:"provisional"`
	Orders       int       `json:"orders"`
	CalculatedAt time.Time `json:"calculated_at"`
}

type bankAccountResponse struct {
	MaskedIBAN string `json:"masked_iban"`
	BIC        string `json:"bic"`
	BankName   string `json:"bank_name"`
	Verified   bool   `json:"verified"`
}

type sellerResponse struct {
	ID                  string               `json:"id"`
	OwnerID             string               `json:"owner_id"`
	Status              string               `json:"status"`
	LegalForm           string               `json:"legal_form"`
	LegalName           string               `json:"legal_name"`
	TaxID               string               `json:"tax_id"`
	LegalAddress        string               `json:"legal_address"`
	BankAccount         *bankAccountResponse `json:"bank_account,omitempty"`
	Documents           []documentResponse   `json:"documents"`
	Members             []memberResponse     `json:"members"`
	CommissionOverrides map[string]int       `json:"commission_overrides"`
	Rating              *ratingResponse      `json:"rating,omitempty"`
	RejectionReason     string               `json:"rejection_reason,omitempty"`
	SuspensionReason    string               `json:"suspension_reason,omitempty"`
	CreatedAt           time.Time            `json:"created_at"`
	UpdatedAt           time.Time            `json:"updated_at"`
}

func toSeller(v query.SellerView) sellerResponse {
	out := sellerResponse{
		ID:                  v.ID,
		OwnerID:             v.OwnerID,
		Status:              v.Status,
		LegalForm:           v.LegalForm,
		LegalName:           v.LegalName,
		TaxID:               v.TaxID,
		LegalAddress:        v.LegalAddress,
		Documents:           make([]documentResponse, 0, len(v.Documents)),
		Members:             make([]memberResponse, 0, len(v.Members)),
		CommissionOverrides: v.CommissionOverrides,
		RejectionReason:     v.RejectionReason,
		SuspensionReason:    v.SuspensionReason,
		CreatedAt:           v.CreatedAt.UTC(),
		UpdatedAt:           v.UpdatedAt.UTC(),
	}
	if out.CommissionOverrides == nil {
		out.CommissionOverrides = map[string]int{}
	}
	if v.BankIBANMasked != "" {
		out.BankAccount = &bankAccountResponse{MaskedIBAN: v.BankIBANMasked, BIC: v.BankBIC, BankName: v.BankName, Verified: v.BankVerified}
	}
	for _, d := range v.Documents {
		out.Documents = append(out.Documents, documentResponse{Kind: d.Kind, ObjectKey: d.ObjectKey, UploadedAt: d.UploadedAt.UTC()})
	}
	for _, m := range v.Members {
		out.Members = append(out.Members, memberResponse{UserID: m.UserID, Role: m.Role, AddedAt: m.AddedAt.UTC()})
	}
	if r := v.Rating; r != nil {
		out.Rating = &ratingResponse{Score: r.Score, Provisional: r.Provisional, Orders: r.Orders, CalculatedAt: r.CalculatedAt.UTC()}
	}
	return out
}

type sellerSummaryResponse struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"`
	LegalName string    `json:"legal_name"`
	TaxID     string    `json:"tax_id"`
	Role      string    `json:"role,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func toSummary(s query.SellerSummary) sellerSummaryResponse {
	return sellerSummaryResponse{
		ID:        s.ID,
		Status:    s.Status,
		LegalName: s.LegalName,
		TaxID:     s.TaxID,
		Role:      s.Role,
		CreatedAt: s.CreatedAt.UTC(),
		UpdatedAt: s.UpdatedAt.UTC(),
	}
}
