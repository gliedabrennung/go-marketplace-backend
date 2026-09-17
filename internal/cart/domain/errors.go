package domain

import "github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"

var (
	ErrInvalidDevice    = kernel.Validation("CART_INVALID_DEVICE_ID", "device id must be 16-128 characters: letters, digits, dash or underscore")
	ErrInvalidSKU       = kernel.Validation("CART_INVALID_SKU", "sku is invalid")
	ErrInvalidQuantity  = kernel.Validation("CART_INVALID_QUANTITY", "quantity is out of range")
	ErrInvalidPromoCode = kernel.Validation("CART_INVALID_PROMO_CODE", "promo code must be 4-32 characters: letters, digits or dash")
	ErrInvalidPrice     = kernel.Validation("CART_INVALID_PRICE", "price must be positive")

	ErrCartNotFound = kernel.NotFound("CART_NOT_FOUND", "cart not found")
	ErrItemNotFound = kernel.NotFound("CART_ITEM_NOT_FOUND", "cart item not found")

	ErrTooManyItems     = kernel.BusinessRule("CART_TOO_MANY_ITEMS", "cart cannot contain more distinct items")
	ErrExceedsStock     = kernel.BusinessRule("CART_QUANTITY_EXCEEDS_STOCK", "requested quantity exceeds available stock")
	ErrOfferUnavailable = kernel.BusinessRule("CART_OFFER_UNAVAILABLE", "offer is not available for sale")
	ErrCurrencyMismatch = kernel.BusinessRule("CART_CURRENCY_MISMATCH", "cart items must share one currency")
	ErrSameOwner        = kernel.BusinessRule("CART_MERGE_SAME_OWNER", "cart cannot be merged into itself")
)
