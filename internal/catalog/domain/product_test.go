package domain_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

func TestCreateProduct(t *testing.T) {
	tr := newTree(t)
	seller := kernel.NewSellerID()
	p, err := domain.CreateProduct(domain.NewProductID(), seller, tr.phonesClass(t), phoneContent(), now)
	require.NoError(t, err)

	assert.Equal(t, domain.ProductStatusDraft, p.Status())
	assert.Equal(t, tr.phones.ID(), p.CategoryID())
	assert.Equal(t, seller, p.SellerID())
	assert.Equal(t, "Смартфон Nova X", p.Title())
	assert.Equal(t, "Nova", p.Brand())
	assert.Equal(t, "Флагман с отличной камерой", p.Description())
	assert.Equal(t, "256", p.Attributes()["memory_gb"].String())
	assert.False(t, p.IsPublished())
	assert.Equal(t, []string{"catalog.product_created.v1"}, names(p.PullEvents()))
}

func TestCreateProduct_Validation(t *testing.T) {
	tr := newTree(t)
	class := tr.phonesClass(t)
	seller := kernel.NewSellerID()

	_, err := domain.CreateProduct(domain.ProductID{}, seller, class, phoneContent(), now)
	require.ErrorIs(t, err, kernel.ErrInvalidID)
	_, err = domain.CreateProduct(domain.NewProductID(), kernel.SellerID{}, class, phoneContent(), now)
	require.ErrorIs(t, err, kernel.ErrInvalidID)

	cases := map[string]struct {
		mutate func(*domain.ProductContent)
		err    error
	}{
		"short title":       {func(c *domain.ProductContent) { c.Title = "ab" }, domain.ErrInvalidTitle},
		"long description":  {func(c *domain.ProductContent) { c.Description = strings.Repeat("a", 10001) }, domain.ErrInvalidDescription},
		"long brand":        {func(c *domain.ProductContent) { c.Brand = strings.Repeat("b", 101) }, domain.ErrInvalidBrand},
		"unknown attribute": {func(c *domain.ProductContent) { c.Attributes = map[string]string{"weight": "1"} }, domain.ErrInvalidAttributes},
		"invalid enum":      {func(c *domain.ProductContent) { c.Attributes = map[string]string{"color": "pink"} }, domain.ErrInvalidAttributes},
	}
	for name, tc := range cases {
		content := phoneContent()
		tc.mutate(&content)
		_, err := domain.CreateProduct(domain.NewProductID(), seller, class, content, now)
		require.ErrorIs(t, err, tc.err, name)
	}
}

func TestProduct_ModerationLifecycle(t *testing.T) {
	tr := newTree(t)
	seller := kernel.NewSellerID()
	moderator := kernel.NewUserID()
	class := tr.phonesClass(t)
	p := draftProduct(t, tr, seller)

	require.ErrorIs(t, p.Publish(moderator, class, now), &domain.TransitionError{})
	require.NoError(t, p.SubmitForModeration(seller, class, now))
	assert.Equal(t, domain.ProductStatusOnModeration, p.Status())
	require.ErrorIs(t, p.UpdateContent(seller, class, phoneContent(), now), domain.ErrProductLocked)

	require.ErrorIs(t, p.Reject(moderator, " ", now), domain.ErrReasonRequired)
	require.NoError(t, p.Reject(moderator, "размытые фото", now))
	assert.Equal(t, domain.ProductStatusRejected, p.Status())
	assert.Equal(t, "размытые фото", p.RejectionReason())

	content := phoneContent()
	content.Title = "Смартфон Nova X Pro"
	require.NoError(t, p.UpdateContent(seller, class, content, now.Add(time.Hour)))
	assert.Equal(t, domain.ProductStatusDraft, p.Status())

	require.NoError(t, p.SubmitForModeration(seller, class, now))
	assert.Empty(t, p.RejectionReason())
	require.NoError(t, p.Publish(moderator, class, now.Add(2*time.Hour)))
	assert.True(t, p.IsPublished())
	assert.Equal(t, now.Add(2*time.Hour), p.PublishedAt())

	require.ErrorIs(t, p.UpdateContent(seller, class, content, now), domain.ErrProductLocked)
	require.ErrorIs(t, p.Reject(moderator, "late", now), &domain.TransitionError{})
	require.ErrorIs(t, p.SubmitForModeration(seller, class, now), &domain.TransitionError{})

	events := p.PullEvents()
	assert.Equal(t, []string{
		"catalog.product_submitted.v1", "catalog.product_rejected.v1", "catalog.product_content_updated.v1",
		"catalog.product_submitted.v1", "catalog.product_published.v1",
	}, names(events))

	published := events[4].(domain.ProductPublished)
	assert.Equal(t, tr.phones.Path(), published.CategoryPath)
	assert.Equal(t, "Смартфон Nova X Pro", published.Title)
	assert.Equal(t, moderator, published.PublishedBy)
	require.Len(t, published.Attributes, 5)
	assert.Equal(t, "warranty_months", published.Attributes[0].Definition.Code())
	assert.True(t, published.Attributes[1].Definition.Filterable())
	assert.Equal(t, "black", published.Attributes[1].Value.String())
}

func TestProduct_SubmissionRequiresRequiredAttributes(t *testing.T) {
	tr := newTree(t)
	seller := kernel.NewSellerID()
	class := tr.phonesClass(t)
	content := phoneContent()
	delete(content.Attributes, "memory_gb")
	delete(content.Attributes, "warranty_months")

	p, err := domain.CreateProduct(domain.NewProductID(), seller, class, content, now)
	require.NoError(t, err, "drafts may be incomplete")

	err = p.SubmitForModeration(seller, class, now)
	require.ErrorIs(t, err, domain.ErrProductIncomplete)
	var fielded interface {
		Fields() []kernel.FieldViolation
	}
	require.ErrorAs(t, err, &fielded)
	fields := []string{}
	for _, f := range fielded.Fields() {
		fields = append(fields, f.Field)
	}
	assert.Equal(t, []string{"attributes.warranty_months", "attributes.memory_gb"}, fields)
	assert.Equal(t, domain.ProductStatusDraft, p.Status())
}

func TestProduct_PublishRevalidatesAgainstCurrentSchema(t *testing.T) {
	tr := newTree(t)
	seller := kernel.NewSellerID()
	p := draftProduct(t, tr, seller)
	require.NoError(t, p.SubmitForModeration(seller, tr.phonesClass(t), now))

	require.NoError(t, tr.phones.RemoveAttribute("nfc", now))
	require.ErrorIs(t, p.Publish(kernel.NewUserID(), tr.phonesClass(t), now), domain.ErrInvalidAttributes)

	other := newTree(t)
	require.ErrorIs(t, p.Publish(kernel.NewUserID(), other.phonesClass(t), now), domain.ErrCategoryMismatch)
	require.ErrorIs(t, p.UpdateContent(seller, other.phonesClass(t), phoneContent(), now), domain.ErrProductLocked)
}

func TestProduct_OnlyAuthorCanEdit(t *testing.T) {
	tr := newTree(t)
	seller := kernel.NewSellerID()
	stranger := kernel.NewSellerID()
	class := tr.phonesClass(t)
	p := draftProduct(t, tr, seller)

	require.ErrorIs(t, p.UpdateContent(stranger, class, phoneContent(), now), domain.ErrNotProductAuthor)
	require.ErrorIs(t, p.SubmitForModeration(stranger, class, now), domain.ErrNotProductAuthor)
	_, err := p.RequestImageUpload(stranger, domain.NewImageID(), "image/jpeg", 100, now)
	require.ErrorIs(t, err, domain.ErrNotProductAuthor)

	other := newTree(t)
	require.ErrorIs(t, p.UpdateContent(seller, other.phonesClass(t), phoneContent(), now), domain.ErrCategoryMismatch)
	require.ErrorIs(t, p.SubmitForModeration(seller, other.phonesClass(t), now), domain.ErrCategoryMismatch)
}

func TestProduct_SnapshotRoundTrip(t *testing.T) {
	tr := newTree(t)
	seller := kernel.NewSellerID()
	p := draftProduct(t, tr, seller)
	img := domain.NewImageID()
	_, err := p.RequestImageUpload(seller, img, "image/png", 2048, now)
	require.NoError(t, err)
	require.NoError(t, p.ConfirmImageUpload(seller, img, domain.ImageProbe{ContentType: "image/png", Size: 2048, Width: 800, Height: 600}, now))
	require.NoError(t, p.SubmitForModeration(seller, tr.phonesClass(t), now))
	require.NoError(t, p.Reject(kernel.NewUserID(), "нет фото сбоку", now))
	p.AdvanceVersion()

	restored, err := domain.RehydrateProduct(p.Snapshot())
	require.NoError(t, err)
	assert.Equal(t, p.Snapshot(), restored.Snapshot())
	assert.Equal(t, 1, restored.Version())
	assert.Empty(t, restored.PullEvents())

	for _, mutate := range []func(*domain.ProductSnapshot){
		func(s *domain.ProductSnapshot) { s.ID = "bad" },
		func(s *domain.ProductSnapshot) { s.CategoryID = "bad" },
		func(s *domain.ProductSnapshot) { s.SellerID = "bad" },
		func(s *domain.ProductSnapshot) {
			s.Attributes = map[string]domain.AttributeValueSnapshot{"x": {Type: "number", Value: "abc"}}
		},
		func(s *domain.ProductSnapshot) {
			s.Attributes = map[string]domain.AttributeValueSnapshot{"x": {Type: "boolean", Value: "maybe"}}
		},
		func(s *domain.ProductSnapshot) {
			s.Attributes = map[string]domain.AttributeValueSnapshot{"x": {Type: "date", Value: "1"}}
		},
		func(s *domain.ProductSnapshot) { s.Images = []domain.ImageSnapshot{{ID: "bad"}} },
	} {
		snap := p.Snapshot()
		mutate(&snap)
		_, err := domain.RehydrateProduct(snap)
		require.Error(t, err)
	}
}
