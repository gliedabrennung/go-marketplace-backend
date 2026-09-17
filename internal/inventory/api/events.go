package api

import "time"

const (
	EventStockChanged        = "inventory.stock_changed.v1"
	EventStockReserved       = "inventory.stock_reserved.v1"
	EventReservationResolved = "inventory.reservation_resolved.v1"
)

type StockChangedV1 struct {
	SKU        string    `json:"sku"`
	SellerID   string    `json:"seller_id"`
	Available  int       `json:"available"`
	Reserved   int       `json:"reserved"`
	Reason     string    `json:"reason"`
	OccurredAt time.Time `json:"occurred_at"`
}

type StockReservedV1 struct {
	SKU           string    `json:"sku"`
	SellerID      string    `json:"seller_id"`
	ReservationID string    `json:"reservation_id"`
	OrderID       string    `json:"order_id,omitempty"`
	Quantity      int       `json:"quantity"`
	ExpiresAt     time.Time `json:"expires_at"`
	OccurredAt    time.Time `json:"occurred_at"`
}

type ReservationResolvedV1 struct {
	SKU           string    `json:"sku"`
	SellerID      string    `json:"seller_id"`
	ReservationID string    `json:"reservation_id"`
	OrderID       string    `json:"order_id,omitempty"`
	Quantity      int       `json:"quantity"`
	Status        string    `json:"status"`
	OccurredAt    time.Time `json:"occurred_at"`
}
