package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/settlement/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

var now = time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)

func money(amount int64) kernel.Money { return kernel.MustMoney(amount, kernel.KZT) }

func TestAccrue(t *testing.T) {
	seller := kernel.NewSellerID()
	entry, err := domain.Accrue(domain.NewEntryID(), "order-1", seller, money(10000), money(1000), now)
	require.NoError(t, err)
	assert.Equal(t, int64(10000), entry.Gross().Amount())
	assert.Equal(t, int64(1000), entry.Commission().Amount())
	assert.Equal(t, int64(9000), entry.Net().Amount())
	assert.Equal(t, seller, entry.SellerID())
	assert.Equal(t, "order-1", entry.OrderID())
	assert.Equal(t, now, entry.AccruedAt())

	_, err = domain.Accrue(domain.EntryID{}, "order-1", seller, money(10000), money(1000), now)
	require.ErrorIs(t, err, kernel.ErrInvalidID)
	_, err = domain.Accrue(domain.NewEntryID(), "", seller, money(10000), money(1000), now)
	require.ErrorIs(t, err, kernel.ErrInvalidID)
	_, err = domain.Accrue(domain.NewEntryID(), "order-1", kernel.SellerID{}, money(10000), money(1000), now)
	require.ErrorIs(t, err, kernel.ErrInvalidID)
	_, err = domain.Accrue(domain.NewEntryID(), "order-1", seller, money(1000), money(10000), now)
	require.ErrorIs(t, err, domain.ErrInvalidAmount)
	usd := kernel.MustMoney(100, kernel.Currency("USD"))
	_, err = domain.Accrue(domain.NewEntryID(), "order-1", seller, money(10000), usd, now)
	require.ErrorIs(t, err, domain.ErrInvalidAmount)
}

func TestSnapshotRoundTrip(t *testing.T) {
	seller := kernel.NewSellerID()
	entry, err := domain.Accrue(domain.NewEntryID(), "order-1", seller, money(10000), money(1000), now)
	require.NoError(t, err)
	restored, err := domain.RehydrateEntry(entry.Snapshot())
	require.NoError(t, err)
	assert.Equal(t, entry.Snapshot(), restored.Snapshot())

	broken := entry.Snapshot()
	broken.ID = "x"
	_, err = domain.RehydrateEntry(broken)
	require.Error(t, err)
	broken = entry.Snapshot()
	broken.SellerID = "x"
	_, err = domain.RehydrateEntry(broken)
	require.Error(t, err)
	broken = entry.Snapshot()
	broken.Currency = "x"
	_, err = domain.RehydrateEntry(broken)
	require.Error(t, err)
}
