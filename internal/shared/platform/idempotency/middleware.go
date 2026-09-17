package idempotency

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/httpx"
)

const (
	HeaderKey      = "Idempotency-Key"
	HeaderReplayed = "Idempotent-Replayed"
	DefaultTTL     = 24 * time.Hour
	maxStoredBody  = 1 << 20
)

var (
	ErrKeyRequired = kernel.Validation("IDEMPOTENCY_KEY_REQUIRED", "Idempotency-Key header is required")
	ErrKeyInvalid  = kernel.Validation("IDEMPOTENCY_KEY_INVALID", "Idempotency-Key must be a UUID")
	ErrKeyReuse    = kernel.BusinessRule("IDEMPOTENCY_KEY_REUSE", "Idempotency-Key was already used with a different request")
	ErrInProgress  = kernel.Conflict("IDEMPOTENCY_REQUEST_IN_PROGRESS", "request with this Idempotency-Key is still being processed")
)

type PrincipalFunc func(r *http.Request) string

type Middleware struct {
	store     Store
	rs        *httpx.Responder
	log       *slog.Logger
	principal PrincipalFunc
	ttl       time.Duration
}

func NewMiddleware(store Store, rs *httpx.Responder, log *slog.Logger, principal PrincipalFunc, ttl time.Duration) *Middleware {
	return &Middleware{store: store, rs: rs, log: log, principal: principal, ttl: ttl}
}

func (m *Middleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := r.Header.Get(HeaderKey)
		if raw == "" {
			m.rs.Error(w, r, ErrKeyRequired)
			return
		}
		if _, err := kernel.ParseID[struct{}](raw); err != nil {
			m.rs.Error(w, r, ErrKeyInvalid)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			m.rs.Error(w, r, httpx.ErrBodyTooLarge)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))

		key := Key{Principal: m.principal(r), Scope: r.Method + " " + r.URL.Path, Value: raw}
		hash := requestHash(r.Method, r.URL.Path, body)

		rec, created, err := m.store.Begin(r.Context(), key, hash, m.ttl)
		if err != nil {
			m.rs.Error(w, r, err)
			return
		}
		if !created {
			m.replay(w, r, rec, hash)
			return
		}

		cw := &captureWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(cw, r)

		ctx := r.Context()
		if cw.status >= http.StatusInternalServerError || cw.overflow {
			if err := m.store.Release(ctx, key); err != nil {
				m.log.WarnContext(ctx, "idempotency release failed", "err", err)
			}
			return
		}
		if err := m.store.Complete(ctx, key, cw.status, cw.Header().Get("Content-Type"), cw.buf.Bytes()); err != nil {
			m.log.WarnContext(ctx, "idempotency complete failed", "err", err)
		}
	})
}

func (m *Middleware) replay(w http.ResponseWriter, r *http.Request, rec Record, hash string) {
	if rec.RequestHash != hash {
		m.rs.Error(w, r, ErrKeyReuse)
		return
	}
	if rec.State != StateCompleted {
		m.rs.Error(w, r, ErrInProgress)
		return
	}
	if rec.ContentType != "" {
		w.Header().Set("Content-Type", rec.ContentType)
	}
	w.Header().Set(HeaderReplayed, "true")
	w.WriteHeader(rec.StatusCode)
	if len(rec.Body) == 0 {
		return
	}
	if _, err := w.Write(rec.Body); err != nil {
		m.log.WarnContext(r.Context(), "idempotency replay write failed", "err", err)
	}
}

func requestHash(method, path string, body []byte) string {
	h := sha256.New()
	h.Write([]byte(method))
	h.Write([]byte{0})
	h.Write([]byte(path))
	h.Write([]byte{0})
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

type captureWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
	buf         bytes.Buffer
	overflow    bool
}

func (c *captureWriter) Unwrap() http.ResponseWriter {
	return c.ResponseWriter
}

func (c *captureWriter) WriteHeader(code int) {
	if c.wroteHeader {
		return
	}
	c.wroteHeader = true
	c.status = code
	c.ResponseWriter.WriteHeader(code)
}

func (c *captureWriter) Write(p []byte) (int, error) {
	if !c.wroteHeader {
		c.WriteHeader(http.StatusOK)
	}
	if !c.overflow {
		if c.buf.Len()+len(p) > maxStoredBody {
			c.overflow = true
			c.buf.Reset()
		} else {
			c.buf.Write(p)
		}
	}
	return c.ResponseWriter.Write(p)
}
