package domain

import (
	"fmt"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

var (
	ErrInvalidLegalForm    = kernel.Validation("SELLER_INVALID_LEGAL_FORM", "legal form is unknown")
	ErrInvalidLegalName    = kernel.Validation("SELLER_INVALID_LEGAL_NAME", "legal name must be 2-300 characters long")
	ErrInvalidTaxID        = kernel.Validation("SELLER_INVALID_TAX_ID", "tax identifier must be a valid 12-digit BIN or IIN")
	ErrInvalidLegalAddress = kernel.Validation("SELLER_INVALID_LEGAL_ADDRESS", "legal address must be 5-500 characters long")
	ErrInvalidIBAN         = kernel.Validation("SELLER_INVALID_IBAN", "IBAN is invalid")
	ErrInvalidBIC          = kernel.Validation("SELLER_INVALID_BIC", "BIC is invalid")
	ErrInvalidBankName     = kernel.Validation("SELLER_INVALID_BANK_NAME", "bank name must be 2-200 characters long")
	ErrInvalidBeneficiary  = kernel.Validation("SELLER_INVALID_BENEFICIARY", "beneficiary must be 2-300 characters long")
	ErrInvalidDocumentKind = kernel.Validation("SELLER_INVALID_DOCUMENT_KIND", "document kind is unknown")
	ErrInvalidDocumentKey  = kernel.Validation("SELLER_INVALID_DOCUMENT_KEY", "document object key is invalid")
	ErrInvalidMemberRole   = kernel.Validation("SELLER_INVALID_MEMBER_ROLE", "member role is unknown")
	ErrReasonRequired      = kernel.Validation("SELLER_REASON_REQUIRED", "reason is required")
	ErrInvalidMetrics      = kernel.Validation("SELLER_INVALID_METRICS", "performance metrics are inconsistent")
	ErrInvalidRatingPolicy = kernel.Validation("SELLER_INVALID_RATING_POLICY", "rating policy is invalid")

	ErrSellerNotFound             = kernel.NotFound("SELLER_NOT_FOUND", "seller not found")
	ErrMemberNotFound             = kernel.NotFound("SELLER_MEMBER_NOT_FOUND", "seller member not found")
	ErrCategoryCommissionNotFound = kernel.NotFound("SELLER_CATEGORY_COMMISSION_NOT_FOUND", "category commission not found")

	ErrSellerAlreadyExists = kernel.Conflict("SELLER_ALREADY_EXISTS", "user already owns an active seller")
	ErrTaxIDTaken          = kernel.Conflict("SELLER_TAX_ID_TAKEN", "tax identifier is already registered by another seller")
	ErrMemberExists        = kernel.Conflict("SELLER_MEMBER_EXISTS", "user is already a member of the seller")
	ErrDuplicateDocument   = kernel.Conflict("SELLER_DUPLICATE_DOCUMENT", "document is already attached")

	ErrNotSellerAdmin = kernel.Forbidden("SELLER_ADMIN_REQUIRED", "seller admin role is required")

	ErrApplicationLocked     = kernel.BusinessRule("SELLER_APPLICATION_LOCKED", "application can be edited only in draft status")
	ErrApplicationIncomplete = kernel.BusinessRule("SELLER_APPLICATION_INCOMPLETE", "application is incomplete")
	ErrSellerTerminated      = kernel.BusinessRule("SELLER_TERMINATED", "seller is terminated")
	ErrBankAccountMissing    = kernel.BusinessRule("SELLER_BANK_ACCOUNT_MISSING", "bank account is not set")
	ErrBankAlreadyVerified   = kernel.BusinessRule("SELLER_BANK_ACCOUNT_ALREADY_VERIFIED", "bank account is already verified")
	ErrTooManyDocuments      = kernel.BusinessRule("SELLER_TOO_MANY_DOCUMENTS", "too many documents attached")
	ErrTooManyMembers        = kernel.BusinessRule("SELLER_TOO_MANY_MEMBERS", "too many seller members")
	ErrCannotRemoveOwner     = kernel.BusinessRule("SELLER_CANNOT_REMOVE_OWNER", "seller owner cannot be removed")
)

type TransitionError struct {
	From SellerStatus
	To   SellerStatus
}

func (e *TransitionError) Error() string {
	return fmt.Sprintf("seller cannot transition from %s to %s", e.From, e.To)
}

func (e *TransitionError) Is(target error) bool {
	_, ok := target.(*TransitionError)
	return ok
}

func (e *TransitionError) Kind() kernel.ErrorKind { return kernel.KindConflict }

func (e *TransitionError) Code() string { return "SELLER_INVALID_TRANSITION" }

func (e *TransitionError) Message() string { return e.Error() }
