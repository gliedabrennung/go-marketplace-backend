package auth

import (
	"context"
	"slices"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type Principal struct {
	UserID    string
	SessionID string
	Roles     []string
}

func (p Principal) HasRole(role string) bool {
	return slices.Contains(p.Roles, role)
}

func (p Principal) HasAnyRole(roles ...string) bool {
	return slices.ContainsFunc(roles, p.HasRole)
}

type principalKey struct{}

func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

func FromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok && p.UserID != ""
}

var (
	ErrUnauthenticated = kernel.Unauthenticated("UNAUTHENTICATED", "authentication required")
	ErrInvalidToken    = kernel.Unauthenticated("INVALID_TOKEN", "access token is invalid or expired")
	ErrForbidden       = kernel.Forbidden("FORBIDDEN", "operation is not permitted")
)

func Require(ctx context.Context) (Principal, error) {
	p, ok := FromContext(ctx)
	if !ok {
		return Principal{}, ErrUnauthenticated
	}
	return p, nil
}
