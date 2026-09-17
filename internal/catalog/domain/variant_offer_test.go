package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

func TestVariantGroup(t *testing.T) {
	tr := newTree(t)
	seller := kernel.NewSellerID()
	class := tr.phonesClass(t)

	_, err := domain.CreateVariantGroup(domain.NewVariantGroupID(), seller, class, nil, now)
	require.ErrorIs(t, err, domain.ErrInvalidVariantAxes)
	_, err = domain.CreateVariantGroup(domain.NewVariantGroupID(), seller, class, []string{"memory_gb"}, now)
	require.ErrorIs(t, err, domain.ErrInvalidVariantAxes, "unit attribute is not an axis")
	_, err = domain.CreateVariantGroup(domain.NewVariantGroupID(), seller, class, []string{"color", "color"}, now)
	require.ErrorIs(t, err, domain.ErrInvalidVariantAxes)
	_, err = domain.CreateVariantGroup(domain.NewVariantGroupID(), seller, class, []string{"unknown"}, now)
	require.ErrorIs(t, err, domain.ErrInvalidVariantAxes)
	_, err = domain.CreateVariantGroup(domain.NewVariantGroupID(), seller, class, []string{"a", "b", "c", "d"}, now)
	require.ErrorIs(t, err, domain.ErrInvalidVariantAxes)
	_, err = domain.CreateVariantGroup(domain.VariantGroupID{}, seller, class, []string{"color"}, now)
	require.ErrorIs(t, err, kernel.ErrInvalidID)

	group, err := domain.CreateVariantGroup(domain.NewVariantGroupID(), seller, class, []string{"color", "model"}, now)
	require.NoError(t, err)
	assert.Equal(t, []string{"color", "model"}, group.Axes())
	assert.Equal(t, tr.phones.ID(), group.CategoryID())
	assert.Equal(t, seller, group.SellerID())

	black := draftProduct(t, tr, seller)
	require.NoError(t, group.AddProduct(seller, black, now))
	require.NoError(t, group.AddProduct(seller, black, now), "adding the same product twice is a no-op")

	duplicate := draftProduct(t, tr, seller)
	require.ErrorIs(t, group.AddProduct(seller, duplicate, now), domain.ErrVariantConflict)

	whiteContent := phoneContent()
	whiteContent.Attributes["color"] = "white"
	white, err := domain.CreateProduct(domain.NewProductID(), seller, class, whiteContent, now)
	require.NoError(t, err)
	require.NoError(t, group.AddProduct(seller, white, now))
	assert.Equal(t, []domain.ProductID{black.ID(), white.ID()}, group.ProductIDs())
	assert.Equal(t, map[string]string{"color": "white", "model": "X1"}, group.Members()[1].AxisValues())

	noModel := phoneContent()
	noModel.Attributes["color"] = "red"
	delete(noModel.Attributes, "model")
	incomplete, err := domain.CreateProduct(domain.NewProductID(), seller, class, noModel, now)
	require.NoError(t, err)
	require.ErrorIs(t, group.AddProduct(seller, incomplete, now), domain.ErrVariantAxisMissing)

	require.ErrorIs(t, group.AddProduct(kernel.NewSellerID(), white, now), domain.ErrNotVariantOwner)
	require.ErrorIs(t, group.AddProduct(seller, draftProduct(t, tr, kernel.NewSellerID()), now), domain.ErrNotProductAuthor)
	otherTree := newTree(t)
	require.ErrorIs(t, group.AddProduct(seller, draftProduct(t, otherTree, seller), now), domain.ErrVariantCategoryMismatch)

	require.ErrorIs(t, group.RemoveProduct(kernel.NewSellerID(), black.ID(), now), domain.ErrNotVariantOwner)
	require.NoError(t, group.RemoveProduct(seller, black.ID(), now))
	require.ErrorIs(t, group.RemoveProduct(seller, black.ID(), now), domain.ErrVariantMemberNotFound)

	assert.Equal(t, []string{
		"catalog.variant_group_created.v1", "catalog.variant_group_changed.v1", "catalog.variant_group_changed.v1", "catalog.variant_group_changed.v1",
	}, names(group.PullEvents()))

	group.AdvanceVersion()
	restored, err := domain.RehydrateVariantGroup(group.Snapshot())
	require.NoError(t, err)
	assert.Equal(t, group.Snapshot(), restored.Snapshot())
	assert.Equal(t, 1, restored.Version())
	for _, mutate := range []func(*domain.VariantGroupSnapshot){
		func(s *domain.VariantGroupSnapshot) { s.ID = "bad" },
		func(s *domain.VariantGroupSnapshot) { s.CategoryID = "bad" },
		func(s *domain.VariantGroupSnapshot) { s.SellerID = "bad" },
		func(s *domain.VariantGroupSnapshot) { s.Members = []domain.VariantMemberSnapshot{{ProductID: "bad"}} },
	} {
		snap := group.Snapshot()
		mutate(&snap)
		_, err := domain.RehydrateVariantGroup(snap)
		require.Error(t, err)
	}
}

func terms(t *testing.T, amount int64) domain.OfferTerms {
	t.Helper()
	tm, err := domain.NewOfferTerms(amount, "KZT", "new", 2)
	require.NoError(t, err)
	return tm
}

func sku(t *testing.T, raw string) domain.SellerSKU {
	t.Helper()
	s, err := domain.NewSellerSKU(raw)
	require.NoError(t, err)
	return s
}

func TestOfferValueObjects(t *testing.T) {
	s, err := domain.NewSellerSKU("NOVA-X_1.black")
	require.NoError(t, err)
	assert.Equal(t, "NOVA-X_1.black", s.String())
	for _, raw := range []string{"", "with space", "кириллица", string(make([]byte, 65))} {
		_, err := domain.NewSellerSKU(raw)
		require.ErrorIs(t, err, domain.ErrInvalidSellerSKU, raw)
	}

	tm, err := domain.NewOfferTerms(199000, "KZT", "refurbished", 30)
	require.NoError(t, err)
	assert.Equal(t, int64(199000), tm.Price().Amount())
	assert.Equal(t, domain.ConditionRefurbished, tm.Condition())
	assert.Equal(t, 30, tm.ProcessingDays())

	_, err = domain.NewOfferTerms(0, "KZT", "new", 1)
	require.ErrorIs(t, err, domain.ErrInvalidPrice)
	_, err = domain.NewOfferTerms(1, "kzt", "new", 1)
	require.ErrorIs(t, err, kernel.ErrInvalidCurrency)
	_, err = domain.NewOfferTerms(1, "KZT", "broken", 1)
	require.ErrorIs(t, err, domain.ErrInvalidCondition)
	_, err = domain.NewOfferTerms(1, "KZT", "used", 31)
	require.ErrorIs(t, err, domain.ErrInvalidProcessingTime)
}

func TestOfferLifecycle(t *testing.T) {
	tr := newTree(t)
	author := kernel.NewSellerID()
	seller := kernel.NewSellerID()

	_, err := domain.CreateOffer(domain.NewOfferID(), draftProduct(t, tr, author), seller, sku(t, "A1"), terms(t, 1000), now)
	require.ErrorIs(t, err, domain.ErrProductNotPublished)
	product := publishedProduct(t, tr, author)
	_, err = domain.CreateOffer(domain.OfferID{}, product, seller, sku(t, "A1"), terms(t, 1000), now)
	require.ErrorIs(t, err, kernel.ErrInvalidID)
	_, err = domain.CreateOffer(domain.NewOfferID(), product, seller, domain.SellerSKU{}, terms(t, 1000), now)
	require.ErrorIs(t, err, domain.ErrInvalidSellerSKU)

	offer, err := domain.CreateOffer(domain.NewOfferID(), product, seller, sku(t, "A1"), terms(t, 1000), now)
	require.NoError(t, err)
	assert.Equal(t, domain.OfferActive, offer.Status())
	assert.Equal(t, product.ID(), offer.ProductID())
	assert.Equal(t, seller, offer.SellerID())
	assert.Equal(t, "A1", offer.SKU().String())

	stranger := kernel.NewSellerID()
	require.ErrorIs(t, offer.ChangeTerms(stranger, terms(t, 900), now), domain.ErrNotOfferOwner)
	require.ErrorIs(t, offer.Pause(stranger, now), domain.ErrNotOfferOwner)

	require.NoError(t, offer.ChangeTerms(seller, terms(t, 1000), now))
	require.NoError(t, offer.ChangeTerms(seller, terms(t, 900), now))
	assert.Equal(t, int64(900), offer.Terms().Price().Amount())
	require.NoError(t, offer.Pause(seller, now))
	require.NoError(t, offer.Pause(seller, now))
	require.NoError(t, offer.Activate(seller, now))
	require.NoError(t, offer.Archive(seller, now))
	require.NoError(t, offer.Archive(seller, now))
	require.ErrorIs(t, offer.Activate(seller, now), domain.ErrOfferArchived)
	require.ErrorIs(t, offer.ChangeTerms(seller, terms(t, 1), now), domain.ErrOfferArchived)
	require.ErrorIs(t, offer.Archive(stranger, now), domain.ErrNotOfferOwner)

	events := offer.PullEvents()
	assert.Equal(t, []string{
		"catalog.offer_created.v1", "catalog.offer_updated.v1", "catalog.offer_status_changed.v1",
		"catalog.offer_status_changed.v1", "catalog.offer_status_changed.v1",
	}, names(events))
	last := events[4].(domain.OfferStatusChanged)
	assert.Equal(t, domain.OfferArchived, last.Offer.Status)
	assert.Equal(t, int64(900), last.Offer.Price.Amount())
	assert.Equal(t, offer.ID().String(), last.AggregateID())

	offer.AdvanceVersion()
	restored, err := domain.RehydrateOffer(offer.Snapshot())
	require.NoError(t, err)
	assert.Equal(t, offer.Snapshot(), restored.Snapshot())
	for _, mutate := range []func(*domain.OfferSnapshot){
		func(s *domain.OfferSnapshot) { s.ID = "bad" },
		func(s *domain.OfferSnapshot) { s.ProductID = "bad" },
		func(s *domain.OfferSnapshot) { s.SellerID = "bad" },
		func(s *domain.OfferSnapshot) { s.SellerSKU = "" },
		func(s *domain.OfferSnapshot) { s.PriceAmount = 0 },
	} {
		snap := offer.Snapshot()
		mutate(&snap)
		_, err := domain.RehydrateOffer(snap)
		require.Error(t, err)
	}
}
