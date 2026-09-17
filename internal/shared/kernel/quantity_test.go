package kernel_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

func TestNewQuantity_Bounds(t *testing.T) {
	_, err := kernel.NewQuantity(-1)
	require.ErrorIs(t, err, kernel.ErrNegativeQuantity)
	_, err = kernel.NewQuantity(kernel.MaxQuantity + 1)
	require.ErrorIs(t, err, kernel.ErrQuantityOverflow)
	q, err := kernel.NewQuantity(0)
	require.NoError(t, err)
	assert.True(t, q.IsZero())
}

func TestQuantity_Arithmetic(t *testing.T) {
	sum, err := kernel.MustQuantity(3).Add(kernel.MustQuantity(4))
	require.NoError(t, err)
	assert.Equal(t, 7, sum.Value())

	diff, err := sum.Sub(kernel.MustQuantity(7))
	require.NoError(t, err)
	assert.True(t, diff.IsZero())

	_, err = kernel.MustQuantity(1).Sub(kernel.MustQuantity(2))
	require.ErrorIs(t, err, kernel.ErrNegativeQuantity)

	_, err = kernel.MustQuantity(kernel.MaxQuantity).Add(kernel.MustQuantity(1))
	require.ErrorIs(t, err, kernel.ErrQuantityOverflow)
}

func TestQuantity_Comparison(t *testing.T) {
	assert.True(t, kernel.MustQuantity(1).LessThan(kernel.MustQuantity(2)))
	assert.False(t, kernel.MustQuantity(2).LessThan(kernel.MustQuantity(2)))
	assert.True(t, kernel.MustQuantity(2).Equals(kernel.MustQuantity(2)))
}
