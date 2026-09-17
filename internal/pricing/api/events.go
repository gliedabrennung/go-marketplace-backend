package api

import "time"

const (
	EventPriceChanged      = "pricing.price_changed.v1"
	EventPromotionChanged  = "pricing.promotion_changed.v1"
	EventPromoCodeChanged  = "pricing.promo_code_changed.v1"
	EventPromoCodeRedeemed = "pricing.promo_code_redeemed.v1"
	EventPromoCodeReleased = "pricing.promo_code_released.v1"
)

type PriceChangedV1 struct {
	SKU        string    `json:"sku"`
	SellerID   string    `json:"seller_id"`
	ProductID  string    `json:"product_id"`
	Amount     int64     `json:"amount"`
	Currency   string    `json:"currency"`
	Active     bool      `json:"active"`
	OccurredAt time.Time `json:"occurred_at"`
}

type PromotionChangedV1 struct {
	PromotionID string    `json:"promotion_id"`
	Status      string    `json:"status"`
	OccurredAt  time.Time `json:"occurred_at"`
}

type PromoCodeChangedV1 struct {
	Code       string    `json:"code"`
	Status     string    `json:"status"`
	OccurredAt time.Time `json:"occurred_at"`
}

type PromoCodeRedemptionV1 struct {
	Code       string    `json:"code"`
	OrderID    string    `json:"order_id"`
	CustomerID string    `json:"customer_id,omitempty"`
	Amount     int64     `json:"amount,omitempty"`
	Currency   string    `json:"currency,omitempty"`
	OccurredAt time.Time `json:"occurred_at"`
}
