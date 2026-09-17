package httpx

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

type Check func(ctx context.Context) error

type Health struct {
	mu       sync.RWMutex
	checks   map[string]Check
	timeout  time.Duration
	draining atomic.Bool
}

func NewHealth(timeout time.Duration) *Health {
	return &Health{checks: make(map[string]Check), timeout: timeout}
}

func (h *Health) Register(name string, c Check) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.checks[name] = c
}

func (h *Health) StartDraining() {
	h.draining.Store(true)
}

type healthResponse struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks,omitempty"`
}

func (h *Health) Liveness(w http.ResponseWriter, _ *http.Request) {
	writeHealth(w, http.StatusOK, healthResponse{Status: "ok"})
}

func (h *Health) Readiness(w http.ResponseWriter, r *http.Request) {
	if h.draining.Load() {
		writeHealth(w, http.StatusServiceUnavailable, healthResponse{Status: "draining"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()

	h.mu.RLock()
	checks := maps.Clone(h.checks)
	h.mu.RUnlock()

	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		results = make(map[string]string, len(checks))
		healthy = true
	)
	for name, check := range checks {
		wg.Go(func() {
			status := "ok"
			if err := check(ctx); err != nil {
				status = "failing"
			}
			mu.Lock()
			defer mu.Unlock()
			results[name] = status
			if status != "ok" {
				healthy = false
			}
		})
	}
	wg.Wait()

	code, status := http.StatusOK, "ok"
	if !healthy {
		code, status = http.StatusServiceUnavailable, "unavailable"
	}
	writeHealth(w, code, healthResponse{Status: status, Checks: results})
}

func writeHealth(w http.ResponseWriter, code int, body healthResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		return
	}
}
