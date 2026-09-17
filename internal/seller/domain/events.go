package domain

import (
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type SellerApplicationOpened struct {
	SellerID  kernel.SellerID
	OwnerID   kernel.UserID
	LegalForm LegalForm
	LegalName string
	TaxID     string
	At        time.Time
}

func (e SellerApplicationOpened) EventName() string     { return "seller.application_opened.v1" }
func (e SellerApplicationOpened) AggregateID() string   { return e.SellerID.String() }
func (e SellerApplicationOpened) OccurredAt() time.Time { return e.At }

type SellerLegalDetailsUpdated struct {
	SellerID  kernel.SellerID
	LegalName string
	TaxID     string
	At        time.Time
}

func (e SellerLegalDetailsUpdated) EventName() string     { return "seller.legal_details_updated.v1" }
func (e SellerLegalDetailsUpdated) AggregateID() string   { return e.SellerID.String() }
func (e SellerLegalDetailsUpdated) OccurredAt() time.Time { return e.At }

type SellerBankAccountChanged struct {
	SellerID   kernel.SellerID
	MaskedIBAN string
	ChangedBy  kernel.UserID
	At         time.Time
}

func (e SellerBankAccountChanged) EventName() string     { return "seller.bank_account_changed.v1" }
func (e SellerBankAccountChanged) AggregateID() string   { return e.SellerID.String() }
func (e SellerBankAccountChanged) OccurredAt() time.Time { return e.At }

type SellerBankAccountVerified struct {
	SellerID   kernel.SellerID
	VerifiedBy kernel.UserID
	At         time.Time
}

func (e SellerBankAccountVerified) EventName() string     { return "seller.bank_account_verified.v1" }
func (e SellerBankAccountVerified) AggregateID() string   { return e.SellerID.String() }
func (e SellerBankAccountVerified) OccurredAt() time.Time { return e.At }

type SellerDocumentAttached struct {
	SellerID kernel.SellerID
	Kind     DocumentKind
	At       time.Time
}

func (e SellerDocumentAttached) EventName() string     { return "seller.document_attached.v1" }
func (e SellerDocumentAttached) AggregateID() string   { return e.SellerID.String() }
func (e SellerDocumentAttached) OccurredAt() time.Time { return e.At }

type SellerApplicationSubmitted struct {
	SellerID kernel.SellerID
	At       time.Time
}

func (e SellerApplicationSubmitted) EventName() string     { return "seller.application_submitted.v1" }
func (e SellerApplicationSubmitted) AggregateID() string   { return e.SellerID.String() }
func (e SellerApplicationSubmitted) OccurredAt() time.Time { return e.At }

type SellerApproved struct {
	SellerID   kernel.SellerID
	OwnerID    kernel.UserID
	ApprovedBy kernel.UserID
	At         time.Time
}

func (e SellerApproved) EventName() string     { return "seller.application_approved.v1" }
func (e SellerApproved) AggregateID() string   { return e.SellerID.String() }
func (e SellerApproved) OccurredAt() time.Time { return e.At }

type SellerApplicationRejected struct {
	SellerID   kernel.SellerID
	Reason     string
	RejectedBy kernel.UserID
	At         time.Time
}

func (e SellerApplicationRejected) EventName() string     { return "seller.application_rejected.v1" }
func (e SellerApplicationRejected) AggregateID() string   { return e.SellerID.String() }
func (e SellerApplicationRejected) OccurredAt() time.Time { return e.At }

type SellerSuspended struct {
	SellerID    kernel.SellerID
	Reason      SuspensionReason
	Note        string
	SuspendedBy kernel.UserID
	At          time.Time
}

func (e SellerSuspended) EventName() string     { return "seller.suspended.v1" }
func (e SellerSuspended) AggregateID() string   { return e.SellerID.String() }
func (e SellerSuspended) OccurredAt() time.Time { return e.At }

type SellerReinstated struct {
	SellerID     kernel.SellerID
	ReinstatedBy kernel.UserID
	At           time.Time
}

func (e SellerReinstated) EventName() string     { return "seller.reinstated.v1" }
func (e SellerReinstated) AggregateID() string   { return e.SellerID.String() }
func (e SellerReinstated) OccurredAt() time.Time { return e.At }

type SellerTerminated struct {
	SellerID     kernel.SellerID
	Reason       string
	TerminatedBy kernel.UserID
	At           time.Time
}

func (e SellerTerminated) EventName() string     { return "seller.terminated.v1" }
func (e SellerTerminated) AggregateID() string   { return e.SellerID.String() }
func (e SellerTerminated) OccurredAt() time.Time { return e.At }

type SellerMemberAdded struct {
	SellerID kernel.SellerID
	UserID   kernel.UserID
	Role     MemberRole
	AddedBy  kernel.UserID
	At       time.Time
}

func (e SellerMemberAdded) EventName() string     { return "seller.member_added.v1" }
func (e SellerMemberAdded) AggregateID() string   { return e.SellerID.String() }
func (e SellerMemberAdded) OccurredAt() time.Time { return e.At }

type SellerMemberRemoved struct {
	SellerID  kernel.SellerID
	UserID    kernel.UserID
	RemovedBy kernel.UserID
	At        time.Time
}

func (e SellerMemberRemoved) EventName() string     { return "seller.member_removed.v1" }
func (e SellerMemberRemoved) AggregateID() string   { return e.SellerID.String() }
func (e SellerMemberRemoved) OccurredAt() time.Time { return e.At }

type SellerCommissionOverrideSet struct {
	SellerID   kernel.SellerID
	CategoryID CategoryID
	Rate       kernel.BasisPoints
	SetBy      kernel.UserID
	At         time.Time
}

func (e SellerCommissionOverrideSet) EventName() string     { return "seller.commission_override_set.v1" }
func (e SellerCommissionOverrideSet) AggregateID() string   { return e.SellerID.String() }
func (e SellerCommissionOverrideSet) OccurredAt() time.Time { return e.At }

type SellerCommissionOverrideCleared struct {
	SellerID   kernel.SellerID
	CategoryID CategoryID
	ClearedBy  kernel.UserID
	At         time.Time
}

func (e SellerCommissionOverrideCleared) EventName() string {
	return "seller.commission_override_cleared.v1"
}
func (e SellerCommissionOverrideCleared) AggregateID() string   { return e.SellerID.String() }
func (e SellerCommissionOverrideCleared) OccurredAt() time.Time { return e.At }

type SellerRatingChanged struct {
	SellerID    kernel.SellerID
	Score       int
	Provisional bool
	Orders      int
	At          time.Time
}

func (e SellerRatingChanged) EventName() string     { return "seller.rating_changed.v1" }
func (e SellerRatingChanged) AggregateID() string   { return e.SellerID.String() }
func (e SellerRatingChanged) OccurredAt() time.Time { return e.At }

type CategoryCommissionChanged struct {
	CategoryID CategoryID
	Rate       kernel.BasisPoints
	ChangedBy  kernel.UserID
	At         time.Time
}

func (e CategoryCommissionChanged) EventName() string     { return "seller.category_commission_changed.v1" }
func (e CategoryCommissionChanged) AggregateID() string   { return e.CategoryID.String() }
func (e CategoryCommissionChanged) OccurredAt() time.Time { return e.At }
