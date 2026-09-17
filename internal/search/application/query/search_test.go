package query_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/search/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/search/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

var ctx = context.Background()

type reader struct {
	criteria    []query.Criteria
	hits        map[string][]query.ProductHit
	suggestions map[string]string
	facets      query.FacetSet
	err         error
}

func (r *reader) Search(_ context.Context, criteria query.Criteria) ([]query.ProductHit, error) {
	r.criteria = append(r.criteria, criteria)
	if r.err != nil {
		return nil, r.err
	}
	found := r.hits[criteria.Query]
	if criteria.After == nil {
		return found, nil
	}
	at := slices.IndexFunc(found, func(hit query.ProductHit) bool { return hit.ProductID == criteria.After.ID })
	return found[at+1:], nil
}

func (r *reader) Facets(_ context.Context, _ query.Filters, _ int) (query.FacetSet, error) {
	return r.facets, r.err
}

func (r *reader) Suggest(_ context.Context, words []string, _ float64) (map[string]string, error) {
	if r.err != nil {
		return nil, r.err
	}
	out := map[string]string{}
	for _, word := range words {
		if suggestion, ok := r.suggestions[word]; ok {
			out[word] = suggestion
		}
	}
	return out, nil
}

func hits(count int) []query.ProductHit {
	out := make([]query.ProductHit, 0, count)
	for i := range count {
		out = append(out, query.ProductHit{
			ProductID:   kernel.NewID[struct{}]().String(),
			Title:       "Смартфон",
			MinPrice:    int64(100000 + i),
			PublishedAt: time.Date(2026, 9, 16, 10, 0, 0, 0, time.UTC),
		})
	}
	return out
}

func TestSearchProducts_PageAndFilters(t *testing.T) {
	source := &reader{hits: map[string][]query.ProductHit{"смартфон": hits(3)}}
	handler := query.NewSearchProductsHandler(source, application.DefaultPolicy())

	result, err := handler.Handle(ctx, query.SearchProducts{
		Query: " смартфон ", Filters: "color:black", Sort: query.SortPriceAsc, Limit: 2, InStock: true, PriceMin: 1000,
	})
	require.NoError(t, err)
	assert.Len(t, result.Page.Items, 2)
	assert.True(t, result.Page.HasMore)
	assert.NotEmpty(t, result.Page.NextCursor)
	assert.Empty(t, result.Corrected)

	require.Len(t, source.criteria, 1)
	criteria := source.criteria[0]
	assert.Equal(t, "смартфон", criteria.Query)
	assert.Equal(t, map[string][]string{"color": {"black"}}, criteria.Attributes)
	assert.True(t, criteria.InStock)
	assert.Equal(t, int64(1000), criteria.PriceMin)

	next, err := handler.Handle(ctx, query.SearchProducts{Query: "смартфон", Sort: query.SortPriceAsc, Limit: 2, Cursor: result.Page.NextCursor})
	require.NoError(t, err)
	require.Len(t, source.criteria, 2)
	require.NotNil(t, source.criteria[1].After)
	assert.Equal(t, result.Page.Items[1].ProductID, source.criteria[1].After.ID)
	assert.False(t, next.Page.HasMore)
}

func TestSearchProducts_TypoCorrection(t *testing.T) {
	source := &reader{
		hits:        map[string][]query.ProductHit{"смартфон nova": hits(1)},
		suggestions: map[string]string{"смартфпн": "смартфон"},
	}
	handler := query.NewSearchProductsHandler(source, application.DefaultPolicy())

	result, err := handler.Handle(ctx, query.SearchProducts{Query: "смартфпн nova"})
	require.NoError(t, err)
	assert.Equal(t, "смартфон nova", result.Corrected)
	require.Len(t, result.Page.Items, 1)

	source.suggestions = map[string]string{}
	result, err = handler.Handle(ctx, query.SearchProducts{Query: "смартфпн nova"})
	require.NoError(t, err)
	assert.Empty(t, result.Corrected)
	assert.Empty(t, result.Page.Items)

	source.suggestions = map[string]string{"смартфпн": "камера"}
	result, err = handler.Handle(ctx, query.SearchProducts{Query: "смартфпн nova"})
	require.NoError(t, err)
	assert.Empty(t, result.Corrected)
}

func TestSearchProducts_Validation(t *testing.T) {
	source := &reader{}
	handler := query.NewSearchProductsHandler(source, application.DefaultPolicy())

	_, err := handler.Handle(ctx, query.SearchProducts{Filters: "broken"})
	require.ErrorIs(t, err, query.ErrInvalidFilters)
	_, err = handler.Handle(ctx, query.SearchProducts{Sort: "cheap"})
	require.ErrorIs(t, err, query.ErrInvalidSort)
	_, err = handler.Handle(ctx, query.SearchProducts{Cursor: "%%%"})
	require.ErrorIs(t, err, query.ErrInvalidCursor)
	_, err = handler.Handle(ctx, query.SearchProducts{CategoryID: "bad"})
	require.ErrorIs(t, err, query.ErrInvalidCategory)

	source.err = errors.New("database is down")
	_, err = handler.Handle(ctx, query.SearchProducts{})
	require.ErrorContains(t, err, "database is down")
}

func TestCategoryFacets(t *testing.T) {
	source := &reader{facets: query.FacetSet{Total: 2, PriceMin: 100, PriceMax: 900}}
	handler := query.NewCategoryFacetsHandler(source, application.DefaultPolicy())

	set, err := handler.Handle(ctx, query.CategoryFacets{CategoryID: kernel.NewID[struct{}]().String(), Filters: "color:black"})
	require.NoError(t, err)
	assert.Equal(t, 2, set.Total)

	_, err = handler.Handle(ctx, query.CategoryFacets{})
	require.ErrorIs(t, err, query.ErrInvalidCategory)
	_, err = handler.Handle(ctx, query.CategoryFacets{CategoryID: "bad"})
	require.ErrorIs(t, err, query.ErrInvalidCategory)
	_, err = handler.Handle(ctx, query.CategoryFacets{CategoryID: kernel.NewID[struct{}]().String(), Filters: "broken"})
	require.ErrorIs(t, err, query.ErrInvalidFilters)
}
