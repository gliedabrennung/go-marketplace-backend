package domain

import (
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type PaymentPending struct {
	PaymentID PaymentID
	OrderID   OrderID
	Amount    kernel.Money
	At        time.Time
}

func (e PaymentPending) EventName() string     { return "payment.pending.v1" }
func (e PaymentPending) AggregateID() string   { return e.PaymentID.String() }
func (e PaymentPending) OccurredAt() time.Time { return e.At }

type PaymentAuthorized struct {
	PaymentID PaymentID
	OrderID   OrderID
	Amount    kernel.Money
	At        time.Time
}

func (e PaymentAuthorized) EventName() string     { return "payment.authorized.v1" }
func (e PaymentAuthorized) AggregateID() string   { return e.PaymentID.String() }
func (e PaymentAuthorized) OccurredAt() time.Time { return e.At }

type PaymentFailed struct {
	PaymentID PaymentID
	OrderID   OrderID
	Reason    string
	At        time.Time
}

func (e PaymentFailed) EventName() string     { return "payment.failed.v1" }
func (e PaymentFailed) AggregateID() string   { return e.PaymentID.String() }
func (e PaymentFailed) OccurredAt() time.Time { return e.At }

type PaymentCaptured struct {
	PaymentID PaymentID
	OrderID   OrderID
	Amount    kernel.Money
	At        time.Time
}

func (e PaymentCaptured) EventName() string     { return "payment.captured.v1" }
func (e PaymentCaptured) AggregateID() string   { return e.PaymentID.String() }
func (e PaymentCaptured) OccurredAt() time.Time { return e.At }

type PaymentCancelled struct {
	PaymentID PaymentID
	OrderID   OrderID
	Reason    string
	At        time.Time
}

func (e PaymentCancelled) EventName() string     { return "payment.cancelled.v1" }
func (e PaymentCancelled) AggregateID() string   { return e.PaymentID.String() }
func (e PaymentCancelled) OccurredAt() time.Time { return e.At }

type RefundRequested struct {
	PaymentID PaymentID
	OrderID   OrderID
	RefundID  RefundID
	Amount    kernel.Money
	Reason    string
	At        time.Time
}

func (e RefundRequested) EventName() string     { return "payment.refund_requested.v1" }
func (e RefundRequested) AggregateID() string   { return e.PaymentID.String() }
func (e RefundRequested) OccurredAt() time.Time { return e.At }

type RefundCompleted struct {
	PaymentID PaymentID
	OrderID   OrderID
	RefundID  RefundID
	Amount    kernel.Money
	Refunded  kernel.Money
	Succeeded bool
	Reason    string
	At        time.Time
}

func (e RefundCompleted) EventName() string     { return "payment.refund_completed.v1" }
func (e RefundCompleted) AggregateID() string   { return e.PaymentID.String() }
func (e RefundCompleted) OccurredAt() time.Time { return e.At }
