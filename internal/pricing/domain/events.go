package domain

import "time"

type PriceChanged struct {
	SKU       string
	SellerID  string
	ProductID string
	Amount    int64
	Currency  string
	Active    bool
	At        time.Time
}

func (e PriceChanged) EventName() string     { return "pricing.price_changed.v1" }
func (e PriceChanged) AggregateID() string   { return e.SKU }
func (e PriceChanged) OccurredAt() time.Time { return e.At }

type PromotionChanged struct {
	PromotionID PromotionID
	Status      PromotionStatus
	At          time.Time
}

func (e PromotionChanged) EventName() string     { return "pricing.promotion_changed.v1" }
func (e PromotionChanged) AggregateID() string   { return e.PromotionID.String() }
func (e PromotionChanged) OccurredAt() time.Time { return e.At }

type PromoCodeChanged struct {
	Code   string
	Status PromoCodeStatus
	At     time.Time
}

func (e PromoCodeChanged) EventName() string     { return "pricing.promo_code_changed.v1" }
func (e PromoCodeChanged) AggregateID() string   { return e.Code }
func (e PromoCodeChanged) OccurredAt() time.Time { return e.At }

type PromoCodeRedeemed struct {
	Code       string
	OrderID    string
	CustomerID string
	Amount     int64
	Currency   string
	At         time.Time
}

func (e PromoCodeRedeemed) EventName() string     { return "pricing.promo_code_redeemed.v1" }
func (e PromoCodeRedeemed) AggregateID() string   { return e.Code }
func (e PromoCodeRedeemed) OccurredAt() time.Time { return e.At }

type PromoCodeReleased struct {
	Code    string
	OrderID string
	At      time.Time
}

func (e PromoCodeReleased) EventName() string     { return "pricing.promo_code_released.v1" }
func (e PromoCodeReleased) AggregateID() string   { return e.Code }
func (e PromoCodeReleased) OccurredAt() time.Time { return e.At }
