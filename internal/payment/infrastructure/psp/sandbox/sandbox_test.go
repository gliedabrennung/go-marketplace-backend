package sandbox_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/payment/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/payment/infrastructure/psp"
	"github.com/gliedabrennung/go-marketplace-backend/internal/payment/infrastructure/psp/sandbox"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/breaker"
)

const (
	apiKey = "sk_test"
	secret = "whsec_test"
)

type receiver struct {
	mu     sync.Mutex
	client *sandbox.Client
	events []application.WebhookEvent
	errors []error
}

func (r *receiver) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	body, _ := io.ReadAll(req.Body)
	headers := map[string]string{}
	for name := range req.Header {
		headers[strings.ToLower(name)] = req.Header.Get(name)
	}
	event, err := r.client.Verify(headers, body, time.Now())
	r.mu.Lock()
	defer r.mu.Unlock()
	if err != nil {
		r.errors = append(r.errors, err)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	r.events = append(r.events, event)
}

func (r *receiver) received() ([]application.WebhookEvent, []error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]application.WebhookEvent(nil), r.events...), append([]error(nil), r.errors...)
}

type harness struct {
	server   *httptest.Server
	client   *sandbox.Client
	receiver *receiver
	browser  *http.Client
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	rec := &receiver{}
	hook := httptest.NewServer(rec)
	t.Cleanup(hook.Close)

	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	psp := sandbox.NewServer(sandbox.ServerConfig{
		APIKey: apiKey, Secret: secret, PublicURL: server.URL + "/sandbox/psp", WebhookURL: hook.URL,
	})
	mux.Handle("/sandbox/psp/", http.StripPrefix("/sandbox/psp", psp))

	client := sandbox.NewClient(sandbox.ClientConfig{BaseURL: server.URL + "/sandbox/psp", APIKey: apiKey, Secret: secret})
	rec.client = client
	browser := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &harness{server: server, client: client, receiver: rec, browser: browser}
}

func (h *harness) intent(t *testing.T, amount int64, save bool) application.Intent {
	t.Helper()
	intent, err := h.client.CreateIntent(context.Background(), application.IntentRequest{
		PaymentID: kernel.NewUserID().String(), OrderID: "order", Amount: kernel.MustMoney(amount, kernel.KZT),
		ReturnURL: "https://shop.example/checkout/return?order=1", Description: "order 1", SaveMethod: save,
	})
	require.NoError(t, err)
	return intent
}

type pressed struct {
	status   int
	location string
}

func (h *harness) press(t *testing.T, intent application.Intent, action string) pressed {
	t.Helper()
	resp, err := h.browser.Post(intent.RedirectURL+"/"+action, "application/x-www-form-urlencoded", nil)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	return pressed{status: resp.StatusCode, location: resp.Header.Get("Location")}
}

func money(amount int64) kernel.Money { return kernel.MustMoney(amount, kernel.KZT) }

func TestHappyPathAuthorizeCaptureRefund(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	intent := h.intent(t, 150000, true)
	assert.True(t, strings.HasPrefix(intent.ProviderPaymentID, "pi_"))

	page, err := h.browser.Get(intent.RedirectURL)
	require.NoError(t, err)
	html, _ := io.ReadAll(page.Body)
	_ = page.Body.Close()
	assert.Contains(t, string(html), "1500.00 KZT")
	assert.Contains(t, string(html), "approve")

	resp := h.press(t, intent, "approve")
	require.Equal(t, http.StatusSeeOther, resp.status)
	location, err := url.Parse(resp.location)
	require.NoError(t, err)
	assert.Equal(t, "authorized", location.Query().Get("status"))
	assert.Equal(t, "1", location.Query().Get("order"))

	events, failures := h.receiver.received()
	require.Empty(t, failures)
	require.Len(t, events, 1)
	assert.Equal(t, application.WebhookAuthorized, events[0].Type)
	assert.Equal(t, intent.ProviderPaymentID, events[0].ProviderPaymentID)
	assert.Equal(t, int64(150000), events[0].Amount)
	assert.NotEmpty(t, events[0].MethodToken)

	assert.Equal(t, http.StatusConflict, h.press(t, intent, "approve").status)

	require.NoError(t, h.client.Capture(ctx, intent.ProviderPaymentID, money(150000), "capture:1"))
	require.NoError(t, h.client.Capture(ctx, intent.ProviderPaymentID, money(150000), "capture:1"))
	refundID, err := h.client.Refund(ctx, intent.ProviderPaymentID, money(50000), "refund:r1")
	require.NoError(t, err)
	again, err := h.client.Refund(ctx, intent.ProviderPaymentID, money(50000), "refund:r1")
	require.NoError(t, err)
	assert.Equal(t, refundID, again)

	_, err = h.client.Refund(ctx, intent.ProviderPaymentID, money(200000), "refund:r2")
	require.ErrorIs(t, err, application.ErrProviderRejected)
	require.ErrorIs(t, h.client.Cancel(ctx, intent.ProviderPaymentID, "cancel:1"), application.ErrProviderRejected)

	txs, err := h.client.Transactions(ctx, time.Now())
	require.NoError(t, err)
	require.Len(t, txs, 1)
	assert.Equal(t, application.ProviderTransaction{
		ProviderPaymentID: intent.ProviderPaymentID, Status: "captured", Captured: 150000, Refunded: 50000, Currency: "KZT",
	}, txs[0])

	_, err = h.client.Refund(ctx, intent.ProviderPaymentID, money(100000), "refund:r3")
	require.NoError(t, err)
	txs, err = h.client.Transactions(ctx, time.Now())
	require.NoError(t, err)
	assert.Equal(t, "refunded", txs[0].Status)
}

func TestDeclineAndCancel(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	declined := h.intent(t, 1000, false)
	resp := h.press(t, declined, "decline")
	require.Equal(t, http.StatusSeeOther, resp.status)
	events, _ := h.receiver.received()
	require.Len(t, events, 1)
	assert.Equal(t, application.WebhookFailed, events[0].Type)
	assert.Equal(t, "declined by customer", events[0].Reason)
	require.NoError(t, h.client.Cancel(ctx, declined.ProviderPaymentID, "cancel:declined"))

	pending := h.intent(t, 1000, false)
	require.NoError(t, h.client.Cancel(ctx, pending.ProviderPaymentID, "cancel:pending"))
	assert.Equal(t, http.StatusConflict, h.press(t, pending, "approve").status)
	require.ErrorIs(t, h.client.Capture(ctx, pending.ProviderPaymentID, money(1000), "capture:pending"), application.ErrProviderRejected)

	page, err := h.browser.Get(pending.RedirectURL)
	require.NoError(t, err)
	html, _ := io.ReadAll(page.Body)
	_ = page.Body.Close()
	assert.Contains(t, string(html), "cancelled")

	missing, err := h.browser.Get(h.server.URL + "/sandbox/psp/pay/pi_missing")
	require.NoError(t, err)
	_ = missing.Body.Close()
	assert.Equal(t, http.StatusNotFound, missing.StatusCode)
}

func TestRejectsInvalidRequests(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	cases := []application.IntentRequest{
		{PaymentID: "p1", Amount: money(0), ReturnURL: "https://shop.example"},
		{PaymentID: "", Amount: money(10), ReturnURL: "https://shop.example"},
		{PaymentID: "p3", Amount: money(10), ReturnURL: "javascript:alert(1)"},
	}
	for _, request := range cases {
		_, err := h.client.CreateIntent(ctx, request)
		require.ErrorIs(t, err, application.ErrProviderRejected)
	}
	_, err := h.client.Transactions(ctx, time.Time{})
	require.NoError(t, err)

	intruder := sandbox.NewClient(sandbox.ClientConfig{BaseURL: h.server.URL + "/sandbox/psp", APIKey: "wrong"})
	_, err = intruder.CreateIntent(ctx, application.IntentRequest{PaymentID: "p", Amount: money(10), ReturnURL: "https://shop.example"})
	require.Error(t, err)
	assert.NotErrorIs(t, err, application.ErrProviderRejected)
}

func TestVerifier(t *testing.T) {
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	body := []byte(`{"id":"evt_1","type":"payment.authorized","created":1,"data":{"intent_id":"pi_1","amount":100,"currency":"KZT"}}`)
	verifier := sandbox.NewVerifier(secret, 5*time.Minute)
	header := func(value string) map[string]string { return map[string]string{"x-sandbox-signature": value} }

	event, err := verifier.Verify(header(sandbox.Sign(secret, body, now.Add(-4*time.Minute))), body, now)
	require.NoError(t, err)
	assert.Equal(t, "evt_1", event.ID)
	assert.Equal(t, "pi_1", event.ProviderPaymentID)

	rotated := "t=" + strings.TrimPrefix(strings.Split(sandbox.Sign(secret, body, now), ",")[0], "t=") +
		",v1=deadbeef," + strings.Split(sandbox.Sign(secret, body, now), ",")[1]
	_, err = verifier.Verify(header(rotated), body, now)
	require.NoError(t, err)

	failures := map[string]map[string]string{
		"expired":   header(sandbox.Sign(secret, body, now.Add(-6*time.Minute))),
		"future":    header(sandbox.Sign(secret, body, now.Add(6*time.Minute))),
		"secret":    header(sandbox.Sign("other", body, now)),
		"missing":   {},
		"garbage":   header("t=abc,v1=00"),
		"no-digest": header("t=1"),
	}
	for name, headers := range failures {
		_, err := verifier.Verify(headers, body, now)
		require.ErrorIs(t, err, application.ErrInvalidSignature, name)
	}
	tampered := []byte(strings.Replace(string(body), "100", "1", 1))
	_, err = verifier.Verify(header(sandbox.Sign(secret, body, now)), tampered, now)
	require.ErrorIs(t, err, application.ErrInvalidSignature)

	malformed := []byte(`{"type":"payment.authorized"}`)
	_, err = verifier.Verify(header(sandbox.Sign(secret, malformed, now)), malformed, now)
	require.ErrorIs(t, err, application.ErrInvalidWebhook)

	_, err = sandbox.NewVerifier("", 0).Verify(header(sandbox.Sign("", body, now)), body, now)
	require.ErrorIs(t, err, application.ErrInvalidSignature)
}

func TestClientOpensBreakerOnOutage(t *testing.T) {
	calls := 0
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(down.Close)
	client := sandbox.NewClient(sandbox.ClientConfig{
		BaseURL: down.URL, APIKey: apiKey, Timeout: time.Second,
		Breaker: breaker.New(breaker.Settings{Failures: 2, Cooldown: time.Hour}),
	})
	ctx := context.Background()
	for range 2 {
		require.Error(t, client.Cancel(ctx, "pi_1", "cancel:1"))
	}
	require.ErrorIs(t, client.Cancel(ctx, "pi_1", "cancel:1"), breaker.ErrOpen)
	assert.Equal(t, 2, calls)
}

func TestRegistry(t *testing.T) {
	client := sandbox.NewClient(sandbox.ClientConfig{BaseURL: "http://localhost"})
	registry := psp.NewRegistry(client)
	assert.Equal(t, "sandbox", registry.Default().Name())
	found, err := registry.Get("sandbox")
	require.NoError(t, err)
	assert.Same(t, client, found)
	_, err = registry.Get("stripe")
	require.ErrorIs(t, err, application.ErrUnknownProvider)
}
