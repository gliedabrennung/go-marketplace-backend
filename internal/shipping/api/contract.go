package api

import (
	"context"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

const MethodStandard = "standard"

var ErrUnknownMethod = kernel.Validation("SHIPPING_UNKNOWN_METHOD", "delivery method is not supported")

type Parcel struct {
	SellerID string
	Subtotal int64
}

type TariffRequest struct {
	Method   string
	Currency string
	Parcels  []Parcel
}

type ParcelCost struct {
	SellerID string
	Cost     int64
}

type TariffQuote struct {
	Method   string
	Currency string
	Parcels  []ParcelCost
	Total    int64
}

type Tariffs interface {
	Quote(ctx context.Context, request TariffRequest) (TariffQuote, error)
}
