package domain

import "github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"

var (
	ErrInvalidSKU       = kernel.Validation("INVENTORY_INVALID_SKU", "sku must be 1-64 characters: letters, digits, dot, dash, underscore or colon")
	ErrInvalidQuantity  = kernel.Validation("INVENTORY_INVALID_QUANTITY", "quantity must be positive")
	ErrInvalidReference = kernel.Validation("INVENTORY_INVALID_REFERENCE", "operation reference is required")
	ErrInvalidTTL       = kernel.Validation("INVENTORY_INVALID_TTL", "reservation must expire in the future")

	ErrStockNotFound       = kernel.NotFound("INVENTORY_STOCK_NOT_FOUND", "stock item not found")
	ErrReservationNotFound = kernel.NotFound("INVENTORY_RESERVATION_NOT_FOUND", "reservation not found")

	ErrStockExists = kernel.Conflict("INVENTORY_STOCK_EXISTS", "stock item already exists")

	ErrNotStockOwner = kernel.Forbidden("INVENTORY_NOT_STOCK_OWNER", "stock item belongs to another seller")

	ErrInsufficientStock       = kernel.BusinessRule("INVENTORY_INSUFFICIENT_STOCK", "available stock is not enough")
	ErrReservationResolved     = kernel.BusinessRule("INVENTORY_RESERVATION_RESOLVED", "reservation is already committed, released or expired")
	ErrReservationExpired      = kernel.BusinessRule("INVENTORY_RESERVATION_EXPIRED", "reservation has expired")
	ErrReservationNotCommitted = kernel.BusinessRule("INVENTORY_RESERVATION_NOT_COMMITTED", "only committed reservation can be restored")
	ErrStockOverflow           = kernel.BusinessRule("INVENTORY_STOCK_OVERFLOW", "stock exceeds the allowed maximum")
)
