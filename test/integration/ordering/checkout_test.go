//go:build integration

package ordering_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/cart"
	cartapp "github.com/gliedabrennung/go-marketplace-backend/internal/cart/application"
	cartapi "github.com/gliedabrennung/go-marketplace-backend/internal/cart/infrastructure/httpapi"
	catalogdomain "github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
	catalogpostgres "github.com/gliedabrennung/go-marketplace-backend/internal/catalog/infrastructure/postgres"
	"github.com/gliedabrennung/go-marketplace-backend/internal/inventory"
	inventoryapp "github.com/gliedabrennung/go-marketplace-backend/internal/inventory/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/ordering"
	orderingapp "github.com/gliedabrennung/go-marketplace-backend/internal/ordering/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/payment"
	"github.com/gliedabrennung/go-marketplace-backend/internal/pricing"
	pricingapp "github.com/gliedabrennung/go-marketplace-backend/internal/pricing/application"
	sellerapi "github.com/gliedabrennung/go-marketplace-backend/internal/seller/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/settlement"
	settlementapp "github.com/gliedabrennung/go-marketplace-backend/internal/settlement/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth/token"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/clock"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/config"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/httpx"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/idempotency"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/observability"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/outbox"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shipping/infrastructure/flat"
	"github.com/gliedabrennung/go-marketplace-backend/test/testdb"
)

var checkoutTables = []string{
	"ordering.checkout_sagas", "ordering.order_status_history", "ordering.order_parts", "ordering.order_items", "ordering.orders",
	"payment.refunds", "payment.payments", "payment.saved_methods", "payment.webhook_events", "payment.reconciliation_reports",
	"cart.items", "cart.carts", "inventory.stock_movements", "inventory.reservations", "inventory.stock_items",
	"pricing.promo_redemptions", "pricing.promo_codes", "pricing.promotions", "pricing.product_categories", "pricing.offer_prices",
	"catalog.offers", "catalog.product_attributes", "catalog.products", "catalog.categories",
	"settlement.entries", "platform.outbox", "platform.idempotency_keys",
}

type sellers struct {
	mu      sync.Mutex
	members map[string]string
}

func (s *sellers) Seller(_ context.Context, sellerID string) (sellerapi.SellerInfo, error) {
	return sellerapi.SellerInfo{ID: sellerID, Status: "active", CanSell: true}, nil
}

func (s *sellers) MemberRole(_ context.Context, sellerID, userID string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return sellerapi.RoleSellerAdmin, s.members[sellerID] == userID, nil
}

func (s *sellers) CommissionRate(context.Context, string, string) (int, error) {
	return 1000, nil
}

type shop struct {
	t         *testing.T
	pool      *pgxpool.Pool
	server    *httptest.Server
	jwt       *token.JWT
	browser   *http.Client
	deps      ordering.Dependencies
	consumers []outbox.Subscription
	handled   map[int64]bool
}

func newShop(t *testing.T) (*shop, *sellers) {
	t.Helper()
	pool := testdb.Pool(t)
	testdb.Truncate(t, pool, checkoutTables...)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	base := "http://" + listener.Addr().String()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	rs := httpx.NewResponder(log)
	ring, err := token.GenerateKeyRing("test")
	require.NoError(t, err)
	jwt := token.NewJWT(ring, token.JWTConfig{Issuer: "marketplace", Audience: "api", TTL: time.Hour})
	registry := prometheus.NewRegistry()
	metrics := observability.NewCommandMetrics(registry)
	idem := idempotency.NewMiddleware(idempotency.NewPostgresStore(pool), rs, log, httpx.PrincipalOrAnonymous, time.Hour)
	team := &sellers{members: map[string]string{}}
	settings := config.Payments{
		Provider: "sandbox", BaseURL: base + "/sandbox/psp", APIKey: "shop-key", WebhookSecret: "shop-webhook-secret-0123456789abcdef",
		WebhookTolerance: 5 * time.Minute, Timeout: 2 * time.Second, BreakerFailures: 5, BreakerCooldown: time.Second,
		SandboxPublicURL: base + "/sandbox/psp", SandboxWebhookURL: base + "/webhooks/payments/sandbox",
	}

	stock := inventory.NewModule(inventory.Dependencies{
		Pool: pool, Clock: clock.System{}, Policy: inventoryapp.DefaultPolicy(), Sellers: team, Responder: rs, Logger: log, Metrics: metrics,
	})
	prices := pricing.NewModule(pricing.Dependencies{
		Pool: pool, Clock: clock.System{}, Policy: pricingapp.DefaultPolicy(), Sellers: team, Responder: rs, Idempotency: idem.Handler,
		Logger: log, Metrics: metrics,
	})
	offers := catalogpostgres.NewOfferLookup(pool)
	tariffs := flat.New(flat.Settings{Fee: 99000, FreeFrom: 1500000})
	carts := cart.NewModule(cart.Dependencies{
		Pool: pool, Clock: clock.System{}, Policy: cartapp.DefaultPolicy(), Offers: offers, Stock: stock.Availability(),
		Pricing: prices.Pricer(), Tariffs: tariffs, Responder: rs, Logger: log, Metrics: metrics,
	})
	payments := payment.NewModule(payment.Dependencies{
		Pool: pool, Clock: clock.System{}, Providers: payment.NewProviders(settings, observability.NewExternalMetrics(registry)),
		Responder: rs, Logger: log, Metrics: metrics,
	})
	policy := orderingapp.DefaultPolicy()
	policy.ReturnURL = "https://shop.example/checkout/result"
	deps := ordering.Dependencies{
		Pool: pool, Clock: clock.System{}, Policy: policy, Carts: carts.Carts(), Offers: offers, Pricing: prices.Pricer(),
		Tariffs: tariffs, Inventory: stock.Reserver(), Payments: payments.Payments(), Sellers: team,
		Business: ordering.NewMetrics(registry), Responder: rs, Idempotency: idem.Handler, Logger: log, Metrics: metrics,
	}

	router := httpx.NewRouter(rs)
	stock.RegisterRoutes(router)
	prices.RegisterRoutes(router)
	carts.RegisterRoutes(router)
	payments.RegisterRoutes(router)
	ordering.NewModule(deps).RegisterRoutes(router)
	settlement.NewModule(settlement.Dependencies{
		Pool: pool, Clock: clock.System{}, Policy: settlementapp.DefaultPolicy(), Sellers: team, Responder: rs, Logger: log, Metrics: metrics,
	}).RegisterRoutes(router)
	router.Handle("/sandbox/psp/", payment.NewSandboxServer(settings, log))
	server := &httptest.Server{
		Listener: listener,
		Config:   &http.Server{Handler: httpx.Chain(router, httpx.Recoverer(log, rs), httpx.Authenticate(jwt, rs)), ReadHeaderTimeout: time.Second},
	}
	server.Start()
	t.Cleanup(server.Close)

	s := &shop{
		t: t, pool: pool, server: server, jwt: jwt, deps: deps, handled: map[int64]bool{},
		browser: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}
	s.consumers = slices(
		inventory.NewWorker(inventory.WorkerDependencies{Pool: pool, Clock: clock.System{}, Policy: inventoryapp.DefaultPolicy(), Sellers: team, Logger: log, Metrics: metrics}).Subscriptions(),
		pricing.NewWorker(pricing.WorkerDependencies{Pool: pool, Clock: clock.System{}, Policy: pricingapp.DefaultPolicy(), Logger: log, Metrics: metrics}).Subscriptions(),
		ordering.NewWorker(deps).Subscriptions(),
		settlement.NewWorker(settlement.WorkerDependencies{
			Pool: pool, Clock: clock.System{}, Policy: settlementapp.DefaultPolicy(), Sellers: team, Logger: log, Metrics: metrics,
		}).Subscriptions(),
	)
	return s, team
}

func slices(groups ...[]outbox.Subscription) []outbox.Subscription {
	var out []outbox.Subscription
	for _, group := range groups {
		out = append(out, group...)
	}
	return out
}

func (s *shop) relay() {
	s.t.Helper()
	ctx := context.Background()
	rows, err := s.pool.Query(ctx, `SELECT id, aggregate_id, event_name, payload FROM platform.outbox ORDER BY id`)
	require.NoError(s.t, err)
	var messages []outbox.Message
	for rows.Next() {
		var msg outbox.Message
		require.NoError(s.t, rows.Scan(&msg.ID, &msg.AggregateID, &msg.EventName, &msg.Payload))
		messages = append(messages, msg)
	}
	require.NoError(s.t, rows.Err())
	for _, msg := range messages {
		if s.handled[msg.ID] {
			continue
		}
		s.handled[msg.ID] = true
		for _, sub := range s.consumers {
			for _, name := range sub.EventNames {
				if name == msg.EventName {
					require.NoError(s.t, sub.Handle(ctx, msg), msg.EventName)
				}
			}
		}
	}
}

func (s *shop) call(p auth.Principal, method, path string, body any, headers map[string]string) (int, []byte) {
	s.t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(s.t, err)
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, s.server.URL+path, reader)
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

func (s *shop) decode(raw []byte, dst any) {
	s.t.Helper()
	require.NoError(s.t, json.Unmarshal(raw, dst), string(raw))
}

func (s *shop) seed(sellerIDs ...kernel.SellerID) []string {
	s.t.Helper()
	ctx := context.Background()
	repos := catalogpostgres.NewRepositories(s.pool, catalogpostgres.NewOutboxWriter())
	at := time.Now().UTC().Add(-time.Hour)
	root, err := catalogdomain.CreateCategory(catalogdomain.NewCategoryID(), "Электроника", "electronics", nil, at)
	require.NoError(s.t, err)
	require.NoError(s.t, repos.Categories().Save(ctx, root))
	class, err := catalogdomain.Classify([]*catalogdomain.Category{root})
	require.NoError(s.t, err)

	offers := make([]string, 0, len(sellerIDs))
	for i, seller := range sellerIDs {
		product, err := catalogdomain.CreateProduct(catalogdomain.NewProductID(), seller, class, catalogdomain.ProductContent{
			Title: "Наушники", Description: "Беспроводные", Brand: "Nova", Attributes: map[string]string{},
		}, at)
		require.NoError(s.t, err)
		require.NoError(s.t, product.SubmitForModeration(seller, class, at))
		require.NoError(s.t, product.Publish(kernel.NewUserID(), class, at))
		require.NoError(s.t, repos.Products().Save(ctx, product))
		sku, err := catalogdomain.NewSellerSKU("SKU-" + string(rune('A'+i)))
		require.NoError(s.t, err)
		terms, err := catalogdomain.NewOfferTerms(int64(500000*(i+1)), "KZT", "new", 1)
		require.NoError(s.t, err)
		offer, err := catalogdomain.CreateOffer(catalogdomain.NewOfferID(), product, seller, sku, terms, at)
		require.NoError(s.t, err)
		require.NoError(s.t, repos.Offers().Save(ctx, offer))
		offers = append(offers, offer.ID().String())
	}
	s.relay()
	return offers
}

type cartView struct {
	CartID     string `json:"cart_id"`
	ItemsCount int    `json:"items_count"`
	Subtotal   int64  `json:"subtotal"`
	Discount   int64  `json:"discount"`
	Shipping   int64  `json:"shipping"`
	Total      int64  `json:"total"`
	Ready      bool   `json:"ready"`
	PromoCode  string `json:"promo_code"`
	Groups     []struct {
		SellerID string `json:"seller_id"`
		Shipping int64  `json:"shipping"`
	} `json:"groups"`
}

type orderView struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	Total       int64  `json:"total"`
	Cancellable bool   `json:"cancellable"`
	Payment     *struct {
		PaymentID   string `json:"payment_id"`
		Status      string `json:"status"`
		RedirectURL string `json:"redirect_url"`
	} `json:"payment"`
	History []struct {
		To    string `json:"to"`
		Actor string `json:"actor"`
	} `json:"history"`
}

type placed struct {
	OrderID    string `json:"order_id"`
	Status     string `json:"status"`
	PaymentID  string `json:"payment_id"`
	PaymentURL string `json:"payment_url"`
	Total      int64  `json:"total"`
}

func principal(roles ...string) auth.Principal {
	return auth.Principal{UserID: kernel.NewUserID().String(), Roles: roles}
}

func key() map[string]string {
	return map[string]string{idempotency.HeaderKey: kernel.NewUserID().String()}
}

func (s *shop) stock(owner auth.Principal, sku string, quantity int) {
	s.t.Helper()
	status, raw := s.call(owner, http.MethodPatch, "/api/v1/seller/offers/"+sku+"/stock", map[string]any{"quantity": quantity, "reference": "seed"}, nil)
	require.Equal(s.t, http.StatusOK, status, string(raw))
}

func (s *shop) order(buyer auth.Principal, id string) orderView {
	s.t.Helper()
	status, raw := s.call(buyer, http.MethodGet, "/api/v1/orders/"+id, nil, nil)
	require.Equal(s.t, http.StatusOK, status, string(raw))
	var view orderView
	s.decode(raw, &view)
	return view
}

func (s *shop) available(sku string) int {
	s.t.Helper()
	var available int
	require.NoError(s.t, s.pool.QueryRow(context.Background(), `SELECT available FROM inventory.stock_items WHERE sku = $1`, sku).Scan(&available))
	return available
}

func (s *shop) place(buyer auth.Principal, headers map[string]string) (int, []byte) {
	return s.call(buyer, http.MethodPost, "/api/v1/orders", map[string]any{
		"address": map[string]any{
			"recipient": "Айгуль Сапарова", "phone": "+7 701 123 45 67", "city": "Алматы", "line": "пр. Абая, 10, кв. 5", "postal_code": "050000",
		},
		"delivery_method": "standard",
		"payment":         map[string]any{"save_method": true},
	}, headers)
}

func TestCheckoutEndToEnd(t *testing.T) {
	s, team := newShop(t)
	sellerA, sellerB := kernel.NewSellerID(), kernel.NewSellerID()
	ownerA, ownerB := principal("buyer"), principal("buyer")
	team.members[sellerA.String()], team.members[sellerB.String()] = ownerA.UserID, ownerB.UserID
	skus := s.seed(sellerA, sellerB)
	s.stock(ownerA, skus[0], 5)
	s.stock(ownerB, skus[1], 2)

	admin := principal("buyer", "platform_admin")
	status, raw := s.call(admin, http.MethodPost, "/api/v1/admin/pricing/promo-codes", map[string]any{
		"code": "WELCOME", "discount": map[string]any{"kind": "percentage", "basis_points": 1000}, "per_customer_limit": 1,
	}, key())
	require.Equal(t, http.StatusCreated, status, string(raw))

	device := map[string]string{cartapi.DeviceHeader: "device-e2e-0123456789abcdef"}
	status, raw = s.call(auth.Principal{}, http.MethodPost, "/api/v1/cart/items", map[string]any{"sku": skus[0], "quantity": 2}, device)
	require.Equal(t, http.StatusOK, status, string(raw))
	status, raw = s.call(auth.Principal{}, http.MethodGet, "/api/v1/cart", nil, nil)
	assert.Equal(t, http.StatusUnauthorized, status, string(raw))

	buyer := principal("buyer")
	status, raw = s.call(buyer, http.MethodPost, "/api/v1/cart/items", map[string]any{"sku": skus[1], "quantity": 1}, nil)
	require.Equal(t, http.StatusOK, status, string(raw))
	status, raw = s.call(buyer, http.MethodPost, "/api/v1/cart/merge", nil, device)
	require.Equal(t, http.StatusOK, status, string(raw))
	status, raw = s.call(buyer, http.MethodPost, "/api/v1/cart/promo-code", map[string]any{"code": "welcome"}, nil)
	require.Equal(t, http.StatusOK, status, string(raw))
	status, raw = s.call(buyer, http.MethodPost, "/api/v1/cart/checkout-preview", map[string]any{"delivery_method": "standard"}, nil)
	require.Equal(t, http.StatusOK, status, string(raw))
	var preview cartView
	s.decode(raw, &preview)
	require.True(t, preview.Ready, string(raw))
	assert.Equal(t, 3, preview.ItemsCount)
	assert.Equal(t, int64(2000000), preview.Subtotal)
	assert.Equal(t, int64(200000), preview.Discount)
	assert.Equal(t, int64(198000), preview.Shipping)
	assert.Equal(t, int64(1998000), preview.Total)

	headers := key()
	status, raw = s.place(buyer, headers)
	require.Equal(t, http.StatusCreated, status, string(raw))
	var created placed
	s.decode(raw, &created)
	assert.Equal(t, "awaiting_payment", created.Status)
	assert.Equal(t, preview.Total, created.Total)
	status, replay := s.place(buyer, headers)
	require.Equal(t, http.StatusCreated, status)
	assert.JSONEq(t, string(raw), string(replay))

	status, raw = s.call(buyer, http.MethodGet, "/api/v1/cart", nil, nil)
	require.Equal(t, http.StatusOK, status)
	var emptied cartView
	s.decode(raw, &emptied)
	assert.Zero(t, emptied.ItemsCount)
	assert.Equal(t, 3, s.available(skus[0]))
	assert.Equal(t, 1, s.available(skus[1]))

	view := s.order(buyer, created.OrderID)
	require.NotNil(t, view.Payment)
	assert.Equal(t, "pending", view.Payment.Status)
	assert.Equal(t, created.PaymentURL, view.Payment.RedirectURL)
	status, _ = s.call(principal("buyer"), http.MethodGet, "/api/v1/orders/"+created.OrderID, nil, nil)
	assert.Equal(t, http.StatusNotFound, status)

	resp, err := s.browser.Post(created.PaymentURL+"/approve", "application/x-www-form-urlencoded", nil)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	location, err := url.Parse(resp.Header.Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, created.OrderID, location.Query().Get("order_id"))
	assert.Equal(t, "authorized", location.Query().Get("status"))

	s.relay()
	view = s.order(buyer, created.OrderID)
	assert.Equal(t, "paid", view.Status)
	assert.Equal(t, "captured", view.Payment.Status)
	assert.Equal(t, []string{"created", "awaiting_payment", "paid"}, []string{view.History[0].To, view.History[1].To, view.History[2].To})
	var reserved int
	require.NoError(t, s.pool.QueryRow(context.Background(), `SELECT reserved FROM inventory.stock_items WHERE sku = $1`, skus[0]).Scan(&reserved))
	assert.Zero(t, reserved)

	status, raw = s.call(ownerA, http.MethodGet, "/api/v1/seller/orders?seller_id="+sellerA.String(), nil, nil)
	require.Equal(t, http.StatusOK, status, string(raw))
	assert.Contains(t, string(raw), created.OrderID)
	assert.NotContains(t, string(raw), skus[1])
	status, _ = s.call(ownerB, http.MethodGet, "/api/v1/seller/orders?seller_id="+sellerA.String(), nil, nil)
	assert.Equal(t, http.StatusNotFound, status)

	status, raw = s.call(buyer, http.MethodGet, "/api/v1/me/payment-methods", nil, nil)
	require.Equal(t, http.StatusOK, status)
	assert.Contains(t, string(raw), "Sandbox Visa")

	status, raw = s.call(buyer, http.MethodPost, "/api/v1/orders/"+created.OrderID+"/cancel", map[string]any{"reason": "передумал"}, key())
	require.Equal(t, http.StatusOK, status, string(raw))
	var cancelled orderView
	s.decode(raw, &cancelled)
	assert.Equal(t, "cancelled", cancelled.Status)
	assert.Equal(t, "refunded", cancelled.Payment.Status)
	assert.Equal(t, 5, s.available(skus[0]))
	assert.Equal(t, 2, s.available(skus[1]))
	var redemptions int
	require.NoError(t, s.pool.QueryRow(context.Background(), `SELECT count(*) FROM pricing.promo_redemptions`).Scan(&redemptions))
	assert.Zero(t, redemptions)

	status, raw = s.call(buyer, http.MethodGet, "/api/v1/orders?status=cancelled", nil, nil)
	require.Equal(t, http.StatusOK, status)
	assert.Contains(t, string(raw), created.OrderID)
	support := principal("support_agent")
	status, raw = s.call(support, http.MethodGet, "/api/v1/admin/checkout-sagas?status=compensated", nil, nil)
	require.Equal(t, http.StatusOK, status, string(raw))
	assert.Contains(t, string(raw), created.OrderID)
}

func TestCheckoutTimeoutAndRetry(t *testing.T) {
	s, team := newShop(t)
	seller := kernel.NewSellerID()
	owner := principal("buyer")
	team.members[seller.String()] = owner.UserID
	skus := s.seed(seller)
	s.stock(owner, skus[0], 3)

	buyer := principal("buyer")
	status, raw := s.call(buyer, http.MethodPost, "/api/v1/cart/items", map[string]any{"sku": skus[0], "quantity": 3}, nil)
	require.Equal(t, http.StatusOK, status, string(raw))
	status, raw = s.place(buyer, key())
	require.Equal(t, http.StatusCreated, status, string(raw))
	var created placed
	s.decode(raw, &created)

	resp, err := s.browser.Post(created.PaymentURL+"/decline", "application/x-www-form-urlencoded", nil)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	s.relay()
	assert.Equal(t, "failed", s.order(buyer, created.OrderID).Payment.Status)

	status, raw = s.call(buyer, http.MethodPost, "/api/v1/payments/"+created.PaymentID+"/retry", nil, key())
	require.Equal(t, http.StatusOK, status, string(raw))
	var retried placed
	s.decode(raw, &retried)
	assert.NotEqual(t, created.PaymentID, retried.PaymentID)
	assert.Equal(t, retried.PaymentID, s.order(buyer, created.OrderID).Payment.PaymentID)

	status, raw = s.call(buyer, http.MethodPost, "/api/v1/cart/items", map[string]any{"sku": skus[0], "quantity": 1}, nil)
	assert.Equal(t, http.StatusUnprocessableEntity, status, string(raw))

	later := s.deps
	later.Clock = clock.NewManual(time.Now().Add(25 * time.Minute))
	jobs := ordering.NewWorker(later).Jobs()
	require.Len(t, jobs, 4)
	for _, job := range jobs {
		require.NoError(t, job.Run(context.Background()), job.Name)
	}
	view := s.order(buyer, created.OrderID)
	assert.Equal(t, "failed", view.Status)
	assert.Equal(t, "cancelled", view.Payment.Status)
	assert.Equal(t, 3, s.available(skus[0]))

	resp, err = s.browser.Post(retried.PaymentURL+"/approve", "application/x-www-form-urlencoded", nil)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
}

type settlementReport struct {
	SellerID   string `json:"seller_id"`
	Currency   string `json:"currency"`
	Gross      int64  `json:"gross"`
	Commission int64  `json:"commission"`
	Net        int64  `json:"net"`
	Entries    []struct {
		OrderID string `json:"order_id"`
		Gross   int64  `json:"gross"`
	} `json:"entries"`
}

func TestFulfilmentAndSettlement(t *testing.T) {
	ctx := context.Background()
	s, team := newShop(t)
	seller := kernel.NewSellerID()
	owner := principal("buyer")
	team.members[seller.String()] = owner.UserID
	skus := s.seed(seller)
	s.stock(owner, skus[0], 5)

	buyer := principal("buyer")
	status, raw := s.call(buyer, http.MethodPost, "/api/v1/cart/items", map[string]any{"sku": skus[0], "quantity": 2}, nil)
	require.Equal(t, http.StatusOK, status, string(raw))
	status, raw = s.place(buyer, key())
	require.Equal(t, http.StatusCreated, status, string(raw))
	var created placed
	s.decode(raw, &created)

	resp, err := s.browser.Post(created.PaymentURL+"/approve", "application/x-www-form-urlencoded", nil)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	s.relay()
	require.Equal(t, "paid", s.order(buyer, created.OrderID).Status)

	stranger := principal("buyer")
	status, raw = s.call(stranger, http.MethodPost, "/api/v1/seller/orders/"+created.OrderID+"/ship", nil, nil)
	assert.Equal(t, http.StatusNotFound, status, string(raw))

	status, raw = s.call(owner, http.MethodPost, "/api/v1/seller/orders/"+created.OrderID+"/ship", nil, nil)
	require.Equal(t, http.StatusNoContent, status, string(raw))
	assert.Equal(t, "shipped", s.order(buyer, created.OrderID).Status)

	status, raw = s.call(owner, http.MethodPost, "/api/v1/seller/orders/"+created.OrderID+"/deliver", nil, nil)
	require.Equal(t, http.StatusNoContent, status, string(raw))
	assert.Equal(t, "delivered", s.order(buyer, created.OrderID).Status)

	later := s.deps
	later.Clock = clock.NewManual(time.Now().Add(15 * 24 * time.Hour))
	jobs := ordering.NewWorker(later).Jobs()
	require.NoError(t, jobs[3].Run(ctx))
	assert.Equal(t, "completed", s.order(buyer, created.OrderID).Status)

	s.relay()
	period := time.Now().UTC().Format("2006-01")
	status, raw = s.call(owner, http.MethodGet, "/api/v1/seller/settlements?seller_id="+seller.String()+"&period="+period, nil, nil)
	require.Equal(t, http.StatusOK, status, string(raw))
	var report settlementReport
	s.decode(raw, &report)
	assert.Equal(t, int64(1000000), report.Gross)
	assert.Equal(t, int64(100000), report.Commission)
	assert.Equal(t, int64(900000), report.Net)
	require.Len(t, report.Entries, 1)
	assert.Equal(t, created.OrderID, report.Entries[0].OrderID)

	status, _ = s.call(stranger, http.MethodGet, "/api/v1/seller/settlements?seller_id="+seller.String()+"&period="+period, nil, nil)
	assert.Equal(t, http.StatusNotFound, status)
	status, _ = s.call(owner, http.MethodGet, "/api/v1/seller/settlements?seller_id="+seller.String()+"&period=bogus", nil, nil)
	assert.Equal(t, http.StatusBadRequest, status)
}
