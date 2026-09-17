package httpx

import (
	"net/http"
	"strconv"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/pagination"
)

var ErrInvalidLimit = kernel.Validation("INVALID_LIMIT", "limit must be a positive integer")

type PageInfo struct {
	NextCursor string `json:"next_cursor,omitempty"`
	HasMore    bool   `json:"has_more"`
}

type PageResponse[T any] struct {
	Data []T      `json:"data"`
	Page PageInfo `json:"page"`
}

func NewPageResponse[S any, T any](p pagination.Page[S], mapFn func(S) T) PageResponse[T] {
	data := make([]T, 0, len(p.Items))
	for _, item := range p.Items {
		data = append(data, mapFn(item))
	}
	return PageResponse[T]{Data: data, Page: PageInfo{NextCursor: p.NextCursor, HasMore: p.HasMore}}
}

func PageParams(r *http.Request) (limit int, cursor string, err error) {
	q := r.URL.Query()
	cursor = q.Get("cursor")
	raw := q.Get("limit")
	if raw == "" {
		return pagination.DefaultLimit, cursor, nil
	}
	limit, convErr := strconv.Atoi(raw)
	if convErr != nil || limit <= 0 {
		return 0, "", ErrInvalidLimit.WithDetail("%q", raw)
	}
	return pagination.NormalizeLimit(limit), cursor, nil
}
