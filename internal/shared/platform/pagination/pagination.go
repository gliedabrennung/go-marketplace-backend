package pagination

import (
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

const (
	DefaultLimit = 20
	MaxLimit     = 100
)

var ErrInvalidCursor = kernel.Validation("INVALID_CURSOR", "cursor is invalid")

type Page[T any] struct {
	Items      []T
	NextCursor string
	HasMore    bool
}

type Keyset struct {
	At time.Time `json:"t"`
	ID string    `json:"id"`
}

func NormalizeLimit(limit int) int {
	if limit <= 0 || limit > MaxLimit {
		return DefaultLimit
	}
	return limit
}

func EncodeKeyset(k Keyset) string {
	raw, err := json.Marshal(k)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func DecodeKeyset(cursor string) (Keyset, bool, error) {
	if cursor == "" {
		return Keyset{}, false, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return Keyset{}, false, ErrInvalidCursor
	}
	var k Keyset
	if err := json.Unmarshal(raw, &k); err != nil || k.ID == "" || k.At.IsZero() {
		return Keyset{}, false, ErrInvalidCursor
	}
	return k, true, nil
}

func Build[T any](rows []T, limit int, keyOf func(T) Keyset) Page[T] {
	if len(rows) <= limit {
		return Page[T]{Items: rows}
	}
	items := rows[:limit]
	return Page[T]{
		Items:      items,
		HasMore:    true,
		NextCursor: EncodeKeyset(keyOf(items[len(items)-1])),
	}
}
