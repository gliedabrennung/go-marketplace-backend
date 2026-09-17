//go:build integration

package inventory_test

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

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/inventory"
	inventoryapi "github.com/gliedabrennung/go-marketplace-backend/internal/inventory/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/inventory/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/inventory/domain"
	sellerapi "github.com/gliedabrennung/go-marketplace-backend/internal/seller/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth/token"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/clock"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/httpx"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/observability"
	"github.com/gliedabrennung/go-marketplace-backend/test/testdb"
)

type members struct {
	mu      sync.Mutex
	sellers map[string]map[string]string
}

func (m *members) Seller(_ context.Context, sellerID string) (sellerapi.SellerInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sellers[sellerID]; !ok {
		return sellerapi.SellerInfo{}, sellerapi.ErrSellerNotFound
	}
	return sellerapi.SellerInfo{ID: sellerID, Status: "active", CanSell: true}, nil
}

func (m *members) MemberRole(_ context.Context, sellerID, userID string) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	role, ok := m.sellers[sellerID][userID]
	return role, ok, nil
}

func (m *members) register(sellerID, userID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sellers[sellerID] = map[string]string{userID: sellerapi.RoleSellerAdmin}
}

type client struct {
	t      *testing.T
	server *httptest.Server
	jwt    *token.JWT
}

func (c *client) token(userID string) string {
	c.t.Helper()
	signed, _, err := c.jwt.Issue(auth.Principal{UserID: userID, Roles: []string{"buyer"}}, time.Now())
	require.NoError(c.t, err)
	return signed
}

func (c *client) call(userID, method, path string, body any) (int, []byte) {
	c.t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(c.t, err)
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, c.server.URL+path, reader)
	require.NoError(c.t, err)
	req.Header.Set("Authorization", "Bearer "+c.token(userID))
	resp, err := c.server.Client().Do(req)
	require.NoError(c.t, err)
	defer func() { require.NoError(c.t, resp.Body.Close()) }()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(c.t, err)
	return resp.StatusCode, raw
}

func TestHTTP_SellerStockAndReservations(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Pool(t)
	testdb.Truncate(t, pool, inventoryTables...)

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	rs := httpx.NewResponder(log)
	ring, err := token.GenerateKeyRing("test")
	require.NoError(t, err)
	jwt := token.NewJWT(ring, token.JWTConfig{Issuer: "marketplace", Audience: "api", TTL: time.Hour})

	directory := &members{sellers: map[string]map[string]string{}}
	module := inventory.NewModule(inventory.Dependencies{
		Pool: pool, Clock: clock.System{}, Policy: application.DefaultPolicy(), Sellers: directory,
		Responder: rs, Logger: log, Metrics: observability.NewCommandMetrics(prometheus.NewRegistry()),
	})
	router := httpx.NewRouter(rs)
	module.RegisterRoutes(router)
	server := httptest.NewServer(httpx.Chain(router, httpx.Recoverer(log, rs), httpx.Authenticate(jwt, rs)))
	t.Cleanup(server.Close)

	owner := kernel.NewUserID().String()
	stranger := kernel.NewUserID().String()
	c := &client{t: t, server: server, jwt: jwt}

	seedStock(ctx, t, pool, "OFFER-1", 0)
	seedStock(ctx, t, pool, "OFFER-2", 0)
	var sellerID string
	require.NoError(t, pool.QueryRow(ctx, `SELECT seller_id::text FROM inventory.stock_items WHERE sku = 'OFFER-1'`).Scan(&sellerID))
	directory.register(sellerID, owner)

	status, raw := c.call(owner, http.MethodPatch, "/api/v1/seller/offers/OFFER-1/stock", map[string]any{})
	assert.Equal(t, http.StatusBadRequest, status, string(raw))

	status, raw = c.call(owner, http.MethodPatch, "/api/v1/seller/offers/OFFER-1/stock", map[string]any{"quantity": 12, "reference": "sync-1"})
	require.Equal(t, http.StatusOK, status, string(raw))
	var stock struct {
		SKU       string `json:"sku"`
		Available int    `json:"available"`
		Reserved  int    `json:"reserved"`
	}
	require.NoError(t, json.Unmarshal(raw, &stock))
	assert.Equal(t, "OFFER-1", stock.SKU)
	assert.Equal(t, 12, stock.Available)

	status, _ = c.call(stranger, http.MethodPatch, "/api/v1/seller/offers/OFFER-1/stock", map[string]any{"quantity": 1})
	assert.Equal(t, http.StatusNotFound, status)
	status, _ = c.call(owner, http.MethodGet, "/api/v1/seller/offers/UNKNOWN/stock", nil)
	assert.Equal(t, http.StatusNotFound, status)

	status, raw = c.call(owner, http.MethodGet, "/api/v1/seller/offers/OFFER-1/stock/movements", nil)
	require.Equal(t, http.StatusOK, status, string(raw))
	assert.Contains(t, string(raw), "correction")

	status, raw = c.call(owner, http.MethodGet, "/api/v1/seller/sellers/"+sellerID+"/stock", nil)
	require.Equal(t, http.StatusOK, status, string(raw))
	assert.Contains(t, string(raw), "OFFER-1")

	reservationID := domain.NewReservationID().String()
	reservation, err := module.Reserver().Reserve(ctx, reservationID, kernel.NewID[struct{}]().String(),
		[]inventoryapi.ReserveLine{{SKU: "OFFER-1", Quantity: 3}})
	require.NoError(t, err)
	assert.Equal(t, reservationID, reservation.ReservationID)

	status, raw = c.call(owner, http.MethodGet, "/api/v1/seller/reservations/"+reservationID, nil)
	require.Equal(t, http.StatusOK, status, string(raw))
	var view struct {
		Status string `json:"status"`
		Lines  []struct {
			SKU      string `json:"sku"`
			Quantity int    `json:"quantity"`
		} `json:"lines"`
	}
	require.NoError(t, json.Unmarshal(raw, &view))
	assert.Equal(t, "held", view.Status)
	require.Len(t, view.Lines, 1)
	assert.Equal(t, 3, view.Lines[0].Quantity)

	available, err := module.Availability().Available(ctx, []string{"OFFER-1", "OFFER-2"})
	require.NoError(t, err)
	assert.Equal(t, map[string]int{"OFFER-1": 9, "OFFER-2": 0}, available)

	require.NoError(t, module.Reserver().Commit(ctx, reservationID))
	available, err = module.Availability().Available(ctx, []string{"OFFER-1"})
	require.NoError(t, err)
	assert.Equal(t, map[string]int{"OFFER-1": 9}, available)

	status, _ = c.call(owner, http.MethodGet, "/api/v1/seller/reservations/"+domain.NewReservationID().String(), nil)
	assert.Equal(t, http.StatusNotFound, status)
}
