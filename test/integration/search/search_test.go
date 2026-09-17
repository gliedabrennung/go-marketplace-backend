//go:build integration

package search_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	catalogdomain "github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
	catalogpostgres "github.com/gliedabrennung/go-marketplace-backend/internal/catalog/infrastructure/postgres"
	searchapp "github.com/gliedabrennung/go-marketplace-backend/internal/search/application"
	searchcommand "github.com/gliedabrennung/go-marketplace-backend/internal/search/application/command"
	searchquery "github.com/gliedabrennung/go-marketplace-backend/internal/search/application/query"
	searchpostgres "github.com/gliedabrennung/go-marketplace-backend/internal/search/infrastructure/postgres"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/clock"
	"github.com/gliedabrennung/go-marketplace-backend/test/testdb"
)

var tables = []string{
	"search.reindex_jobs", "search.lexicon", "search.product_stats", "search.product_offers", "search.sellers",
	"search.documents_a", "search.documents_b",
	"catalog.variant_members", "catalog.variant_groups", "catalog.offers", "catalog.import_jobs",
	"catalog.product_images", "catalog.product_attributes", "catalog.products",
	"catalog.category_attributes", "catalog.categories", "platform.outbox",
}

type fixture struct {
	pool     *pgxpool.Pool
	index    *searchpostgres.Index
	reads    *searchpostgres.ReadModel
	jobs     *searchpostgres.ReindexJobs
	request  *searchcommand.RequestReindexHandler
	run      *searchcommand.RunReindexHandler
	offers   *searchcommand.IndexOfferHandler
	stock    *searchcommand.IndexStockHandler
	sellers  *searchcommand.IndexSellerHandler
	search   *searchquery.SearchProductsHandler
	facets   *searchquery.CategoryFacetsHandler
	category string
	seller   kernel.SellerID
	products map[string]string
}

func setup(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	pool := testdb.Pool(t)
	testdb.Truncate(t, pool, tables...)
	_, err := pool.Exec(ctx, `
		INSERT INTO search.index_state (id, active) VALUES (true, 'documents_a')
		ON CONFLICT (id) DO UPDATE SET active = 'documents_a', rebuilt_at = NULL`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `CREATE OR REPLACE VIEW search.documents AS SELECT * FROM search.documents_a`)
	require.NoError(t, err)

	index := searchpostgres.NewIndex(pool)
	policy := searchapp.DefaultPolicy()
	policy.FeedBatch = 2
	policy.LexiconMinWeight = 1
	base := searchcommand.NewBase(index, clock.System{}, policy)
	jobs := searchpostgres.NewReindexJobs(pool)
	reads := searchpostgres.NewReadModel(pool)

	f := &fixture{
		pool: pool, index: index, reads: reads, jobs: jobs,
		request: searchcommand.NewRequestReindexHandler(base, jobs),
		run:     searchcommand.NewRunReindexHandler(base, jobs, catalogpostgres.NewFeed(pool)),
		offers:  searchcommand.NewIndexOfferHandler(base),
		stock:   searchcommand.NewIndexStockHandler(base),
		sellers: searchcommand.NewIndexSellerHandler(base),
		search:  searchquery.NewSearchProductsHandler(reads, policy),
		facets:  searchquery.NewCategoryFacetsHandler(reads, policy),
		seller:  kernel.NewSellerID(),

		products: map[string]string{},
	}
	f.seedCatalog(ctx, t)
	return f
}

func (f *fixture) seedCatalog(ctx context.Context, t *testing.T) {
	t.Helper()
	repos := catalogpostgres.NewRepositories(f.pool, catalogpostgres.NewOutboxWriter())
	at := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)

	root, err := catalogdomain.CreateCategory(catalogdomain.NewCategoryID(), "Все товары", "all", nil, at)
	require.NoError(t, err)
	require.NoError(t, repos.Categories().Save(ctx, root))

	phones, err := catalogdomain.CreateCategory(catalogdomain.NewCategoryID(), "Смартфоны", "smartphones", root, at)
	require.NoError(t, err)
	inherited, err := catalogdomain.Classify([]*catalogdomain.Category{root})
	require.NoError(t, err)
	for _, spec := range []catalogdomain.AttributeSpec{
		{Code: "color", Name: "Цвет", Type: "enum", Required: true, Filterable: true, Options: []string{"black", "white"}},
		{Code: "memory_gb", Name: "Память", Type: "unit", Unit: "GB", Required: true, Filterable: true},
	} {
		definition, err := catalogdomain.NewAttributeDefinition(spec)
		require.NoError(t, err)
		require.NoError(t, phones.DefineAttribute(definition, inherited.Schema(), nil, at))
	}
	require.NoError(t, repos.Categories().Save(ctx, phones))
	f.category = phones.ID().String()

	chain, err := repos.Categories().FindChain(ctx, phones.ID())
	require.NoError(t, err)
	class, err := catalogdomain.Classify(chain)
	require.NoError(t, err)

	contents := []struct {
		key     string
		title   string
		brand   string
		color   string
		memory  string
		summary string
	}{
		{"black", "Смартфон Nova X", "Nova", "black", "256", "Флагманский смартфон с отличной камерой"},
		{"white", "Смартфон Nova Lite", "Nova", "white", "128", "Лёгкий смартфон для повседневных задач"},
		{"tablet", "Планшет Nova Tab", "Nova", "black", "512", "Большой экран для работы и учёбы"},
	}
	for _, c := range contents {
		product, err := catalogdomain.CreateProduct(catalogdomain.NewProductID(), f.seller, class, catalogdomain.ProductContent{
			Title: c.title, Description: c.summary, Brand: c.brand,
			Attributes: map[string]string{"color": c.color, "memory_gb": c.memory},
		}, at)
		require.NoError(t, err)
		require.NoError(t, product.SubmitForModeration(f.seller, class, at))
		require.NoError(t, product.Publish(kernel.NewUserID(), class, at))
		require.NoError(t, repos.Products().Save(ctx, product))
		f.products[c.key] = product.ID().String()
		at = at.Add(time.Second)
	}
}

func (f *fixture) reindex(ctx context.Context, t *testing.T) searchcommand.RunReindexResult {
	t.Helper()
	admin := auth.Principal{UserID: kernel.NewUserID().String(), Roles: []string{"platform_admin"}}
	_, err := f.request.Handle(ctx, searchcommand.RequestReindex{Actor: admin})
	require.NoError(t, err)
	result, err := f.run.Handle(ctx, searchcommand.RunReindex{})
	require.NoError(t, err)
	require.Equal(t, searchapp.ReindexCompleted, result.Status)
	return result
}

func (f *fixture) offer(ctx context.Context, t *testing.T, productKey string, price int64, condition, status string, available int) string {
	t.Helper()
	offerID := kernel.NewID[struct{}]().String()
	_, err := f.offers.Handle(ctx, searchcommand.IndexOffer{Offer: searchapp.OfferState{
		OfferID: offerID, ProductID: f.products[productKey], SellerID: f.seller.String(),
		Price: price, Currency: "KZT", Condition: condition, Status: status, UpdatedAt: time.Now().UTC(),
	}})
	require.NoError(t, err)
	_, err = f.stock.Handle(ctx, searchcommand.IndexStock{SKU: offerID, Available: available})
	require.NoError(t, err)
	return offerID
}

func titles(result searchquery.SearchResult) []string {
	out := make([]string, 0, len(result.Page.Items))
	for _, hit := range result.Page.Items {
		out = append(out, hit.Title)
	}
	return out
}

func TestSearch_FullTextFiltersAndSorting(t *testing.T) {
	ctx := context.Background()
	f := setup(t)

	result := f.reindex(ctx, t)
	assert.Equal(t, 3, result.Processed)

	_, err := f.sellers.Handle(ctx, searchcommand.IndexSeller{SellerID: f.seller.String(), CanSell: true})
	require.NoError(t, err)
	blackOffer := f.offer(ctx, t, "black", 249000, "new", "active", 5)
	f.offer(ctx, t, "white", 149000, "used", "active", 2)
	f.offer(ctx, t, "tablet", 349000, "new", "paused", 4)

	found, err := f.search.Handle(ctx, searchquery.SearchProducts{Query: "смартфоны"})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"Смартфон Nova X", "Смартфон Nova Lite"}, titles(found))

	found, err = f.search.Handle(ctx, searchquery.SearchProducts{Query: "камера"})
	require.NoError(t, err)
	assert.Equal(t, []string{"Смартфон Nova X"}, titles(found))

	found, err = f.search.Handle(ctx, searchquery.SearchProducts{CategoryID: f.category, Filters: "color:black"})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"Смартфон Nova X", "Планшет Nova Tab"}, titles(found))

	found, err = f.search.Handle(ctx, searchquery.SearchProducts{CategoryID: f.category, Filters: "memory_gb:128|256"})
	require.NoError(t, err)
	assert.Len(t, found.Page.Items, 2)

	found, err = f.search.Handle(ctx, searchquery.SearchProducts{CategoryID: f.category, InStock: true, Sort: searchquery.SortPriceAsc})
	require.NoError(t, err)
	assert.Equal(t, []string{"Смартфон Nova Lite", "Смартфон Nova X"}, titles(found))
	assert.Equal(t, int64(149000), found.Page.Items[0].MinPrice)
	assert.True(t, found.Page.Items[0].InStock)

	found, err = f.search.Handle(ctx, searchquery.SearchProducts{CategoryID: f.category, PriceMin: 200000, PriceMax: 300000})
	require.NoError(t, err)
	assert.Equal(t, []string{"Смартфон Nova X"}, titles(found))

	found, err = f.search.Handle(ctx, searchquery.SearchProducts{CategoryID: f.category, Condition: "used"})
	require.NoError(t, err)
	assert.Equal(t, []string{"Смартфон Nova Lite"}, titles(found))

	page, err := f.search.Handle(ctx, searchquery.SearchProducts{CategoryID: f.category, Sort: searchquery.SortNew, Limit: 2})
	require.NoError(t, err)
	require.Len(t, page.Page.Items, 2)
	assert.True(t, page.Page.HasMore)
	next, err := f.search.Handle(ctx, searchquery.SearchProducts{
		CategoryID: f.category, Sort: searchquery.SortNew, Limit: 2, Cursor: page.Page.NextCursor,
	})
	require.NoError(t, err)
	require.Len(t, next.Page.Items, 1)
	assert.False(t, next.Page.HasMore)
	assert.NotContains(t, titles(next), titles(page)[0])

	popular, err := f.search.Handle(ctx, searchquery.SearchProducts{CategoryID: f.category, Sort: searchquery.SortPopular})
	require.NoError(t, err)
	assert.Positive(t, popular.Page.Items[0].Offers)

	_, err = f.stock.Handle(ctx, searchcommand.IndexStock{SKU: blackOffer, Available: 0})
	require.NoError(t, err)
	sold, err := f.search.Handle(ctx, searchquery.SearchProducts{CategoryID: f.category, InStock: true})
	require.NoError(t, err)
	assert.Equal(t, []string{"Смартфон Nova Lite"}, titles(sold))
}

func TestSearch_FacetsTypoAndSellerState(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	f.reindex(ctx, t)

	_, err := f.sellers.Handle(ctx, searchcommand.IndexSeller{SellerID: f.seller.String(), CanSell: true})
	require.NoError(t, err)
	f.offer(ctx, t, "black", 249000, "new", "active", 5)
	f.offer(ctx, t, "white", 149000, "used", "active", 2)

	set, err := f.facets.Handle(ctx, searchquery.CategoryFacets{CategoryID: f.category})
	require.NoError(t, err)
	assert.Equal(t, 3, set.Total)
	assert.Equal(t, int64(149000), set.PriceMin)
	assert.Equal(t, int64(249000), set.PriceMax)
	colors := map[string]int{}
	for _, facet := range set.Attributes {
		if facet.Code != "color" {
			continue
		}
		for _, value := range facet.Values {
			colors[value.Value] = value.Count
		}
	}
	assert.Equal(t, map[string]int{"black": 2, "white": 1}, colors)
	require.NotEmpty(t, set.Numbers)
	assert.Equal(t, "memory_gb", set.Numbers[0].Code)
	assert.InDelta(t, 128.0, set.Numbers[0].Min, 0.001)
	assert.InDelta(t, 512.0, set.Numbers[0].Max, 0.001)
	assert.ElementsMatch(t, []string{"new", "used"}, []string{set.Conditions[0].Value, set.Conditions[1].Value})

	filtered, err := f.facets.Handle(ctx, searchquery.CategoryFacets{CategoryID: f.category, Filters: "color:white"})
	require.NoError(t, err)
	assert.Equal(t, 1, filtered.Total)

	corrected, err := f.search.Handle(ctx, searchquery.SearchProducts{Query: "смартфн"})
	require.NoError(t, err)
	assert.NotEmpty(t, corrected.Corrected)
	assert.NotEmpty(t, corrected.Page.Items)

	_, err = f.sellers.Handle(ctx, searchcommand.IndexSeller{SellerID: f.seller.String(), CanSell: false})
	require.NoError(t, err)
	inStock, err := f.search.Handle(ctx, searchquery.SearchProducts{CategoryID: f.category, InStock: true})
	require.NoError(t, err)
	assert.Empty(t, inStock.Page.Items)
}

func TestSearch_ReindexSwapsTablesWithoutDowntime(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	f.reindex(ctx, t)

	active, err := f.index.ActiveTable(ctx)
	require.NoError(t, err)
	assert.Equal(t, "documents_b", active)
	found, err := f.search.Handle(ctx, searchquery.SearchProducts{CategoryID: f.category})
	require.NoError(t, err)
	assert.Len(t, found.Page.Items, 3)

	f.reindex(ctx, t)
	active, err = f.index.ActiveTable(ctx)
	require.NoError(t, err)
	assert.Equal(t, "documents_a", active)
	found, err = f.search.Handle(ctx, searchquery.SearchProducts{CategoryID: f.category})
	require.NoError(t, err)
	assert.Len(t, found.Page.Items, 3)

	var lexicon int
	require.NoError(t, f.pool.QueryRow(ctx, "SELECT count(*) FROM search.lexicon").Scan(&lexicon))
	assert.Positive(t, lexicon)
}
