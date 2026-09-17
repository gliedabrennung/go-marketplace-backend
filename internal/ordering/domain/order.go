package domain

import (
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

const MaxItems = 100

type Item struct {
	SKU        string
	ProductID  string
	CategoryID string
	SellerID   kernel.SellerID
	Title      string
	Quantity   int
	UnitPrice  kernel.Money
	Base       kernel.Money
	Final      kernel.Money
}

func (i Item) Discount() kernel.Money {
	discount, _ := i.Base.Sub(i.Final)
	return discount
}

type ShippingCost struct {
	SellerID kernel.SellerID
	Cost     kernel.Money
}

type Part struct {
	SellerID kernel.SellerID
	Subtotal kernel.Money
	Discount kernel.Money
	Shipping kernel.Money
	Total    kernel.Money
}

type StatusChange struct {
	From   Status
	To     Status
	Actor  Actor
	Reason string
	At     time.Time
}

type PlaceSpec struct {
	ID             OrderID
	BuyerID        kernel.UserID
	Address        Address
	DeliveryMethod string
	PromoCode      string
	Currency       kernel.Currency
	Items          []Item
	Shipping       []ShippingCost
}

type Order struct {
	id             OrderID
	buyerID        kernel.UserID
	status         Status
	address        Address
	deliveryMethod string
	promoCode      string
	items          []Item
	parts          []Part
	subtotal       kernel.Money
	discount       kernel.Money
	shipping       kernel.Money
	total          kernel.Money
	paymentID      string
	history        []StatusChange
	createdAt      time.Time
	updatedAt      time.Time
	deliveredAt    time.Time
	version        int

	events kernel.EventBuffer
}

func Place(spec PlaceSpec, now time.Time) (*Order, error) {
	if spec.ID.IsZero() || spec.BuyerID.IsZero() {
		return nil, kernel.ErrInvalidID
	}
	if len(spec.Items) == 0 {
		return nil, ErrEmptyOrder
	}
	if len(spec.Items) > MaxItems {
		return nil, ErrTooManyItems.WithDetail("limit %d", MaxItems)
	}
	address, err := NewAddress(spec.Address)
	if err != nil {
		return nil, err
	}
	if err := validateItems(spec.Items, spec.Currency); err != nil {
		return nil, err
	}
	parts, err := buildParts(spec.Items, spec.Shipping, spec.Currency)
	if err != nil {
		return nil, err
	}
	o := &Order{
		id: spec.ID, buyerID: spec.BuyerID, status: StatusCreated, address: address,
		deliveryMethod: strings.TrimSpace(spec.DeliveryMethod), promoCode: spec.PromoCode,
		items: slices.Clone(spec.Items), parts: parts, createdAt: now, updatedAt: now,
	}
	zero := kernel.ZeroLike(spec.Items[0].Base)
	o.subtotal, o.discount, o.shipping, o.total = zero, zero, zero, zero
	for _, part := range parts {
		o.subtotal = mustAdd(o.subtotal, part.Subtotal)
		o.discount = mustAdd(o.discount, part.Discount)
		o.shipping = mustAdd(o.shipping, part.Shipping)
		o.total = mustAdd(o.total, part.Total)
	}
	o.history = append(o.history, StatusChange{To: StatusCreated, Actor: Actor{Kind: ActorBuyer, ID: spec.BuyerID.String()}, At: now})
	o.events.Record(OrderCreated{Order: o.summary(), At: now})
	return o, nil
}

func validateItems(items []Item, currency kernel.Currency) error {
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.SKU) == "" || seen[item.SKU] || item.SellerID.IsZero() || item.Quantity <= 0 ||
			utf8.RuneCountInString(item.Title) > 500 {
			return ErrInvalidItem.WithDetail("sku %q", item.SKU)
		}
		seen[item.SKU] = true
		for _, amount := range []kernel.Money{item.UnitPrice, item.Base, item.Final} {
			if amount.Currency() != currency {
				return ErrCurrencyMismatch
			}
		}
		expected, err := item.UnitPrice.MulQuantity(kernel.MustQuantity(item.Quantity))
		if err != nil || !expected.Equals(item.Base) || item.UnitPrice.Amount() <= 0 {
			return ErrInvalidItem.WithDetail("sku %q base price mismatch", item.SKU)
		}
		if compare, _ := item.Final.Compare(item.Base); compare > 0 || item.Final.Amount() < 0 {
			return ErrInvalidItem.WithDetail("sku %q discount exceeds price", item.SKU)
		}
	}
	return nil
}

func buildParts(items []Item, shipping []ShippingCost, currency kernel.Currency) ([]Part, error) {
	parts := []Part{}
	for _, item := range items {
		index := slices.IndexFunc(parts, func(p Part) bool { return p.SellerID == item.SellerID })
		if index < 0 {
			zero := kernel.ZeroLike(item.Base)
			parts = append(parts, Part{SellerID: item.SellerID, Subtotal: zero, Discount: zero, Shipping: zero, Total: zero})
			index = len(parts) - 1
		}
		part := &parts[index]
		part.Subtotal = mustAdd(part.Subtotal, item.Base)
		part.Discount = mustAdd(part.Discount, item.Discount())
		part.Total = mustAdd(part.Total, item.Final)
	}
	if len(shipping) != len(parts) {
		return nil, ErrInvalidShipping
	}
	for _, cost := range shipping {
		index := slices.IndexFunc(parts, func(p Part) bool { return p.SellerID == cost.SellerID })
		if index < 0 || cost.Cost.Amount() < 0 || !parts[index].Shipping.IsZero() {
			return nil, ErrInvalidShipping
		}
		if cost.Cost.Currency() != currency {
			return nil, ErrCurrencyMismatch
		}
		parts[index].Shipping = cost.Cost
		parts[index].Total = mustAdd(parts[index].Total, cost.Cost)
	}
	return parts, nil
}

func mustAdd(a, b kernel.Money) kernel.Money {
	sum, err := a.Add(b)
	if err != nil {
		panic(err)
	}
	return sum
}

func (o *Order) AwaitPayment(paymentID string, now time.Time) error {
	if strings.TrimSpace(paymentID) == "" {
		return ErrInvalidReference
	}
	if o.status == StatusAwaitingPayment {
		if o.paymentID == paymentID {
			return nil
		}
		o.paymentID, o.updatedAt = paymentID, now
		o.events.Record(OrderAwaitingPayment{OrderID: o.id, BuyerID: o.buyerID, PaymentID: paymentID, At: now})
		return nil
	}
	if err := o.transition(StatusAwaitingPayment, System(), "", now); err != nil {
		return err
	}
	o.paymentID = paymentID
	o.events.Record(OrderAwaitingPayment{OrderID: o.id, BuyerID: o.buyerID, PaymentID: paymentID, At: now})
	return nil
}

func (o *Order) MarkPaid(paid kernel.Money, now time.Time) error {
	if o.status == StatusPaid {
		return nil
	}
	if err := o.status.CanTransitionTo(StatusPaid); err != nil {
		return err
	}
	if !paid.Equals(o.total) {
		return ErrPaidAmountMismatch.WithDetail("expected %s, got %s", o.total, paid)
	}
	if err := o.transition(StatusPaid, System(), "", now); err != nil {
		return err
	}
	o.events.Record(OrderPaid{Order: o.summary(), PaymentID: o.paymentID, At: now})
	return nil
}

func (o *Order) Fail(reason string, now time.Time) error {
	if o.status == StatusFailed {
		return nil
	}
	if err := o.transition(StatusFailed, System(), reason, now); err != nil {
		return err
	}
	o.events.Record(OrderFailed{OrderID: o.id, BuyerID: o.buyerID, Reason: strings.TrimSpace(reason), At: now})
	return nil
}

func (o *Order) MarkShipped(actor Actor, now time.Time) error {
	if o.status == StatusShipped {
		return nil
	}
	if o.status == StatusPaid {
		if err := o.transition(StatusInFulfilment, actor, "", now); err != nil {
			return err
		}
	}
	if err := o.transition(StatusShipped, actor, "", now); err != nil {
		return err
	}
	o.events.Record(OrderShipped{OrderID: o.id, BuyerID: o.buyerID, At: now})
	return nil
}

func (o *Order) MarkDelivered(actor Actor, now time.Time) error {
	if o.status == StatusDelivered {
		return nil
	}
	if err := o.transition(StatusDelivered, actor, "", now); err != nil {
		return err
	}
	o.deliveredAt = now
	o.events.Record(OrderDelivered{OrderID: o.id, BuyerID: o.buyerID, At: now})
	return nil
}

func (o *Order) Complete(now time.Time) error {
	if o.status == StatusCompleted {
		return nil
	}
	if err := o.transition(StatusCompleted, System(), "", now); err != nil {
		return err
	}
	o.events.Record(OrderCompleted{Order: o.summary(), At: now})
	return nil
}

func (o *Order) ReadyToComplete(returnWindow time.Duration, now time.Time) bool {
	return o.status == StatusDelivered && !o.deliveredAt.IsZero() && now.Sub(o.deliveredAt) >= returnWindow
}

func (o *Order) Cancel(actor Actor, reason string, now time.Time) error {
	if o.status == StatusCancelled {
		return nil
	}
	reason = strings.TrimSpace(reason)
	if !within(reason, 1, 500) {
		return ErrInvalidReason
	}
	if actor.Kind == ActorBuyer && actor.ID != o.buyerID.String() {
		return ErrCancellationDenied
	}
	if actor.Kind != ActorSystem && !slices.Contains(cancellable, o.status) {
		return ErrCancellationDenied.WithDetail("status %s", o.status)
	}
	refund := o.status.IsPaid()
	if err := o.transition(StatusCancelled, actor, reason, now); err != nil {
		if o.status.IsTerminal() {
			return ErrCancellationDenied.WithDetail("status %s", o.status)
		}
		return err
	}
	o.events.Record(OrderCancelled{
		OrderID: o.id, BuyerID: o.buyerID, Actor: actor, Reason: reason, RefundRequired: refund, Total: o.total, At: now,
	})
	return nil
}

func (o *Order) transition(target Status, actor Actor, reason string, now time.Time) error {
	if err := o.status.CanTransitionTo(target); err != nil {
		return err
	}
	if !actor.valid() {
		return kernel.ErrInvalidID
	}
	o.history = append(o.history, StatusChange{From: o.status, To: target, Actor: actor, Reason: strings.TrimSpace(reason), At: now})
	o.status, o.updatedAt = target, now
	return nil
}

func (o *Order) summary() Summary {
	return Summary{
		OrderID: o.id, BuyerID: o.buyerID, Currency: o.total.Currency(), Subtotal: o.subtotal, Discount: o.discount,
		Shipping: o.shipping, Total: o.total, PromoCode: o.promoCode, Items: slices.Clone(o.items), Parts: slices.Clone(o.parts),
	}
}

func (o *Order) SellerIDs() []kernel.SellerID {
	out := make([]kernel.SellerID, 0, len(o.parts))
	for _, part := range o.parts {
		out = append(out, part.SellerID)
	}
	return out
}

func (o *Order) ID() OrderID { return o.id }

func (o *Order) BuyerID() kernel.UserID { return o.buyerID }

func (o *Order) Status() Status { return o.status }

func (o *Order) Address() Address { return o.address }

func (o *Order) DeliveryMethod() string { return o.deliveryMethod }

func (o *Order) PromoCode() string { return o.promoCode }

func (o *Order) Items() []Item { return slices.Clone(o.items) }

func (o *Order) Parts() []Part { return slices.Clone(o.parts) }

func (o *Order) Subtotal() kernel.Money { return o.subtotal }

func (o *Order) Discount() kernel.Money { return o.discount }

func (o *Order) Shipping() kernel.Money { return o.shipping }

func (o *Order) Total() kernel.Money { return o.total }

func (o *Order) PaymentID() string { return o.paymentID }

func (o *Order) History() []StatusChange { return slices.Clone(o.history) }

func (o *Order) CreatedAt() time.Time { return o.createdAt }

func (o *Order) UpdatedAt() time.Time { return o.updatedAt }

func (o *Order) DeliveredAt() time.Time { return o.deliveredAt }

func (o *Order) Version() int { return o.version }

func (o *Order) AdvanceVersion() { o.version++ }

func (o *Order) PullEvents() []kernel.DomainEvent { return o.events.Pull() }
