//go:build integration

package catalog_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog"
	catalogapi "github.com/gliedabrennung/go-marketplace-backend/internal/catalog/api"
	catalogapp "github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/infrastructure/importfile"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/infrastructure/media"
	catalogmemory "github.com/gliedabrennung/go-marketplace-backend/internal/catalog/infrastructure/memory"
	sellerapi "github.com/gliedabrennung/go-marketplace-backend/internal/seller/api"
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

type directory struct {
	mu      sync.Mutex
	sellers map[string]sellerapi.SellerInfo
	members map[string]map[string]string
}

func (d *directory) Seller(_ context.Context, sellerID string) (sellerapi.SellerInfo, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	info, ok := d.sellers[sellerID]
	if !ok {
		return sellerapi.SellerInfo{}, sellerapi.ErrSellerNotFound
	}
	return info, nil
}

func (d *directory) MemberRole(_ context.Context, sellerID, userID string) (string, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	role, ok := d.members[sellerID][userID]
	return role, ok, nil
}

func (d *directory) register(userID string) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	id := kernel.NewSellerID().String()
	d.sellers[id] = sellerapi.SellerInfo{ID: id, Status: "active", CanSell: true, PayoutsAllowed: true}
	d.members[id] = map[string]string{userID: sellerapi.RoleSellerAdmin}
	return id
}

type allowAll struct{}

func (allowAll) Allow(context.Context, string) error { return nil }

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
	if a.token != "" {
		req.Header.Set("Authorization", "Bearer "+a.token)
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

func (c *client) decode(raw []byte, dst any) {
	c.t.Helper()
	require.NoError(c.t, json.Unmarshal(raw, dst))
}

func (c *client) problem(raw []byte) string {
	c.t.Helper()
	var p httpx.Problem
	require.NoError(c.t, json.Unmarshal(raw, &p))
	return p.Code
}

func pngBytes(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for x := range width {
		for y := range height {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func dispatch(t *testing.T, worker *catalog.Worker, eventName string, payload any) {
	t.Helper()
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	for _, sub := range worker.Subscriptions() {
		for _, name := range sub.EventNames {
			if name != eventName {
				continue
			}
			require.NoError(t, sub.Handle(context.Background(), outbox.Message{EventName: eventName, Payload: raw}))
			return
		}
	}
	t.Fatalf("no subscription for %s", eventName)
}

func TestHTTP_CatalogLifecycle(t *testing.T) {
	pool := testdb.Pool(t)
	testdb.Truncate(t, pool, catalogTables...)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	rs := httpx.NewResponder(log)
	ring, err := token.GenerateKeyRing("test")
	require.NoError(t, err)
	jwt := token.NewJWT(ring, token.JWTConfig{Issuer: "marketplace", Audience: "api", TTL: time.Hour})
	idem := idempotency.NewMiddleware(idempotency.NewPostgresStore(pool), rs, log, httpx.PrincipalOrAnonymous, time.Hour)
	metrics := observability.NewCommandMetrics(prometheus.NewRegistry())

	objects := catalogmemory.NewObjects()
	sellers := &directory{sellers: map[string]sellerapi.SellerInfo{}, members: map[string]map[string]string{}}
	module := catalog.NewModule(catalog.Dependencies{
		Pool: pool, Clock: clock.System{}, Policy: catalogapp.DefaultPolicy(), Sellers: sellers,
		Storage: objects, Prober: media.NewProber(), Limiter: allowAll{},
		Responder: rs, Idempotency: idem.Handler, Logger: log, Metrics: metrics,
	})
	worker := catalog.NewWorker(catalog.WorkerDependencies{
		Pool: pool, Clock: clock.System{}, Policy: catalogapp.DefaultPolicy(), Sellers: sellers,
		Thumbnails: media.NewThumbnailer(objects), Source: importfile.NewSource(objects),
		Reports: importfile.NewReportWriter(objects), Logger: log, Metrics: metrics,
	})

	router := httpx.NewRouter(rs)
	module.RegisterRoutes(router)
	server := httptest.NewServer(httpx.Chain(router, httpx.Recoverer(log, rs), httpx.Authenticate(jwt, rs)))
	t.Cleanup(server.Close)

	c := &client{t: t, server: server, jwt: jwt}
	admin := c.actor("platform_admin")
	moderator := c.actor("content_moderator")
	owner := c.actor()
	stranger := c.actor()
	seller := sellers.register(owner.id)

	status, raw := c.call(admin, http.MethodPost, "/api/v1/admin/catalog/categories", map[string]string{"name": "Все товары", "slug": "all"}, true)
	require.Equal(t, http.StatusCreated, status, string(raw))
	var rootCategory struct {
		CategoryID string `json:"category_id"`
	}
	c.decode(raw, &rootCategory)

	status, raw = c.call(owner, http.MethodPost, "/api/v1/admin/catalog/categories", map[string]string{"name": "Электроника", "slug": "electronics"}, true)
	assert.Equal(t, http.StatusForbidden, status, string(raw))

	status, raw = c.call(admin, http.MethodPost, "/api/v1/admin/catalog/categories",
		map[string]string{"parent_id": rootCategory.CategoryID, "name": "Смартфоны", "slug": "smartphones"}, true)
	require.Equal(t, http.StatusCreated, status, string(raw))
	var phones struct {
		CategoryID string `json:"category_id"`
	}
	c.decode(raw, &phones)

	status, raw = c.call(admin, http.MethodPut, "/api/v1/admin/catalog/categories/"+rootCategory.CategoryID+"/attributes/brand_country",
		map[string]any{"name": "Страна бренда", "type": "string"}, false)
	require.Equal(t, http.StatusNoContent, status, string(raw))
	status, raw = c.call(admin, http.MethodPut, "/api/v1/admin/catalog/categories/"+phones.CategoryID+"/attributes/color",
		map[string]any{"name": "Цвет", "type": "enum", "required": true, "filterable": true, "options": []string{"black", "white"}}, false)
	require.Equal(t, http.StatusNoContent, status, string(raw))
	status, raw = c.call(admin, http.MethodPut, "/api/v1/admin/catalog/categories/"+phones.CategoryID+"/attributes/brand_country",
		map[string]any{"name": "Страна", "type": "string"}, false)
	assert.Equal(t, http.StatusConflict, status)
	assert.Equal(t, "CATALOG_DUPLICATE_ATTRIBUTE", c.problem(raw))

	status, raw = c.call(actor{}, http.MethodGet, "/api/v1/catalog/categories", nil, false)
	require.Equal(t, http.StatusOK, status, string(raw))
	var tree struct {
		Data []struct {
			ID    string `json:"id"`
			Slug  string `json:"slug"`
			Depth int    `json:"depth"`
		} `json:"data"`
	}
	c.decode(raw, &tree)
	require.Len(t, tree.Data, 2)
	assert.Equal(t, "all", tree.Data[0].Slug)
	assert.Equal(t, 2, tree.Data[1].Depth)

	status, raw = c.call(actor{}, http.MethodGet, "/api/v1/catalog/categories/"+phones.CategoryID, nil, false)
	require.Equal(t, http.StatusOK, status, string(raw))
	var categoryView struct {
		Attributes []struct {
			Code       string `json:"code"`
			CategoryID string `json:"category_id"`
		} `json:"attributes"`
		Path []struct {
			Slug string `json:"slug"`
		} `json:"path"`
	}
	c.decode(raw, &categoryView)
	require.Len(t, categoryView.Attributes, 2)
	assert.Equal(t, "brand_country", categoryView.Attributes[0].Code)
	assert.Equal(t, rootCategory.CategoryID, categoryView.Attributes[0].CategoryID)
	require.Len(t, categoryView.Path, 2)
	assert.Equal(t, "all", categoryView.Path[0].Slug)

	productBody := map[string]any{
		"category_id": phones.CategoryID,
		"title":       "Смартфон Nova X",
		"description": "Флагман с отличной камерой",
		"brand":       "Nova",
		"attributes":  map[string]string{"color": "black", "brand_country": "KZ"},
	}
	status, raw = c.call(stranger, http.MethodPost, "/api/v1/seller/sellers/"+seller+"/products", productBody, true)
	assert.Equal(t, http.StatusNotFound, status, string(raw))
	status, raw = c.call(owner, http.MethodPost, "/api/v1/seller/sellers/"+seller+"/products", productBody, true)
	require.Equal(t, http.StatusCreated, status, string(raw))
	var product struct {
		ProductID string `json:"product_id"`
	}
	c.decode(raw, &product)

	picture := pngBytes(t, 1200, 600)
	status, raw = c.call(owner, http.MethodPost, "/api/v1/seller/products/"+product.ProductID+"/images",
		map[string]any{"content_type": "image/png", "size": len(picture)}, true)
	require.Equal(t, http.StatusCreated, status, string(raw))
	var upload struct {
		ImageID string `json:"image_id"`
		Upload  struct {
			Method string `json:"method"`
			URL    string `json:"url"`
		} `json:"upload"`
	}
	c.decode(raw, &upload)
	assert.Equal(t, http.MethodPut, upload.Upload.Method)

	ctx := context.Background()
	imageKey := strings.TrimPrefix(upload.Upload.URL, "memory://")
	require.NoError(t, objects.Put(ctx, imageKey, "image/png", picture))
	status, raw = c.call(owner, http.MethodPost, "/api/v1/seller/products/"+product.ProductID+"/images/"+upload.ImageID+"/confirm", nil, true)
	require.Equal(t, http.StatusNoContent, status, string(raw))

	dispatch(t, worker, catalogapi.EventProductImageUploaded,
		catalogapi.ProductImageUploadedV1{ProductID: product.ProductID, ImageID: upload.ImageID})
	_, ok := objects.Get(strings.TrimSuffix(imageKey, "original") + "small.jpg")
	assert.True(t, ok)

	status, raw = c.call(owner, http.MethodPost, "/api/v1/seller/products/"+product.ProductID+"/submit", nil, true)
	require.Equal(t, http.StatusNoContent, status, string(raw))
	status, raw = c.call(actor{}, http.MethodGet, "/api/v1/catalog/products/"+product.ProductID, nil, false)
	assert.Equal(t, http.StatusNotFound, status, string(raw))

	status, raw = c.call(moderator, http.MethodGet, "/api/v1/admin/catalog/moderation-queue?limit=10", nil, false)
	require.Equal(t, http.StatusOK, status, string(raw))
	assert.Contains(t, string(raw), product.ProductID)

	status, raw = c.call(moderator, http.MethodPost, "/api/v1/admin/catalog/products/"+product.ProductID+"/reject",
		map[string]string{"reason": "нужны фото получше"}, true)
	require.Equal(t, http.StatusNoContent, status, string(raw))
	status, raw = c.call(owner, http.MethodPost, "/api/v1/seller/products/"+product.ProductID+"/submit", nil, true)
	require.Equal(t, http.StatusNoContent, status, string(raw))
	status, raw = c.call(moderator, http.MethodPost, "/api/v1/admin/catalog/products/"+product.ProductID+"/publish", nil, true)
	require.Equal(t, http.StatusNoContent, status, string(raw))

	status, raw = c.call(actor{}, http.MethodGet, "/api/v1/catalog/products/"+product.ProductID, nil, false)
	require.Equal(t, http.StatusOK, status, string(raw))
	var view struct {
		Status     string `json:"status"`
		Attributes []struct {
			Code  string `json:"code"`
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"attributes"`
		Images []struct {
			Status      string `json:"status"`
			SmallURL    string `json:"small_url"`
			OriginalURL string `json:"original_url"`
		} `json:"images"`
	}
	c.decode(raw, &view)
	assert.Equal(t, "published", view.Status)
	require.Len(t, view.Attributes, 2)
	assert.Equal(t, "Цвет", view.Attributes[1].Name)
	assert.Equal(t, "black", view.Attributes[1].Value)
	require.Len(t, view.Images, 1)
	assert.Equal(t, "processed", view.Images[0].Status)
	assert.NotEmpty(t, view.Images[0].SmallURL)
	assert.Empty(t, view.Images[0].OriginalURL)

	status, raw = c.call(owner, http.MethodPost, "/api/v1/seller/sellers/"+seller+"/offers", map[string]any{
		"product_id": product.ProductID, "seller_sku": "NOVA-X-BLACK", "price": 249000, "condition": "new", "processing_days": 2,
	}, true)
	require.Equal(t, http.StatusCreated, status, string(raw))
	var offer struct {
		OfferID string `json:"offer_id"`
	}
	c.decode(raw, &offer)

	status, raw = c.call(actor{}, http.MethodGet, "/api/v1/catalog/products/"+product.ProductID+"/offers", nil, false)
	require.Equal(t, http.StatusOK, status, string(raw))
	var offers struct {
		Data []struct {
			ID       string `json:"id"`
			Price    int64  `json:"price"`
			Currency string `json:"currency"`
		} `json:"data"`
	}
	c.decode(raw, &offers)
	require.Len(t, offers.Data, 1)
	assert.Equal(t, offer.OfferID, offers.Data[0].ID)
	assert.Equal(t, int64(249000), offers.Data[0].Price)
	assert.Equal(t, "KZT", offers.Data[0].Currency)

	status, raw = c.call(owner, http.MethodPost, "/api/v1/seller/sellers/"+seller+"/offer-imports/upload",
		map[string]any{"format": "csv", "size": 512}, true)
	require.Equal(t, http.StatusCreated, status, string(raw))
	var importUpload struct {
		ObjectKey string `json:"object_key"`
	}
	c.decode(raw, &importUpload)

	csv := "seller_sku,product_id,price,condition,processing_days\n" +
		"NOVA-X-BLACK," + product.ProductID + ",199000,new,1\n" +
		"BROKEN," + product.ProductID + ",-5,new,1\n"
	require.NoError(t, objects.Put(ctx, importUpload.ObjectKey, "text/csv", []byte(csv)))

	status, raw = c.call(owner, http.MethodPost, "/api/v1/seller/offers/bulk", map[string]any{
		"seller_id": seller, "format": "csv", "object_key": importUpload.ObjectKey,
	}, true)
	require.Equal(t, http.StatusAccepted, status, string(raw))
	var job struct {
		JobID string `json:"job_id"`
	}
	c.decode(raw, &job)

	dispatch(t, worker, catalogapi.EventImportScheduled, catalogapi.ImportScheduledV1{JobID: job.JobID})

	status, raw = c.call(owner, http.MethodGet, "/api/v1/seller/offer-imports/"+job.JobID, nil, false)
	require.Equal(t, http.StatusOK, status, string(raw))
	var jobView struct {
		Status        string `json:"status"`
		TotalRows     int    `json:"total_rows"`
		SucceededRows int    `json:"succeeded_rows"`
		FailedRows    int    `json:"failed_rows"`
		ReportURL     string `json:"report_url"`
		Errors        []struct {
			Row   int    `json:"row"`
			Field string `json:"field"`
			Code  string `json:"code"`
		} `json:"errors"`
	}
	c.decode(raw, &jobView)
	assert.Equal(t, "completed", jobView.Status)
	assert.Equal(t, 2, jobView.TotalRows)
	assert.Equal(t, 1, jobView.SucceededRows)
	require.Len(t, jobView.Errors, 1)
	assert.Equal(t, 3, jobView.Errors[0].Row)
	assert.Equal(t, "price", jobView.Errors[0].Field)
	assert.NotEmpty(t, jobView.ReportURL)

	status, raw = c.call(stranger, http.MethodGet, "/api/v1/seller/offer-imports/"+job.JobID, nil, false)
	assert.Equal(t, http.StatusNotFound, status, string(raw))

	status, raw = c.call(actor{}, http.MethodGet, "/api/v1/catalog/products/"+product.ProductID+"/offers", nil, false)
	require.Equal(t, http.StatusOK, status, string(raw))
	c.decode(raw, &offers)
	require.Len(t, offers.Data, 1)
	assert.Equal(t, int64(199000), offers.Data[0].Price)

	var events int
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM platform.outbox WHERE event_name LIKE 'catalog.%'").Scan(&events))
	assert.Positive(t, events)
	var audited int
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM platform.audit_log WHERE action LIKE 'catalog.%'").Scan(&audited))
	assert.Equal(t, 6, audited)
}
