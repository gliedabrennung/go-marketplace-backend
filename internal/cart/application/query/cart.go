package query

import (
	"context"
	"errors"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/cart/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/cart/domain"
)

type Reader interface {
	FindByOwner(ctx context.Context, owner domain.Owner) (*domain.Cart, error)
}

type GetCart struct {
	Owner          application.OwnerRef
	DeliveryMethod string
}

type GetCartHandler struct {
	reader    Reader
	clock     application.Clock
	policy    application.Policy
	assembler *application.Assembler
}

func NewGetCartHandler(reader Reader, clock application.Clock, policy application.Policy, assembler *application.Assembler) *GetCartHandler {
	return &GetCartHandler{reader: reader, clock: clock, policy: policy, assembler: assembler}
}

func (h *GetCartHandler) Handle(ctx context.Context, q GetCart) (application.View, error) {
	owner, err := q.Owner.Resolve()
	if err != nil {
		return application.View{}, err
	}
	cart, err := h.reader.FindByOwner(ctx, owner)
	if !errors.Is(err, domain.ErrCartNotFound) {
		if err != nil {
			return application.View{}, err
		}
		return h.assembler.Assemble(ctx, cart, q.Owner.UserID, q.DeliveryMethod)
	}
	transient, err := domain.New(domain.NewCartID(), owner, h.policy.Limits, h.clock.Now())
	if err != nil {
		return application.View{}, err
	}
	view, err := h.assembler.Assemble(ctx, transient, q.Owner.UserID, q.DeliveryMethod)
	view.CartID, view.ExpiresAt, view.UpdatedAt = "", time.Time{}, time.Time{}
	return view, err
}

type CheckoutItem struct {
	SKU      string
	SellerID string
	Quantity int
	Price    int64
}

type Checkout struct {
	CartID    string
	Currency  string
	PromoCode string
	Items     []CheckoutItem
}

type GetCheckout struct {
	UserID string
}

type GetCheckoutHandler struct {
	reader Reader
}

func NewGetCheckoutHandler(reader Reader) *GetCheckoutHandler {
	return &GetCheckoutHandler{reader: reader}
}

func (h *GetCheckoutHandler) Handle(ctx context.Context, q GetCheckout) (Checkout, error) {
	owner, err := application.OwnerRef{UserID: q.UserID}.Resolve()
	if err != nil {
		return Checkout{}, err
	}
	cart, err := h.reader.FindByOwner(ctx, owner)
	if errors.Is(err, domain.ErrCartNotFound) {
		return Checkout{}, application.ErrCartEmpty
	}
	if err != nil {
		return Checkout{}, err
	}
	if cart.IsEmpty() {
		return Checkout{}, application.ErrCartEmpty
	}
	out := Checkout{CartID: cart.ID().String(), Currency: cart.Currency(), PromoCode: cart.PromoCode()}
	for _, item := range cart.Items() {
		out.Items = append(out.Items, CheckoutItem{SKU: item.SKU(), SellerID: item.SellerID(), Quantity: item.Quantity(), Price: item.Price()})
	}
	return out, nil
}
