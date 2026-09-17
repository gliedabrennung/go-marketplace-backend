package api

import "time"

const (
	EventOrderCreated         = "ordering.order_created.v1"
	EventOrderAwaitingPayment = "ordering.order_awaiting_payment.v1"
	EventOrderPaid            = "ordering.order_paid.v1"
	EventOrderFailed          = "ordering.order_failed.v1"
	EventOrderCancelled       = "ordering.order_cancelled.v1"
	EventOrderShipped         = "ordering.order_shipped.v1"
	EventOrderDelivered       = "ordering.order_delivered.v1"
	EventOrderCompleted       = "ordering.order_completed.v1"
)

type ItemV1 struct {
	SKU        string `json:"sku"`
	ProductID  string `json:"product_id"`
	CategoryID string `json:"category_id,omitempty"`
	SellerID   string `json:"seller_id"`
	Title      string `json:"title"`
	Quantity   int    `json:"quantity"`
	UnitPrice  int64  `json:"unit_price"`
	Subtotal   int64  `json:"subtotal"`
	Total      int64  `json:"total"`
}

type PartV1 struct {
	SellerID string `json:"seller_id"`
	Subtotal int64  `json:"subtotal"`
	Discount int64  `json:"discount"`
	Shipping int64  `json:"shipping"`
	Total    int64  `json:"total"`
}

type OrderV1 struct {
	OrderID    string    `json:"order_id"`
	BuyerID    string    `json:"buyer_id"`
	PaymentID  string    `json:"payment_id,omitempty"`
	Currency   string    `json:"currency"`
	Subtotal   int64     `json:"subtotal"`
	Discount   int64     `json:"discount"`
	Shipping   int64     `json:"shipping"`
	Total      int64     `json:"total"`
	PromoCode  string    `json:"promo_code,omitempty"`
	Items      []ItemV1  `json:"items"`
	Parts      []PartV1  `json:"parts"`
	OccurredAt time.Time `json:"occurred_at"`
}

type OrderShipmentV1 struct {
	OrderID    string    `json:"order_id"`
	BuyerID    string    `json:"buyer_id"`
	OccurredAt time.Time `json:"occurred_at"`
}

type OrderStatusV1 struct {
	OrderID        string    `json:"order_id"`
	BuyerID        string    `json:"buyer_id"`
	PaymentID      string    `json:"payment_id,omitempty"`
	ActorKind      string    `json:"actor_kind,omitempty"`
	Reason         string    `json:"reason,omitempty"`
	RefundRequired bool      `json:"refund_required,omitempty"`
	Total          int64     `json:"total,omitempty"`
	Currency       string    `json:"currency,omitempty"`
	OccurredAt     time.Time `json:"occurred_at"`
}
