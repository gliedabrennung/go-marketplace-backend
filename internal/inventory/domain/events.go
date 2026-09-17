package domain

import (
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type StockChanged struct {
	SKU       SKU
	SellerID  kernel.SellerID
	Available int
	Reserved  int
	Reason    MovementReason
	At        time.Time
}

func (e StockChanged) EventName() string     { return "inventory.stock_changed.v1" }
func (e StockChanged) AggregateID() string   { return e.SKU.String() }
func (e StockChanged) OccurredAt() time.Time { return e.At }

type StockReserved struct {
	SKU           SKU
	SellerID      kernel.SellerID
	ReservationID ReservationID
	OrderID       OrderID
	Quantity      int
	ExpiresAt     time.Time
	At            time.Time
}

func (e StockReserved) EventName() string     { return "inventory.stock_reserved.v1" }
func (e StockReserved) AggregateID() string   { return e.SKU.String() }
func (e StockReserved) OccurredAt() time.Time { return e.At }

type ReservationResolved struct {
	SKU           SKU
	SellerID      kernel.SellerID
	ReservationID ReservationID
	OrderID       OrderID
	Quantity      int
	Status        HoldStatus
	At            time.Time
}

func (e ReservationResolved) EventName() string     { return "inventory.reservation_resolved.v1" }
func (e ReservationResolved) AggregateID() string   { return e.SKU.String() }
func (e ReservationResolved) OccurredAt() time.Time { return e.At }
