package command_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/ordering/application/command"
	"github.com/gliedabrennung/go-marketplace-backend/internal/ordering/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

func TestFulfilmentBySellerAndAutoComplete(t *testing.T) {
	e := newEnv()
	who := buyer()
	ownerA := buyer()
	e.team[sellerA] = ownerA.UserID
	result := e.order(t, who, "")
	e.authorize(t, result)
	require.Equal(t, "paid", e.view(t, who, result.OrderID).Status)

	stranger := buyer()
	_, err := e.ship.Handle(ctx, command.MarkOrderShipped{Actor: stranger, OrderID: result.OrderID})
	require.ErrorIs(t, err, command.ErrNotOrderSeller)
	_, err = e.ship.Handle(ctx, command.MarkOrderShipped{Actor: auth.Principal{}, OrderID: result.OrderID})
	require.ErrorIs(t, err, auth.ErrUnauthenticated)
	_, err = e.ship.Handle(ctx, command.MarkOrderShipped{Actor: ownerA, OrderID: "bogus"})
	require.ErrorIs(t, err, domain.ErrOrderNotFound)

	_, err = e.deliver.Handle(ctx, command.MarkOrderDelivered{Actor: ownerA, OrderID: result.OrderID})
	require.ErrorIs(t, err, &domain.TransitionError{})

	_, err = e.ship.Handle(ctx, command.MarkOrderShipped{Actor: ownerA, OrderID: result.OrderID})
	require.NoError(t, err)
	assert.Equal(t, "shipped", e.view(t, who, result.OrderID).Status)

	_, err = e.deliver.Handle(ctx, command.MarkOrderDelivered{Actor: support(), OrderID: result.OrderID})
	require.NoError(t, err)
	assert.Equal(t, "delivered", e.view(t, who, result.OrderID).Status)

	processed, err := e.complete.Handle(ctx, command.CompleteDeliveredOrders{})
	require.NoError(t, err)
	assert.Zero(t, processed)
	assert.Equal(t, "delivered", e.view(t, who, result.OrderID).Status)

	e.clock.Advance(15 * 24 * time.Hour)
	processed, err = e.complete.Handle(ctx, command.CompleteDeliveredOrders{})
	require.NoError(t, err)
	assert.Equal(t, 1, processed)
	assert.Equal(t, "completed", e.view(t, who, result.OrderID).Status)

	processed, err = e.complete.Handle(ctx, command.CompleteDeliveredOrders{})
	require.NoError(t, err)
	assert.Zero(t, processed)
}
