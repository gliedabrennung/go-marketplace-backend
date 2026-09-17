package command_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/inventory/application/command"
	"github.com/gliedabrennung/go-marketplace-backend/internal/inventory/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/inventory/domain"
	sellerapi "github.com/gliedabrennung/go-marketplace-backend/internal/seller/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/pagination"
)

func TestEnsureAndSetStock(t *testing.T) {
	e := newEnv()
	owner := user()
	seller := e.sellers.add(owner)

	_, err := e.ensure.Handle(ctx, command.EnsureStock{SKU: "плохой", SellerID: seller})
	require.ErrorIs(t, err, domain.ErrInvalidSKU)
	_, err = e.ensure.Handle(ctx, command.EnsureStock{SKU: "SKU-1", SellerID: "bad"})
	require.ErrorIs(t, err, kernel.ErrInvalidID)

	_, err = e.ensure.Handle(ctx, command.EnsureStock{SKU: "SKU-1", SellerID: seller})
	require.NoError(t, err)
	_, err = e.ensure.Handle(ctx, command.EnsureStock{SKU: "SKU-1", SellerID: kernel.NewSellerID().String()})
	require.NoError(t, err)

	view, err := e.stock.Handle(ctx, query.GetStock{Actor: owner, SKU: "SKU-1"})
	require.NoError(t, err)
	assert.Equal(t, seller, view.SellerID)
	assert.Zero(t, view.Available)

	_, err = e.set.Handle(ctx, command.SetStock{Actor: user(), SKU: "SKU-1", Quantity: 5})
	require.ErrorIs(t, err, sellerapi.ErrSellerNotFound)
	_, err = e.set.Handle(ctx, command.SetStock{SKU: "SKU-1", Quantity: 5})
	require.ErrorIs(t, err, auth.ErrUnauthenticated)
	_, err = e.set.Handle(ctx, command.SetStock{Actor: owner, SKU: "SKU-2", Quantity: 5})
	require.ErrorIs(t, err, domain.ErrStockNotFound)
	_, err = e.set.Handle(ctx, command.SetStock{Actor: owner, SKU: "плохой", Quantity: 5})
	require.ErrorIs(t, err, domain.ErrStockNotFound)
	_, err = e.set.Handle(ctx, command.SetStock{Actor: owner, SKU: "SKU-1", Quantity: -1})
	require.ErrorIs(t, err, kernel.ErrNegativeQuantity)

	result, err := e.set.Handle(ctx, command.SetStock{Actor: owner, SKU: "SKU-1", Quantity: 12, Reference: "sync-1"})
	require.NoError(t, err)
	assert.Equal(t, 12, result.Available)
	assert.Equal(t, seller, result.SellerID)
	assert.Equal(t, e.clock.Now(), result.UpdatedAt)

	movements, err := e.movements.Handle(ctx, query.ListMovements{Actor: owner, SKU: "SKU-1"})
	require.NoError(t, err)
	require.Len(t, movements, 1)
	assert.Equal(t, query.MovementView{
		SKU: "SKU-1", Delta: 12, Reason: "correction", ReferenceID: "sync-1", OccurredAt: e.clock.Now(),
	}, movements[0])

	_, err = e.movements.Handle(ctx, query.ListMovements{Actor: user(), SKU: "SKU-1"})
	require.ErrorIs(t, err, domain.ErrStockNotFound)
	_, err = e.movements.Handle(ctx, query.ListMovements{Actor: owner, SKU: "плохой"})
	require.ErrorIs(t, err, domain.ErrStockNotFound)
	_, err = e.stock.Handle(ctx, query.GetStock{Actor: user(), SKU: "SKU-1"})
	require.ErrorIs(t, err, domain.ErrStockNotFound)
}

func TestSellerStockListing(t *testing.T) {
	e := newEnv()
	owner := user()
	seller := e.sellers.add(owner)
	for _, sku := range []string{"SKU-1", "SKU-2", "SKU-3"} {
		e.stocked(t, owner, seller, sku, 4)
	}

	page, err := e.sellerStock.Handle(ctx, query.ListSellerStock{Actor: owner, SellerID: seller, Limit: 2})
	require.NoError(t, err)
	require.Len(t, page.Items, 2)
	assert.True(t, page.HasMore)
	assert.Equal(t, []string{"SKU-1", "SKU-2"}, []string{page.Items[0].SKU, page.Items[1].SKU})

	page, err = e.sellerStock.Handle(ctx, query.ListSellerStock{Actor: owner, SellerID: seller, Limit: 2, Cursor: page.NextCursor})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.Equal(t, "SKU-3", page.Items[0].SKU)
	assert.False(t, page.HasMore)

	_, err = e.sellerStock.Handle(ctx, query.ListSellerStock{Actor: user(), SellerID: seller})
	require.ErrorIs(t, err, sellerapi.ErrSellerNotFound)
	_, err = e.sellerStock.Handle(ctx, query.ListSellerStock{Actor: owner, SellerID: "bad"})
	require.ErrorIs(t, err, sellerapi.ErrSellerNotFound)
	_, err = e.sellerStock.Handle(ctx, query.ListSellerStock{Actor: owner, SellerID: seller, Cursor: "%%%"})
	require.ErrorIs(t, err, pagination.ErrInvalidCursor)
}

func TestReservationLifecycle(t *testing.T) {
	e := newEnv()
	owner := user()
	seller := e.sellers.add(owner)
	e.stocked(t, owner, seller, "SKU-1", 10)
	e.stocked(t, owner, seller, "SKU-2", 4)

	reservation := domain.NewReservationID().String()
	orderID := kernel.NewID[struct{}]().String()
	lines := []command.Line{{SKU: "SKU-2", Quantity: 2}, {SKU: "SKU-1", Quantity: 3}}

	_, err := e.reserve.Handle(ctx, command.ReserveStock{ReservationID: "bad", Lines: lines})
	require.ErrorIs(t, err, domain.ErrReservationNotFound)
	_, err = e.reserve.Handle(ctx, command.ReserveStock{ReservationID: reservation})
	require.ErrorIs(t, err, command.ErrInvalidLines)
	_, err = e.reserve.Handle(ctx, command.ReserveStock{ReservationID: reservation, Lines: []command.Line{{SKU: "SKU-1", Quantity: 1}, {SKU: "SKU-1", Quantity: 2}}})
	require.ErrorIs(t, err, command.ErrInvalidLines)
	_, err = e.reserve.Handle(ctx, command.ReserveStock{ReservationID: reservation, Lines: []command.Line{{SKU: "SKU-1", Quantity: 0}}})
	require.ErrorIs(t, err, domain.ErrInvalidQuantity)
	_, err = e.reserve.Handle(ctx, command.ReserveStock{ReservationID: reservation, OrderID: "bad", Lines: lines})
	require.ErrorIs(t, err, kernel.ErrInvalidID)
	_, err = e.reserve.Handle(ctx, command.ReserveStock{ReservationID: reservation, Lines: []command.Line{{SKU: "SKU-9", Quantity: 1}}})
	require.ErrorIs(t, err, domain.ErrStockNotFound)
	_, err = e.reserve.Handle(ctx, command.ReserveStock{ReservationID: reservation, Lines: []command.Line{{SKU: "SKU-2", Quantity: 5}}})
	require.ErrorIs(t, err, domain.ErrInsufficientStock)

	held, err := e.reserve.Handle(ctx, command.ReserveStock{ReservationID: reservation, OrderID: orderID, Lines: lines})
	require.NoError(t, err)
	assert.Equal(t, reservation, held.ReservationID)
	assert.Equal(t, e.clock.Now().Add(20*time.Minute), held.ExpiresAt)
	assert.Equal(t, 7, e.available(t, owner, "SKU-1"))
	assert.Equal(t, 2, e.available(t, owner, "SKU-2"))

	repeat, err := e.reserve.Handle(ctx, command.ReserveStock{ReservationID: reservation, OrderID: orderID, Lines: lines})
	require.NoError(t, err)
	assert.Equal(t, held.ExpiresAt, repeat.ExpiresAt)
	assert.Equal(t, 7, e.available(t, owner, "SKU-1"))

	view, err := e.reservation.Handle(ctx, query.GetReservation{ReservationID: reservation})
	require.NoError(t, err)
	assert.Equal(t, orderID, view.OrderID)
	assert.Equal(t, "held", view.Status)
	assert.Equal(t, []query.ReservationLineView{
		{SKU: "SKU-1", Quantity: 3, Status: "held"},
		{SKU: "SKU-2", Quantity: 2, Status: "held"},
	}, view.Lines)

	_, err = e.commit.Handle(ctx, command.CommitReservation{ReservationID: "bad"})
	require.ErrorIs(t, err, domain.ErrReservationNotFound)
	_, err = e.commit.Handle(ctx, command.CommitReservation{ReservationID: domain.NewReservationID().String()})
	require.ErrorIs(t, err, domain.ErrReservationNotFound)

	_, err = e.commit.Handle(ctx, command.CommitReservation{ReservationID: reservation})
	require.NoError(t, err)
	assert.Equal(t, 7, e.available(t, owner, "SKU-1"))
	stock, err := e.stock.Handle(ctx, query.GetStock{Actor: owner, SKU: "SKU-1"})
	require.NoError(t, err)
	assert.Zero(t, stock.Reserved)

	_, err = e.commit.Handle(ctx, command.CommitReservation{ReservationID: reservation})
	require.NoError(t, err)
	_, err = e.release.Handle(ctx, command.ReleaseReservation{ReservationID: reservation})
	require.ErrorIs(t, err, domain.ErrReservationResolved)

	_, err = e.restore.Handle(ctx, command.RestoreReservation{ReservationID: reservation})
	require.NoError(t, err)
	assert.Equal(t, 10, e.available(t, owner, "SKU-1"))
	assert.Equal(t, 4, e.available(t, owner, "SKU-2"))
	_, err = e.restore.Handle(ctx, command.RestoreReservation{ReservationID: "bad"})
	require.ErrorIs(t, err, domain.ErrReservationNotFound)
}

func TestReleaseAndExpire(t *testing.T) {
	e := newEnv()
	owner := user()
	seller := e.sellers.add(owner)
	e.stocked(t, owner, seller, "SKU-1", 5)

	released := domain.NewReservationID().String()
	_, err := e.reserve.Handle(ctx, command.ReserveStock{ReservationID: released, Lines: []command.Line{{SKU: "SKU-1", Quantity: 2}}})
	require.NoError(t, err)
	_, err = e.release.Handle(ctx, command.ReleaseReservation{ReservationID: released})
	require.NoError(t, err)
	assert.Equal(t, 5, e.available(t, owner, "SKU-1"))

	expiring := domain.NewReservationID().String()
	_, err = e.reserve.Handle(ctx, command.ReserveStock{ReservationID: expiring, Lines: []command.Line{{SKU: "SKU-1", Quantity: 4}}})
	require.NoError(t, err)
	assert.Equal(t, 1, e.available(t, owner, "SKU-1"))

	count, err := e.expire.Handle(ctx, command.ExpireReservations{})
	require.NoError(t, err)
	assert.Zero(t, count)

	e.clock.Advance(21 * time.Minute)
	count, err = e.expire.Handle(ctx, command.ExpireReservations{Limit: 10})
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	assert.Equal(t, 5, e.available(t, owner, "SKU-1"))

	count, err = e.expire.Handle(ctx, command.ExpireReservations{Limit: 10})
	require.NoError(t, err)
	assert.Zero(t, count)

	view, err := e.reservation.Handle(ctx, query.GetReservation{ReservationID: expiring})
	require.NoError(t, err)
	assert.Equal(t, "expired", view.Status)
	_, err = e.reservation.Handle(ctx, query.GetReservation{ReservationID: "bad"})
	require.ErrorIs(t, err, domain.ErrReservationNotFound)
	_, err = e.reservation.Handle(ctx, query.GetReservation{ReservationID: domain.NewReservationID().String()})
	require.ErrorIs(t, err, domain.ErrReservationNotFound)
}

func TestReturnStock(t *testing.T) {
	e := newEnv()
	owner := user()
	seller := e.sellers.add(owner)
	e.stocked(t, owner, seller, "SKU-1", 1)

	_, err := e.returns.Handle(ctx, command.ReturnStock{Lines: []command.Line{{SKU: "SKU-1", Quantity: 2}}})
	require.ErrorIs(t, err, domain.ErrInvalidReference)
	_, err = e.returns.Handle(ctx, command.ReturnStock{Reference: "return-1"})
	require.ErrorIs(t, err, command.ErrInvalidLines)
	_, err = e.returns.Handle(ctx, command.ReturnStock{Reference: "return-1", Lines: []command.Line{{SKU: "SKU-9", Quantity: 1}}})
	require.ErrorIs(t, err, domain.ErrStockNotFound)

	_, err = e.returns.Handle(ctx, command.ReturnStock{Reference: "return-1", Lines: []command.Line{{SKU: "SKU-1", Quantity: 2}}})
	require.NoError(t, err)
	assert.Equal(t, 3, e.available(t, owner, "SKU-1"))

	available, err := e.reads.Available(ctx, []string{"SKU-1", "SKU-9"})
	require.NoError(t, err)
	assert.Equal(t, map[string]int{"SKU-1": 3}, available)
}
