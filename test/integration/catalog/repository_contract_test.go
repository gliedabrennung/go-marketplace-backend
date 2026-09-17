//go:build integration

package catalog_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/infrastructure/memory"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/infrastructure/postgres"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/test/testdb"
)

var catalogTables = []string{
	"catalog.variant_members", "catalog.variant_groups", "catalog.offers", "catalog.import_jobs",
	"catalog.product_images", "catalog.product_attributes", "catalog.products",
	"catalog.category_attributes", "catalog.categories",
	"platform.outbox", "platform.idempotency_keys", "platform.audit_log",
}

func implementations() map[string]func(t *testing.T) application.Repositories {
	return map[string]func(t *testing.T) application.Repositories{
		"memory": func(*testing.T) application.Repositories { return memory.NewStore() },
		"postgres": func(t *testing.T) application.Repositories {
			pool := testdb.Pool(t)
			testdb.Truncate(t, pool, catalogTables...)
			return postgres.NewRepositories(pool, postgres.NewOutboxWriter())
		},
	}
}

func now() time.Time {
	return time.Now().UTC().Truncate(time.Microsecond)
}

func attribute(t *testing.T, spec domain.AttributeSpec) domain.AttributeDefinition {
	t.Helper()
	def, err := domain.NewAttributeDefinition(spec)
	require.NoError(t, err)
	return def
}

func saveTree(ctx context.Context, t *testing.T, repos application.Repositories) (root, phones *domain.Category) {
	t.Helper()
	at := now()
	root, err := domain.CreateCategory(domain.NewCategoryID(), "Все товары", "all", nil, at)
	require.NoError(t, err)
	require.NoError(t, root.DefineAttribute(attribute(t, domain.AttributeSpec{Code: "brand_country", Name: "Страна", Type: "string"}), domain.Schema{}, nil, at))
	require.NoError(t, repos.Categories().Save(ctx, root))

	phones, err = domain.CreateCategory(domain.NewCategoryID(), "Смартфоны", "smartphones", root, at)
	require.NoError(t, err)
	inherited, err := domain.Classify([]*domain.Category{root})
	require.NoError(t, err)
	require.NoError(t, phones.DefineAttribute(attribute(t, domain.AttributeSpec{
		Code: "color", Name: "Цвет", Type: "enum", Required: true, Filterable: true, Options: []string{"black", "white"},
	}), inherited.Schema(), nil, at))
	require.NoError(t, repos.Categories().Save(ctx, phones))
	return root, phones
}

func classification(ctx context.Context, t *testing.T, repos application.Repositories, id domain.CategoryID) domain.Classification {
	t.Helper()
	chain, err := repos.Categories().FindChain(ctx, id)
	require.NoError(t, err)
	class, err := domain.Classify(chain)
	require.NoError(t, err)
	return class
}

func publishedProduct(ctx context.Context, t *testing.T, repos application.Repositories, class domain.Classification, seller kernel.SellerID, color string) *domain.Product {
	t.Helper()
	at := now()
	product, err := domain.CreateProduct(domain.NewProductID(), seller, class, domain.ProductContent{
		Title: "Смартфон Nova", Description: "Флагман", Brand: "Nova",
		Attributes: map[string]string{"color": color, "brand_country": "KZ"},
	}, at)
	require.NoError(t, err)
	require.NoError(t, product.SubmitForModeration(seller, class, at))
	require.NoError(t, product.Publish(kernel.NewUserID(), class, at))
	require.NoError(t, repos.Products().Save(ctx, product))
	return product
}

func TestCategoryRepositoryContract(t *testing.T) {
	ctx := context.Background()
	for name, factory := range implementations() {
		t.Run(name, func(t *testing.T) {
			t.Run("not found", func(t *testing.T) {
				_, err := factory(t).Categories().FindByID(ctx, domain.NewCategoryID())
				require.ErrorIs(t, err, domain.ErrCategoryNotFound)
				_, err = factory(t).Categories().FindChain(ctx, domain.NewCategoryID())
				require.ErrorIs(t, err, domain.ErrCategoryNotFound)
			})

			t.Run("round trip with chain and descendants", func(t *testing.T) {
				repos := factory(t)
				root, phones := saveTree(ctx, t, repos)

				reloaded, err := repos.Categories().FindByID(ctx, phones.ID())
				require.NoError(t, err)
				assert.Equal(t, phones.Snapshot(), reloaded.Snapshot())

				chain, err := repos.Categories().FindChain(ctx, phones.ID())
				require.NoError(t, err)
				require.Len(t, chain, 2)
				assert.Equal(t, root.ID(), chain[0].ID())
				assert.Equal(t, phones.ID(), chain[1].ID())

				codes, err := repos.Categories().DescendantAttributeCodes(ctx, root.ID())
				require.NoError(t, err)
				assert.Equal(t, []string{"color"}, codes)
			})

			t.Run("sibling slug is unique", func(t *testing.T) {
				repos := factory(t)
				root, _ := saveTree(ctx, t, repos)
				twin, err := domain.CreateCategory(domain.NewCategoryID(), "Телефоны", "smartphones", root, now())
				require.NoError(t, err)
				require.ErrorIs(t, repos.Categories().Save(ctx, twin), domain.ErrCategorySlugTaken)
			})

			t.Run("stale version is rejected", func(t *testing.T) {
				repos := factory(t)
				_, phones := saveTree(ctx, t, repos)
				fresh, err := repos.Categories().FindByID(ctx, phones.ID())
				require.NoError(t, err)
				require.NoError(t, fresh.Rename("Телефоны", "phones", now()))
				require.NoError(t, repos.Categories().Save(ctx, fresh))

				stale, err := domain.RehydrateCategory(phones.Snapshot())
				require.NoError(t, err)
				require.NoError(t, stale.Rename("Мобильные", "mobile", now()))
				require.ErrorIs(t, repos.Categories().Save(ctx, stale), kernel.ErrConcurrentModification)
			})
		})
	}
}

func TestProductRepositoryContract(t *testing.T) {
	ctx := context.Background()
	for name, factory := range implementations() {
		t.Run(name, func(t *testing.T) {
			t.Run("not found", func(t *testing.T) {
				_, err := factory(t).Products().FindByID(ctx, domain.NewProductID())
				require.ErrorIs(t, err, domain.ErrProductNotFound)
			})

			t.Run("round trip with images", func(t *testing.T) {
				repos := factory(t)
				_, phones := saveTree(ctx, t, repos)
				class := classification(ctx, t, repos, phones.ID())
				seller := kernel.NewSellerID()
				at := now()

				product, err := domain.CreateProduct(domain.NewProductID(), seller, class, domain.ProductContent{
					Title: "Смартфон Nova", Description: "Флагман", Brand: "Nova",
					Attributes: map[string]string{"color": "black", "brand_country": "KZ"},
				}, at)
				require.NoError(t, err)
				first := domain.NewImageID()
				_, err = product.RequestImageUpload(seller, first, "image/png", 2048, at)
				require.NoError(t, err)
				require.NoError(t, product.ConfirmImageUpload(seller, first, domain.ImageProbe{ContentType: "image/png", Size: 2048, Width: 800, Height: 600}, at))
				require.NoError(t, product.MarkImageProcessed(first, at))
				second := domain.NewImageID()
				_, err = product.RequestImageUpload(seller, second, "image/jpeg", 1024, at)
				require.NoError(t, err)
				require.NoError(t, repos.Products().Save(ctx, product))

				reloaded, err := repos.Products().FindByID(ctx, product.ID())
				require.NoError(t, err)
				assert.Equal(t, product.Snapshot(), reloaded.Snapshot())
				assert.Equal(t, domain.ImageObjectKey(product.ID(), first, domain.ImageVariantSmall), reloaded.CoverKey())

				require.NoError(t, reloaded.SubmitForModeration(seller, class, at))
				require.NoError(t, reloaded.Publish(kernel.NewUserID(), class, at))
				require.NoError(t, repos.Products().Save(ctx, reloaded))
				published, err := repos.Products().FindByID(ctx, product.ID())
				require.NoError(t, err)
				assert.Equal(t, domain.ProductStatusPublished, published.Status())
				assert.False(t, published.PublishedAt().IsZero())

				require.ErrorIs(t, repos.Products().Save(ctx, product), kernel.ErrConcurrentModification)
			})
		})
	}
}

func TestOfferRepositoryContract(t *testing.T) {
	ctx := context.Background()
	for name, factory := range implementations() {
		t.Run(name, func(t *testing.T) {
			t.Run("not found", func(t *testing.T) {
				repos := factory(t)
				_, err := repos.Offers().FindByID(ctx, domain.NewOfferID())
				require.ErrorIs(t, err, domain.ErrOfferNotFound)
				sku, err := domain.NewSellerSKU("SKU-1")
				require.NoError(t, err)
				_, err = repos.Offers().FindBySellerSKU(ctx, kernel.NewSellerID(), sku)
				require.ErrorIs(t, err, domain.ErrOfferNotFound)
			})

			t.Run("uniqueness and archiving", func(t *testing.T) {
				repos := factory(t)
				_, phones := saveTree(ctx, t, repos)
				class := classification(ctx, t, repos, phones.ID())
				seller := kernel.NewSellerID()
				black := publishedProduct(ctx, t, repos, class, seller, "black")
				white := publishedProduct(ctx, t, repos, class, seller, "white")
				at := now()

				sku, err := domain.NewSellerSKU("SKU-1")
				require.NoError(t, err)
				terms, err := domain.NewOfferTerms(100000, "KZT", "new", 2)
				require.NoError(t, err)
				offer, err := domain.CreateOffer(domain.NewOfferID(), black, seller, sku, terms, at)
				require.NoError(t, err)
				require.NoError(t, repos.Offers().Save(ctx, offer))

				twin, err := domain.CreateOffer(domain.NewOfferID(), white, seller, sku, terms, at)
				require.NoError(t, err)
				require.ErrorIs(t, repos.Offers().Save(ctx, twin), domain.ErrSellerSKUTaken)

				otherSKU, err := domain.NewSellerSKU("SKU-2")
				require.NoError(t, err)
				duplicate, err := domain.CreateOffer(domain.NewOfferID(), black, seller, otherSKU, terms, at)
				require.NoError(t, err)
				require.ErrorIs(t, repos.Offers().Save(ctx, duplicate), domain.ErrOfferExists)

				found, err := repos.Offers().FindBySellerSKU(ctx, seller, sku)
				require.NoError(t, err)
				assert.Equal(t, offer.Snapshot(), found.Snapshot())

				require.NoError(t, found.Archive(seller, at))
				require.NoError(t, repos.Offers().Save(ctx, found))
				_, err = repos.Offers().FindBySellerSKU(ctx, seller, sku)
				require.ErrorIs(t, err, domain.ErrOfferNotFound)

				reused, err := domain.CreateOffer(domain.NewOfferID(), black, seller, sku, terms, at)
				require.NoError(t, err)
				require.NoError(t, repos.Offers().Save(ctx, reused))
			})
		})
	}
}

func TestVariantAndImportRepositoryContract(t *testing.T) {
	ctx := context.Background()
	for name, factory := range implementations() {
		t.Run(name, func(t *testing.T) {
			t.Run("product belongs to one group", func(t *testing.T) {
				repos := factory(t)
				_, phones := saveTree(ctx, t, repos)
				class := classification(ctx, t, repos, phones.ID())
				seller := kernel.NewSellerID()
				black := publishedProduct(ctx, t, repos, class, seller, "black")
				at := now()

				group, err := domain.CreateVariantGroup(domain.NewVariantGroupID(), seller, class, []string{"color"}, at)
				require.NoError(t, err)
				require.NoError(t, group.AddProduct(seller, black, at))
				require.NoError(t, repos.VariantGroups().Save(ctx, group))

				reloaded, err := repos.VariantGroups().FindByID(ctx, group.ID())
				require.NoError(t, err)
				assert.Equal(t, group.Snapshot(), reloaded.Snapshot())

				other, err := domain.CreateVariantGroup(domain.NewVariantGroupID(), seller, class, []string{"color"}, at)
				require.NoError(t, err)
				require.NoError(t, other.AddProduct(seller, black, at))
				require.ErrorIs(t, repos.VariantGroups().Save(ctx, other), domain.ErrProductAlreadyGrouped)

				_, err = repos.VariantGroups().FindByID(ctx, domain.NewVariantGroupID())
				require.ErrorIs(t, err, domain.ErrVariantGroupNotFound)
			})

			t.Run("import job keeps row errors", func(t *testing.T) {
				repos := factory(t)
				seller := kernel.NewSellerID()
				at := now()
				job, err := domain.ScheduleImport(domain.NewImportJobID(), seller, kernel.NewUserID(), domain.ImportCSV,
					domain.ImportObjectPrefix(seller)+"offers.csv", at)
				require.NoError(t, err)
				require.NoError(t, repos.Imports().Save(ctx, job))

				running, err := repos.Imports().FindByID(ctx, job.ID())
				require.NoError(t, err)
				require.NoError(t, running.Start(at))
				require.NoError(t, running.RecordSuccess())
				require.NoError(t, running.RecordFailure(domain.ImportRowError{Row: 3, Field: "price", Code: "CATALOG_INVALID_PRICE", Message: "bad price"}))
				require.NoError(t, running.Complete(domain.ImportObjectPrefix(seller)+"reports/report.csv", at))
				require.NoError(t, repos.Imports().Save(ctx, running))

				reloaded, err := repos.Imports().FindByID(ctx, job.ID())
				require.NoError(t, err)
				assert.Equal(t, running.Snapshot(), reloaded.Snapshot())

				_, err = repos.Imports().FindByID(ctx, domain.NewImportJobID())
				require.ErrorIs(t, err, domain.ErrImportJobNotFound)
			})
		})
	}
}
