package domain

import "github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"

var (
	ErrInvalidSKU       = kernel.Validation("PRICING_INVALID_SKU", "sku must be 1-64 characters: letters, digits, dot, dash, underscore or colon")
	ErrInvalidPromoCode = kernel.Validation("PRICING_INVALID_PROMO_CODE", "promo code must be 4-32 characters: capital letters, digits or dash")
	ErrInvalidName      = kernel.Validation("PRICING_INVALID_NAME", "name must be 1-200 characters long")
	ErrInvalidDiscount  = kernel.Validation("PRICING_INVALID_DISCOUNT", "discount parameters are invalid")
	ErrInvalidPeriod    = kernel.Validation("PRICING_INVALID_PERIOD", "period end must be after its start")
	ErrInvalidTarget    = kernel.Validation("PRICING_INVALID_TARGET", "promotion must target at least one sku, seller or category")
	ErrInvalidPrice     = kernel.Validation("PRICING_INVALID_PRICE", "price must be a positive amount in minor units")
	ErrInvalidLimits    = kernel.Validation("PRICING_INVALID_LIMITS", "promo code limits must not be negative")
	ErrInvalidQuantity  = kernel.Validation("PRICING_INVALID_QUANTITY", "quantity must be positive")
	ErrCurrencyMismatch = kernel.Validation("PRICING_CURRENCY_MISMATCH", "all prices in a quote must share one currency")
	ErrEmptyQuote       = kernel.Validation("PRICING_EMPTY_QUOTE", "quote must contain at least one line")

	ErrPromotionNotFound  = kernel.NotFound("PRICING_PROMOTION_NOT_FOUND", "promotion not found")
	ErrPromoCodeNotFound  = kernel.NotFound("PRICING_PROMO_CODE_NOT_FOUND", "promo code not found")
	ErrOfferPriceNotFound = kernel.NotFound("PRICING_OFFER_PRICE_NOT_FOUND", "offer price not found")

	ErrPromoCodeExists = kernel.Conflict("PRICING_PROMO_CODE_EXISTS", "promo code already exists")

	ErrPromotionEnded     = kernel.BusinessRule("PRICING_PROMOTION_ENDED", "ended promotion cannot be changed")
	ErrPromoCodeInactive  = kernel.BusinessRule("PRICING_PROMO_CODE_INACTIVE", "promo code is not active")
	ErrPromoCodeExpired   = kernel.BusinessRule("PRICING_PROMO_CODE_EXPIRED", "promo code is outside its validity period")
	ErrPromoCodeDepleted  = kernel.BusinessRule("PRICING_PROMO_CODE_DEPLETED", "promo code usage limit is reached")
	ErrPromoCodePerBuyer  = kernel.BusinessRule("PRICING_PROMO_CODE_PER_BUYER", "promo code usage limit for this buyer is reached")
	ErrCartBelowMinimum   = kernel.BusinessRule("PRICING_CART_BELOW_MINIMUM", "cart total is below the promo code minimum")
	ErrPromoAlreadyUsed   = kernel.BusinessRule("PRICING_PROMO_ALREADY_USED", "promo code is already applied to this order")
	ErrOfferPriceInactive = kernel.BusinessRule("PRICING_OFFER_PRICE_INACTIVE", "offer is not available for sale")
)
