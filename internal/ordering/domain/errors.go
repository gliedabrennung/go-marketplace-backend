package domain

import (
	"fmt"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

var (
	ErrEmptyOrder         = kernel.Validation("ORDER_EMPTY", "order must contain at least one item")
	ErrTooManyItems       = kernel.Validation("ORDER_TOO_MANY_ITEMS", "order contains too many items")
	ErrInvalidItem        = kernel.Validation("ORDER_INVALID_ITEM", "order item is invalid")
	ErrInvalidAddress     = kernel.Validation("ORDER_INVALID_ADDRESS", "shipping address is invalid")
	ErrInvalidShipping    = kernel.Validation("ORDER_INVALID_SHIPPING", "shipping costs must cover every seller exactly once")
	ErrInvalidReason      = kernel.Validation("ORDER_INVALID_REASON", "cancellation reason must be 1-500 characters long")
	ErrInvalidReference   = kernel.Validation("ORDER_INVALID_REFERENCE", "reference must not be empty")
	ErrCurrencyMismatch   = kernel.Validation("ORDER_CURRENCY_MISMATCH", "all amounts must share the order currency")
	ErrOrderNotFound      = kernel.NotFound("ORDER_NOT_FOUND", "order not found")
	ErrSagaNotFound       = kernel.NotFound("ORDER_SAGA_NOT_FOUND", "checkout saga not found")
	ErrPaidAmountMismatch = kernel.BusinessRule("ORDER_PAID_AMOUNT_MISMATCH", "paid amount differs from order total")
	ErrCancellationDenied = kernel.BusinessRule("ORDER_CANCELLATION_NOT_ALLOWED", "order can no longer be cancelled")
	ErrSagaNotRunning     = kernel.Conflict("ORDER_SAGA_NOT_RUNNING", "checkout saga is not running")
	ErrSagaNotManual      = kernel.Conflict("ORDER_SAGA_NOT_MANUAL", "checkout saga does not require manual intervention")
)

type TransitionError struct {
	From Status
	To   Status
}

func (e *TransitionError) Error() string {
	return fmt.Sprintf("order cannot transition from %s to %s", e.From, e.To)
}

func (e *TransitionError) Is(target error) bool {
	_, ok := target.(*TransitionError)
	return ok
}

func (e *TransitionError) Kind() kernel.ErrorKind { return kernel.KindConflict }

func (e *TransitionError) Code() string { return "ORDER_INVALID_TRANSITION" }

func (e *TransitionError) Message() string { return e.Error() }
