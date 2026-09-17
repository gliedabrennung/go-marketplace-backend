//go:build integration

package inventory_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/inventory/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/inventory/application/command"
	"github.com/gliedabrennung/go-marketplace-backend/internal/inventory/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/inventory/infrastructure/postgres"
	sellerapi "github.com/gliedabrennung/go-marketplace-backend/internal/seller/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/clock"
	"github.com/gliedabrennung/go-marketplace-backend/test/testdb"
)

type directory struct{}

func (directory) Seller(_ context.Context, sellerID string) (sellerapi.SellerInfo, error) {
	return sellerapi.SellerInfo{ID: sellerID, Status: "active", CanSell: true}, nil
}

func (directory) MemberRole(context.Context, string, string) (string, bool, error) {
	return sellerapi.RoleSellerAdmin, true, nil
}

func seedStock(ctx context.Context, t *testing.T, pool *pgxpool.Pool, raw string, quantity int) {
	t.Helper()
	repos := postgres.NewRepositories(pool, postgres.NewOutboxWriter())
	item, err := domain.OpenStock(sku(t, raw), kernel.NewSellerID(), now())
	require.NoError(t, err)
	if quantity > 0 {
		require.NoError(t, item.Restock(item.SellerID(), kernel.MustQuantity(quantity), "supply-"+raw, now()))
	}
	require.NoError(t, repos.Stock().Save(ctx, item))
}

func TestInventory_ConcurrentReservations_NoOversell(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Pool(t)
	testdb.Truncate(t, pool, inventoryTables...)
	seedStock(ctx, t, pool, "HOT-SKU", 10)

	base := command.NewBase(postgres.NewUnitOfWork(pool, postgres.NewOutboxWriter()), clock.System{}, directory{}, application.DefaultPolicy())
	reserve := command.NewReserveStockHandler(base)

	const attempts = 200
	var reserved, rejected atomic.Int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := reserve.Handle(ctx, command.ReserveStock{
				ReservationID: domain.NewReservationID().String(),
				Lines:         []command.Line{{SKU: "HOT-SKU", Quantity: 1}},
			})
			switch {
			case err == nil:
				reserved.Add(1)
			case errors.Is(err, domain.ErrInsufficientStock):
				rejected.Add(1)
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()

	assert.Equal(t, int64(10), reserved.Load())
	assert.Equal(t, int64(attempts-10), rejected.Load())

	var available, reservedUnits int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT available, reserved FROM inventory.stock_items WHERE sku = 'HOT-SKU'`).Scan(&available, &reservedUnits))
	assert.Zero(t, available)
	assert.Equal(t, 10, reservedUnits)

	var holds int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM inventory.reservations WHERE sku = 'HOT-SKU' AND status = 'held'`).Scan(&holds))
	assert.Equal(t, 10, holds)

	var movements int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM inventory.stock_movements WHERE sku = 'HOT-SKU' AND reason = 'reserve'`).Scan(&movements))
	assert.Equal(t, 10, movements)
}

func TestInventory_MultiLineReservationIsAtomic(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Pool(t)
	testdb.Truncate(t, pool, inventoryTables...)
	seedStock(ctx, t, pool, "SKU-A", 5)
	seedStock(ctx, t, pool, "SKU-B", 1)

	base := command.NewBase(postgres.NewUnitOfWork(pool, postgres.NewOutboxWriter()), clock.System{}, directory{}, application.DefaultPolicy())
	reserve := command.NewReserveStockHandler(base)
	release := command.NewReleaseReservationHandler(base)

	_, err := reserve.Handle(ctx, command.ReserveStock{
		ReservationID: domain.NewReservationID().String(),
		Lines:         []command.Line{{SKU: "SKU-A", Quantity: 2}, {SKU: "SKU-B", Quantity: 3}},
	})
	require.ErrorIs(t, err, domain.ErrInsufficientStock)

	var available int
	require.NoError(t, pool.QueryRow(ctx, `SELECT available FROM inventory.stock_items WHERE sku = 'SKU-A'`).Scan(&available))
	assert.Equal(t, 5, available)
	var holds int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM inventory.reservations`).Scan(&holds))
	assert.Zero(t, holds)

	reservation := domain.NewReservationID().String()
	result, err := reserve.Handle(ctx, command.ReserveStock{
		ReservationID: reservation,
		Lines:         []command.Line{{SKU: "SKU-A", Quantity: 2}, {SKU: "SKU-B", Quantity: 1}},
	})
	require.NoError(t, err)
	assert.True(t, result.ExpiresAt.After(time.Now().UTC()))

	_, err = release.Handle(ctx, command.ReleaseReservation{ReservationID: reservation})
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, `SELECT available FROM inventory.stock_items WHERE sku = 'SKU-A'`).Scan(&available))
	assert.Equal(t, 5, available)

	var events int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM platform.outbox WHERE event_name LIKE 'inventory.%'`).Scan(&events))
	assert.Positive(t, events)
}
