package ratelimit

import (
	"log/slog"
	"net"
	"net/http"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/httpx"
)

type SubjectFunc func(r *http.Request) string

func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func Middleware(l *Limiter, limit Limit, subject SubjectFunc, rs *httpx.Responder, log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			decision, err := l.Allow(r.Context(), limit, subject(r))
			if err != nil {
				log.WarnContext(r.Context(), "rate limiter unavailable, allowing request", "limit", limit.Name, "err", err)
				next.ServeHTTP(w, r)
				return
			}
			if err := decision.Err(limit); err != nil {
				rs.Error(w, r, err)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
