package httpx

import (
	"context"
	"net/http"
	"strings"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/reqctx"
)

type TokenVerifier interface {
	Verify(ctx context.Context, token string) (auth.Principal, error)
}

func Authenticate(v TokenVerifier, rs *Responder) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if header == "" {
				next.ServeHTTP(w, r)
				return
			}
			scheme, raw, found := strings.Cut(header, " ")
			if !found || !strings.EqualFold(scheme, "Bearer") || raw == "" {
				rs.Error(w, r, auth.ErrInvalidToken)
				return
			}
			p, err := v.Verify(r.Context(), raw)
			if err != nil {
				rs.Error(w, r, err)
				return
			}
			ctx := reqctx.WithUserID(auth.WithPrincipal(r.Context(), p), p.UserID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func RequireAuthenticated(rs *Responder) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := auth.FromContext(r.Context()); !ok {
				rs.Error(w, r, auth.ErrUnauthenticated)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func PrincipalOrAnonymous(r *http.Request) string {
	if p, ok := auth.FromContext(r.Context()); ok {
		return p.UserID
	}
	return "anonymous"
}
