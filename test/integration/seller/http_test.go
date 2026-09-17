//go:build integration

package seller_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/seller"
	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth/token"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/clock"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/httpx"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/idempotency"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/observability"
	"github.com/gliedabrennung/go-marketplace-backend/test/testdb"
)

type client struct {
	t      *testing.T
	server *httptest.Server
	jwt    *token.JWT
}

type actor struct {
	id    string
	token string
}

func (c *client) actor(roles ...string) actor {
	c.t.Helper()
	id := kernel.NewUserID().String()
	signed, _, err := c.jwt.Issue(auth.Principal{UserID: id, Roles: append([]string{"buyer"}, roles...)}, time.Now())
	require.NoError(c.t, err)
	return actor{id: id, token: signed}
}

func (c *client) call(a actor, method, path string, body any, idempotent bool) (int, []byte) {
	c.t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(c.t, err)
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, c.server.URL+path, reader)
	require.NoError(c.t, err)
	req.Header.Set("Authorization", "Bearer "+a.token)
	if idempotent {
		req.Header.Set(idempotency.HeaderKey, kernel.NewID[struct{}]().String())
	}
	resp, err := c.server.Client().Do(req)
	require.NoError(c.t, err)
	defer func() { require.NoError(c.t, resp.Body.Close()) }()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(c.t, err)
	return resp.StatusCode, raw
}

func (c *client) code(raw []byte) string {
	c.t.Helper()
	var p httpx.Problem
	require.NoError(c.t, json.Unmarshal(raw, &p))
	return p.Code
}

func TestHTTP_SellerOnboardingAndModeration(t *testing.T) {
	pool := testdb.Pool(t)
	testdb.Truncate(t, pool, append(sellerTables, "platform.audit_log")...)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	rs := httpx.NewResponder(log)
	ring, err := token.GenerateKeyRing("test")
	require.NoError(t, err)
	jwt := token.NewJWT(ring, token.JWTConfig{Issuer: "marketplace", Audience: "api", TTL: time.Hour})
	idem := idempotency.NewMiddleware(idempotency.NewPostgresStore(pool), rs, log, httpx.PrincipalOrAnonymous, time.Hour)

	module := seller.NewModule(seller.Dependencies{
		Pool: pool, Clock: clock.System{}, RatingPolicy: domain.DefaultRatingPolicy(), CommissionPolicy: domain.DefaultCommissionPolicy(),
		Responder: rs, Idempotency: idem.Handler, Logger: log, Metrics: observability.NewCommandMetrics(prometheus.NewRegistry()),
	})
	router := httpx.NewRouter(rs)
	module.RegisterRoutes(router)
	server := httptest.NewServer(httpx.Chain(router, httpx.Recoverer(log, rs), httpx.Authenticate(jwt, rs)))
	t.Cleanup(server.Close)

	c := &client{t: t, server: server, jwt: jwt}
	owner := c.actor()
	stranger := c.actor()
	moderator := c.actor("content_moderator")
	admin := c.actor("platform_admin")
	ctx := context.Background()

	status, raw := c.call(owner, http.MethodPost, "/api/v1/seller/applications", map[string]string{
		"legal_form": "legal_entity", "legal_name": "ТОО Ромашка", "tax_id": validBIN, "legal_address": "г. Алматы, пр. Абая 1",
	}, true)
	require.Equal(t, http.StatusCreated, status, string(raw))
	var created struct {
		SellerID string `json:"seller_id"`
	}
	require.NoError(t, json.Unmarshal(raw, &created))
	base := "/api/v1/seller/sellers/" + created.SellerID

	status, raw = c.call(owner, http.MethodPost, base+"/submit", nil, true)
	assert.Equal(t, http.StatusUnprocessableEntity, status)
	assert.Equal(t, "SELLER_APPLICATION_INCOMPLETE", c.code(raw))

	status, _ = c.call(owner, http.MethodPut, base+"/bank-account", map[string]string{
		"iban": validIBAN, "bic": "HSBKKZKX", "bank_name": "Halyk Bank", "beneficiary": "ТОО Ромашка",
	}, false)
	require.Equal(t, http.StatusNoContent, status)
	for _, kind := range []string{"registration_certificate", "bank_confirmation", "charter"} {
		status, raw = c.call(owner, http.MethodPost, base+"/documents", map[string]string{"kind": kind, "object_key": "sellers/" + kind + ".pdf"}, true)
		require.Equal(t, http.StatusNoContent, status, string(raw))
	}

	status, raw = c.call(stranger, http.MethodGet, base, nil, false)
	assert.Equal(t, http.StatusNotFound, status)
	assert.Equal(t, "SELLER_NOT_FOUND", c.code(raw))

	status, _ = c.call(owner, http.MethodPost, base+"/submit", nil, true)
	require.Equal(t, http.StatusNoContent, status)

	status, raw = c.call(moderator, http.MethodGet, "/api/v1/admin/seller-applications?limit=10", nil, false)
	require.Equal(t, http.StatusOK, status)
	assert.Contains(t, string(raw), created.SellerID)
	status, _ = c.call(owner, http.MethodGet, "/api/v1/admin/seller-applications", nil, false)
	assert.Equal(t, http.StatusForbidden, status)

	status, raw = c.call(moderator, http.MethodPost, "/api/v1/admin/sellers/"+created.SellerID+"/approve", nil, true)
	require.Equal(t, http.StatusNoContent, status, string(raw))

	status, raw = c.call(owner, http.MethodGet, base, nil, false)
	require.Equal(t, http.StatusOK, status)
	var view struct {
		Status      string `json:"status"`
		BankAccount struct {
			MaskedIBAN string `json:"masked_iban"`
			Verified   bool   `json:"verified"`
		} `json:"bank_account"`
		Documents []any `json:"documents"`
	}
	require.NoError(t, json.Unmarshal(raw, &view))
	assert.Equal(t, "active", view.Status)
	assert.True(t, view.BankAccount.Verified)
	assert.Equal(t, "****************0100", view.BankAccount.MaskedIBAN)
	assert.NotContains(t, string(raw), validIBAN)
	assert.Len(t, view.Documents, 3)

	category := domain.NewCategoryID().String()
	status, raw = c.call(admin, http.MethodPut, "/api/v1/admin/sellers/"+created.SellerID+"/commission-overrides/"+category, map[string]int{}, false)
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Equal(t, "SELLER_BASIS_POINTS_REQUIRED", c.code(raw))
	status, _ = c.call(admin, http.MethodPut, "/api/v1/admin/sellers/"+created.SellerID+"/commission-overrides/"+category, map[string]int{"basis_points": 650}, false)
	require.Equal(t, http.StatusNoContent, status)
	otherCategory := domain.NewCategoryID().String()
	status, _ = c.call(admin, http.MethodPut, "/api/v1/admin/category-commissions/"+otherCategory, map[string]int{"basis_points": 1400}, false)
	require.Equal(t, http.StatusNoContent, status)

	directory := module.Directory()
	info, err := directory.Seller(ctx, created.SellerID)
	require.NoError(t, err)
	assert.True(t, info.CanSell)
	assert.True(t, info.PayoutsAllowed)
	rate, err := directory.CommissionRate(ctx, created.SellerID, category)
	require.NoError(t, err)
	assert.Equal(t, 650, rate)
	rate, err = directory.CommissionRate(ctx, created.SellerID, otherCategory)
	require.NoError(t, err)
	assert.Equal(t, 1400, rate)
	rate, err = directory.CommissionRate(ctx, created.SellerID, domain.NewCategoryID().String())
	require.NoError(t, err)
	assert.Equal(t, 1000, rate)
	role, ok, err := directory.MemberRole(ctx, created.SellerID, owner.id)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, api.RoleSellerAdmin, role)
	_, ok, err = directory.MemberRole(ctx, created.SellerID, stranger.id)
	require.NoError(t, err)
	assert.False(t, ok)
	_, err = directory.Seller(ctx, "bad")
	require.ErrorIs(t, err, api.ErrSellerNotFound)

	status, _ = c.call(moderator, http.MethodPost, "/api/v1/admin/sellers/"+created.SellerID+"/suspend", map[string]string{"reason": "fraud"}, true)
	assert.Equal(t, http.StatusForbidden, status)
	status, _ = c.call(admin, http.MethodPost, "/api/v1/admin/sellers/"+created.SellerID+"/suspend", map[string]string{"reason": "counterfeit"}, true)
	require.Equal(t, http.StatusNoContent, status)
	info, err = directory.Seller(ctx, created.SellerID)
	require.NoError(t, err)
	assert.False(t, info.CanSell)

	status, raw = c.call(admin, http.MethodPost, "/api/v1/admin/sellers/"+created.SellerID+"/approve", nil, true)
	assert.Equal(t, http.StatusConflict, status)
	assert.Equal(t, "SELLER_INVALID_TRANSITION", c.code(raw))

	status, raw = c.call(owner, http.MethodGet, "/api/v1/seller/sellers", nil, false)
	require.Equal(t, http.StatusOK, status)
	assert.Contains(t, string(raw), `"role":"seller_admin"`)

	var events, audits int
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM platform.outbox WHERE aggregate_id = $1", created.SellerID).Scan(&events))
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM platform.audit_log WHERE object_id = $1", created.SellerID).Scan(&audits))
	assert.Equal(t, 9, events)
	assert.Equal(t, 3, audits)
}
