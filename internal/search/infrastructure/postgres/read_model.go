package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/gliedabrennung/go-marketplace-backend/internal/search/application/query"
	platform "github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/postgres"
)

type ReadModel struct {
	db platform.Querier
}

func NewReadModel(db platform.Querier) *ReadModel {
	return &ReadModel{db: db}
}

type builder struct {
	args []any
}

func (b *builder) arg(value any) string {
	b.args = append(b.args, value)
	return "$" + strconv.Itoa(len(b.args))
}

func (b *builder) conditions(filters query.Filters) (where []string, rank string) {
	rank = "0::float8"
	where = []string{"TRUE"}
	if filters.Query != "" {
		q := b.arg(filters.Query)
		rank = fmt.Sprintf("ts_rank_cd(d.document, websearch_to_tsquery('russian', %s))", q)
		where = append(where, fmt.Sprintf("d.document @@ websearch_to_tsquery('russian', %s)", q))
	}
	if filters.CategoryID != "" {
		where = append(where, fmt.Sprintf("%s::uuid = ANY (d.category_path)", b.arg(filters.CategoryID)))
	}
	if filters.SellerID != "" {
		where = append(where, fmt.Sprintf(`EXISTS (SELECT 1 FROM search.product_offers o
			WHERE o.product_id = d.product_id AND o.status = 'active' AND o.seller_id = %s::uuid)`, b.arg(filters.SellerID)))
	}
	if filters.Condition != "" {
		where = append(where, fmt.Sprintf("%s = ANY (COALESCE(st.conditions, '{}'))", b.arg(filters.Condition)))
	}
	if filters.InStock {
		where = append(where, "COALESCE(st.offers, 0) > 0")
	}
	if filters.PriceMin > 0 {
		where = append(where, fmt.Sprintf("st.min_price >= %s", b.arg(filters.PriceMin)))
	}
	if filters.PriceMax > 0 {
		where = append(where, fmt.Sprintf("st.min_price <= %s", b.arg(filters.PriceMax)))
	}
	for _, code := range slices.Sorted(maps.Keys(filters.Attributes)) {
		where = append(where, fmt.Sprintf("d.attributes -> %s ?| %s::text[]", b.arg(code), b.arg(filters.Attributes[code])))
	}
	return where, rank
}

func (m *ReadModel) Search(ctx context.Context, criteria query.Criteria) ([]query.ProductHit, error) {
	b := &builder{}
	where, rank := b.conditions(criteria.Filters)
	order, keyset := ordering(b, criteria, rank)
	if keyset != "" {
		where = append(where, keyset)
	}
	sql := fmt.Sprintf(`
		SELECT d.product_id::text, d.seller_id::text, d.category_id::text, d.title, d.brand, d.cover_key,
			COALESCE(st.min_price, 0), COALESCE(st.currency, 'KZT'), COALESCE(st.offers, 0), COALESCE(st.sellers, 0),
			d.published_at, %s
		FROM search.documents d
		LEFT JOIN search.product_stats st ON st.product_id = d.product_id
		WHERE %s
		ORDER BY %s
		LIMIT %s`, rank, strings.Join(where, " AND "), order, b.arg(criteria.Limit+1))

	rows, err := m.db.Query(ctx, sql, b.args...)
	if err != nil {
		return nil, fmt.Errorf("search products: %w", err)
	}
	hits, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (query.ProductHit, error) {
		var hit query.ProductHit
		err := row.Scan(&hit.ProductID, &hit.SellerID, &hit.CategoryID, &hit.Title, &hit.Brand, &hit.CoverKey,
			&hit.MinPrice, &hit.Currency, &hit.Offers, &hit.Sellers, &hit.PublishedAt, &hit.Rank)
		hit.PublishedAt = hit.PublishedAt.UTC()
		hit.InStock = hit.Offers > 0
		return hit, err
	})
	if err != nil {
		return nil, fmt.Errorf("scan search results: %w", err)
	}
	return hits, nil
}

func ordering(b *builder, criteria query.Criteria, rank string) (order, keyset string) {
	after := criteria.After
	switch criteria.Sort {
	case query.SortPriceAsc:
		order = "COALESCE(st.min_price, 9223372036854775807) ASC, d.product_id ASC"
		if after != nil {
			keyset = fmt.Sprintf("(COALESCE(st.min_price, 9223372036854775807), d.product_id) > (%s, %s::uuid)",
				b.arg(after.Price), b.arg(after.ID))
		}
	case query.SortPriceDesc:
		order = "COALESCE(st.min_price, -1) DESC, d.product_id DESC"
		if after != nil {
			keyset = fmt.Sprintf("(COALESCE(st.min_price, -1), d.product_id) < (%s, %s::uuid)",
				b.arg(after.Price), b.arg(after.ID))
		}
	case query.SortNew:
		order = "d.published_at DESC, d.product_id DESC"
		if after != nil {
			keyset = fmt.Sprintf("(d.published_at, d.product_id) < (%s, %s::uuid)", b.arg(after.At), b.arg(after.ID))
		}
	case query.SortPopular:
		order = fmt.Sprintf("COALESCE(st.offers, 0) DESC, %s DESC, d.product_id DESC", rank)
		if after != nil {
			keyset = fmt.Sprintf("(COALESCE(st.offers, 0), d.product_id) < (%s, %s::uuid)",
				b.arg(after.Offers), b.arg(after.ID))
		}
	default:
		order = fmt.Sprintf("%s DESC, d.published_at DESC, d.product_id DESC", rank)
		if after != nil {
			keyset = fmt.Sprintf("(%s, d.published_at, d.product_id) < (%s, %s, %s::uuid)",
				rank, b.arg(after.Rank), b.arg(after.At), b.arg(after.ID))
		}
	}
	return order, keyset
}

type facetRow struct {
	Code  string  `json:"code"`
	Value string  `json:"value"`
	Total int     `json:"total"`
	Min   float64 `json:"min"`
	Max   float64 `json:"max"`
}

func (m *ReadModel) Facets(ctx context.Context, filters query.Filters, limit int) (query.FacetSet, error) {
	b := &builder{}
	where, _ := b.conditions(filters)
	sql := fmt.Sprintf(`
		WITH filtered AS (
			SELECT d.product_id, d.attributes, d.numbers, st.min_price, COALESCE(st.conditions, '{}') AS conditions
			FROM search.documents d
			LEFT JOIN search.product_stats st ON st.product_id = d.product_id
			WHERE %s
		),
		attribute_values AS (
			SELECT a.key AS code, v AS value, count(*) AS total
			FROM filtered f, jsonb_each(f.attributes) a, jsonb_array_elements_text(a.value) v
			GROUP BY 1, 2
		),
		numeric_ranges AS (
			SELECT n.key AS code, MIN((n.value)::numeric) AS min, MAX((n.value)::numeric) AS max
			FROM filtered f, jsonb_each(f.numbers) n
			GROUP BY 1
		),
		offer_conditions AS (
			SELECT c AS value, count(*) AS total FROM filtered f, unnest(f.conditions) c GROUP BY 1
		)
		SELECT
			(SELECT count(*) FROM filtered),
			COALESCE((SELECT min(min_price) FROM filtered), 0),
			COALESCE((SELECT max(min_price) FROM filtered), 0),
			COALESCE((SELECT jsonb_agg(jsonb_build_object('code', code, 'value', value, 'total', total)
				ORDER BY total DESC, value) FROM attribute_values), '[]'),
			COALESCE((SELECT jsonb_agg(jsonb_build_object('code', code, 'min', min, 'max', max) ORDER BY code)
				FROM numeric_ranges), '[]'),
			COALESCE((SELECT jsonb_agg(jsonb_build_object('value', value, 'total', total) ORDER BY total DESC, value)
				FROM offer_conditions), '[]')`, strings.Join(where, " AND "))

	var (
		set                             query.FacetSet
		attributes, numbers, conditions []byte
	)
	err := m.db.QueryRow(ctx, sql, b.args...).
		Scan(&set.Total, &set.PriceMin, &set.PriceMax, &attributes, &numbers, &conditions)
	if err != nil {
		return query.FacetSet{}, fmt.Errorf("select facets: %w", err)
	}

	values, err := decodeFacets(attributes)
	if err != nil {
		return query.FacetSet{}, err
	}
	set.Attributes = groupFacets(values, limit)
	ranges, err := decodeFacets(numbers)
	if err != nil {
		return query.FacetSet{}, err
	}
	for _, row := range ranges {
		set.Numbers = append(set.Numbers, query.NumberFacet{Code: row.Code, Min: row.Min, Max: row.Max})
	}
	rows, err := decodeFacets(conditions)
	if err != nil {
		return query.FacetSet{}, err
	}
	for _, row := range rows {
		set.Conditions = append(set.Conditions, query.FacetValue{Value: row.Value, Count: row.Total})
	}
	return set, nil
}

func decodeFacets(raw []byte) ([]facetRow, error) {
	var rows []facetRow
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("decode facets: %w", err)
	}
	return rows, nil
}

func groupFacets(rows []facetRow, limit int) []query.AttributeFacet {
	order := make([]string, 0, len(rows))
	byCode := map[string][]query.FacetValue{}
	for _, row := range rows {
		if _, seen := byCode[row.Code]; !seen {
			order = append(order, row.Code)
		}
		if len(byCode[row.Code]) < limit {
			byCode[row.Code] = append(byCode[row.Code], query.FacetValue{Value: row.Value, Count: row.Total})
		}
	}
	slices.Sort(order)
	facets := make([]query.AttributeFacet, 0, len(order))
	for _, code := range order {
		facets = append(facets, query.AttributeFacet{Code: code, Values: byCode[code]})
	}
	return facets
}

func (m *ReadModel) Suggest(ctx context.Context, words []string, similarity float64) (map[string]string, error) {
	rows, err := m.db.Query(ctx, `
		SELECT w.word, l.word
		FROM unnest($1::text[]) AS w(word)
		JOIN LATERAL (
			SELECT word FROM search.lexicon
			WHERE similarity(word, w.word) >= $2
			ORDER BY similarity(word, w.word) DESC, weight DESC
			LIMIT 1
		) l ON TRUE`, words, similarity)
	if err != nil {
		return nil, fmt.Errorf("suggest words: %w", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var word, suggestion string
		if err := rows.Scan(&word, &suggestion); err != nil {
			return nil, fmt.Errorf("scan suggestion: %w", err)
		}
		out[word] = suggestion
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read suggestions: %w", err)
	}
	return out, nil
}
