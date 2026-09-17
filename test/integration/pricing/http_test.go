//go:build integration

package pricing_test

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

	catalogapi "github.com/gliedabrennung/go-marketplace-backend/internal/catalog/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/pricing"
	pricingapi "github.com/gliedabrennung/go-marketplace-backend/internal/pricing/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/pricing/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/pricing/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth/token"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/clock"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/httpx"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/idempotency"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/observability"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/outbox"
	"github.com/gliedabrennung/go-marketplace-backend/test/testdb"
)

type team map[string]string

func (m team) MemberRole(_ context.Context, sellerID, userID string) (string, bool, error) {
	return "seller_admin", m[sellerID] == userID, nil
}

type client struct {
	t      *testing.T
	server *httptest.Server
	jwt    *token.JWT
}

func (c *client) call(p auth.Principal, method, path string, body any, idempotent bool) (int, []byte) {
	c.t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(c.t, err)
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, c.server.URL+path, reader)
	require.NoError(c.t, err)
	if p.UserID != "" {
		signed, _, err := c.jwt.Issue(p, time.Now())
		require.NoError(c.t, err)
		req.Header.Set("Authorization", "Bearer "+signed)
	}
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

func deliver(t *testing.T, worker *pricing.Worker, eventName string, payload any) {
	t.Helper()
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	for _, sub := range worker.Subscriptions() {
		for _, name := range sub.EventNames {
			if name == eventName {
				require.NoError(t, sub.Handle(context.Background(), outbox.Message{EventName: eventName, Payload: raw}))
				return
			}
		}
	}
	t.Fatalf("no subscription for %s", eventName)
}

func TestHTTP_PromotionsQuoteAndRedemption(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Pool(t)
	testdb.Truncate(t, pool, pricingTables...)

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	rs := httpx.NewResponder(log)
	ring, err := token.GenerateKeyRing("test")
	require.NoError(t, err)
	jwt := token.NewJWT(ring, token.JWTConfig{Issuer: "marketplace", Audience: "api", TTL: time.Hour})
	idem := idempotency.NewMiddleware(idempotency.NewPostgresStore(pool), rs, log, httpx.PrincipalOrAnonymous, time.Hour)
	metrics := observability.NewCommandMetrics(prometheus.NewRegistry())

	seller := kernel.NewSellerID().String()
	owner := auth.Principal{UserID: kernel.NewUserID().String(), Roles: []string{"buyer"}}
	members := team{seller: owner.UserID}
	module := pricing.NewModule(pricing.Dependencies{
		Pool: pool, Clock: clock.System{}, Policy: application.DefaultPolicy(), Sellers: members,
		Responder: rs, Idempotency: idem.Handler, Logger: log, Metrics: metrics,
	})
	worker := pricing.NewWorker(pricing.WorkerDependencies{
		Pool: pool, Clock: clock.System{}, Policy: application.DefaultPolicy(), Logger: log, Metrics: metrics,
	})
	router := httpx.NewRouter(rs)
	module.RegisterRoutes(router)
	server := httptest.NewServer(httpx.Chain(router, httpx.Recoverer(log, rs), httpx.Authenticate(jwt, rs)))
	t.Cleanup(server.Close)
	c := &client{t: t, server: server, jwt: jwt}

	admin := auth.Principal{UserID: kernel.NewUserID().String(), Roles: []string{"buyer", "platform_admin"}}
	buyer := auth.Principal{UserID: kernel.NewUserID().String(), Roles: []string{"buyer"}}
	category := domain.NewCategoryID().String()
	phoneProduct := domain.NewProductID().String()
	phone := kernel.NewID[struct{}]().String()

	deliver(t, worker, catalogapi.EventOfferCreated, catalogapi.OfferV1{
		OfferID: phone, ProductID: phoneProduct, SellerID: seller, PriceAmount: 300000, Currency: "KZT", Status: "active",
	})
	deliver(t, worker, catalogapi.EventProductPublished, catalogapi.ProductPublishedV1{
		ProductID: phoneProduct, SellerID: seller, CategoryPath: []string{category},
	})

	status, raw := c.call(buyer, http.MethodPost, "/api/v1/admin/pricing/promotions", map[string]any{}, true)
	assert.Equal(t, http.StatusForbidden, status, string(raw))

	status, raw = c.call(admin, http.MethodPost, "/api/v1/admin/pricing/promotions", map[string]any{
		"name":     "Смартфоны -10%",
		"discount": map[string]any{"kind": "percentage", "basis_points": 1000},
		"target":   map[string]any{"categories": []string{category}},
		"priority": 10,
	}, true)
	require.Equal(t, http.StatusCreated, status, string(raw))
	var created struct {
		PromotionID string `json:"promotion_id"`
	}
	require.NoError(t, json.Unmarshal(raw, &created))

	status, raw = c.call(admin, http.MethodPut, "/api/v1/admin/pricing/promotions/"+created.PromotionID+"/status", map[string]string{"status": "active"}, false)
	require.Equal(t, http.StatusNoContent, status, string(raw))

	status, raw = c.call(admin, http.MethodGet, "/api/v1/admin/pricing/promotions?status=active", nil, false)
	require.Equal(t, http.StatusOK, status, string(raw))
	assert.Contains(t, string(raw), created.PromotionID)
	status, raw = c.call(admin, http.MethodGet, "/api/v1/admin/pricing/promotions/"+created.PromotionID, nil, false)
	require.Equal(t, http.StatusOK, status, string(raw))
	assert.Contains(t, string(raw), `"status":"active"`)

	status, raw = c.call(admin, http.MethodPost, "/api/v1/admin/pricing/promo-codes", map[string]any{
		"code": "phone-5000", "discount": map[string]any{"kind": "fixed", "amount": 5000},
		"min_cart_amount": 100000, "per_customer_limit": 1,
	}, true)
	require.Equal(t, http.StatusCreated, status, string(raw))
	status, raw = c.call(admin, http.MethodGet, "/api/v1/admin/pricing/promo-codes/PHONE-5000", nil, false)
	require.Equal(t, http.StatusOK, status, string(raw))

	status, raw = c.call(owner, http.MethodPut, "/api/v1/seller/offers/"+phone+"/compare-at-price", map[string]any{"compare_at": 350000}, false)
	require.Equal(t, http.StatusNoContent, status, string(raw))
	status, _ = c.call(buyer, http.MethodPut, "/api/v1/seller/offers/"+phone+"/compare-at-price", map[string]any{"compare_at": 350000}, false)
	assert.Equal(t, http.StatusNotFound, status)

	status, raw = c.call(buyer, http.MethodPost, "/api/v1/pricing/quote", map[string]any{
		"lines": []map[string]any{{"sku": phone, "quantity": 2}}, "promo_code": "PHONE-5000",
	}, false)
	require.Equal(t, http.StatusOK, status, string(raw))
	var quote struct {
		Subtotal  int64  `json:"subtotal"`
		Discount  int64  `json:"discount"`
		Total     int64  `json:"total"`
		PromoCode string `json:"promo_code"`
		Lines     []struct {
			CompareAt int64 `json:"compare_at"`
			Discounts []struct {
				Kind   string `json:"kind"`
				Amount int64  `json:"amount"`
			} `json:"discounts"`
		} `json:"lines"`
	}
	require.NoError(t, json.Unmarshal(raw, &quote))
	assert.Equal(t, int64(600000), quote.Subtotal)
	assert.Equal(t, int64(65000), quote.Discount)
	assert.Equal(t, int64(535000), quote.Total)
	assert.Equal(t, "PHONE-5000", quote.PromoCode)
	require.Len(t, quote.Lines, 1)
	assert.Equal(t, int64(350000), quote.Lines[0].CompareAt)
	require.Len(t, quote.Lines[0].Discounts, 2)

	status, raw = c.call(auth.Principal{}, http.MethodPost, "/api/v1/pricing/quote", map[string]any{
		"lines": []map[string]any{{"sku": "UNKNOWN", "quantity": 1}},
	}, false)
	assert.Equal(t, http.StatusNotFound, status, string(raw))

	order := domain.NewOrderID().String()
	pricer := module.Pricer()
	priced, err := pricer.Quote(ctx, pricingapi.QuoteRequest{Lines: []pricingapi.QuoteLine{{SKU: phone, Quantity: 1}}, CustomerID: buyer.UserID})
	require.NoError(t, err)
	assert.Equal(t, int64(270000), priced.Total)

	require.NoError(t, pricer.Redeem(ctx, "PHONE-5000", order, buyer.UserID, priced.Total, "KZT"))
	require.ErrorIs(t, pricer.Redeem(ctx, "PHONE-5000", order, buyer.UserID, priced.Total, "KZT"), pricingapi.ErrPromoAlreadyUsed)
	require.ErrorIs(t, pricer.Redeem(ctx, "PHONE-5000", domain.NewOrderID().String(), buyer.UserID, priced.Total, "KZT"), domain.ErrPromoCodePerBuyer)
	require.NoError(t, pricer.Release(ctx, "PHONE-5000", order))
	require.NoError(t, pricer.Redeem(ctx, "PHONE-5000", domain.NewOrderID().String(), buyer.UserID, priced.Total, "KZT"))

	deliver(t, worker, catalogapi.EventOfferStatusChanged, catalogapi.OfferV1{
		OfferID: phone, ProductID: phoneProduct, SellerID: seller, PriceAmount: 300000, Currency: "KZT", Status: "archived",
	})
	_, err = pricer.Quote(ctx, pricingapi.QuoteRequest{Lines: []pricingapi.QuoteLine{{SKU: phone, Quantity: 1}}})
	require.ErrorIs(t, err, domain.ErrOfferPriceInactive)

	var events int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM platform.outbox WHERE event_name LIKE 'pricing.%'`).Scan(&events))
	assert.Positive(t, events)
}
