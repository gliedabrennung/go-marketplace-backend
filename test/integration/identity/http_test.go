//go:build integration

package identity_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/identity"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/infrastructure/postgres"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/infrastructure/security"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth/token"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/clock"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/httpx"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/idempotency"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/inbox"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/observability"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/outbox"
	"github.com/gliedabrennung/go-marketplace-backend/test/testdb"
)

const password = "correct horse 42"

type allowAll struct{}

func (allowAll) Allow(context.Context, application.LimitedAction, string) error { return nil }

type captureSender struct {
	mu    sync.Mutex
	links map[string]string
}

func (s *captureSender) SendEmailConfirmation(_ context.Context, email domain.Email, t string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.links[email.String()] = t
	return nil
}

func (s *captureSender) link(email string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.links[email]
}

type harness struct {
	t          *testing.T
	pool       *pgxpool.Pool
	server     *httptest.Server
	sender     *captureSender
	log        *slog.Logger
	registered map[string]bool
}

type tokens struct {
	UserID       string `json:"user_id"`
	SessionID    string `json:"session_id"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	pool := testdb.Pool(t)
	testdb.Truncate(t, pool, identityTables...)

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	rs := httpx.NewResponder(log)
	ring, err := token.GenerateKeyRing("test")
	require.NoError(t, err)
	jwt := token.NewJWT(ring, token.JWTConfig{Issuer: "marketplace", Audience: "api", TTL: 15 * time.Minute})
	hasher, err := security.NewArgon2idHasher(security.Argon2Params{MemoryKiB: 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32})
	require.NoError(t, err)
	sender := &captureSender{links: map[string]string{}}
	idem := idempotency.NewMiddleware(idempotency.NewPostgresStore(pool), rs, log, httpx.PrincipalOrAnonymous, time.Hour)

	router := httpx.NewRouter(rs)
	identity.NewModule(identity.Dependencies{
		Pool: pool, Limiter: allowAll{}, Tokens: jwt, Sender: sender, Hasher: hasher,
		Clock: clock.System{}, Policy: application.DefaultPolicy(), Responder: rs,
		Idempotency: idem.Handler, Logger: log, Metrics: observability.NewCommandMetrics(prometheus.NewRegistry()),
	}).RegisterRoutes(router)

	server := httptest.NewServer(httpx.Chain(router, httpx.Recoverer(log, rs), httpx.Authenticate(jwt, rs)))
	t.Cleanup(server.Close)
	return &harness{t: t, pool: pool, server: server, sender: sender, log: log, registered: map[string]bool{}}
}

func (h *harness) call(method, path, bearer string, body any, headers map[string]string) (int, []byte, http.Header) {
	h.t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(h.t, err)
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, h.server.URL+path, reader)
	require.NoError(h.t, err)
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := h.server.Client().Do(req)
	require.NoError(h.t, err)
	defer func() { require.NoError(h.t, resp.Body.Close()) }()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(h.t, err)
	return resp.StatusCode, raw, resp.Header
}

func (h *harness) problemCode(raw []byte) string {
	h.t.Helper()
	var p httpx.Problem
	require.NoError(h.t, json.Unmarshal(raw, &p))
	return p.Code
}

func (h *harness) signIn(email string) tokens {
	h.t.Helper()
	credentials := map[string]string{"email": email, "password": password}
	if !h.registered[email] {
		status, raw, _ := h.call(http.MethodPost, "/api/v1/auth/email/register", "", credentials, nil)
		require.Equal(h.t, http.StatusCreated, status, string(raw))
		status, raw, _ = h.call(http.MethodPost, "/api/v1/auth/email/confirm", "", map[string]string{"token": h.sender.link(email)}, nil)
		require.Equal(h.t, http.StatusNoContent, status, string(raw))
		h.registered[email] = true
	}
	status, raw, _ := h.call(http.MethodPost, "/api/v1/auth/email/sign-in", "", map[string]string{
		"email": email, "password": password, "device_name": "integration",
	}, nil)
	require.Equal(h.t, http.StatusOK, status, string(raw))
	var tk tokens
	require.NoError(h.t, json.Unmarshal(raw, &tk))
	return tk
}

func TestHTTP_SignInAndRefreshRotation(t *testing.T) {
	h := newHarness(t)
	first := h.signIn("buyer@example.kz")

	status, raw, _ := h.call(http.MethodGet, "/api/v1/me", first.AccessToken, nil, nil)
	require.Equal(t, http.StatusOK, status)
	assert.Contains(t, string(raw), `"email":"buyer@example.kz"`)
	assert.NotContains(t, string(raw), "phone")

	status, raw, _ = h.call(http.MethodGet, "/api/v1/me", "", nil, nil)
	assert.Equal(t, http.StatusUnauthorized, status)
	assert.Equal(t, "UNAUTHENTICATED", h.problemCode(raw))

	status, raw, _ = h.call(http.MethodPost, "/api/v1/auth/token/refresh", "", map[string]string{"refresh_token": first.RefreshToken}, nil)
	require.Equal(t, http.StatusOK, status)
	var rotated tokens
	require.NoError(t, json.Unmarshal(raw, &rotated))
	assert.Equal(t, first.SessionID, rotated.SessionID)

	status, raw, _ = h.call(http.MethodPost, "/api/v1/auth/token/refresh", "", map[string]string{"refresh_token": first.RefreshToken}, nil)
	assert.Equal(t, http.StatusUnauthorized, status)
	assert.Equal(t, "IDENTITY_REFRESH_TOKEN_REUSED", h.problemCode(raw))

	status, raw, _ = h.call(http.MethodPost, "/api/v1/auth/token/refresh", "", map[string]string{"refresh_token": rotated.RefreshToken}, nil)
	assert.Equal(t, http.StatusUnauthorized, status)
	assert.Equal(t, "IDENTITY_SESSION_REVOKED", h.problemCode(raw))

	second := h.signIn("buyer@example.kz")
	status, raw, _ = h.call(http.MethodGet, "/api/v1/me/sessions?limit=10", second.AccessToken, nil, nil)
	require.Equal(t, http.StatusOK, status)
	var page struct {
		Data []struct {
			ID      string `json:"id"`
			Current bool   `json:"current"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(raw, &page))
	require.Len(t, page.Data, 1)
	assert.True(t, page.Data[0].Current)

	status, _, _ = h.call(http.MethodPost, "/api/v1/auth/sign-out", second.AccessToken, nil, nil)
	assert.Equal(t, http.StatusNoContent, status)
	status, _, _ = h.call(http.MethodPost, "/api/v1/auth/token/refresh", "", map[string]string{"refresh_token": second.RefreshToken}, nil)
	assert.Equal(t, http.StatusUnauthorized, status)
}

func TestHTTP_EmailRegistrationRequiresConfirmation(t *testing.T) {
	h := newHarness(t)
	creds := map[string]string{"email": "buyer@example.kz", "password": password}

	status, _, _ := h.call(http.MethodPost, "/api/v1/auth/email/register", "", creds, nil)
	require.Equal(t, http.StatusCreated, status)

	status, raw, _ := h.call(http.MethodPost, "/api/v1/auth/email/sign-in", "", creds, nil)
	assert.Equal(t, http.StatusUnauthorized, status)
	assert.Equal(t, "IDENTITY_INVALID_CREDENTIALS", h.problemCode(raw))

	status, raw, _ = h.call(http.MethodPost, "/api/v1/auth/email/confirm", "", map[string]string{"token": "garbage"}, nil)
	assert.Equal(t, http.StatusUnauthorized, status)
	assert.Equal(t, "IDENTITY_INVALID_CONFIRMATION_TOKEN", h.problemCode(raw))

	status, _, _ = h.call(http.MethodPost, "/api/v1/auth/email/confirm", "", map[string]string{"token": h.sender.link("buyer@example.kz")}, nil)
	require.Equal(t, http.StatusNoContent, status)

	status, raw, _ = h.call(http.MethodPost, "/api/v1/auth/email/sign-in", "", creds, nil)
	require.Equal(t, http.StatusOK, status, string(raw))

	status, raw, _ = h.call(http.MethodPost, "/api/v1/auth/email/register", "", map[string]string{"email": "x@example.kz", "password": "short"}, nil)
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Equal(t, "IDENTITY_WEAK_PASSWORD", h.problemCode(raw))

	status, _, _ = h.call(http.MethodPost, "/api/v1/auth/phone/code", "", map[string]string{"phone": "+77011234567"}, nil)
	assert.Equal(t, http.StatusNotFound, status, "phone sign-in is not available")
}

func TestHTTP_AdminBlockRevokesSessionsThroughOutbox(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	adminTokens := h.signIn("admin@example.kz")
	repos := postgres.NewRepositories(h.pool, postgres.NewOutboxWriter())
	adminID, err := kernel.ParseUserID(adminTokens.UserID)
	require.NoError(t, err)
	admin, err := repos.Users().FindByID(ctx, adminID)
	require.NoError(t, err)
	require.NoError(t, admin.GrantRole(domain.RolePlatformAdmin, adminID, time.Now()))
	require.NoError(t, repos.Users().Save(ctx, admin))
	adminTokens = h.signIn("admin@example.kz")

	target := h.signIn("buyer@example.kz")
	h.signIn("buyer@example.kz")
	path := "/api/v1/admin/users/" + target.UserID + "/block"
	body := map[string]string{"reason": "chargeback fraud"}

	status, raw, _ := h.call(http.MethodPost, path, adminTokens.AccessToken, body, nil)
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Equal(t, "IDEMPOTENCY_KEY_REQUIRED", h.problemCode(raw))

	status, raw, _ = h.call(http.MethodPost, path, target.AccessToken, body, map[string]string{"Idempotency-Key": kernel.NewID[struct{}]().String()})
	assert.Equal(t, http.StatusForbidden, status)
	assert.Equal(t, "FORBIDDEN", h.problemCode(raw))

	key := map[string]string{"Idempotency-Key": kernel.NewID[struct{}]().String()}
	status, _, _ = h.call(http.MethodPost, path, adminTokens.AccessToken, body, key)
	require.Equal(t, http.StatusNoContent, status)
	status, _, header := h.call(http.MethodPost, path, adminTokens.AccessToken, body, key)
	assert.Equal(t, http.StatusNoContent, status)
	assert.Equal(t, "true", header.Get(idempotency.HeaderReplayed))

	status, raw, _ = h.call(http.MethodPost, "/api/v1/auth/token/refresh", "", map[string]string{"refresh_token": target.RefreshToken}, nil)
	assert.Equal(t, http.StatusForbidden, status)
	assert.Equal(t, "IDENTITY_USER_BLOCKED", h.problemCode(raw))

	var auditEntries int
	require.NoError(t, h.pool.QueryRow(ctx, "SELECT count(*) FROM platform.audit_log WHERE action = 'identity.user.block' AND object_id = $1", target.UserID).Scan(&auditEntries))
	assert.Equal(t, 1, auditEntries)

	worker := identity.NewWorker(identity.WorkerDependencies{
		Pool: h.pool, Clock: clock.System{}, Logger: h.log, Metrics: observability.NewCommandMetrics(prometheus.NewRegistry()),
	})
	dispatcher := outbox.NewDispatcher(inbox.NewGuard(h.pool), h.log, prometheus.NewRegistry(), worker.Subscriptions()...)
	relay := outbox.NewRelay(h.pool, "platform", "outbox", dispatcher, outbox.DefaultRelayConfig(), h.log, outbox.NewRelayMetrics(prometheus.NewRegistry()))
	for {
		n, err := relay.ProcessBatch(ctx)
		require.NoError(t, err)
		if n == 0 {
			break
		}
	}

	var unpublished, processed int
	require.NoError(t, h.pool.QueryRow(ctx, "SELECT count(*) FROM platform.outbox WHERE published_at IS NULL").Scan(&unpublished))
	require.NoError(t, h.pool.QueryRow(ctx, "SELECT count(*) FROM platform.processed_messages WHERE consumer_group = 'identity.revoke_sessions_on_block'").Scan(&processed))
	assert.Zero(t, unpublished)
	assert.Equal(t, 1, processed)

	ids, err := postgres.NewReadModel(h.pool).ActiveSessionIDs(ctx, target.UserID)
	require.NoError(t, err)
	assert.Empty(t, ids)

	rows, err := h.pool.Query(ctx, "SELECT revoke_reason FROM identity.sessions WHERE user_id = $1 ORDER BY created_at", target.UserID)
	require.NoError(t, err)
	var reasons []string
	for rows.Next() {
		var reason string
		require.NoError(t, rows.Scan(&reason))
		reasons = append(reasons, reason)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []string{"user_blocked", "user_blocked"}, reasons)
	assert.Equal(t, api.EventUserBlocked, "identity.user_blocked.v1")
}
