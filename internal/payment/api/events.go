package api

import "time"

const (
	EventPending         = "payment.pending.v1"
	EventAuthorized      = "payment.authorized.v1"
	EventFailed          = "payment.failed.v1"
	EventCaptured        = "payment.captured.v1"
	EventCancelled       = "payment.cancelled.v1"
	EventRefundRequested = "payment.refund_requested.v1"
	EventRefundCompleted = "payment.refund_completed.v1"
)

type PaymentV1 struct {
	PaymentID  string    `json:"payment_id"`
	OrderID    string    `json:"order_id"`
	Amount     int64     `json:"amount,omitempty"`
	Currency   string    `json:"currency,omitempty"`
	Reason     string    `json:"reason,omitempty"`
	OccurredAt time.Time `json:"occurred_at"`
}

type RefundV1 struct {
	PaymentID  string    `json:"payment_id"`
	OrderID    string    `json:"order_id"`
	RefundID   string    `json:"refund_id"`
	Amount     int64     `json:"amount"`
	Refunded   int64     `json:"refunded"`
	Currency   string    `json:"currency"`
	Succeeded  bool      `json:"succeeded"`
	Reason     string    `json:"reason,omitempty"`
	OccurredAt time.Time `json:"occurred_at"`
}
