package domain

import "github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"

var (
	ErrInvalidAmount  = kernel.Validation("SETTLEMENT_INVALID_AMOUNT", "settlement amounts must be non-negative and net must not exceed gross")
	ErrEntryNotFound  = kernel.NotFound("SETTLEMENT_ENTRY_NOT_FOUND", "settlement entry not found")
	ErrAlreadyAccrued = kernel.Conflict("SETTLEMENT_ALREADY_ACCRUED", "order is already accrued for this seller")
)
