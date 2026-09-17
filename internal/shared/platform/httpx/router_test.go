package httpx_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/httpx"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/observability"
)

func newTestRouter(t *testing.T) (http.Handler, *prometheus.Registry) {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	rs := httpx.NewResponder(log)
	reg := prometheus.NewRegistry()

	rt := httpx.NewRouter(rs)
	rt.HandleFunc("GET /api/v1/orders/{id}", func(w http.ResponseWriter, r *http.Request) {
		rs.JSON(w, r, http.StatusOK, map[string]string{"id": r.PathValue("id")})
	})
	rt.HandleFunc("POST /api/v1/orders", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Route-Middleware", "applied")
			next.ServeHTTP(w, r)
		})
	})

	return httpx.Chain(rt, httpx.Observe(log, observability.NewHTTPMetrics(reg))), reg
}

func TestRouter_DispatchesWithPathValues(t *testing.T) {
	h, reg := newTestRouter(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/orders/42", nil))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"id":"42"}`, rec.Body.String())
	assert.InDelta(t, 1, counter(t, reg, "GET", "/api/v1/orders/{id}", "200"), 0)
}

func TestRouter_AppliesRouteMiddleware(t *testing.T) {
	h, _ := newTestRouter(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/orders", nil))
	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, "applied", rec.Header().Get("X-Route-Middleware"))
}

func TestRouter_NotFoundIsProblem(t *testing.T) {
	h, reg := newTestRouter(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/unknown", nil))

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))
	var p httpx.Problem
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &p))
	assert.Equal(t, "ROUTE_NOT_FOUND", p.Code)
	assert.InDelta(t, 1, counter(t, reg, "GET", "unmatched", "404"), 0)
}

func TestRouter_MethodNotAllowedIsProblem(t *testing.T) {
	h, _ := newTestRouter(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/v1/orders/42", nil))

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	assert.Contains(t, rec.Header().Get("Allow"), http.MethodGet)
	var p httpx.Problem
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &p))
	assert.Equal(t, "METHOD_NOT_ALLOWED", p.Code)
	assert.Equal(t, http.StatusMethodNotAllowed, p.Status)
}

func counter(t *testing.T, reg *prometheus.Registry, method, route, status string) float64 {
	t.Helper()
	families, err := reg.Gather()
	require.NoError(t, err)
	for _, f := range families {
		if f.GetName() != "http_requests_total" {
			continue
		}
		for _, m := range f.GetMetric() {
			labels := map[string]string{}
			for _, l := range m.GetLabel() {
				labels[l.GetName()] = l.GetValue()
			}
			if labels["method"] == method && labels["route"] == route && labels["status"] == status {
				return m.GetCounter().GetValue()
			}
		}
	}
	t.Fatalf("metric http_requests_total{method=%q,route=%q,status=%q} not found", method, route, status)
	return 0
}
