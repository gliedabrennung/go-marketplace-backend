package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/inventory/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

var now = time.Date(2026, 9, 16, 10, 0, 0, 0, time.UTC)

func sku(t *testing.T, raw string) domain.SKU {
	t.Helper()
	value, err := domain.NewSKU(raw)
	require.NoError(t, err)
	return value
}

func stocked(t *testing.T, quantity int) (*domain.StockItem, kernel.SellerID) {
	t.Helper()
	seller := kernel.NewSellerID()
	item, err := domain.OpenStock(sku(t, "SKU-1"), seller, now)
	require.NoError(t, err)
	require.NoError(t, item.Restock(seller, kernel.MustQuantity(quantity), "supply-1", now))
	item.PullEvents()
	item.PullMovements()
	return item, seller
}

func names(events []kernel.DomainEvent) []string {
	out := make([]string, len(events))
	for i, event := range events {
		out[i] = event.EventName()
	}
	return out
}

func TestNewSKU(t *testing.T) {
	value, err := domain.NewSKU("  OFFER-01.a:b_c  ")
	require.NoError(t, err)
	assert.Equal(t, "OFFER-01.a:b_c", value.String())
	assert.False(t, value.IsZero())

	for _, raw := range []string{"", "  ", "плохой", "a b", string(make([]byte, 65))} {
		_, err := domain.NewSKU(raw)
		require.ErrorIs(t, err, domain.ErrInvalidSKU, raw)
	}
}

func TestOpenStockAndRestock(t *testing.T) {
	seller := kernel.NewSellerID()
	_, err := domain.OpenStock(domain.SKU{}, seller, now)
	require.ErrorIs(t, err, kernel.ErrInvalidID)
	_, err = domain.OpenStock(sku(t, "SKU-1"), kernel.SellerID{}, now)
	require.ErrorIs(t, err, kernel.ErrInvalidID)

	item, err := domain.OpenStock(sku(t, "SKU-1"), seller, now)
	require.NoError(t, err)
	assert.Equal(t, []string{"inventory.stock_changed.v1"}, names(item.PullEvents()))

	require.ErrorIs(t, item.Restock(kernel.NewSellerID(), kernel.MustQuantity(5), "supply-1", now), domain.ErrNotStockOwner)
	require.ErrorIs(t, item.Restock(seller, kernel.MustQuantity(0), "supply-1", now), domain.ErrInvalidQuantity)
	require.ErrorIs(t, item.Restock(seller, kernel.MustQuantity(5), "", now), domain.ErrInvalidReference)

	require.NoError(t, item.Restock(seller, kernel.MustQuantity(5), "supply-1", now))
	assert.Equal(t, 5, item.Available().Value())
	assert.Equal(t, 5, item.Physical())

	movements := item.PullMovements()
	require.Len(t, movements, 1)
	assert.Equal(t, domain.Movement{
		SKU: "SKU-1", Delta: 5, Reason: domain.MovementRestock, ReferenceID: "supply-1", OccurredAt: now,
	}, movements[0])
	assert.Empty(t, item.PullMovements())

	require.ErrorIs(t, item.Restock(seller, kernel.MustQuantity(kernel.MaxQuantity), "supply-2", now), domain.ErrStockOverflow)
}

func TestSetAvailable(t *testing.T) {
	item, seller := stocked(t, 10)

	require.ErrorIs(t, item.SetAvailable(kernel.NewSellerID(), kernel.MustQuantity(3), "sync-1", now), domain.ErrNotStockOwner)
	require.ErrorIs(t, item.SetAvailable(seller, kernel.MustQuantity(3), "", now), domain.ErrInvalidReference)

	require.NoError(t, item.SetAvailable(seller, kernel.MustQuantity(10), "sync-1", now))
	assert.Empty(t, item.PullMovements())

	require.NoError(t, item.SetAvailable(seller, kernel.MustQuantity(3), "sync-2", now))
	assert.Equal(t, 3, item.Available().Value())
	movements := item.PullMovements()
	require.Len(t, movements, 1)
	assert.Equal(t, -7, movements[0].Delta)
	assert.Equal(t, domain.MovementCorrection, movements[0].Reason)
}

func TestReserveIsIdempotentAndBounded(t *testing.T) {
	item, _ := stocked(t, 10)
	reservation := domain.NewReservationID()
	order := domain.NewReservationID()
	expires := now.Add(20 * time.Minute)

	require.ErrorIs(t, item.Reserve(domain.ReservationID{}, domain.OrderID{}, kernel.MustQuantity(1), expires, now), kernel.ErrInvalidID)
	require.ErrorIs(t, item.Reserve(reservation, domain.OrderID{}, kernel.MustQuantity(0), expires, now), domain.ErrInvalidQuantity)
	require.ErrorIs(t, item.Reserve(reservation, domain.OrderID{}, kernel.MustQuantity(1), now, now), domain.ErrInvalidTTL)

	orderID, err := domain.ParseOrderID(order.String())
	require.NoError(t, err)
	require.NoError(t, item.Reserve(reservation, orderID, kernel.MustQuantity(4), expires, now))
	assert.Equal(t, 6, item.Available().Value())
	assert.Equal(t, 4, item.Reserved().Value())
	assert.Equal(t, 10, item.Physical())
	assert.Equal(t, []string{"inventory.stock_changed.v1", "inventory.stock_reserved.v1"}, names(item.PullEvents()))

	require.NoError(t, item.Reserve(reservation, orderID, kernel.MustQuantity(4), expires, now))
	assert.Equal(t, 6, item.Available().Value())
	assert.Empty(t, item.PullEvents())

	require.ErrorIs(t, item.Reserve(domain.NewReservationID(), orderID, kernel.MustQuantity(7), expires, now), domain.ErrInsufficientStock)
	assert.Equal(t, 6, item.Available().Value())

	hold, err := item.Hold(reservation)
	require.NoError(t, err)
	assert.Equal(t, domain.HoldHeld, hold.Status())
	assert.Equal(t, 4, hold.Quantity().Value())
	assert.Equal(t, orderID, hold.OrderID())
	assert.True(t, hold.IsHeld())
	_, err = item.Hold(domain.NewReservationID())
	require.ErrorIs(t, err, domain.ErrReservationNotFound)

	movements := item.PullMovements()
	require.Len(t, movements, 1)
	assert.Equal(t, 0, movements[0].Delta)
	assert.Equal(t, domain.MovementReserve, movements[0].Reason)
	assert.Equal(t, reservation.String(), movements[0].ReferenceID)
}

func TestCommitReleaseAndExpire(t *testing.T) {
	item, _ := stocked(t, 10)
	expires := now.Add(20 * time.Minute)

	committed := domain.NewReservationID()
	released := domain.NewReservationID()
	expired := domain.NewReservationID()
	for _, id := range []domain.ReservationID{committed, released, expired} {
		require.NoError(t, item.Reserve(id, domain.OrderID{}, kernel.MustQuantity(2), expires, now))
	}
	item.PullEvents()
	item.PullMovements()

	require.ErrorIs(t, item.Commit(domain.NewReservationID(), now), domain.ErrReservationNotFound)

	require.NoError(t, item.Commit(committed, now))
	assert.Equal(t, 4, item.Available().Value())
	assert.Equal(t, 4, item.Reserved().Value())
	assert.Equal(t, 8, item.Physical())
	require.NoError(t, item.Commit(committed, now))
	require.ErrorIs(t, item.Release(committed, now), domain.ErrReservationResolved)

	require.NoError(t, item.Release(released, now))
	assert.Equal(t, 6, item.Available().Value())
	assert.Equal(t, 2, item.Reserved().Value())

	later := now.Add(21 * time.Minute)
	due, err := item.ExpireDue(later)
	require.NoError(t, err)
	assert.Equal(t, []domain.ReservationID{expired}, due)
	assert.Equal(t, 8, item.Available().Value())
	assert.Zero(t, item.Reserved().Value())

	due, err = item.ExpireDue(later)
	require.NoError(t, err)
	assert.Empty(t, due)

	hold, err := item.Hold(expired)
	require.NoError(t, err)
	assert.Equal(t, domain.HoldExpired, hold.Status())
	assert.False(t, hold.IsHeld())
	assert.Equal(t, expires, hold.ExpiresAt())

	deltas := map[domain.MovementReason]int{}
	for _, movement := range item.PullMovements() {
		deltas[movement.Reason] += movement.Delta
	}
	assert.Equal(t, map[domain.MovementReason]int{domain.MovementCommit: -2, domain.MovementRelease: 0}, deltas)
	assert.Contains(t, names(item.PullEvents()), "inventory.reservation_resolved.v1")
}

func TestRestoreCommittedReservation(t *testing.T) {
	item, _ := stocked(t, 5)
	reservation := domain.NewReservationID()
	require.NoError(t, item.Reserve(reservation, domain.OrderID{}, kernel.MustQuantity(3), now.Add(time.Hour), now))

	require.ErrorIs(t, item.Restore(domain.NewReservationID(), now), domain.ErrReservationNotFound)
	require.ErrorIs(t, item.Restore(reservation, now), domain.ErrReservationNotCommitted)

	require.NoError(t, item.Commit(reservation, now))
	assert.Equal(t, 2, item.Physical())
	item.PullMovements()
	item.PullEvents()

	require.NoError(t, item.Restore(reservation, now))
	assert.Equal(t, 5, item.Available().Value())
	assert.Equal(t, 5, item.Physical())
	require.NoError(t, item.Restore(reservation, now))
	assert.Equal(t, 5, item.Available().Value())

	hold, err := item.Hold(reservation)
	require.NoError(t, err)
	assert.Equal(t, domain.HoldRestored, hold.Status())
	movements := item.PullMovements()
	require.Len(t, movements, 1)
	assert.Equal(t, domain.MovementReturn, movements[0].Reason)
	assert.Equal(t, 3, movements[0].Delta)
	assert.Contains(t, names(item.PullEvents()), "inventory.reservation_resolved.v1")

	restored, err := domain.RehydrateStockItem(item.Snapshot())
	require.NoError(t, err)
	assert.Equal(t, item.Snapshot(), restored.Snapshot())
}

func TestReturnUnits(t *testing.T) {
	item, _ := stocked(t, 1)

	require.ErrorIs(t, item.ReturnUnits("return-1", kernel.MustQuantity(0), now), domain.ErrInvalidQuantity)
	require.ErrorIs(t, item.ReturnUnits("", kernel.MustQuantity(1), now), domain.ErrInvalidReference)

	require.NoError(t, item.ReturnUnits("return-1", kernel.MustQuantity(2), now))
	assert.Equal(t, 3, item.Available().Value())
	movements := item.PullMovements()
	require.Len(t, movements, 1)
	assert.Equal(t, domain.MovementReturn, movements[0].Reason)
	assert.Equal(t, 2, movements[0].Delta)
}

func TestSnapshotRoundTrip(t *testing.T) {
	item, _ := stocked(t, 7)
	reservation := domain.NewReservationID()
	require.NoError(t, item.Reserve(reservation, domain.OrderID{}, kernel.MustQuantity(3), now.Add(time.Hour), now))
	item.AdvanceVersion()

	snap := item.Snapshot()
	restored, err := domain.RehydrateStockItem(snap)
	require.NoError(t, err)
	assert.Equal(t, snap, restored.Snapshot())
	assert.Equal(t, 4, restored.Available().Value())
	assert.Equal(t, 3, restored.Reserved().Value())
	require.Len(t, restored.Holds(), 1)

	broken := snap
	broken.SKU = "плохой"
	_, err = domain.RehydrateStockItem(broken)
	require.ErrorIs(t, err, domain.ErrInvalidSKU)

	broken = snap
	broken.SellerID = "bad"
	_, err = domain.RehydrateStockItem(broken)
	require.Error(t, err)

	broken = snap
	broken.Available = -1
	_, err = domain.RehydrateStockItem(broken)
	require.ErrorIs(t, err, kernel.ErrNegativeQuantity)

	broken = snap
	broken.Holds = []domain.HoldSnapshot{{ReservationID: "bad", Quantity: 1, Status: string(domain.HoldHeld)}}
	_, err = domain.RehydrateStockItem(broken)
	require.Error(t, err)

	broken = snap
	broken.Holds = []domain.HoldSnapshot{{ReservationID: reservation.String(), Quantity: 1, Status: "unknown"}}
	_, err = domain.RehydrateStockItem(broken)
	require.ErrorContains(t, err, "unknown hold status")
}
