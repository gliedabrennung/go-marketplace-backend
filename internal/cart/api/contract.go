package api

import (
	"context"

	"github.com/gliedabrennung/go-marketplace-backend/internal/cart/application"
)

var ErrCartEmpty = application.ErrCartEmpty

type Item struct {
	SKU      string
	SellerID string
	Quantity int
	Price    int64
}

type Checkout struct {
	CartID    string
	Currency  string
	PromoCode string
	Items     []Item
}

type Carts interface {
	Checkout(ctx context.Context, userID string) (Checkout, error)
	RemoveOrdered(ctx context.Context, userID string, skus []string) error
}
