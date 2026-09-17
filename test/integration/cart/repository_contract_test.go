//go:build integration

package cart_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/cart/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/cart/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/cart/infrastructure/memory"
	"github.com/gliedabrennung/go-marketplace-backend/internal/cart/infrastructure/postgres"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/test/testdb"
)

func implementations() map[string]func(t *testing.T) application.Repositories {
	return map[string]func(t *testing.T) application.Repositories{
		"memory": func(*testing.T) application.Repositories { return memory.NewStore() },
		"postgres": func(t *testing.T) application.Repositories {
			pool := testdb.Pool(t)
			testdb.Truncate(t, pool, "cart.items", "cart.carts")
			return postgres.NewRepositories(pool)
		},
	}
}

func offer(sku string, price int64) domain.Offer {
	return domain.Offer{SKU: sku, SellerID: kernel.NewSellerID().String(), Price: price, Currency: "KZT"}
}

func TestCartRepositoryContract(t *testing.T) {
	ctx := context.Background()
	limits := domain.DefaultLimits()
	for name, factory := range implementations() {
		t.Run(name, func(t *testing.T) {
			repos := factory(t)
			at := time.Now().UTC().Truncate(time.Microsecond)
			owner, err := domain.DeviceOwner("device-" + name + "-0123456789")
			require.NoError(t, err)
			_, err = repos.Carts().FindByOwner(ctx, owner)
			require.ErrorIs(t, err, domain.ErrCartNotFound)

			cart, err := domain.New(domain.NewCartID(), owner, limits, at)
			require.NoError(t, err)
			require.NoError(t, cart.Add(offer("SKU-B", 500), 2, 10, limits, at))
			require.NoError(t, cart.Add(offer("SKU-A", 900), 1, 10, limits, at))
			require.NoError(t, cart.ApplyPromoCode("SALE", limits, at))
			require.NoError(t, repos.Carts().Save(ctx, cart))

			loaded, err := repos.Carts().FindByOwner(ctx, owner)
			require.NoError(t, err)
			assert.Equal(t, cart.Snapshot(), loaded.Snapshot())

			duplicate, err := domain.New(domain.NewCartID(), owner, limits, at)
			require.NoError(t, err)
			require.ErrorIs(t, repos.Carts().Save(ctx, duplicate), kernel.ErrConcurrentModification)

			require.NoError(t, loaded.Remove("SKU-B", limits, at.Add(time.Second)))
			require.NoError(t, repos.Carts().Save(ctx, loaded))
			require.ErrorIs(t, repos.Carts().Save(ctx, cart), kernel.ErrConcurrentModification)
			reloaded, err := repos.Carts().FindByOwner(ctx, owner)
			require.NoError(t, err)
			assert.Len(t, reloaded.Items(), 1)

			user, err := domain.UserOwner(kernel.NewUserID())
			require.NoError(t, err)
			permanent, err := domain.New(domain.NewCartID(), user, limits, at)
			require.NoError(t, err)
			require.NoError(t, repos.Carts().Save(ctx, permanent))

			removed, err := repos.Carts().DeleteExpired(ctx, at.Add(limits.AnonymousTTL+time.Hour), 10)
			require.NoError(t, err)
			assert.Equal(t, 1, removed)
			_, err = repos.Carts().FindByOwner(ctx, owner)
			require.ErrorIs(t, err, domain.ErrCartNotFound)

			stored, err := repos.Carts().FindByOwner(ctx, user)
			require.NoError(t, err)
			require.NoError(t, stored.ApplyPromoCode("WELCOME", limits, at))
			require.NoError(t, repos.Carts().Save(ctx, stored))
			require.ErrorIs(t, repos.Carts().Delete(ctx, permanent), kernel.ErrConcurrentModification)
			require.NoError(t, repos.Carts().Delete(ctx, stored))
			_, err = repos.Carts().FindByOwner(ctx, user)
			require.ErrorIs(t, err, domain.ErrCartNotFound)
		})
	}
}
