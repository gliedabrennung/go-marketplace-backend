package api

import (
	"context"
	"errors"

	"github.com/gliedabrennung/go-marketplace-backend/internal/pricing/domain"
)

var (
	ErrPromoAlreadyUsed   = domain.ErrPromoAlreadyUsed
	ErrOfferPriceNotFound = domain.ErrOfferPriceNotFound
	ErrOfferPriceInactive = domain.ErrOfferPriceInactive
)

var promoCodeErrors = []error{
	domain.ErrPromoCodeNotFound, domain.ErrPromoCodeInactive, domain.ErrPromoCodeExpired, domain.ErrPromoCodeDepleted,
	domain.ErrPromoCodePerBuyer, domain.ErrCartBelowMinimum, domain.ErrPromoAlreadyUsed, domain.ErrInvalidPromoCode,
}

func IsPromoCodeError(err error) bool {
	for _, target := range promoCodeErrors {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}

func IsUnpricedOffer(err error) bool {
	return errors.Is(err, domain.ErrOfferPriceNotFound) || errors.Is(err, domain.ErrOfferPriceInactive)
}

type QuoteLine struct {
	SKU      string
	Quantity int
}

type QuoteRequest struct {
	Lines      []QuoteLine
	PromoCode  string
	CustomerID string
}

type Discount struct {
	RuleID    string
	Kind      string
	Amount    int64
	PromoCode string
}

type PricedLine struct {
	SKU       string
	SellerID  string
	ProductID string
	Quantity  int
	UnitPrice int64
	CompareAt int64
	Base      int64
	Final     int64
	Discounts []Discount
}

type Quote struct {
	Lines     []PricedLine
	Subtotal  int64
	Discount  int64
	Total     int64
	Currency  string
	PromoCode string
}

type Pricer interface {
	Quote(ctx context.Context, request QuoteRequest) (Quote, error)
	Redeem(ctx context.Context, code, orderID, customerID string, subtotal int64, currency string) error
	Release(ctx context.Context, code, orderID string) error
}
