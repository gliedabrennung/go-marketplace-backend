package application

import (
	"context"
	"time"

	cartapi "github.com/gliedabrennung/go-marketplace-backend/internal/cart/api"
	catalogapi "github.com/gliedabrennung/go-marketplace-backend/internal/catalog/api"
	inventoryapi "github.com/gliedabrennung/go-marketplace-backend/internal/inventory/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/ordering/domain"
	paymentapi "github.com/gliedabrennung/go-marketplace-backend/internal/payment/api"
	pricingapi "github.com/gliedabrennung/go-marketplace-backend/internal/pricing/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	shippingapi "github.com/gliedabrennung/go-marketplace-backend/internal/shipping/api"
)

var (
	ErrItemsUnavailable   = kernel.BusinessRule("ORDER_ITEMS_UNAVAILABLE", "some cart items are no longer available")
	ErrTotalChanged       = kernel.Conflict("ORDER_TOTAL_CHANGED", "order total differs from the checkout preview")
	ErrRetryNotAllowed    = kernel.BusinessRule("ORDER_PAYMENT_RETRY_NOT_ALLOWED", "payment cannot be retried for this order")
	ErrPaymentUnavailable = kernel.BusinessRule("ORDER_PAYMENT_UNAVAILABLE", "payment could not be initiated")
)

type Clock interface {
	Now() time.Time
}

type Repositories interface {
	Orders() domain.OrderRepository
	Sagas() domain.SagaRepository
}

type UnitOfWork interface {
	Do(ctx context.Context, fn func(ctx context.Context, repos Repositories) error) error
}

type Carts interface {
	Checkout(ctx context.Context, userID string) (cartapi.Checkout, error)
	RemoveOrdered(ctx context.Context, userID string, skus []string) error
}

type Offers interface {
	Offers(ctx context.Context, offerIDs []string) (map[string]catalogapi.OfferSummary, error)
}

type Pricing interface {
	Quote(ctx context.Context, request pricingapi.QuoteRequest) (pricingapi.Quote, error)
	Redeem(ctx context.Context, code, orderID, customerID string, subtotal int64, currency string) error
	Release(ctx context.Context, code, orderID string) error
}

type Tariffs interface {
	Quote(ctx context.Context, request shippingapi.TariffRequest) (shippingapi.TariffQuote, error)
}

type Inventory = inventoryapi.Reserver

type Payments = paymentapi.Payments

type SellerMembership interface {
	MemberRole(ctx context.Context, sellerID, userID string) (string, bool, error)
}

type Metrics interface {
	OrderPlaced(status, currency string, total int64)
	SagaStep(step, outcome string)
	Compensation(step, outcome string)
}

type NopMetrics struct{}

func (NopMetrics) OrderPlaced(string, string, int64) {}

func (NopMetrics) SagaStep(string, string) {}

func (NopMetrics) Compensation(string, string) {}

type Policy struct {
	PaymentTimeout       time.Duration
	CompensationAttempts int
	StalledAfter         time.Duration
	BatchSize            int
	ReturnURL            string
	ReturnWindow         time.Duration
}

func DefaultPolicy() Policy {
	return Policy{
		PaymentTimeout: 20 * time.Minute, CompensationAttempts: 8, StalledAfter: 5 * time.Minute, BatchSize: 50,
		ReturnWindow: 14 * 24 * time.Hour,
		ReturnURL:    "http://localhost:3000/checkout/result",
	}
}
