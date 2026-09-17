package domain

import (
	"fmt"
	"slices"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type HoldSnapshot struct {
	ReservationID string
	OrderID       string
	Quantity      int
	Status        string
	ExpiresAt     time.Time
	CreatedAt     time.Time
	ResolvedAt    time.Time
}

type StockItemSnapshot struct {
	SKU       string
	SellerID  string
	Available int
	Reserved  int
	Holds     []HoldSnapshot
	CreatedAt time.Time
	UpdatedAt time.Time
	Version   int
}

func (s *StockItem) Snapshot() StockItemSnapshot {
	snap := StockItemSnapshot{
		SKU: s.sku.String(), SellerID: s.sellerID.String(),
		Available: s.available.Value(), Reserved: s.reserved.Value(),
		CreatedAt: s.createdAt, UpdatedAt: s.updatedAt, Version: s.version,
		Holds: make([]HoldSnapshot, 0, len(s.holds)),
	}
	for _, hold := range s.holds {
		snap.Holds = append(snap.Holds, HoldSnapshot{
			ReservationID: hold.reservationID.String(), OrderID: optionalOrder(hold.orderID),
			Quantity: hold.quantity.Value(), Status: string(hold.status),
			ExpiresAt: hold.expiresAt, CreatedAt: hold.createdAt, ResolvedAt: hold.resolvedAt,
		})
	}
	return snap
}

func RehydrateStockItem(s StockItemSnapshot) (*StockItem, error) {
	sku, err := NewSKU(s.SKU)
	if err != nil {
		return nil, fmt.Errorf("rehydrate stock item: %w", err)
	}
	seller, err := kernel.ParseSellerID(s.SellerID)
	if err != nil {
		return nil, fmt.Errorf("rehydrate stock item %s seller: %w", s.SKU, err)
	}
	available, err := kernel.NewQuantity(s.Available)
	if err != nil {
		return nil, fmt.Errorf("rehydrate stock item %s available: %w", s.SKU, err)
	}
	reserved, err := kernel.NewQuantity(s.Reserved)
	if err != nil {
		return nil, fmt.Errorf("rehydrate stock item %s reserved: %w", s.SKU, err)
	}
	item := &StockItem{
		sku: sku, sellerID: seller, available: available, reserved: reserved,
		createdAt: s.CreatedAt, updatedAt: s.UpdatedAt, version: s.Version,
		holds: make([]Hold, 0, len(s.Holds)),
	}
	for _, snap := range s.Holds {
		hold, err := restoreHold(snap)
		if err != nil {
			return nil, fmt.Errorf("rehydrate stock item %s hold: %w", s.SKU, err)
		}
		item.holds = append(item.holds, hold)
	}
	return item, nil
}

func restoreHold(snap HoldSnapshot) (Hold, error) {
	id, err := ParseReservationID(snap.ReservationID)
	if err != nil {
		return Hold{}, err
	}
	quantity, err := kernel.NewQuantity(snap.Quantity)
	if err != nil {
		return Hold{}, err
	}
	hold := Hold{
		reservationID: id, quantity: quantity, status: HoldStatus(snap.Status),
		expiresAt: snap.ExpiresAt, createdAt: snap.CreatedAt, resolvedAt: snap.ResolvedAt,
	}
	if !slices.Contains([]HoldStatus{HoldHeld, HoldCommitted, HoldReleased, HoldExpired, HoldRestored}, hold.status) {
		return Hold{}, fmt.Errorf("unknown hold status %q", snap.Status)
	}
	if snap.OrderID != "" {
		if hold.orderID, err = ParseOrderID(snap.OrderID); err != nil {
			return Hold{}, err
		}
	}
	return hold, nil
}

func optionalOrder(id OrderID) string {
	if id.IsZero() {
		return ""
	}
	return id.String()
}
