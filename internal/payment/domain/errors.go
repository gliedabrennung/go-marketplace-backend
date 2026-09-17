package domain

import (
	"fmt"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

var (
	ErrInvalidAmount   = kernel.Validation("PAYMENT_INVALID_AMOUNT", "amount must be positive")
	ErrInvalidProvider = kernel.Validation("PAYMENT_INVALID_PROVIDER", "payment provider is required")
	ErrInvalidIntent   = kernel.Validation("PAYMENT_INVALID_INTENT", "provider payment id and redirect url are required")
	ErrInvalidMethod   = kernel.Validation("PAYMENT_INVALID_METHOD", "saved payment method requires provider token and label")

	ErrPaymentNotFound = kernel.NotFound("PAYMENT_NOT_FOUND", "payment not found")
	ErrRefundNotFound  = kernel.NotFound("PAYMENT_REFUND_NOT_FOUND", "refund not found")
	ErrMethodNotFound  = kernel.NotFound("PAYMENT_METHOD_NOT_FOUND", "saved payment method not found")

	ErrAmountMismatch      = kernel.BusinessRule("PAYMENT_AMOUNT_MISMATCH", "authorized amount differs from the payment amount")
	ErrCaptureExceedsHold  = kernel.BusinessRule("PAYMENT_CAPTURE_EXCEEDS_AUTHORIZATION", "capture amount exceeds the authorized amount")
	ErrRefundExceedsCharge = kernel.BusinessRule("PAYMENT_REFUND_EXCEEDS_CAPTURE", "refunds exceed the captured amount")
	ErrPaymentTerminal     = kernel.BusinessRule("PAYMENT_TERMINAL", "payment in a terminal status cannot change")
	ErrRefundResolved      = kernel.BusinessRule("PAYMENT_REFUND_RESOLVED", "refund is already completed or failed")
	ErrCurrencyMismatch    = kernel.BusinessRule("PAYMENT_CURRENCY_MISMATCH", "amount currency differs from the payment currency")
	ErrNotMethodOwner      = kernel.Forbidden("PAYMENT_NOT_METHOD_OWNER", "payment method belongs to another buyer")
)

type TransitionError struct {
	From Status
	To   Status
}

func (e *TransitionError) Error() string {
	return fmt.Sprintf("payment cannot transition from %s to %s", e.From, e.To)
}

func (e *TransitionError) Is(target error) bool {
	_, ok := target.(*TransitionError)
	return ok
}

func (e *TransitionError) Kind() kernel.ErrorKind { return kernel.KindConflict }

func (e *TransitionError) Code() string { return "PAYMENT_INVALID_TRANSITION" }

func (e *TransitionError) Message() string { return e.Error() }
