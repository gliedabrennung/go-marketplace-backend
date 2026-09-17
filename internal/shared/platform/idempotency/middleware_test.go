package idempotency_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/httpx"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/idempotency"
)

type memoryStore struct {
	mu      sync.Mutex
	records map[idempotency.Key]idempotency.Record
}

func newMemoryStore() *memoryStore {
	return &memoryStore{records: make(map[idempotency.Key]idempotency.Record)}
}

func (s *memoryStore) Begin(_ context.Context, key idempotency.Key, hash string, _ time.Duration) (idempotency.Record, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rec, ok := s.records[key]; ok {
		return rec, false, nil
	}
	rec := idempotency.Record{State: idempotency.StateInProgress, RequestHash: hash}
	s.records[key] = rec
	return rec, true, nil
}

func (s *memoryStore) Complete(_ context.Context, key idempotency.Key, status int, contentType string, body []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec := s.records[key]
	rec.State = idempotency.StateCompleted
	rec.StatusCode = status
	rec.ContentType = contentType
	rec.Body = append([]byte(nil), body...)
	s.records[key] = rec
	return nil
}

func (s *memoryStore) Release(_ context.Context, key idempotency.Key) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.records, key)
	return nil
}

const validKey = "0190f5a2-7c3e-7b1a-9c2d-1e2f3a4b5c6d"

type fixture struct {
	store   *memoryStore
	calls   atomic.Int32
	status  int
	before  func()
	handler http.Handler
}

func newFixture() *fixture {
	f := &fixture{store: newMemoryStore(), status: http.StatusCreated}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	mw := idempotency.NewMiddleware(f.store, httpx.NewResponder(log), log,
		func(*http.Request) string { return "buyer-1" }, idempotency.DefaultTTL)
	f.handler = mw.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if f.before != nil {
			f.before()
		}
		n := f.calls.Add(1)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(f.status)
		if err := json.NewEncoder(w).Encode(map[string]any{"call": n, "echo": string(body)}); err != nil {
			return
		}
	}))
	return f
}

func (f *fixture) do(key, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", strings.NewReader(body))
	if key != "" {
		req.Header.Set(idempotency.HeaderKey, key)
	}
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	return rec
}

func problemCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var p httpx.Problem
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &p))
	return p.Code
}

func TestMiddleware_RequiresKey(t *testing.T) {
	f := newFixture()
	rec := f.do("", `{}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "IDEMPOTENCY_KEY_REQUIRED", problemCode(t, rec))
	assert.Zero(t, f.calls.Load())
}

func TestMiddleware_RejectsNonUUIDKey(t *testing.T) {
	f := newFixture()
	rec := f.do("abc", `{}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "IDEMPOTENCY_KEY_INVALID", problemCode(t, rec))
}

func TestMiddleware_ReplaysOriginalResponse(t *testing.T) {
	f := newFixture()
	first := f.do(validKey, `{"cart":"1"}`)
	second := f.do(validKey, `{"cart":"1"}`)

	assert.Equal(t, http.StatusCreated, first.Code)
	assert.Equal(t, http.StatusCreated, second.Code)
	assert.Equal(t, first.Body.String(), second.Body.String())
	assert.Equal(t, "true", second.Header().Get(idempotency.HeaderReplayed))
	assert.EqualValues(t, 1, f.calls.Load())
}

func TestMiddleware_RejectsKeyReuseWithDifferentBody(t *testing.T) {
	f := newFixture()
	f.do(validKey, `{"cart":"1"}`)
	rec := f.do(validKey, `{"cart":"2"}`)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Equal(t, "IDEMPOTENCY_KEY_REUSE", problemCode(t, rec))
	assert.EqualValues(t, 1, f.calls.Load())
}

func TestMiddleware_ConflictWhileInProgress(t *testing.T) {
	f := newFixture()
	entered := make(chan struct{})
	release := make(chan struct{})
	f.before = func() {
		close(entered)
		<-release
	}

	firstDone := make(chan *httptest.ResponseRecorder)
	go func() { firstDone <- f.do(validKey, `{}`) }()
	<-entered

	rec := f.do(validKey, `{}`)
	close(release)
	first := <-firstDone

	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Equal(t, "IDEMPOTENCY_REQUEST_IN_PROGRESS", problemCode(t, rec))
	assert.Equal(t, http.StatusCreated, first.Code)
}

func TestMiddleware_ServerErrorReleasesKey(t *testing.T) {
	f := newFixture()
	f.status = http.StatusInternalServerError
	f.do(validKey, `{}`)
	f.status = http.StatusCreated
	rec := f.do(validKey, `{}`)
	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.EqualValues(t, 2, f.calls.Load())
}
