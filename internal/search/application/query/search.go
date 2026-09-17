package query

import (
	"context"
	"strings"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/search/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/pagination"
)

type ProductHit struct {
	ProductID   string
	SellerID    string
	CategoryID  string
	Title       string
	Brand       string
	CoverKey    string
	MinPrice    int64
	Currency    string
	Offers      int
	Sellers     int
	InStock     bool
	PublishedAt time.Time
	Rank        float64
}

type FacetValue struct {
	Value string
	Count int
}

type AttributeFacet struct {
	Code   string
	Values []FacetValue
}

type NumberFacet struct {
	Code string
	Min  float64
	Max  float64
}

type FacetSet struct {
	Total      int
	Attributes []AttributeFacet
	Numbers    []NumberFacet
	Conditions []FacetValue
	PriceMin   int64
	PriceMax   int64
}

type ReadModel interface {
	Search(ctx context.Context, criteria Criteria) ([]ProductHit, error)
	Facets(ctx context.Context, filters Filters, limit int) (FacetSet, error)
	Suggest(ctx context.Context, words []string, similarity float64) (map[string]string, error)
}

type SearchProducts struct {
	Query      string
	CategoryID string
	Filters    string
	PriceMin   int64
	PriceMax   int64
	SellerID   string
	Condition  string
	InStock    bool
	Sort       string
	Limit      int
	Cursor     string
}

type SearchResult struct {
	Page      pagination.Page[ProductHit]
	Corrected string
}

type SearchProductsHandler struct {
	reader ReadModel
	policy application.Policy
}

func NewSearchProductsHandler(reader ReadModel, policy application.Policy) *SearchProductsHandler {
	return &SearchProductsHandler{reader: reader, policy: policy}
}

func (h *SearchProductsHandler) Handle(ctx context.Context, q SearchProducts) (SearchResult, error) {
	criteria, err := h.criteria(q)
	if err != nil {
		return SearchResult{}, err
	}
	hits, err := h.reader.Search(ctx, criteria)
	if err != nil {
		return SearchResult{}, err
	}
	corrected := ""
	if len(hits) == 0 && criteria.Query != "" && criteria.After == nil {
		corrected, hits, err = h.retryCorrected(ctx, criteria)
		if err != nil {
			return SearchResult{}, err
		}
	}
	page := pagination.Build(hits, criteria.Limit, func(hit ProductHit) pagination.Keyset {
		return pagination.Keyset{At: hit.PublishedAt, ID: hit.ProductID}
	})
	if page.HasMore {
		page.NextCursor = EncodeCursor(criteria.Sort, page.Items[len(page.Items)-1])
	}
	return SearchResult{Page: page, Corrected: corrected}, nil
}

func (h *SearchProductsHandler) criteria(q SearchProducts) (Criteria, error) {
	attributes, err := ParseFilters(q.Filters)
	if err != nil {
		return Criteria{}, err
	}
	sort, err := NormalizeSort(q.Sort)
	if err != nil {
		return Criteria{}, err
	}
	cursor, err := DecodeCursor(q.Cursor)
	if err != nil {
		return Criteria{}, err
	}
	filters := Filters{
		Query: strings.TrimSpace(q.Query), CategoryID: q.CategoryID, Attributes: attributes,
		PriceMin: q.PriceMin, PriceMax: q.PriceMax, SellerID: q.SellerID, Condition: q.Condition, InStock: q.InStock,
	}
	if err := ValidateFilters(filters); err != nil {
		return Criteria{}, err
	}
	return Criteria{Filters: filters, Sort: sort, Limit: pagination.NormalizeLimit(q.Limit), After: cursor}, nil
}

func (h *SearchProductsHandler) retryCorrected(ctx context.Context, criteria Criteria) (string, []ProductHit, error) {
	words := Words(criteria.Query)
	if len(words) == 0 {
		return "", nil, nil
	}
	suggestions, err := h.reader.Suggest(ctx, words, h.policy.TypoSimilarity)
	if err != nil || len(suggestions) == 0 {
		return "", nil, err
	}
	corrected := criteria.Query
	replaced := false
	for _, word := range words {
		suggestion, ok := suggestions[word]
		if !ok || suggestion == word {
			continue
		}
		corrected = strings.ReplaceAll(corrected, word, suggestion)
		replaced = true
	}
	if !replaced {
		return "", nil, nil
	}
	criteria.Query = corrected
	hits, err := h.reader.Search(ctx, criteria)
	if err != nil {
		return "", nil, err
	}
	if len(hits) == 0 {
		return "", nil, nil
	}
	return corrected, hits, nil
}

type CategoryFacets struct {
	CategoryID string
	Query      string
	Filters    string
	PriceMin   int64
	PriceMax   int64
	SellerID   string
	Condition  string
	InStock    bool
}

type CategoryFacetsHandler struct {
	reader ReadModel
	policy application.Policy
}

func NewCategoryFacetsHandler(reader ReadModel, policy application.Policy) *CategoryFacetsHandler {
	return &CategoryFacetsHandler{reader: reader, policy: policy}
}

func (h *CategoryFacetsHandler) Handle(ctx context.Context, q CategoryFacets) (FacetSet, error) {
	attributes, err := ParseFilters(q.Filters)
	if err != nil {
		return FacetSet{}, err
	}
	filters := Filters{
		Query: strings.TrimSpace(q.Query), CategoryID: q.CategoryID, Attributes: attributes,
		PriceMin: q.PriceMin, PriceMax: q.PriceMax, SellerID: q.SellerID, Condition: q.Condition, InStock: q.InStock,
	}
	if filters.CategoryID == "" {
		return FacetSet{}, ErrInvalidCategory
	}
	if err := ValidateFilters(filters); err != nil {
		return FacetSet{}, err
	}
	return h.reader.Facets(ctx, filters, h.policy.MaxFacetValues)
}
