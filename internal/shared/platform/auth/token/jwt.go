package token

import (
	"context"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

type JWTConfig struct {
	Issuer   string
	Audience string
	TTL      time.Duration
	Leeway   time.Duration
}

type claims struct {
	jwt.RegisteredClaims
	SessionID string   `json:"sid"`
	Roles     []string `json:"roles"`
}

type JWT struct {
	keys *KeyRing
	cfg  JWTConfig
}

func NewJWT(keys *KeyRing, cfg JWTConfig) *JWT {
	return &JWT{keys: keys, cfg: cfg}
}

func (j *JWT) Issue(p auth.Principal, now time.Time) (string, time.Time, error) {
	kid, key, err := j.keys.Signing()
	if err != nil {
		return "", time.Time{}, err
	}
	expires := now.Add(j.cfg.TTL)
	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    j.cfg.Issuer,
			Subject:   p.UserID,
			Audience:  jwt.ClaimStrings{j.cfg.Audience},
			ExpiresAt: jwt.NewNumericDate(expires),
			NotBefore: jwt.NewNumericDate(now),
			IssuedAt:  jwt.NewNumericDate(now),
			ID:        kernel.NewID[struct{}]().String(),
		},
		SessionID: p.SessionID,
		Roles:     p.Roles,
	})
	token.Header["kid"] = kid
	signed, err := token.SignedString(key)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign access token: %w", err)
	}
	return signed, expires, nil
}

func (j *JWT) Verify(_ context.Context, raw string) (auth.Principal, error) {
	var c claims
	_, err := jwt.ParseWithClaims(raw, &c, j.keyFunc,
		jwt.WithValidMethods([]string{jwt.SigningMethodES256.Alg()}),
		jwt.WithIssuer(j.cfg.Issuer),
		jwt.WithAudience(j.cfg.Audience),
		jwt.WithLeeway(j.cfg.Leeway),
		jwt.WithExpirationRequired(),
	)
	if err != nil || c.Subject == "" {
		return auth.Principal{}, auth.ErrInvalidToken
	}
	return auth.Principal{UserID: c.Subject, SessionID: c.SessionID, Roles: c.Roles}, nil
}

func (j *JWT) keyFunc(t *jwt.Token) (any, error) {
	kid, ok := t.Header["kid"].(string)
	if !ok {
		return nil, fmt.Errorf("token has no kid")
	}
	key, ok := j.keys.Public(kid)
	if !ok {
		return nil, fmt.Errorf("unknown kid %q", kid)
	}
	return key, nil
}
