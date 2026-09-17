package query

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

const (
	SortRelevance = "relevance"
	SortPriceAsc  = "price_asc"
	SortPriceDesc = "price_desc"
	SortNew       = "new"
	SortPopular   = "popular"
)

var (
	ErrInvalidSort     = kernel.Validation("SEARCH_INVALID_SORT", "sort must be relevance, price_asc, price_desc, new or popular")
	ErrInvalidFilters  = kernel.Validation("SEARCH_INVALID_FILTERS", "filters must be code:value pairs separated by commas")
	ErrInvalidCategory = kernel.Validation("SEARCH_INVALID_CATEGORY", "category must be a valid identifier")
	ErrInvalidPrice    = kernel.Validation("SEARCH_INVALID_PRICE_RANGE", "price range is invalid")
	ErrInvalidCursor   = kernel.Validation("SEARCH_INVALID_CURSOR", "cursor is invalid")
)

var sorts = map[string]struct{}{
	SortRelevance: {}, SortPriceAsc: {}, SortPriceDesc: {}, SortNew: {}, SortPopular: {},
}

type Filters struct {
	Query      string
	CategoryID string
	Attributes map[string][]string
	PriceMin   int64
	PriceMax   int64
	SellerID   string
	Condition  string
	InStock    bool
}

type Criteria struct {
	Filters
	Sort  string
	Limit int
	After *Cursor
}

type Cursor struct {
	Rank   float64   `json:"r,omitempty"`
	Price  int64     `json:"p,omitempty"`
	Offers int       `json:"o,omitempty"`
	At     time.Time `json:"t,omitzero"`
	ID     string    `json:"id"`
}

func ParseFilters(raw string) (map[string][]string, error) {
	if strings.TrimSpace(raw) == "" {
		return map[string][]string{}, nil
	}
	out := map[string][]string{}
	for pair := range strings.SplitSeq(raw, ",") {
		code, values, found := strings.Cut(strings.TrimSpace(pair), ":")
		code = strings.TrimSpace(code)
		if !found || code == "" || strings.TrimSpace(values) == "" {
			return nil, ErrInvalidFilters.WithDetail("%q", pair)
		}
		for value := range strings.SplitSeq(values, "|") {
			value = strings.TrimSpace(value)
			if value == "" {
				return nil, ErrInvalidFilters.WithDetail("%q", pair)
			}
			out[code] = append(out[code], value)
		}
	}
	return out, nil
}

func NormalizeSort(sort string) (string, error) {
	if sort == "" {
		return SortRelevance, nil
	}
	if _, ok := sorts[sort]; !ok {
		return "", ErrInvalidSort.WithDetail("%q", sort)
	}
	return sort, nil
}

func ValidateFilters(filters Filters) error {
	if filters.CategoryID != "" {
		if _, err := kernel.ParseID[struct{}](filters.CategoryID); err != nil {
			return ErrInvalidCategory.WithDetail("%q", filters.CategoryID)
		}
	}
	if filters.SellerID != "" {
		if _, err := kernel.ParseSellerID(filters.SellerID); err != nil {
			return ErrInvalidFilters.WithDetail("seller %q", filters.SellerID)
		}
	}
	if filters.PriceMin < 0 || filters.PriceMax < 0 || (filters.PriceMax > 0 && filters.PriceMax < filters.PriceMin) {
		return ErrInvalidPrice
	}
	return nil
}

func EncodeCursor(sort string, hit ProductHit) string {
	cursor := Cursor{ID: hit.ProductID}
	switch sort {
	case SortRelevance:
		cursor.Rank, cursor.At = hit.Rank, hit.PublishedAt
	case SortPriceAsc, SortPriceDesc:
		cursor.Price = hit.MinPrice
	case SortPopular:
		cursor.Offers, cursor.Rank = hit.Offers, hit.Rank
	default:
		cursor.At = hit.PublishedAt
	}
	raw, err := json.Marshal(cursor)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func DecodeCursor(raw string) (*Cursor, error) {
	if raw == "" {
		return nil, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, ErrInvalidCursor
	}
	var cursor Cursor
	if err := json.Unmarshal(decoded, &cursor); err != nil || cursor.ID == "" {
		return nil, ErrInvalidCursor
	}
	if _, err := kernel.ParseID[struct{}](cursor.ID); err != nil {
		return nil, ErrInvalidCursor
	}
	return &cursor, nil
}

func Words(query string) []string {
	fields := strings.FieldsFunc(query, func(r rune) bool {
		return r != '-' && r != '_' && !isAlphanumeric(r)
	})
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if len(field) > 2 {
			out = append(out, strings.ToLower(field))
		}
	}
	return out
}

func isAlphanumeric(r rune) bool {
	switch {
	case r >= '0' && r <= '9', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= 'а' && r <= 'я', r >= 'А' && r <= 'Я':
		return true
	default:
		return false
	}
}
