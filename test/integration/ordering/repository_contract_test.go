//go:build integration

package ordering_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/ordering/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/ordering/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/ordering/infrastructure/memory"
	"github.com/gliedabrennung/go-marketplace-backend/internal/ordering/infrastructure/postgres"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/test/testdb"
)

var orderingTables = []string{
	"ordering.checkout_sagas", "ordering.order_status_history", "ordering.order_parts", "ordering.order_items",
	"ordering.orders", "platform.outbox", "platform.idempotency_keys",
}

func implementations() map[string]func(t *testing.T) application.Repositories {
	return map[string]func(t *testing.T) application.Repositories{
		"memory": func(*testing.T) application.Repositories { return memory.NewStore() },
		"postgres": func(t *testing.T) application.Repositories {
			pool := testdb.Pool(t)
			testdb.Truncate(t, pool, orderingTables...)
			return postgres.NewRepositories(pool, postgres.NewOutboxWriter())
		},
	}
}

func money(amount int64) kernel.Money { return kernel.MustMoney(amount, kernel.KZT) }

func placeOrder(t *testing.T, at time.Time) *domain.Order {
	t.Helper()
	sellerA, sellerB := kernel.NewSellerID(), kernel.NewSellerID()
	order, err := domain.Place(domain.PlaceSpec{
		ID: domain.NewOrderID(), BuyerID: kernel.NewUserID(), DeliveryMethod: "standard", PromoCode: "SALE", Currency: kernel.KZT,
		Address: domain.Address{Recipient: "Айгуль", Phone: "+77011234567", City: "Алматы", Line: "Абая, 1", PostalCode: "050000"},
		Items: []domain.Item{
			{SKU: "A", ProductID: "p-a", SellerID: sellerA, Title: "Товар A", Quantity: 2, UnitPrice: money(1000), Base: money(2000), Final: money(1800)},
			{SKU: "B", ProductID: "p-b", SellerID: sellerB, Title: "Товар B", Quantity: 1, UnitPrice: money(500), Base: money(500), Final: money(500)},
		},
		Shipping: []domain.ShippingCost{{SellerID: sellerA, Cost: money(300)}, {SellerID: sellerB, Cost: money(0)}},
	}, at)
	require.NoError(t, err)
	return order
}

func TestOrderAndSagaRepositoryContract(t *testing.T) {
	ctx := context.Background()
	for name, factory := range implementations() {
		t.Run(name, func(t *testing.T) {
			repos := factory(t)
			at := time.Now().UTC().Truncate(time.Microsecond)
			_, err := repos.Orders().FindByID(ctx, domain.NewOrderID())
			require.ErrorIs(t, err, domain.ErrOrderNotFound)
			_, err = repos.Sagas().FindByOrder(ctx, domain.NewOrderID())
			require.ErrorIs(t, err, domain.ErrSagaNotFound)
			_, err = repos.Sagas().FindByPayment(ctx, "missing")
			require.ErrorIs(t, err, domain.ErrSagaNotFound)

			order := placeOrder(t, at)
			require.NoError(t, repos.Orders().Save(ctx, order))
			saga, err := domain.StartSaga(domain.SagaSpec{
				OrderID: order.ID(), BuyerID: order.BuyerID(), ReservationID: kernel.NewUserID().String(),
				PaymentID: "pay-" + name, PromoCode: "SALE", Amount: order.Total(), Deadline: at.Add(20 * time.Minute),
			}, at)
			require.NoError(t, err)
			require.NoError(t, repos.Sagas().Save(ctx, saga))

			loaded, err := repos.Orders().FindByID(ctx, order.ID())
			require.NoError(t, err)
			assert.Equal(t, order.Snapshot(), loaded.Snapshot())

			require.NoError(t, loaded.AwaitPayment("pay-"+name, at.Add(time.Second)))
			require.NoError(t, repos.Orders().Save(ctx, loaded))
			require.ErrorIs(t, repos.Orders().Save(ctx, order), kernel.ErrConcurrentModification)
			reloaded, err := repos.Orders().FindByID(ctx, order.ID())
			require.NoError(t, err)
			assert.Equal(t, loaded.Snapshot(), reloaded.Snapshot())
			assert.Len(t, reloaded.History(), 2)

			byPayment, err := repos.Sagas().FindByPayment(ctx, "pay-"+name)
			require.NoError(t, err)
			assert.Equal(t, saga.Snapshot(), byPayment.Snapshot())

			ids, err := repos.Sagas().Expired(ctx, at.Add(10*time.Minute), 10)
			require.NoError(t, err)
			assert.Empty(t, ids)
			ids, err = repos.Sagas().Expired(ctx, at.Add(21*time.Minute), 10)
			require.NoError(t, err)
			assert.Equal(t, []domain.OrderID{order.ID()}, ids)

			require.NoError(t, byPayment.AwaitPayment("pay-"+name, at))
			started, err := byPayment.BeginCommit("pay-"+name, at)
			require.NoError(t, err)
			require.True(t, started)
			require.NoError(t, repos.Sagas().Save(ctx, byPayment))
			require.ErrorIs(t, repos.Sagas().Save(ctx, saga), kernel.ErrConcurrentModification)
			ids, err = repos.Sagas().Stalled(ctx, at.Add(time.Minute), 10)
			require.NoError(t, err)
			assert.Equal(t, []domain.OrderID{order.ID()}, ids)
			ids, err = repos.Sagas().Expired(ctx, at.Add(time.Hour), 10)
			require.NoError(t, err)
			assert.Empty(t, ids)

			committing, err := repos.Sagas().FindByOrder(ctx, order.ID())
			require.NoError(t, err)
			require.NoError(t, committing.BeginCompensation("test", "refund-1", at))
			committing.CompensationFailed(assert.AnError, 5, at)
			committing.Compensated(domain.CompensationCancel, at)
			require.NoError(t, repos.Sagas().Save(ctx, committing))
			ids, err = repos.Sagas().Compensating(ctx, at.Add(5*time.Second), 10)
			require.NoError(t, err)
			assert.Empty(t, ids)
			ids, err = repos.Sagas().Compensating(ctx, at.Add(11*time.Second), 10)
			require.NoError(t, err)
			assert.Equal(t, []domain.OrderID{order.ID()}, ids)

			final, err := repos.Sagas().FindByOrder(ctx, order.ID())
			require.NoError(t, err)
			assert.Equal(t, committing.Snapshot(), final.Snapshot())
		})
	}
}
