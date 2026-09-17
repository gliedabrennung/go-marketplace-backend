//go:build integration

package settlement_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/settlement/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/settlement/infrastructure/memory"
	"github.com/gliedabrennung/go-marketplace-backend/internal/settlement/infrastructure/postgres"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/test/testdb"
)

func implementations() map[string]func(t *testing.T) domain.Repository {
	return map[string]func(t *testing.T) domain.Repository{
		"memory": func(*testing.T) domain.Repository { return memory.NewStore().Entries() },
		"postgres": func(t *testing.T) domain.Repository {
			pool := testdb.Pool(t)
			testdb.Truncate(t, pool, "settlement.entries")
			return postgres.NewRepository(pool)
		},
	}
}

func money(amount int64) kernel.Money { return kernel.MustMoney(amount, kernel.KZT) }

func TestSettlementRepositoryContract(t *testing.T) {
	ctx := context.Background()
	for name, factory := range implementations() {
		t.Run(name, func(t *testing.T) {
			repo := factory(t)
			seller := kernel.NewSellerID()
			orderA, orderB := kernel.NewUserID().String(), kernel.NewUserID().String()
			at := time.Now().UTC().Truncate(time.Microsecond)

			_, err := repo.FindByOrderAndSeller(ctx, orderA, seller.String())
			require.ErrorIs(t, err, domain.ErrEntryNotFound)

			entry, err := domain.Accrue(domain.NewEntryID(), orderA, seller, money(10000), money(1000), at)
			require.NoError(t, err)
			require.NoError(t, repo.Save(ctx, entry))
			require.ErrorIs(t, repo.Save(ctx, entry), domain.ErrAlreadyAccrued)

			loaded, err := repo.FindByOrderAndSeller(ctx, orderA, seller.String())
			require.NoError(t, err)
			assert.Equal(t, entry.Snapshot(), loaded.Snapshot())

			other, err := domain.Accrue(domain.NewEntryID(), orderB, seller, money(5000), money(500), at.Add(time.Hour))
			require.NoError(t, err)
			require.NoError(t, repo.Save(ctx, other))

			entries, err := repo.ListBySeller(ctx, seller.String(), at.Add(-time.Minute), at.Add(2*time.Hour))
			require.NoError(t, err)
			require.Len(t, entries, 2)
			assert.Equal(t, orderA, entries[0].OrderID())
			assert.Equal(t, orderB, entries[1].OrderID())

			entries, err = repo.ListBySeller(ctx, seller.String(), at.Add(30*time.Minute), at.Add(2*time.Hour))
			require.NoError(t, err)
			require.Len(t, entries, 1)
			assert.Equal(t, orderB, entries[0].OrderID())

			entries, err = repo.ListBySeller(ctx, kernel.NewSellerID().String(), at.Add(-time.Hour), at.Add(time.Hour))
			require.NoError(t, err)
			assert.Empty(t, entries)
		})
	}
}
