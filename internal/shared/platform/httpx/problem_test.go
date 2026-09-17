package httpx_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/httpx"
)

func TestStatusFor(t *testing.T) {
	cases := map[kernel.ErrorKind]int{
		kernel.KindValidation:      http.StatusBadRequest,
		kernel.KindUnauthenticated: http.StatusUnauthorized,
		kernel.KindForbidden:       http.StatusForbidden,
		kernel.KindNotFound:        http.StatusNotFound,
		kernel.KindConflict:        http.StatusConflict,
		kernel.KindBusinessRule:    http.StatusUnprocessableEntity,
		kernel.KindRateLimited:     http.StatusTooManyRequests,
		kernel.KindInternal:        http.StatusInternalServerError,
	}
	for kind, want := range cases {
		assert.Equal(t, want, httpx.StatusFor(kind), kind.String())
	}
}

func TestResponder_ErrorMapsDomainError(t *testing.T) {
	errStock := kernel.Conflict("INVENTORY_INSUFFICIENT_STOCK", "Недостаточно товара на складе")
	err := fmt.Errorf("place order: %w", errStock.
		WithDetail("Доступно 2 единицы, запрошено 5").
		WithFields(kernel.FieldViolation{Field: "items[0].quantity", Code: "EXCEEDS_AVAILABLE", Message: "Максимум 2"}))

	p := respond(t, err)

	assert.Equal(t, http.StatusConflict, p.Status)
	assert.Equal(t, "INVENTORY_INSUFFICIENT_STOCK", p.Code)
	assert.Equal(t, "https://api.marketplace.kz/errors/inventory-insufficient-stock", p.Type)
	assert.Equal(t, "Недостаточно товара на складе", p.Title)
	assert.Equal(t, "Доступно 2 единицы, запрошено 5", p.Detail)
	assert.Equal(t, "/api/v1/orders", p.Instance)
	require.Len(t, p.Errors, 1)
	assert.Equal(t, "items[0].quantity", p.Errors[0].Field)
}

func TestResponder_ErrorHidesInternalDetails(t *testing.T) {
	p := respond(t, errors.New("pq: password authentication failed for user admin"))
	assert.Equal(t, http.StatusInternalServerError, p.Status)
	assert.Equal(t, "INTERNAL", p.Code)
	assert.Empty(t, p.Detail)
	assert.NotContains(t, p.Title, "password")
}

type limited struct{}

func (limited) Error() string             { return "limited" }
func (limited) Kind() kernel.ErrorKind    { return kernel.KindRateLimited }
func (limited) Code() string              { return "RATE_LIMIT_EXCEEDED" }
func (limited) RetryAfter() time.Duration { return 1500 * time.Millisecond }

func TestResponder_ErrorSetsRetryAfter(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", nil)
	httpx.NewResponder(slog.New(slog.NewTextHandler(io.Discard, nil))).Error(rec, req, limited{})
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.Equal(t, "2", rec.Header().Get("Retry-After"))
	assert.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))
}

func respond(t *testing.T, err error) httpx.Problem {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", nil)
	httpx.NewResponder(slog.New(slog.NewTextHandler(io.Discard, nil))).Error(rec, req, err)
	var p httpx.Problem
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &p))
	assert.Equal(t, p.Status, rec.Code)
	return p
}
