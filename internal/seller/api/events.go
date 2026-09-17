package api

import "time"

const (
	EventApplicationOpened         = "seller.application_opened.v1"
	EventLegalDetailsUpdated       = "seller.legal_details_updated.v1"
	EventBankAccountChanged        = "seller.bank_account_changed.v1"
	EventBankAccountVerified       = "seller.bank_account_verified.v1"
	EventDocumentAttached          = "seller.document_attached.v1"
	EventApplicationSubmitted      = "seller.application_submitted.v1"
	EventApplicationApproved       = "seller.application_approved.v1"
	EventApplicationRejected       = "seller.application_rejected.v1"
	EventSuspended                 = "seller.suspended.v1"
	EventReinstated                = "seller.reinstated.v1"
	EventTerminated                = "seller.terminated.v1"
	EventMemberAdded               = "seller.member_added.v1"
	EventMemberRemoved             = "seller.member_removed.v1"
	EventCommissionOverrideSet     = "seller.commission_override_set.v1"
	EventCommissionOverrideCleared = "seller.commission_override_cleared.v1"
	EventRatingChanged             = "seller.rating_changed.v1"
	EventCategoryCommissionChanged = "seller.category_commission_changed.v1"
)

type ApplicationOpenedV1 struct {
	SellerID   string    `json:"seller_id"`
	OwnerID    string    `json:"owner_id"`
	LegalForm  string    `json:"legal_form"`
	LegalName  string    `json:"legal_name"`
	TaxID      string    `json:"tax_id"`
	OccurredAt time.Time `json:"occurred_at"`
}

type LegalDetailsUpdatedV1 struct {
	SellerID   string    `json:"seller_id"`
	LegalName  string    `json:"legal_name"`
	TaxID      string    `json:"tax_id"`
	OccurredAt time.Time `json:"occurred_at"`
}

type BankAccountChangedV1 struct {
	SellerID   string    `json:"seller_id"`
	MaskedIBAN string    `json:"masked_iban"`
	ChangedBy  string    `json:"changed_by"`
	OccurredAt time.Time `json:"occurred_at"`
}

type SellerActionV1 struct {
	SellerID   string    `json:"seller_id"`
	ActorID    string    `json:"actor_id,omitempty"`
	Reason     string    `json:"reason,omitempty"`
	Note       string    `json:"note,omitempty"`
	OccurredAt time.Time `json:"occurred_at"`
}

type DocumentAttachedV1 struct {
	SellerID   string    `json:"seller_id"`
	Kind       string    `json:"kind"`
	OccurredAt time.Time `json:"occurred_at"`
}

type ApplicationApprovedV1 struct {
	SellerID   string    `json:"seller_id"`
	OwnerID    string    `json:"owner_id"`
	ApprovedBy string    `json:"approved_by"`
	OccurredAt time.Time `json:"occurred_at"`
}

type MemberChangedV1 struct {
	SellerID   string    `json:"seller_id"`
	UserID     string    `json:"user_id"`
	Role       string    `json:"role,omitempty"`
	ChangedBy  string    `json:"changed_by"`
	OccurredAt time.Time `json:"occurred_at"`
}

type CommissionOverrideV1 struct {
	SellerID    string    `json:"seller_id"`
	CategoryID  string    `json:"category_id"`
	BasisPoints *int      `json:"basis_points,omitempty"`
	ChangedBy   string    `json:"changed_by"`
	OccurredAt  time.Time `json:"occurred_at"`
}

type RatingChangedV1 struct {
	SellerID    string    `json:"seller_id"`
	Score       int       `json:"score"`
	Provisional bool      `json:"provisional"`
	Orders      int       `json:"orders"`
	OccurredAt  time.Time `json:"occurred_at"`
}

type CategoryCommissionChangedV1 struct {
	CategoryID  string    `json:"category_id"`
	BasisPoints int       `json:"basis_points"`
	ChangedBy   string    `json:"changed_by"`
	OccurredAt  time.Time `json:"occurred_at"`
}
