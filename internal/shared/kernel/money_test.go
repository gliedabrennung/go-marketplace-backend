package kernel_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

func TestNewMoney_RejectsNegativeAmount(t *testing.T) {
	_, err := kernel.NewMoney(-1, kernel.KZT)
	require.ErrorIs(t, err, kernel.ErrNegativeAmount)
}

func TestNewMoney_RequiresCurrency(t *testing.T) {
	_, err := kernel.NewMoney(100, "")
	require.ErrorIs(t, err, kernel.ErrCurrencyRequired)
}

func TestNewMoney_RejectsMalformedCurrency(t *testing.T) {
	for _, c := range []kernel.Currency{"kzt", "KZ", "KZTT", "K1T"} {
		_, err := kernel.NewMoney(100, c)
		require.ErrorIs(t, err, kernel.ErrInvalidCurrency, string(c))
	}
}

func TestMoney_AddSameCurrency(t *testing.T) {
	sum, err := kernel.MustMoney(150, kernel.KZT).Add(kernel.MustMoney(250, kernel.KZT))
	require.NoError(t, err)
	assert.True(t, sum.Equals(kernel.MustMoney(400, kernel.KZT)))
}

func TestMoney_AddCurrencyMismatch(t *testing.T) {
	_, err := kernel.MustMoney(1, kernel.KZT).Add(kernel.MustMoney(1, kernel.USD))
	require.ErrorIs(t, err, kernel.ErrCurrencyMismatch)
}

func TestMoney_AddOverflow(t *testing.T) {
	_, err := kernel.MustMoney(math.MaxInt64, kernel.KZT).Add(kernel.MustMoney(1, kernel.KZT))
	require.ErrorIs(t, err, kernel.ErrMoneyOverflow)
}

func TestMoney_SubNeverNegative(t *testing.T) {
	_, err := kernel.MustMoney(10, kernel.KZT).Sub(kernel.MustMoney(11, kernel.KZT))
	require.ErrorIs(t, err, kernel.ErrNegativeAmount)
}

func TestMoney_SubCurrencyMismatch(t *testing.T) {
	_, err := kernel.MustMoney(10, kernel.KZT).Sub(kernel.MustMoney(1, kernel.RUB))
	require.ErrorIs(t, err, kernel.ErrCurrencyMismatch)
}

func TestMoney_MulQuantity(t *testing.T) {
	m, err := kernel.MustMoney(1999, kernel.KZT).MulQuantity(kernel.MustQuantity(3))
	require.NoError(t, err)
	assert.Equal(t, int64(5997), m.Amount())
}

func TestMoney_MulQuantityByZero(t *testing.T) {
	m, err := kernel.MustMoney(math.MaxInt64, kernel.KZT).MulQuantity(kernel.MustQuantity(0))
	require.NoError(t, err)
	assert.True(t, m.IsZero())
}

func TestMoney_MulQuantityOverflow(t *testing.T) {
	_, err := kernel.MustMoney(math.MaxInt64/2+1, kernel.KZT).MulQuantity(kernel.MustQuantity(2))
	require.ErrorIs(t, err, kernel.ErrMoneyOverflow)
}

func TestMoney_ApplyPercentRoundsDownInFavourOfBuyer(t *testing.T) {
	cases := []struct {
		amount int64
		bp     int
		want   int64
	}{
		{amount: 1000, bp: 1000, want: 100},
		{amount: 999, bp: 1000, want: 99},
		{amount: 1, bp: 5000, want: 0},
		{amount: 12345, bp: 10000, want: 12345},
		{amount: 12345, bp: 0, want: 0},
		{amount: math.MaxInt64, bp: 10000, want: math.MaxInt64},
	}
	for _, tc := range cases {
		got := kernel.MustMoney(tc.amount, kernel.KZT).ApplyPercent(kernel.MustBasisPoints(tc.bp))
		assert.Equal(t, tc.want, got.Amount(), "amount=%d bp=%d", tc.amount, tc.bp)
	}
}

func TestMoney_ApplyPercentNeverExceedsAmount(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		amount := rapid.Int64Range(0, math.MaxInt64).Draw(t, "amount")
		bp := rapid.IntRange(0, kernel.MaxBasisPoints).Draw(t, "bp")
		got := kernel.MustMoney(amount, kernel.KZT).ApplyPercent(kernel.MustBasisPoints(bp))
		if got.Amount() < 0 || got.Amount() > amount {
			t.Fatalf("discount %d out of range for amount %d", got.Amount(), amount)
		}
	})
}

func TestMoney_AddSubRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a := rapid.Int64Range(0, math.MaxInt64/2).Draw(t, "a")
		b := rapid.Int64Range(0, math.MaxInt64/2).Draw(t, "b")
		ma := kernel.MustMoney(a, kernel.KZT)
		sum, err := ma.Add(kernel.MustMoney(b, kernel.KZT))
		if err != nil {
			t.Fatal(err)
		}
		back, err := sum.Sub(kernel.MustMoney(b, kernel.KZT))
		if err != nil {
			t.Fatal(err)
		}
		if !back.Equals(ma) {
			t.Fatalf("round trip mismatch: %s != %s", back, ma)
		}
	})
}

func TestMoney_Compare(t *testing.T) {
	a := kernel.MustMoney(10, kernel.KZT)
	b := kernel.MustMoney(20, kernel.KZT)
	c, err := a.Compare(b)
	require.NoError(t, err)
	assert.Equal(t, -1, c)
	c, err = b.Compare(a)
	require.NoError(t, err)
	assert.Equal(t, 1, c)
	c, err = a.Compare(a)
	require.NoError(t, err)
	assert.Equal(t, 0, c)
	_, err = a.Compare(kernel.MustMoney(10, kernel.USD))
	require.ErrorIs(t, err, kernel.ErrCurrencyMismatch)
}

func TestMoney_String(t *testing.T) {
	assert.Equal(t, "12.05 KZT", kernel.MustMoney(1205, kernel.KZT).String())
}

func TestSumMoney(t *testing.T) {
	total, err := kernel.SumMoney(kernel.KZT, kernel.MustMoney(1, kernel.KZT), kernel.MustMoney(2, kernel.KZT))
	require.NoError(t, err)
	assert.Equal(t, int64(3), total.Amount())

	_, err = kernel.SumMoney(kernel.KZT, kernel.MustMoney(1, kernel.USD))
	require.ErrorIs(t, err, kernel.ErrCurrencyMismatch)
}

func TestNewBasisPoints_Bounds(t *testing.T) {
	_, err := kernel.NewBasisPoints(-1)
	require.ErrorIs(t, err, kernel.ErrInvalidBasis)
	_, err = kernel.NewBasisPoints(10_001)
	require.ErrorIs(t, err, kernel.ErrInvalidBasis)
	bp, err := kernel.NewBasisPoints(1250)
	require.NoError(t, err)
	assert.Equal(t, "12.50%", bp.String())
}
