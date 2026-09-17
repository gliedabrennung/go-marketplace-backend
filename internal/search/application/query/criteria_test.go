package query_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/search/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

func TestParseFilters(t *testing.T) {
	filters, err := query.ParseFilters(" color:black|white , memory_gb:256 ")
	require.NoError(t, err)
	assert.Equal(t, map[string][]string{"color": {"black", "white"}, "memory_gb": {"256"}}, filters)

	empty, err := query.ParseFilters("")
	require.NoError(t, err)
	assert.Empty(t, empty)

	for _, raw := range []string{"color", "color:", ":black", "color:black|"} {
		_, err := query.ParseFilters(raw)
		require.ErrorIs(t, err, query.ErrInvalidFilters, raw)
	}
}

func TestNormalizeSort(t *testing.T) {
	sort, err := query.NormalizeSort("")
	require.NoError(t, err)
	assert.Equal(t, query.SortRelevance, sort)

	sort, err = query.NormalizeSort(query.SortPriceAsc)
	require.NoError(t, err)
	assert.Equal(t, query.SortPriceAsc, sort)

	_, err = query.NormalizeSort("cheapest")
	require.ErrorIs(t, err, query.ErrInvalidSort)
}

func TestValidateFilters(t *testing.T) {
	require.NoError(t, query.ValidateFilters(query.Filters{
		CategoryID: kernel.NewID[struct{}]().String(), SellerID: kernel.NewSellerID().String(), PriceMin: 100, PriceMax: 200,
	}))
	require.ErrorIs(t, query.ValidateFilters(query.Filters{CategoryID: "nope"}), query.ErrInvalidCategory)
	require.ErrorIs(t, query.ValidateFilters(query.Filters{SellerID: "nope"}), query.ErrInvalidFilters)
	require.ErrorIs(t, query.ValidateFilters(query.Filters{PriceMin: 500, PriceMax: 100}), query.ErrInvalidPrice)
	require.ErrorIs(t, query.ValidateFilters(query.Filters{PriceMin: -1}), query.ErrInvalidPrice)
}

func TestCursorRoundTrip(t *testing.T) {
	hit := query.ProductHit{
		ProductID:   kernel.NewID[struct{}]().String(),
		Rank:        0.42,
		MinPrice:    199000,
		Offers:      3,
		PublishedAt: time.Date(2026, 9, 16, 10, 0, 0, 0, time.UTC),
	}

	relevance, err := query.DecodeCursor(query.EncodeCursor(query.SortRelevance, hit))
	require.NoError(t, err)
	assert.InDelta(t, hit.Rank, relevance.Rank, 0.0001)
	assert.Equal(t, hit.PublishedAt, relevance.At)

	price, err := query.DecodeCursor(query.EncodeCursor(query.SortPriceAsc, hit))
	require.NoError(t, err)
	assert.Equal(t, hit.MinPrice, price.Price)

	popular, err := query.DecodeCursor(query.EncodeCursor(query.SortPopular, hit))
	require.NoError(t, err)
	assert.Equal(t, hit.Offers, popular.Offers)

	newest, err := query.DecodeCursor(query.EncodeCursor(query.SortNew, hit))
	require.NoError(t, err)
	assert.Equal(t, hit.PublishedAt, newest.At)

	none, err := query.DecodeCursor("")
	require.NoError(t, err)
	assert.Nil(t, none)

	for _, raw := range []string{"%%%", "e30", "eyJpZCI6ImJhZCJ9"} {
		_, err := query.DecodeCursor(raw)
		require.ErrorIs(t, err, query.ErrInvalidCursor, raw)
	}
}

func TestWords(t *testing.T) {
	assert.Equal(t, []string{"смартфон", "nova"}, query.Words("Смартфон  Nova  X!"))
	assert.Empty(t, query.Words("  ?? "))
}
