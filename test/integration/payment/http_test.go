//go:build integration

package payment_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/payment"
	paymentapi "github.com/gliedabrennung/go-marketplace-backend/internal/payment/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/payment/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/payment/infrastructure/psp/sandbox"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth/token"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/clock"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/config"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/httpx"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/observability"
	"github.com/gliedabrennung/go-marketplace-backend/test/testdb"
)

const secret = "integration-webhook-secret-0123456789"

type stand struct {
	t        *testing.T
	pool     *pgxpool.Pool
	server   *httptest.Server
	jwt      *token.JWT
	module   *payment.Module
	settings config.Payments
	browser  *http.Client
	outcomes map[string]int
}

func newStand(t *testing.T, allowlist []netip.Prefix) *stand {
	t.Helper()
	pool := testdb.Pool(t)
	testdb.Truncate(t, pool, paymentTables...)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	base := "http://" + listener.Addr().String()
	settings := config.Payments{
		Provider: "sandbox", BaseURL: base + "/sandbox/psp", APIKey: "integration-key", WebhookSecret: secret,
		WebhookTolerance: 5 * time.Minute, Timeout: 2 * time.Second, BreakerFailures: 5, BreakerCooldown: time.Second,
		SandboxPublicURL: base + "/sandbox/psp", SandboxWebhookURL: base + "/webhooks/payments/sandbox",
	}

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	rs := httpx.NewResponder(log)
	ring, err := token.GenerateKeyRing("test")
	require.NoError(t, err)
	jwt := token.NewJWT(ring, token.JWTConfig{Issuer: "marketplace", Audience: "api", TTL: time.Hour})
	registry := prometheus.NewRegistry()
	s := &stand{t: t, pool: pool, jwt: jwt, settings: settings, outcomes: map[string]int{}}
	s.module = payment.NewModule(payment.Dependencies{
		Pool: pool, Clock: clock.System{}, Providers: payment.NewProviders(settings, observability.NewExternalMetrics(registry)),
		Allowlist: allowlist, Outcomes: func(_, outcome string) { s.outcomes[outcome]++ },
		Responder: rs, Logger: log, Metrics: observability.NewCommandMetrics(registry),
	})
	router := httpx.NewRouter(rs)
	s.module.RegisterRoutes(router)
	router.Handle("/sandbox/psp/", payment.NewSandboxServer(settings, log))

	s.server = &httptest.Server{
		Listener: listener,
		Config:   &http.Server{Handler: httpx.Chain(router, httpx.Recoverer(log, rs), httpx.Authenticate(jwt, rs)), ReadHeaderTimeout: time.Second},
	}
	s.server.Start()
	t.Cleanup(s.server.Close)
	s.browser = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return s
}

func (s *stand) call(p auth.Principal, method, path string, body []byte, headers map[string]string) (int, []byte) {
	s.t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, s.server.URL+path, bytes.NewReader(body))
	require.NoError(s.t, err)
	if p.UserID != "" {
		signed, _, err := s.jwt.Issue(p, time.Now())
		require.NoError(s.t, err)
		req.Header.Set("Authorization", "Bearer "+signed)
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	resp, err := s.browser.Do(req)
	require.NoError(s.t, err)
	defer func() { require.NoError(s.t, resp.Body.Close()) }()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(s.t, err)
	return resp.StatusCode, raw
}

func (s *stand) create(buyer auth.Principal, amount int64, save bool, method string) paymentapi.Created {
	s.t.Helper()
	created, err := s.module.Payments().Create(context.Background(), paymentapi.CreateRequest{
		PaymentID: domain.NewPaymentID().String(), OrderID: domain.NewOrderID().String(), BuyerID: buyer.UserID,
		Amount: amount, Currency: "KZT", ReturnURL: "https://shop.example/checkout/return", SaveMethod: save, MethodID: method,
	})
	require.NoError(s.t, err)
	require.Equal(s.t, "pending", created.Status)
	return created
}

func (s *stand) press(created paymentapi.Created, action string) *url.URL {
	s.t.Helper()
	resp, err := s.browser.Post(created.RedirectURL+"/"+action, "application/x-www-form-urlencoded", nil)
	require.NoError(s.t, err)
	require.NoError(s.t, resp.Body.Close())
	require.Equal(s.t, http.StatusSeeOther, resp.StatusCode)
	location, err := url.Parse(resp.Header.Get("Location"))
	require.NoError(s.t, err)
	return location
}

func (s *stand) info(paymentID string) paymentapi.Info {
	s.t.Helper()
	info, err := s.module.Payments().Info(context.Background(), paymentID)
	require.NoError(s.t, err)
	return info
}

func (s *stand) outboxEvents(aggregate string) []string {
	s.t.Helper()
	rows, err := s.pool.Query(context.Background(),
		`SELECT event_name FROM platform.outbox WHERE aggregate_id = $1 ORDER BY id`, aggregate)
	require.NoError(s.t, err)
	var names []string
	for rows.Next() {
		var name string
		require.NoError(s.t, rows.Scan(&name))
		names = append(names, name)
	}
	require.NoError(s.t, rows.Err())
	return names
}

func buyer() auth.Principal {
	return auth.Principal{UserID: kernel.NewUserID().String(), Roles: []string{"buyer"}}
}

func TestHTTP_SandboxCheckoutPayment(t *testing.T) {
	ctx := context.Background()
	s := newStand(t, nil)
	owner := buyer()
	created := s.create(owner, 125000, true, "")

	resp, err := s.browser.Get(created.RedirectURL)
	require.NoError(t, err)
	page, _ := io.ReadAll(resp.Body)
	require.NoError(t, resp.Body.Close())
	assert.Contains(t, string(page), "1250.00 KZT")

	location := s.press(created, "approve")
	assert.Equal(t, "authorized", location.Query().Get("status"))
	assert.Equal(t, "authorized", s.info(created.PaymentID).Status)
	assert.Equal(t, 1, s.outcomes["payment.authorized"])

	payments := s.module.Payments()
	require.NoError(t, payments.Capture(ctx, created.PaymentID, 0))
	require.NoError(t, payments.Capture(ctx, created.PaymentID, 0))
	require.NoError(t, payments.Refund(ctx, created.PaymentID, domain.NewRefundID().String(), 25000, "partial"))
	info := s.info(created.PaymentID)
	assert.Equal(t, "captured", info.Status)
	assert.Equal(t, int64(125000), info.Captured)
	assert.Equal(t, int64(25000), info.Refunded)

	status, raw := s.call(owner, http.MethodGet, "/api/v1/payments/"+created.PaymentID, nil, nil)
	require.Equal(t, http.StatusOK, status, string(raw))
	assert.Contains(t, string(raw), `"status":"captured"`)
	assert.NotContains(t, string(raw), "redirect_url")
	status, _ = s.call(buyer(), http.MethodGet, "/api/v1/payments/"+created.PaymentID, nil, nil)
	assert.Equal(t, http.StatusNotFound, status)
	status, _ = s.call(auth.Principal{}, http.MethodGet, "/api/v1/payments/"+created.PaymentID, nil, nil)
	assert.Equal(t, http.StatusUnauthorized, status)

	status, raw = s.call(owner, http.MethodGet, "/api/v1/me/payment-methods", nil, nil)
	require.Equal(t, http.StatusOK, status, string(raw))
	var methods struct {
		Items []struct {
			ID    string `json:"id"`
			Label string `json:"label"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(raw, &methods))
	require.Len(t, methods.Items, 1)
	assert.NotContains(t, string(raw), "tok_")

	reuse := s.create(owner, 5000, false, methods.Items[0].ID)
	resp, err = s.browser.Get(reuse.RedirectURL)
	require.NoError(t, err)
	page, _ = io.ReadAll(resp.Body)
	require.NoError(t, resp.Body.Close())
	assert.Contains(t, string(page), "Сохранённый способ оплаты")
	require.NoError(t, payments.Cancel(ctx, reuse.PaymentID, "order expired"))
	assert.Equal(t, "cancelled", s.info(reuse.PaymentID).Status)

	status, _ = s.call(owner, http.MethodDelete, "/api/v1/me/payment-methods/"+methods.Items[0].ID, nil, nil)
	assert.Equal(t, http.StatusNoContent, status)
	status, raw = s.call(owner, http.MethodGet, "/api/v1/me/payment-methods", nil, nil)
	require.Equal(t, http.StatusOK, status)
	assert.JSONEq(t, `{"items":[]}`, string(raw))

	assert.Equal(t, []string{
		"payment.pending.v1", "payment.authorized.v1", "payment.captured.v1",
		"payment.refund_requested.v1", "payment.refund_completed.v1",
	}, s.outboxEvents(created.PaymentID))
}

func TestHTTP_WebhookSecurity(t *testing.T) {
	s := newStand(t, nil)
	owner := buyer()
	created := s.create(owner, 3000, false, "")
	intent := strings.TrimPrefix(created.RedirectURL, s.settings.SandboxPublicURL+"/pay/")
	body := []byte(`{"id":"evt_manual","type":"payment.authorized","created":1,"data":{"intent_id":"` + intent + `","amount":3000,"currency":"KZT"}}`)
	send := func(signature string) (int, []byte) {
		return s.call(auth.Principal{}, http.MethodPost, "/webhooks/payments/sandbox", body,
			map[string]string{sandbox.SignatureHeader: signature, "Content-Type": "application/json"})
	}

	status, _ := send(sandbox.Sign("wrong-secret", body, time.Now()))
	assert.Equal(t, http.StatusUnauthorized, status)
	status, _ = send(sandbox.Sign(secret, body, time.Now().Add(-10*time.Minute)))
	assert.Equal(t, http.StatusUnauthorized, status)
	status, _ = send("")
	assert.Equal(t, http.StatusUnauthorized, status)
	status, _ = s.call(auth.Principal{}, http.MethodPost, "/webhooks/payments/unknown", body, nil)
	assert.Equal(t, http.StatusNotFound, status)
	assert.Equal(t, "pending", s.info(created.PaymentID).Status)

	status, raw := send(sandbox.Sign(secret, body, time.Now()))
	require.Equal(t, http.StatusOK, status, string(raw))
	assert.JSONEq(t, `{"received":true,"duplicate":false}`, string(raw))
	status, raw = send(sandbox.Sign(secret, body, time.Now()))
	require.Equal(t, http.StatusOK, status, string(raw))
	assert.JSONEq(t, `{"received":true,"duplicate":true}`, string(raw))
	assert.Equal(t, "authorized", s.info(created.PaymentID).Status)
	assert.Equal(t, []string{"payment.pending.v1", "payment.authorized.v1"}, s.outboxEvents(created.PaymentID))

	large := bytes.Repeat([]byte("a"), 70<<10)
	status, _ = s.call(auth.Principal{}, http.MethodPost, "/webhooks/payments/sandbox", large, nil)
	assert.Equal(t, http.StatusBadRequest, status)

	blocked := newStand(t, []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")})
	status, _ = blocked.call(auth.Principal{}, http.MethodPost, "/webhooks/payments/sandbox", body,
		map[string]string{sandbox.SignatureHeader: sandbox.Sign(secret, body, time.Now())})
	assert.Equal(t, http.StatusForbidden, status)
}

func TestHTTP_DeclineLateApprovalAndReconciliation(t *testing.T) {
	ctx := context.Background()
	s := newStand(t, nil)
	owner := buyer()

	declined := s.create(owner, 1000, false, "")
	assert.Equal(t, "failed", s.press(declined, "decline").Query().Get("status"))
	assert.Equal(t, "failed", s.info(declined.PaymentID).Status)
	assert.Equal(t, 1, s.outcomes["payment.failed"])

	expired := s.create(owner, 2000, false, "")
	require.NoError(t, s.module.Payments().Cancel(ctx, expired.PaymentID, "timeout"))
	resp, err := s.browser.Post(expired.RedirectURL+"/approve", "application/x-www-form-urlencoded", nil)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	assert.Equal(t, http.StatusConflict, resp.StatusCode)

	captured := s.create(owner, 4000, false, "")
	s.press(captured, "approve")
	require.NoError(t, s.module.Payments().Capture(ctx, captured.PaymentID, 0))
	_, err = s.pool.Exec(ctx, `UPDATE payment.payments SET captured = 3000 WHERE id = $1`, captured.PaymentID)
	require.NoError(t, err)

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	worker := payment.NewWorker(payment.WorkerDependencies{
		Pool: s.pool, Clock: clock.NewManual(time.Now().UTC().Add(24 * time.Hour)),
		Providers: payment.NewProviders(s.settings, observability.NewExternalMetrics(prometheus.NewRegistry())),
		Logger:    log, Metrics: observability.NewCommandMetrics(prometheus.NewRegistry()),
	})
	jobs := worker.Jobs()
	require.Len(t, jobs, 2)
	require.NoError(t, jobs[0].Run(ctx))
	require.NoError(t, jobs[0].Run(ctx))
	require.NoError(t, jobs[1].Run(ctx))

	day := time.Now().UTC().Format(time.DateOnly)
	admin := auth.Principal{UserID: kernel.NewUserID().String(), Roles: []string{"platform_admin"}}
	status, raw := s.call(admin, http.MethodGet, "/api/v1/admin/payments/reconciliations/"+day, nil, nil)
	require.Equal(t, http.StatusOK, status, string(raw))
	var report struct {
		Checked    int `json:"checked"`
		Mismatches []struct {
			PaymentID string `json:"payment_id"`
			Field     string `json:"field"`
		} `json:"mismatches"`
	}
	require.NoError(t, json.Unmarshal(raw, &report))
	assert.Equal(t, 3, report.Checked)
	require.Len(t, report.Mismatches, 1)
	assert.Equal(t, captured.PaymentID, report.Mismatches[0].PaymentID)
	assert.Equal(t, "captured", report.Mismatches[0].Field)

	status, _ = s.call(owner, http.MethodGet, "/api/v1/admin/payments/reconciliations/"+day, nil, nil)
	assert.Equal(t, http.StatusForbidden, status)
	status, _ = s.call(admin, http.MethodGet, "/api/v1/admin/payments/reconciliations/yesterday", nil, nil)
	assert.Equal(t, http.StatusBadRequest, status)
	status, _ = s.call(admin, http.MethodGet, "/api/v1/admin/payments/reconciliations/2020-01-01", nil, nil)
	assert.Equal(t, http.StatusNotFound, status)
}
