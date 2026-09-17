package domain

import (
	"strings"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

const (
	maxDocuments = 20
	maxMembers   = 50
)

type Seller struct {
	id                  kernel.SellerID
	ownerID             kernel.UserID
	status              SellerStatus
	legal               LegalDetails
	bank                BankAccount
	bankVerified        bool
	documents           []Document
	members             []Member
	commissionOverrides map[CategoryID]kernel.BasisPoints
	rating              Rating
	rejectionReason     string
	suspensionReason    SuspensionReason
	suspensionNote      string
	createdAt           time.Time
	updatedAt           time.Time
	version             int

	events kernel.EventBuffer
}

func OpenApplication(id kernel.SellerID, owner kernel.UserID, legal LegalDetails, now time.Time) (*Seller, error) {
	if id.IsZero() || owner.IsZero() {
		return nil, kernel.ErrInvalidID
	}
	if legal.IsZero() {
		return nil, ErrInvalidTaxID
	}
	s := &Seller{
		id:                  id,
		ownerID:             owner,
		status:              StatusDraft,
		legal:               legal,
		members:             []Member{{userID: owner, role: MemberAdmin, addedAt: now}},
		commissionOverrides: map[CategoryID]kernel.BasisPoints{},
		createdAt:           now,
		updatedAt:           now,
	}
	s.events.Record(SellerApplicationOpened{
		SellerID: id, OwnerID: owner, LegalForm: legal.Form(), LegalName: legal.Name(), TaxID: legal.TaxID().String(), At: now,
	})
	return s, nil
}

func (s *Seller) UpdateLegalDetails(actor kernel.UserID, legal LegalDetails, now time.Time) error {
	if err := s.requireAdmin(actor); err != nil {
		return err
	}
	if s.status != StatusDraft {
		return ErrApplicationLocked
	}
	if legal.IsZero() {
		return ErrInvalidTaxID
	}
	s.legal = legal
	s.touch(now)
	s.events.Record(SellerLegalDetailsUpdated{SellerID: s.id, LegalName: legal.Name(), TaxID: legal.TaxID().String(), At: now})
	return nil
}

func (s *Seller) ChangeBankAccount(actor kernel.UserID, bank BankAccount, now time.Time) error {
	if err := s.requireAdmin(actor); err != nil {
		return err
	}
	if err := s.requireNotTerminated(); err != nil {
		return err
	}
	if bank.IsZero() {
		return ErrInvalidIBAN
	}
	if s.bank.Equals(bank) {
		return nil
	}
	s.bank = bank
	s.bankVerified = false
	s.touch(now)
	s.events.Record(SellerBankAccountChanged{SellerID: s.id, MaskedIBAN: bank.IBAN().Masked(), ChangedBy: actor, At: now})
	return nil
}

func (s *Seller) VerifyBankAccount(moderator kernel.UserID, now time.Time) error {
	if err := s.requireNotTerminated(); err != nil {
		return err
	}
	if s.bank.IsZero() {
		return ErrBankAccountMissing
	}
	if s.bankVerified {
		return ErrBankAlreadyVerified
	}
	s.bankVerified = true
	s.touch(now)
	s.events.Record(SellerBankAccountVerified{SellerID: s.id, VerifiedBy: moderator, At: now})
	return nil
}

func (s *Seller) AttachDocument(actor kernel.UserID, doc Document, now time.Time) error {
	if err := s.requireAdmin(actor); err != nil {
		return err
	}
	if s.status != StatusDraft {
		return ErrApplicationLocked
	}
	if len(s.documents) >= maxDocuments {
		return ErrTooManyDocuments
	}
	for _, existing := range s.documents {
		if existing.objectKey == doc.objectKey {
			return ErrDuplicateDocument
		}
	}
	s.documents = append(s.documents, doc)
	s.touch(now)
	s.events.Record(SellerDocumentAttached{SellerID: s.id, Kind: doc.kind, At: now})
	return nil
}

func (s *Seller) SubmitForReview(actor kernel.UserID, now time.Time) error {
	if err := s.requireAdmin(actor); err != nil {
		return err
	}
	if err := s.status.CanTransitionTo(StatusPendingReview); err != nil {
		return err
	}
	if violations := s.missingForReview(); len(violations) > 0 {
		return ErrApplicationIncomplete.WithFields(violations...)
	}
	s.status = StatusPendingReview
	s.rejectionReason = ""
	s.touch(now)
	s.events.Record(SellerApplicationSubmitted{SellerID: s.id, At: now})
	return nil
}

func (s *Seller) missingForReview() []kernel.FieldViolation {
	var out []kernel.FieldViolation
	if s.bank.IsZero() {
		out = append(out, kernel.FieldViolation{Field: "bank_account", Code: "REQUIRED", Message: "bank account is required"})
	}
	required := []DocumentKind{DocumentRegistrationCertificate, DocumentBankConfirmation}
	if s.legal.Form() == LegalEntity {
		required = append(required, DocumentCharter)
	}
	for _, kind := range required {
		if !s.hasDocument(kind) {
			out = append(out, kernel.FieldViolation{Field: "documents." + string(kind), Code: "REQUIRED", Message: string(kind) + " is required"})
		}
	}
	return out
}

func (s *Seller) Approve(moderator kernel.UserID, now time.Time) error {
	if s.status != StatusPendingReview {
		return &TransitionError{From: s.status, To: StatusActive}
	}
	if err := s.transition(StatusActive, now); err != nil {
		return err
	}
	s.bankVerified = true
	s.events.Record(SellerApproved{SellerID: s.id, OwnerID: s.ownerID, ApprovedBy: moderator, At: now})
	return nil
}

func (s *Seller) Reject(moderator kernel.UserID, reason string, now time.Time) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return ErrReasonRequired
	}
	if err := s.transition(StatusDraft, now); err != nil {
		return err
	}
	s.rejectionReason = reason
	s.events.Record(SellerApplicationRejected{SellerID: s.id, Reason: reason, RejectedBy: moderator, At: now})
	return nil
}

func (s *Seller) Suspend(by kernel.UserID, reason SuspensionReason, note string, now time.Time) error {
	note = strings.TrimSpace(note)
	if reason == SuspendedManually && note == "" {
		return ErrReasonRequired
	}
	if err := s.transition(StatusSuspended, now); err != nil {
		return err
	}
	s.suspensionReason = reason
	s.suspensionNote = note
	s.events.Record(SellerSuspended{SellerID: s.id, Reason: reason, Note: note, SuspendedBy: by, At: now})
	return nil
}

func (s *Seller) Reinstate(by kernel.UserID, now time.Time) error {
	if s.status != StatusSuspended {
		return &TransitionError{From: s.status, To: StatusActive}
	}
	if err := s.transition(StatusActive, now); err != nil {
		return err
	}
	s.suspensionReason = ""
	s.suspensionNote = ""
	s.events.Record(SellerReinstated{SellerID: s.id, ReinstatedBy: by, At: now})
	return nil
}

func (s *Seller) Terminate(by kernel.UserID, reason string, now time.Time) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return ErrReasonRequired
	}
	if err := s.transition(StatusTerminated, now); err != nil {
		return err
	}
	s.events.Record(SellerTerminated{SellerID: s.id, Reason: reason, TerminatedBy: by, At: now})
	return nil
}

func (s *Seller) transition(target SellerStatus, now time.Time) error {
	if err := s.status.CanTransitionTo(target); err != nil {
		return err
	}
	s.status = target
	s.touch(now)
	return nil
}

func (s *Seller) requireAdmin(actor kernel.UserID) error {
	role, ok := s.MemberRole(actor)
	if !ok || role != MemberAdmin {
		return ErrNotSellerAdmin
	}
	return nil
}

func (s *Seller) requireNotTerminated() error {
	if s.status == StatusTerminated {
		return ErrSellerTerminated
	}
	return nil
}

func (s *Seller) touch(now time.Time) {
	s.updatedAt = now
}

func (s *Seller) hasDocument(kind DocumentKind) bool {
	for _, d := range s.documents {
		if d.kind == kind {
			return true
		}
	}
	return false
}
