package pagination_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/pagination"
)

func TestKeysetRoundTrip(t *testing.T) {
	k := pagination.Keyset{At: time.Date(2026, 9, 15, 10, 0, 0, 123, time.UTC), ID: "0190f5a2-7c3e-7b1a-9c2d-1e2f3a4b5c6d"}
	got, ok, err := pagination.DecodeKeyset(pagination.EncodeKeyset(k))
	require.NoError(t, err)
	assert.True(t, ok)
	assert.True(t, k.At.Equal(got.At))
	assert.Equal(t, k.ID, got.ID)
}

func TestDecodeKeyset_Empty(t *testing.T) {
	_, ok, err := pagination.DecodeKeyset("")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestDecodeKeyset_Invalid(t *testing.T) {
	for _, c := range []string{"%%%", "e30", "bm90LWpzb24"} {
		_, _, err := pagination.DecodeKeyset(c)
		require.ErrorIs(t, err, pagination.ErrInvalidCursor, c)
	}
}

func TestBuild(t *testing.T) {
	type row struct {
		id string
		at time.Time
	}
	now := time.Now().UTC()
	rows := []row{{"a", now}, {"b", now.Add(-time.Minute)}, {"c", now.Add(-2 * time.Minute)}}
	keyOf := func(r row) pagination.Keyset { return pagination.Keyset{At: r.at, ID: r.id} }

	page := pagination.Build(rows, 2, keyOf)
	assert.Len(t, page.Items, 2)
	assert.True(t, page.HasMore)
	next, _, err := pagination.DecodeKeyset(page.NextCursor)
	require.NoError(t, err)
	assert.Equal(t, "b", next.ID)

	last := pagination.Build(rows[:2], 2, keyOf)
	assert.False(t, last.HasMore)
	assert.Empty(t, last.NextCursor)
}

func TestNormalizeLimit(t *testing.T) {
	assert.Equal(t, pagination.DefaultLimit, pagination.NormalizeLimit(0))
	assert.Equal(t, pagination.DefaultLimit, pagination.NormalizeLimit(pagination.MaxLimit+1))
	assert.Equal(t, 50, pagination.NormalizeLimit(50))
}
