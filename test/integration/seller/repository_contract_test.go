//go:build integration

package seller_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/infrastructure/memory"
	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/infrastructure/postgres"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/test/testdb"
)

const (
	validBIN   = "990140000384"
	otherBIN   = "050440001239"
	validIBAN  = "KZ86125KZT5004100100"
	secondIBAN = "KZ75125KZT2069100100"
)

var sellerTables = []string{
	"seller.commission_overrides", "seller.documents", "seller.members", "seller.sellers", "seller.category_commissions",
	"platform.outbox", "platform.idempotency_keys",
}

func implementations() map[string]func(t *testing.T) application.Repositories {
	return map[string]func(t *testing.T) application.Repositories{
		"memory": func(*testing.T) application.Repositories { return memory.NewStore() },
		"postgres": func(t *testing.T) application.Repositories {
			pool := testdb.Pool(t)
			testdb.Truncate(t, pool, sellerTables...)
			return postgres.NewRepositories(pool, postgres.NewOutboxWriter())
		},
	}
}

func now() time.Time {
	return time.Now().UTC().Truncate(time.Microsecond)
}

func richSeller(t *testing.T, taxID string) *domain.Seller {
	t.Helper()
	at := now()
	owner := kernel.NewUserID()
	legal, err := domain.NewLegalDetails("legal_entity", "ТОО Ромашка", taxID, "г. Алматы, пр. Абая 1")
	require.NoError(t, err)
	s, err := domain.OpenApplication(kernel.NewSellerID(), owner, legal, at)
	require.NoError(t, err)
	bank, err := domain.NewBankAccount(validIBAN, "HSBKKZKX", "Halyk Bank", "ТОО Ромашка")
	require.NoError(t, err)
	require.NoError(t, s.ChangeBankAccount(owner, bank, at))
	for _, kind := range []string{"registration_certificate", "bank_confirmation", "charter"} {
		doc, err := domain.NewDocument(kind, "sellers/"+kind+".pdf", at)
		require.NoError(t, err)
		require.NoError(t, s.AttachDocument(owner, doc, at))
	}
	require.NoError(t, s.AddMember(owner, kernel.NewUserID(), domain.MemberOperator, at))
	require.NoError(t, s.SubmitForReview(owner, at))
	require.NoError(t, s.Approve(kernel.NewUserID(), at))
	require.NoError(t, s.SetCommissionOverride(domain.NewCategoryID(), kernel.MustBasisPoints(750), kernel.NewUserID(), at))
	_, err = s.ApplyPerformance(domain.PerformanceMetrics{Orders: 30, LateShipments: 3, Reviews: 5, ReviewScoreSum: 22}, domain.DefaultRatingPolicy(), at)
	require.NoError(t, err)
	return s
}

func TestSellerRepositoryContract(t *testing.T) {
	ctx := context.Background()
	for name, factory := range implementations() {
		t.Run(name, func(t *testing.T) {
			t.Run("FindByID returns ErrSellerNotFound", func(t *testing.T) {
				_, err := factory(t).Sellers().FindByID(ctx, kernel.NewSellerID())
				require.ErrorIs(t, err, domain.ErrSellerNotFound)
			})

			t.Run("Save round-trips the whole aggregate and bumps version", func(t *testing.T) {
				repos := factory(t)
				s := richSeller(t, validBIN)
				require.NoError(t, repos.Sellers().Save(ctx, s))
				assert.Equal(t, 1, s.Version())
				assert.Empty(t, s.PullEvents())

				loaded, err := repos.Sellers().FindByID(ctx, s.ID())
				require.NoError(t, err)
				assert.Equal(t, s.Snapshot(), loaded.Snapshot())

				owner := loaded.OwnerID()
				bank, err := domain.NewBankAccount(secondIBAN, "CASPKZKA", "Kaspi Bank", "ТОО Ромашка")
				require.NoError(t, err)
				require.NoError(t, loaded.ChangeBankAccount(owner, bank, now()))
				require.NoError(t, loaded.RemoveMember(owner, loaded.Members()[1].UserID(), now()))
				require.NoError(t, repos.Sellers().Save(ctx, loaded))
				assert.Equal(t, 2, loaded.Version())

				reloaded, err := repos.Sellers().FindByID(ctx, s.ID())
				require.NoError(t, err)
				assert.Equal(t, loaded.Snapshot(), reloaded.Snapshot())
				assert.False(t, reloaded.PayoutsAllowed())
			})

			t.Run("stale version is rejected", func(t *testing.T) {
				repos := factory(t)
				s := richSeller(t, validBIN)
				stale, err := domain.RehydrateSeller(s.Snapshot())
				require.NoError(t, err)
				require.NoError(t, repos.Sellers().Save(ctx, s))
				require.ErrorIs(t, repos.Sellers().Save(ctx, stale), kernel.ErrConcurrentModification)

				first, err := repos.Sellers().FindByID(ctx, s.ID())
				require.NoError(t, err)
				second, err := repos.Sellers().FindByID(ctx, s.ID())
				require.NoError(t, err)
				require.NoError(t, first.Suspend(kernel.NewUserID(), domain.SuspendedManually, "fraud", now()))
				require.NoError(t, second.Terminate(kernel.NewUserID(), "closed", now()))
				require.NoError(t, repos.Sellers().Save(ctx, first))
				require.ErrorIs(t, repos.Sellers().Save(ctx, second), kernel.ErrConcurrentModification)
			})

			t.Run("owner and tax id are unique among non-terminated sellers", func(t *testing.T) {
				repos := factory(t)
				first := richSeller(t, validBIN)
				require.NoError(t, repos.Sellers().Save(ctx, first))

				sameTax := richSeller(t, validBIN)
				require.ErrorIs(t, repos.Sellers().Save(ctx, sameTax), domain.ErrTaxIDTaken)

				legal, err := domain.NewLegalDetails("legal_entity", "Второе ТОО", otherBIN, "г. Астана, ул. 2")
				require.NoError(t, err)
				sameOwner, err := domain.OpenApplication(kernel.NewSellerID(), first.OwnerID(), legal, now())
				require.NoError(t, err)
				require.ErrorIs(t, repos.Sellers().Save(ctx, sameOwner), domain.ErrSellerAlreadyExists)

				require.NoError(t, first.Terminate(kernel.NewUserID(), "closed", now()))
				require.NoError(t, repos.Sellers().Save(ctx, first))
				fresh, err := domain.RehydrateSeller(sameTax.Snapshot())
				require.NoError(t, err)
				require.NoError(t, repos.Sellers().Save(ctx, fresh))
				freshOwner, err := domain.RehydrateSeller(sameOwner.Snapshot())
				require.NoError(t, err)
				require.NoError(t, repos.Sellers().Save(ctx, freshOwner), "terminated seller releases its owner")
			})
		})
	}
}

func TestCategoryCommissionRepositoryContract(t *testing.T) {
	ctx := context.Background()
	for name, factory := range implementations() {
		t.Run(name, func(t *testing.T) {
			repos := factory(t)
			category := domain.NewCategoryID()
			_, err := repos.CategoryCommissions().FindByCategory(ctx, category)
			require.ErrorIs(t, err, domain.ErrCategoryCommissionNotFound)

			c, err := domain.SetCategoryCommission(category, kernel.MustBasisPoints(1200), kernel.NewUserID(), now())
			require.NoError(t, err)
			stale, err := domain.RehydrateCategoryCommission(category.String(), 1200, c.UpdatedAt(), 0)
			require.NoError(t, err)
			require.NoError(t, repos.CategoryCommissions().Save(ctx, c))
			require.ErrorIs(t, repos.CategoryCommissions().Save(ctx, stale), kernel.ErrConcurrentModification)

			loaded, err := repos.CategoryCommissions().FindByCategory(ctx, category)
			require.NoError(t, err)
			assert.Equal(t, 1200, loaded.Rate().Value())
			assert.Equal(t, c.UpdatedAt(), loaded.UpdatedAt())
			loaded.Change(kernel.MustBasisPoints(900), kernel.NewUserID(), now())
			require.NoError(t, repos.CategoryCommissions().Save(ctx, loaded))
			assert.Equal(t, 2, loaded.Version())
		})
	}
}
