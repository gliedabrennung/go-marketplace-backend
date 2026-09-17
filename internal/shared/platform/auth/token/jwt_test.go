package token_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth/token"
)

func newJWT(t *testing.T, kid, audience string) *token.JWT {
	t.Helper()
	ring, err := token.GenerateKeyRing(kid)
	require.NoError(t, err)
	return token.NewJWT(ring, token.JWTConfig{
		Issuer: "marketplace", Audience: audience, TTL: 15 * time.Minute, Leeway: 0,
	})
}

func TestJWT_IssueAndVerify(t *testing.T) {
	j := newJWT(t, "k1", "api")
	p := auth.Principal{UserID: "u-1", SessionID: "s-1", Roles: []string{"buyer"}}

	signed, exp, err := j.Issue(p, time.Now())
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now().Add(15*time.Minute), exp, time.Second)

	got, err := j.Verify(context.Background(), signed)
	require.NoError(t, err)
	assert.Equal(t, p, got)
}

func TestJWT_RejectsExpired(t *testing.T) {
	j := newJWT(t, "k1", "api")
	signed, _, err := j.Issue(auth.Principal{UserID: "u-1"}, time.Now().Add(-time.Hour))
	require.NoError(t, err)
	_, err = j.Verify(context.Background(), signed)
	require.ErrorIs(t, err, auth.ErrInvalidToken)
}

func TestJWT_RejectsForeignKey(t *testing.T) {
	issuer := newJWT(t, "k1", "api")
	verifier := newJWT(t, "k1", "api")
	signed, _, err := issuer.Issue(auth.Principal{UserID: "u-1"}, time.Now())
	require.NoError(t, err)
	_, err = verifier.Verify(context.Background(), signed)
	require.ErrorIs(t, err, auth.ErrInvalidToken)
}

func TestJWT_RejectsWrongAudience(t *testing.T) {
	ring, err := token.GenerateKeyRing("k1")
	require.NoError(t, err)
	issuer := token.NewJWT(ring, token.JWTConfig{Issuer: "marketplace", Audience: "admin", TTL: time.Minute})
	verifier := token.NewJWT(ring, token.JWTConfig{Issuer: "marketplace", Audience: "api", TTL: time.Minute})
	signed, _, err := issuer.Issue(auth.Principal{UserID: "u-1"}, time.Now())
	require.NoError(t, err)
	_, err = verifier.Verify(context.Background(), signed)
	require.ErrorIs(t, err, auth.ErrInvalidToken)
}

func TestJWT_RejectsTamperedPayload(t *testing.T) {
	j := newJWT(t, "k1", "api")
	signed, _, err := j.Issue(auth.Principal{UserID: "u-1", Roles: []string{"buyer"}}, time.Now())
	require.NoError(t, err)
	parts := strings.Split(signed, ".")
	require.Len(t, parts, 3)
	parts[1] = parts[1][:len(parts[1])-2] + "AA"
	_, err = j.Verify(context.Background(), strings.Join(parts, "."))
	require.ErrorIs(t, err, auth.ErrInvalidToken)
}

func TestJWT_RejectsNoneAlgorithm(t *testing.T) {
	j := newJWT(t, "k1", "api")
	_, err := j.Verify(context.Background(), "eyJhbGciOiJub25lIiwia2lkIjoiazEifQ.eyJzdWIiOiJ1LTEifQ.")
	require.ErrorIs(t, err, auth.ErrInvalidToken)
}
