//go:build integration

package inventory_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/inventory/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/inventory/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/inventory/infrastructure/memory"
	"github.com/gliedabrennung/go-marketplace-backend/internal/inventory/infrastructure/postgres"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/test/testdb"
)

var inventoryTables = []string{
	"inventory.stock_movements", "inventory.reservations", "inventory.stock_items", "platform.outbox",
}

func implementations() map[string]func(t *testing.T) application.Repositories {
	return map[string]func(t *testing.T) application.Repositories{
		"memory": func(*testing.T) application.Repositories { return memory.NewStore() },
		"postgres": func(t *testing.T) application.Repositories {
			pool := testdb.Pool(t)
			testdb.Truncate(t, pool, inventoryTables...)
			return postgres.NewRepositories(pool, postgres.NewOutboxWriter())
		},
	}
}

func now() time.Time {
	return time.Now().UTC().Truncate(time.Microsecond)
}

func sku(t *testing.T, raw string) domain.SKU {
	t.Helper()
	value, err := domain.NewSKU(raw)
	require.NoError(t, err)
	return value
}

func stocked(ctx context.Context, t *testing.T, repos application.Repositories, raw string, quantity int) (*domain.StockItem, kernel.SellerID) {
	t.Helper()
	seller := kernel.NewSellerID()
	item, err := domain.OpenStock(sku(t, raw), seller, now())
	require.NoError(t, err)
	require.NoError(t, item.Restock(seller, kernel.MustQuantity(quantity), "supply-"+raw, now()))
	require.NoError(t, repos.Stock().Save(ctx, item))
	return item, seller
}

func TestStockRepositoryContract(t *testing.T) {
	ctx := context.Background()
	for name, factory := range implementations() {
		t.Run(name, func(t *testing.T) {
			t.Run("not found", func(t *testing.T) {
				repos := factory(t)
				_, err := repos.Stock().FindBySKU(ctx, sku(t, "MISSING"))
				require.ErrorIs(t, err, domain.ErrStockNotFound)
				items, err := repos.Stock().Lock(ctx, []domain.SKU{sku(t, "MISSING")})
				require.NoError(t, err)
				assert.Empty(t, items)
				items, err = repos.Stock().LockByReservation(ctx, domain.NewReservationID())
				require.NoError(t, err)
				assert.Empty(t, items)
			})

			t.Run("round trip with holds and movements", func(t *testing.T) {
				repos := factory(t)
				item, seller := stocked(ctx, t, repos, "SKU-1", 10)

				reloaded, err := repos.Stock().FindBySKU(ctx, item.SKU())
				require.NoError(t, err)
				assert.Equal(t, item.Snapshot(), reloaded.Snapshot())
				assert.Equal(t, seller, reloaded.SellerID())

				reservation := domain.NewReservationID()
				require.NoError(t, reloaded.Reserve(reservation, domain.OrderID{}, kernel.MustQuantity(4), now().Add(time.Hour), now()))
				require.NoError(t, repos.Stock().Save(ctx, reloaded))

				held, err := repos.Stock().LockByReservation(ctx, reservation)
				require.NoError(t, err)
				require.Len(t, held, 1)
				assert.Equal(t, 6, held[0].Available().Value())
				assert.Equal(t, 4, held[0].Reserved().Value())

				require.NoError(t, held[0].Commit(reservation, now()))
				require.NoError(t, repos.Stock().Save(ctx, held[0]))
				committed, err := repos.Stock().FindBySKU(ctx, item.SKU())
				require.NoError(t, err)
				assert.Equal(t, 6, committed.Physical())
				hold, err := committed.Hold(reservation)
				require.NoError(t, err)
				assert.Equal(t, domain.HoldCommitted, hold.Status())

				require.ErrorIs(t, repos.Stock().Save(ctx, reloaded), kernel.ErrConcurrentModification)
			})

			t.Run("duplicate stock item", func(t *testing.T) {
				repos := factory(t)
				stocked(ctx, t, repos, "SKU-1", 1)
				twin, err := domain.OpenStock(sku(t, "SKU-1"), kernel.NewSellerID(), now())
				require.NoError(t, err)
				require.ErrorIs(t, repos.Stock().Save(ctx, twin), domain.ErrStockExists)
			})

			t.Run("expired holds are selected", func(t *testing.T) {
				repos := factory(t)
				fresh, _ := stocked(ctx, t, repos, "SKU-1", 5)
				stale, _ := stocked(ctx, t, repos, "SKU-2", 5)

				require.NoError(t, fresh.Reserve(domain.NewReservationID(), domain.OrderID{}, kernel.MustQuantity(1), now().Add(time.Hour), now()))
				require.NoError(t, repos.Stock().Save(ctx, fresh))
				expiring := domain.NewReservationID()
				require.NoError(t, stale.Reserve(expiring, domain.OrderID{}, kernel.MustQuantity(2), now().Add(time.Minute), now()))
				require.NoError(t, repos.Stock().Save(ctx, stale))

				items, err := repos.Stock().LockExpired(ctx, now().Add(2*time.Minute), 10)
				require.NoError(t, err)
				require.Len(t, items, 1)
				assert.Equal(t, "SKU-2", items[0].SKU().String())

				released, err := items[0].ExpireDue(now().Add(2 * time.Minute))
				require.NoError(t, err)
				assert.Equal(t, []domain.ReservationID{expiring}, released)
				require.NoError(t, repos.Stock().Save(ctx, items[0]))

				items, err = repos.Stock().LockExpired(ctx, now().Add(2*time.Minute), 10)
				require.NoError(t, err)
				assert.Empty(t, items)
			})

			t.Run("movements are idempotent", func(t *testing.T) {
				repos := factory(t)
				item, seller := stocked(ctx, t, repos, "SKU-1", 5)
				require.NoError(t, item.Restock(seller, kernel.MustQuantity(3), "supply-SKU-1", now()))
				require.NoError(t, repos.Stock().Save(ctx, item))

				reloaded, err := repos.Stock().FindBySKU(ctx, item.SKU())
				require.NoError(t, err)
				assert.Equal(t, 8, reloaded.Available().Value())
			})
		})
	}
}
