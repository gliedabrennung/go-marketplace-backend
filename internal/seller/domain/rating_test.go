package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

var at = time.Date(2026, 9, 16, 10, 0, 0, 0, time.UTC)

func TestRatingPolicy_Calculate(t *testing.T) {
	policy := domain.DefaultRatingPolicy()

	perfect, err := policy.Calculate(domain.PerformanceMetrics{Orders: 100, Reviews: 10, ReviewScoreSum: 50}, at)
	require.NoError(t, err)
	assert.Equal(t, 100, perfect.Score())
	assert.Equal(t, 500, perfect.AverageReview())
	assert.False(t, perfect.Provisional())
	assert.Equal(t, 100, perfect.Orders())
	assert.Equal(t, at, perfect.CalculatedAt())

	poor, err := policy.Calculate(domain.PerformanceMetrics{Orders: 100, CancelledBySeller: 20, LateShipments: 25, Reviews: 10, ReviewScoreSum: 10}, at)
	require.NoError(t, err)
	assert.Zero(t, poor.Score())
	assert.Equal(t, kernel.MustBasisPoints(2000), poor.CancellationRate())
	assert.Equal(t, kernel.MustBasisPoints(2500), poor.LateShipmentRate())
	assert.True(t, policy.RequiresSuspension(poor))

	mixed, err := policy.Calculate(domain.PerformanceMetrics{Orders: 50, CancelledBySeller: 5, LateShipments: 5, Reviews: 20, ReviewScoreSum: 80}, at)
	require.NoError(t, err)
	assert.Equal(t, (50*35+60*25+75*40)/100, mixed.Score())
	assert.False(t, policy.RequiresSuspension(mixed))

	fresh, err := policy.Calculate(domain.PerformanceMetrics{Orders: 3, CancelledBySeller: 3}, at)
	require.NoError(t, err)
	assert.True(t, fresh.Provisional())
	assert.Equal(t, (0*35+100*25+80*40)/100, fresh.Score())
	assert.False(t, policy.RequiresSuspension(fresh), "provisional rating never suspends")

	none, err := policy.Calculate(domain.PerformanceMetrics{}, at)
	require.NoError(t, err)
	assert.Zero(t, none.CancellationRate().Value())
	assert.False(t, none.IsZero())
}

func TestPerformanceMetrics_Validate(t *testing.T) {
	invalid := []domain.PerformanceMetrics{
		{Orders: -1},
		{Orders: 1, CancelledBySeller: 2},
		{Orders: 1, LateShipments: 2},
		{Reviews: 2, ReviewScoreSum: 1},
		{Reviews: 1, ReviewScoreSum: 6},
	}
	for _, m := range invalid {
		_, err := domain.DefaultRatingPolicy().Calculate(m, at)
		require.ErrorIs(t, err, domain.ErrInvalidMetrics, "%+v", m)
	}
}

func TestRatingPolicy_Validate(t *testing.T) {
	for _, p := range []domain.RatingPolicy{
		{},
		{MinOrders: -1, CancellationWeight: 1},
		{SuspendBelow: 101, CancellationWeight: 1},
		{CancellationWeight: -1, ReviewWeight: 2},
	} {
		_, err := p.Calculate(domain.PerformanceMetrics{}, at)
		require.ErrorIs(t, err, domain.ErrInvalidRatingPolicy, "%+v", p)
	}
}

func TestRating_ScoreIsAlwaysWithinBounds(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		orders := rapid.IntRange(0, 100_000).Draw(t, "orders")
		reviews := rapid.IntRange(0, 10_000).Draw(t, "reviews")
		m := domain.PerformanceMetrics{
			Orders:            orders,
			CancelledBySeller: rapid.IntRange(0, orders).Draw(t, "cancelled"),
			LateShipments:     rapid.IntRange(0, orders).Draw(t, "late"),
			Reviews:           reviews,
			ReviewScoreSum:    rapid.IntRange(reviews, 5*reviews).Draw(t, "scores"),
		}
		r, err := domain.DefaultRatingPolicy().Calculate(m, at)
		if err != nil {
			t.Fatal(err)
		}
		if r.Score() < 0 || r.Score() > 100 {
			t.Fatalf("score %d out of bounds for %+v", r.Score(), m)
		}
	})
}
