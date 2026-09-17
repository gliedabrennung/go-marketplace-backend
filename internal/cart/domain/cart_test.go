package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/cart/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

var (
	now    = time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	limits = domain.Limits{MaxItems: 3, MaxQuantity: 10, AnonymousTTL: 24 * time.Hour}
)

func offer(sku string, price int64) domain.Offer {
	return domain.Offer{SKU: sku, SellerID: "seller-" + sku, Price: price, Currency: "KZT"}
}

func userCart(t *testing.T) *domain.Cart {
	t.Helper()
	owner, err := domain.UserOwner(kernel.NewUserID())
	require.NoError(t, err)
	cart, err := domain.New(domain.NewCartID(), owner, limits, now)
	require.NoError(t, err)
	return cart
}

func deviceCart(t *testing.T) *domain.Cart {
	t.Helper()
	owner, err := domain.DeviceOwner("device-0123456789abcdef")
	require.NoError(t, err)
	cart, err := domain.New(domain.NewCartID(), owner, limits, now)
	require.NoError(t, err)
	return cart
}

func TestOwners(t *testing.T) {
	_, err := domain.DeviceOwner("short")
	require.ErrorIs(t, err, domain.ErrInvalidDevice)
	_, err = domain.DeviceOwner("bad device id with spaces")
	require.ErrorIs(t, err, domain.ErrInvalidDevice)
	_, err = domain.UserOwner(kernel.UserID{})
	require.ErrorIs(t, err, kernel.ErrInvalidID)
	_, err = domain.New(domain.CartID{}, domain.Owner{}, limits, now)
	require.ErrorIs(t, err, kernel.ErrInvalidID)

	user := kernel.NewUserID()
	owner, err := domain.RehydrateOwner("user", user.String())
	require.NoError(t, err)
	assert.Equal(t, domain.OwnerUser, owner.Kind())
	assert.Equal(t, user.String(), owner.ID())
	assert.False(t, owner.IsAnonymous())
	device, err := domain.RehydrateOwner("device", "device-0123456789abcdef")
	require.NoError(t, err)
	assert.True(t, device.IsAnonymous())
	_, err = domain.RehydrateOwner("user", "bogus")
	require.Error(t, err)
	_, err = domain.RehydrateOwner("robot", "x")
	require.ErrorIs(t, err, domain.ErrInvalidDevice)

	assert.Equal(t, 100, domain.DefaultLimits().MaxItems)
	code, err := domain.NormalizePromoCode(" sale-10 ")
	require.NoError(t, err)
	assert.Equal(t, "SALE-10", code)
	_, err = domain.NormalizePromoCode("x")
	require.ErrorIs(t, err, domain.ErrInvalidPromoCode)
}

func TestAddAndUpdateItems(t *testing.T) {
	cart := userCart(t)
	assert.True(t, cart.IsEmpty())
	assert.True(t, cart.ExpiresAt().IsZero())

	require.NoError(t, cart.Add(offer("A", 1000), 2, 5, limits, now))
	require.NoError(t, cart.Add(offer("A", 1100), 3, 5, limits, now.Add(time.Minute)))
	item, ok := cart.Item("A")
	require.True(t, ok)
	assert.Equal(t, 5, item.Quantity())
	assert.Equal(t, int64(1100), item.Price())
	assert.Equal(t, "seller-A", item.SellerID())
	assert.Equal(t, now, item.AddedAt())
	assert.Equal(t, "KZT", cart.Currency())

	require.ErrorIs(t, cart.Add(offer("A", 1100), 1, 5, limits, now), domain.ErrExceedsStock)
	require.ErrorIs(t, cart.Add(offer("B", 10), 1, 0, limits, now), domain.ErrOfferUnavailable)
	require.ErrorIs(t, cart.Add(offer("B", 10), 0, 5, limits, now), domain.ErrInvalidQuantity)
	require.ErrorIs(t, cart.Add(offer("B", 10), 11, 50, limits, now), domain.ErrInvalidQuantity)
	require.ErrorIs(t, cart.Add(offer("B", 0), 1, 5, limits, now), domain.ErrInvalidPrice)
	require.ErrorIs(t, cart.Add(offer("bad sku", 10), 1, 5, limits, now), domain.ErrInvalidSKU)
	usd := offer("B", 10)
	usd.Currency = "USD"
	require.ErrorIs(t, cart.Add(usd, 1, 5, limits, now), domain.ErrCurrencyMismatch)

	require.NoError(t, cart.Add(offer("B", 10), 1, 5, limits, now))
	require.NoError(t, cart.Add(offer("C", 10), 1, 5, limits, now))
	require.ErrorIs(t, cart.Add(offer("D", 10), 1, 5, limits, now), domain.ErrTooManyItems)

	require.NoError(t, cart.SetQuantity(offer("B", 12), 4, 4, limits, now))
	item, _ = cart.Item("B")
	assert.Equal(t, 4, item.Quantity())
	require.ErrorIs(t, cart.SetQuantity(offer("B", 12), 5, 4, limits, now), domain.ErrExceedsStock)
	require.ErrorIs(t, cart.SetQuantity(offer("Z", 12), 1, 4, limits, now), domain.ErrItemNotFound)
	require.ErrorIs(t, cart.SetQuantity(offer("B", 0), 1, 4, limits, now), domain.ErrInvalidPrice)
	require.NoError(t, cart.SetQuantity(offer("B", 12), 0, 4, limits, now))
	_, ok = cart.Item("B")
	assert.False(t, ok)

	require.ErrorIs(t, cart.Remove("B", limits, now), domain.ErrItemNotFound)
	require.NoError(t, cart.Remove("C", limits, now))
	assert.Len(t, cart.Items(), 1)

	assert.False(t, cart.RemoveSKUs([]string{"Z"}, limits, now))
	assert.True(t, cart.RemoveSKUs([]string{"A", "Z"}, limits, now.Add(time.Hour)))
	assert.True(t, cart.IsEmpty())
	assert.Equal(t, now.Add(time.Hour), cart.UpdatedAt())
}

func TestPromoCodeAndExpiry(t *testing.T) {
	cart := deviceCart(t)
	assert.Equal(t, now.Add(24*time.Hour), cart.ExpiresAt())
	require.ErrorIs(t, cart.ApplyPromoCode("??", limits, now), domain.ErrInvalidPromoCode)
	require.NoError(t, cart.ApplyPromoCode("welcome", limits, now.Add(time.Hour)))
	assert.Equal(t, "WELCOME", cart.PromoCode())
	assert.Equal(t, now.Add(25*time.Hour), cart.ExpiresAt())
	cart.ClearPromoCode(limits, now)
	assert.Empty(t, cart.PromoCode())
}

func TestAbsorbAnonymousCart(t *testing.T) {
	user := userCart(t)
	require.NoError(t, user.Add(offer("A", 100), 2, 10, limits, now))
	require.NoError(t, user.Add(offer("B", 100), 9, 10, limits, now))

	device := deviceCart(t)
	require.NoError(t, device.Add(offer("A", 100), 3, 10, limits, now))
	require.NoError(t, device.Add(offer("B", 100), 5, 10, limits, now))
	require.NoError(t, device.Add(offer("C", 100), 4, 10, limits, now))
	require.NoError(t, device.ApplyPromoCode("SALE", limits, now))

	require.ErrorIs(t, user.Absorb(user, nil, limits, now), domain.ErrSameOwner)
	require.NoError(t, user.Absorb(device, map[string]int{"A": 4, "B": 12, "C": 2}, limits, now))

	quantities := map[string]int{}
	for _, item := range user.Items() {
		quantities[item.SKU()] = item.Quantity()
	}
	assert.Equal(t, map[string]int{"A": 4, "B": 10, "C": 2}, quantities)
	assert.Equal(t, "SALE", user.PromoCode())

	crowded := userCart(t)
	for _, sku := range []string{"X", "Y", "Z"} {
		require.NoError(t, crowded.Add(offer(sku, 100), 1, 10, limits, now))
	}
	require.NoError(t, crowded.ApplyPromoCode("MINE", limits, now))
	require.NoError(t, crowded.Absorb(device, map[string]int{"A": 10, "B": 10, "C": 10}, limits, now))
	assert.Len(t, crowded.Items(), 3)
	assert.Equal(t, "MINE", crowded.PromoCode())

	foreign := deviceCart(t)
	usd := offer("Q", 5)
	usd.Currency = "USD"
	require.NoError(t, foreign.Add(usd, 1, 10, limits, now))
	require.NoError(t, crowded.Absorb(foreign, map[string]int{"Q": 10}, limits, now))
	_, ok := crowded.Item("Q")
	assert.False(t, ok)

	empty := userCart(t)
	require.NoError(t, empty.Absorb(foreign, map[string]int{}, limits, now))
	assert.True(t, empty.IsEmpty())
	assert.Equal(t, "USD", empty.Currency())
}

func TestSnapshotRoundTrip(t *testing.T) {
	cart := deviceCart(t)
	require.NoError(t, cart.Add(offer("A", 100), 2, 10, limits, now))
	require.NoError(t, cart.ApplyPromoCode("SALE", limits, now))
	cart.AdvanceVersion()

	restored, err := domain.Rehydrate(cart.Snapshot())
	require.NoError(t, err)
	assert.Equal(t, cart.Snapshot(), restored.Snapshot())
	assert.Equal(t, cart.ID(), restored.ID())
	assert.Equal(t, cart.Owner(), restored.Owner())
	assert.Equal(t, 1, restored.Version())

	broken := cart.Snapshot()
	broken.ID = "bogus"
	_, err = domain.Rehydrate(broken)
	require.Error(t, err)
	broken = cart.Snapshot()
	broken.OwnerKind = "robot"
	_, err = domain.Rehydrate(broken)
	require.Error(t, err)
}
