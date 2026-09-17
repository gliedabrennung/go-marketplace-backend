package domain

import (
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type Summary struct {
	OrderID   OrderID
	BuyerID   kernel.UserID
	Currency  kernel.Currency
	Subtotal  kernel.Money
	Discount  kernel.Money
	Shipping  kernel.Money
	Total     kernel.Money
	PromoCode string
	Items     []Item
	Parts     []Part
}

type OrderCreated struct {
	Order Summary
	At    time.Time
}

func (e OrderCreated) EventName() string     { return "ordering.order_created.v1" }
func (e OrderCreated) AggregateID() string   { return e.Order.OrderID.String() }
func (e OrderCreated) OccurredAt() time.Time { return e.At }

type OrderAwaitingPayment struct {
	OrderID   OrderID
	BuyerID   kernel.UserID
	PaymentID string
	At        time.Time
}

func (e OrderAwaitingPayment) EventName() string     { return "ordering.order_awaiting_payment.v1" }
func (e OrderAwaitingPayment) AggregateID() string   { return e.OrderID.String() }
func (e OrderAwaitingPayment) OccurredAt() time.Time { return e.At }

type OrderPaid struct {
	Order     Summary
	PaymentID string
	At        time.Time
}

func (e OrderPaid) EventName() string     { return "ordering.order_paid.v1" }
func (e OrderPaid) AggregateID() string   { return e.Order.OrderID.String() }
func (e OrderPaid) OccurredAt() time.Time { return e.At }

type OrderFailed struct {
	OrderID OrderID
	BuyerID kernel.UserID
	Reason  string
	At      time.Time
}

func (e OrderFailed) EventName() string     { return "ordering.order_failed.v1" }
func (e OrderFailed) AggregateID() string   { return e.OrderID.String() }
func (e OrderFailed) OccurredAt() time.Time { return e.At }

type OrderCancelled struct {
	OrderID        OrderID
	BuyerID        kernel.UserID
	Actor          Actor
	Reason         string
	RefundRequired bool
	Total          kernel.Money
	At             time.Time
}

func (e OrderCancelled) EventName() string     { return "ordering.order_cancelled.v1" }
func (e OrderCancelled) AggregateID() string   { return e.OrderID.String() }
func (e OrderCancelled) OccurredAt() time.Time { return e.At }

type OrderShipped struct {
	OrderID OrderID
	BuyerID kernel.UserID
	At      time.Time
}

func (e OrderShipped) EventName() string     { return "ordering.order_shipped.v1" }
func (e OrderShipped) AggregateID() string   { return e.OrderID.String() }
func (e OrderShipped) OccurredAt() time.Time { return e.At }

type OrderDelivered struct {
	OrderID OrderID
	BuyerID kernel.UserID
	At      time.Time
}

func (e OrderDelivered) EventName() string     { return "ordering.order_delivered.v1" }
func (e OrderDelivered) AggregateID() string   { return e.OrderID.String() }
func (e OrderDelivered) OccurredAt() time.Time { return e.At }

type OrderCompleted struct {
	Order Summary
	At    time.Time
}

func (e OrderCompleted) EventName() string     { return "ordering.order_completed.v1" }
func (e OrderCompleted) AggregateID() string   { return e.Order.OrderID.String() }
func (e OrderCompleted) OccurredAt() time.Time { return e.At }
