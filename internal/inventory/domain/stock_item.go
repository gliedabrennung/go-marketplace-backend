package domain

import (
	"slices"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type HoldStatus string

const (
	HoldHeld      HoldStatus = "held"
	HoldCommitted HoldStatus = "committed"
	HoldReleased  HoldStatus = "released"
	HoldExpired   HoldStatus = "expired"
	HoldRestored  HoldStatus = "restored"
)

type Hold struct {
	reservationID ReservationID
	orderID       OrderID
	quantity      kernel.Quantity
	status        HoldStatus
	expiresAt     time.Time
	createdAt     time.Time
	resolvedAt    time.Time
}

func (h Hold) ReservationID() ReservationID { return h.reservationID }

func (h Hold) OrderID() OrderID { return h.orderID }

func (h Hold) Quantity() kernel.Quantity { return h.quantity }

func (h Hold) Status() HoldStatus { return h.status }

func (h Hold) ExpiresAt() time.Time { return h.expiresAt }

func (h Hold) IsHeld() bool { return h.status == HoldHeld }

type StockItem struct {
	sku       SKU
	sellerID  kernel.SellerID
	available kernel.Quantity
	reserved  kernel.Quantity
	holds     []Hold
	movements []Movement
	createdAt time.Time
	updatedAt time.Time
	version   int

	events kernel.EventBuffer
}

func OpenStock(sku SKU, seller kernel.SellerID, now time.Time) (*StockItem, error) {
	if sku.IsZero() || seller.IsZero() {
		return nil, kernel.ErrInvalidID
	}
	item := &StockItem{sku: sku, sellerID: seller, createdAt: now, updatedAt: now}
	item.events.Record(StockChanged{
		SKU: sku, SellerID: seller, Available: 0, Reserved: 0, Reason: MovementCorrection, At: now,
	})
	return item, nil
}

func (s *StockItem) Restock(seller kernel.SellerID, quantity kernel.Quantity, reference string, now time.Time) error {
	if err := s.requireOwner(seller); err != nil {
		return err
	}
	if quantity.IsZero() {
		return ErrInvalidQuantity
	}
	if reference == "" {
		return ErrInvalidReference
	}
	next, err := s.available.Add(quantity)
	if err != nil {
		return ErrStockOverflow
	}
	s.available = next
	s.record(MovementRestock, reference, quantity.Value(), now)
	return nil
}

func (s *StockItem) SetAvailable(seller kernel.SellerID, target kernel.Quantity, reference string, now time.Time) error {
	if err := s.requireOwner(seller); err != nil {
		return err
	}
	if reference == "" {
		return ErrInvalidReference
	}
	delta := target.Value() - s.available.Value()
	if delta == 0 {
		return nil
	}
	s.available = target
	s.record(MovementCorrection, reference, delta, now)
	return nil
}

func (s *StockItem) Reserve(id ReservationID, order OrderID, quantity kernel.Quantity, expiresAt, now time.Time) error {
	if id.IsZero() {
		return kernel.ErrInvalidID
	}
	if quantity.IsZero() {
		return ErrInvalidQuantity
	}
	if !expiresAt.After(now) {
		return ErrInvalidTTL
	}
	if hold, found := s.hold(id); found {
		if hold.status == HoldHeld {
			return nil
		}
		return ErrReservationResolved
	}
	available, err := s.available.Sub(quantity)
	if err != nil {
		return ErrInsufficientStock.WithDetail("sku %s: requested %d, available %d", s.sku, quantity.Value(), s.available.Value())
	}
	reserved, err := s.reserved.Add(quantity)
	if err != nil {
		return ErrStockOverflow
	}
	s.available, s.reserved = available, reserved
	s.holds = append(s.holds, Hold{
		reservationID: id, orderID: order, quantity: quantity, status: HoldHeld,
		expiresAt: expiresAt, createdAt: now,
	})
	s.record(MovementReserve, id.String(), 0, now)
	s.events.Record(StockReserved{
		SKU: s.sku, SellerID: s.sellerID, ReservationID: id, OrderID: order,
		Quantity: quantity.Value(), ExpiresAt: expiresAt, At: now,
	})
	return nil
}

func (s *StockItem) Commit(id ReservationID, now time.Time) error {
	return s.resolve(id, HoldCommitted, MovementCommit, now)
}

func (s *StockItem) Release(id ReservationID, now time.Time) error {
	return s.resolve(id, HoldReleased, MovementRelease, now)
}

func (s *StockItem) Expire(id ReservationID, now time.Time) error {
	return s.resolve(id, HoldExpired, MovementRelease, now)
}

func (s *StockItem) Restore(id ReservationID, now time.Time) error {
	index := slices.IndexFunc(s.holds, func(h Hold) bool { return h.reservationID == id })
	if index < 0 {
		return ErrReservationNotFound
	}
	hold := &s.holds[index]
	switch hold.status {
	case HoldRestored:
		return nil
	case HoldCommitted:
	default:
		return ErrReservationNotCommitted
	}
	available, err := s.available.Add(hold.quantity)
	if err != nil {
		return ErrStockOverflow
	}
	s.available = available
	hold.status, hold.resolvedAt = HoldRestored, now
	s.record(MovementReturn, id.String(), hold.quantity.Value(), now)
	s.events.Record(ReservationResolved{
		SKU: s.sku, SellerID: s.sellerID, ReservationID: id, OrderID: hold.orderID,
		Quantity: hold.quantity.Value(), Status: HoldRestored, At: now,
	})
	return nil
}

func (s *StockItem) ExpireDue(now time.Time) ([]ReservationID, error) {
	var expired []ReservationID
	for _, hold := range s.holds {
		if hold.status == HoldHeld && !hold.expiresAt.After(now) {
			expired = append(expired, hold.reservationID)
		}
	}
	for _, id := range expired {
		if err := s.Expire(id, now); err != nil {
			return nil, err
		}
	}
	return expired, nil
}

func (s *StockItem) ReturnUnits(reference string, quantity kernel.Quantity, now time.Time) error {
	if quantity.IsZero() {
		return ErrInvalidQuantity
	}
	if reference == "" {
		return ErrInvalidReference
	}
	next, err := s.available.Add(quantity)
	if err != nil {
		return ErrStockOverflow
	}
	s.available = next
	s.record(MovementReturn, reference, quantity.Value(), now)
	return nil
}

func (s *StockItem) resolve(id ReservationID, status HoldStatus, reason MovementReason, now time.Time) error {
	index := slices.IndexFunc(s.holds, func(h Hold) bool { return h.reservationID == id })
	if index < 0 {
		return ErrReservationNotFound
	}
	hold := &s.holds[index]
	if hold.status == status {
		return nil
	}
	if hold.status != HoldHeld {
		return ErrReservationResolved
	}
	reserved, err := s.reserved.Sub(hold.quantity)
	if err != nil {
		return ErrInsufficientStock
	}
	s.reserved = reserved
	delta := 0
	if status == HoldCommitted {
		delta = -hold.quantity.Value()
	} else {
		available, err := s.available.Add(hold.quantity)
		if err != nil {
			return ErrStockOverflow
		}
		s.available = available
	}
	hold.status, hold.resolvedAt = status, now
	s.record(reason, id.String(), delta, now)
	s.events.Record(ReservationResolved{
		SKU: s.sku, SellerID: s.sellerID, ReservationID: id, OrderID: hold.orderID,
		Quantity: hold.quantity.Value(), Status: status, At: now,
	})
	return nil
}

func (s *StockItem) record(reason MovementReason, reference string, delta int, now time.Time) {
	s.updatedAt = now
	s.movements = append(s.movements, Movement{
		SKU: s.sku.String(), Delta: delta, Reason: reason, ReferenceID: reference, OccurredAt: now,
	})
	s.events.Record(StockChanged{
		SKU: s.sku, SellerID: s.sellerID, Available: s.available.Value(), Reserved: s.reserved.Value(),
		Reason: reason, At: now,
	})
}

func (s *StockItem) requireOwner(seller kernel.SellerID) error {
	if seller != s.sellerID {
		return ErrNotStockOwner
	}
	return nil
}

func (s *StockItem) hold(id ReservationID) (Hold, bool) {
	index := slices.IndexFunc(s.holds, func(h Hold) bool { return h.reservationID == id })
	if index < 0 {
		return Hold{}, false
	}
	return s.holds[index], true
}

func (s *StockItem) SKU() SKU { return s.sku }

func (s *StockItem) SellerID() kernel.SellerID { return s.sellerID }

func (s *StockItem) Available() kernel.Quantity { return s.available }

func (s *StockItem) Reserved() kernel.Quantity { return s.reserved }

func (s *StockItem) Physical() int { return s.available.Value() + s.reserved.Value() }

func (s *StockItem) Holds() []Hold { return slices.Clone(s.holds) }

func (s *StockItem) Hold(id ReservationID) (Hold, error) {
	hold, found := s.hold(id)
	if !found {
		return Hold{}, ErrReservationNotFound
	}
	return hold, nil
}

func (s *StockItem) UpdatedAt() time.Time { return s.updatedAt }

func (s *StockItem) Version() int { return s.version }

func (s *StockItem) AdvanceVersion() { s.version++ }

func (s *StockItem) PullEvents() []kernel.DomainEvent { return s.events.Pull() }

func (s *StockItem) PullMovements() []Movement {
	movements := slices.Clone(s.movements)
	s.movements = nil
	return movements
}
