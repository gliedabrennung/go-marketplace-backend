package application

import (
	"context"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/cart/domain"
	catalogapi "github.com/gliedabrennung/go-marketplace-backend/internal/catalog/api"
	pricingapi "github.com/gliedabrennung/go-marketplace-backend/internal/pricing/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	shippingapi "github.com/gliedabrennung/go-marketplace-backend/internal/shipping/api"
)

var (
	ErrOwnerRequired = kernel.Unauthenticated("CART_OWNER_REQUIRED", "sign in or provide the X-Device-ID header")
	ErrCartEmpty     = kernel.BusinessRule("CART_EMPTY", "cart is empty")
)

type Clock interface {
	Now() time.Time
}

type Repositories interface {
	Carts() domain.Repository
}

type UnitOfWork interface {
	Do(ctx context.Context, fn func(ctx context.Context, repos Repositories) error) error
}

type Offers interface {
	Offers(ctx context.Context, offerIDs []string) (map[string]catalogapi.OfferSummary, error)
}

type Stock interface {
	Available(ctx context.Context, skus []string) (map[string]int, error)
}

type Pricing interface {
	Quote(ctx context.Context, request pricingapi.QuoteRequest) (pricingapi.Quote, error)
}

type Tariffs interface {
	Quote(ctx context.Context, request shippingapi.TariffRequest) (shippingapi.TariffQuote, error)
}

type Policy struct {
	Limits     domain.Limits
	PurgeBatch int
}

func DefaultPolicy() Policy {
	return Policy{Limits: domain.DefaultLimits(), PurgeBatch: 500}
}

type OwnerRef struct {
	UserID   string
	DeviceID string
}

func (r OwnerRef) Resolve() (domain.Owner, error) {
	if r.UserID != "" {
		user, err := kernel.ParseUserID(r.UserID)
		if err != nil {
			return domain.Owner{}, ErrOwnerRequired
		}
		return domain.UserOwner(user)
	}
	if r.DeviceID == "" {
		return domain.Owner{}, ErrOwnerRequired
	}
	return domain.DeviceOwner(r.DeviceID)
}

func Purchasable(offer catalogapi.OfferSummary, found bool) bool {
	return found && offer.Status == "active" && offer.ProductPublished
}
