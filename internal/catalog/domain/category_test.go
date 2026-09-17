package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

func TestCreateCategory(t *testing.T) {
	root, err := domain.CreateCategory(domain.NewCategoryID(), " Электроника ", "electronics", nil, now)
	require.NoError(t, err)
	assert.Equal(t, "Электроника", root.Name())
	assert.Equal(t, "electronics", root.Slug())
	assert.True(t, root.ParentID().IsZero())
	assert.Equal(t, 1, root.Depth())
	assert.Equal(t, []domain.CategoryID{root.ID()}, root.Path())

	child, err := domain.CreateCategory(domain.NewCategoryID(), "Смартфоны", "smartphones", root, now)
	require.NoError(t, err)
	assert.Equal(t, root.ID(), child.ParentID())
	assert.Equal(t, []domain.CategoryID{root.ID()}, child.Ancestors())
	assert.Equal(t, []domain.CategoryID{root.ID(), child.ID()}, child.Path())

	events := child.PullEvents()
	require.Len(t, events, 1)
	created := events[0].(domain.CategoryCreated)
	assert.Equal(t, child.Path(), created.Path)
	assert.Equal(t, "catalog.category_created.v1", created.EventName())
}

func TestCreateCategory_Validation(t *testing.T) {
	_, err := domain.CreateCategory(domain.CategoryID{}, "Name", "slug", nil, now)
	require.ErrorIs(t, err, kernel.ErrInvalidID)
	_, err = domain.CreateCategory(domain.NewCategoryID(), "", "slug", nil, now)
	require.ErrorIs(t, err, domain.ErrInvalidCategoryName)
	for _, slug := range []string{"", "Upper", "with space", "under_score"} {
		_, err = domain.CreateCategory(domain.NewCategoryID(), "Name", slug, nil, now)
		require.ErrorIs(t, err, domain.ErrInvalidSlug, slug)
	}

	parent, err := domain.CreateCategory(domain.NewCategoryID(), "L1", "l1", nil, now)
	require.NoError(t, err)
	for depth := 2; depth <= domain.MaxCategoryDepth; depth++ {
		parent, err = domain.CreateCategory(domain.NewCategoryID(), "L", "l", parent, now)
		require.NoError(t, err)
	}
	_, err = domain.CreateCategory(domain.NewCategoryID(), "Too deep", "deep", parent, now)
	require.ErrorIs(t, err, domain.ErrCategoryTooDeep)
}

func TestCategory_Rename(t *testing.T) {
	c, err := domain.CreateCategory(domain.NewCategoryID(), "Phones", "phones", nil, now)
	require.NoError(t, err)
	c.PullEvents()
	require.NoError(t, c.Rename("Phones", "phones", now))
	assert.Empty(t, c.PullEvents())
	require.NoError(t, c.Rename("Смартфоны", "smartphones", now))
	assert.Equal(t, []string{"catalog.category_renamed.v1"}, names(c.PullEvents()))
	require.ErrorIs(t, c.Rename("", "x", now), domain.ErrInvalidCategoryName)
}

func TestCategory_AttributeCodesAreUniqueAcrossTree(t *testing.T) {
	tr := newTree(t)
	inherited := classify(t, tr.root, tr.electronics).Schema()

	dup := attr(t, domain.AttributeSpec{Code: "warranty_months", Name: "Гарантия", Type: "number"})
	require.ErrorIs(t, tr.phones.DefineAttribute(dup, inherited, nil, now), domain.ErrDuplicateAttribute, "inherited code")

	own := attr(t, domain.AttributeSpec{Code: "color", Name: "Цвет", Type: "string"})
	require.ErrorIs(t, tr.phones.DefineAttribute(own, inherited, nil, now), domain.ErrDuplicateAttribute, "own code")

	descendant := attr(t, domain.AttributeSpec{Code: "nfc", Name: "NFC", Type: "boolean"})
	require.ErrorIs(t, tr.electronics.DefineAttribute(descendant, classify(t, tr.root).Schema(), []string{"nfc", "color"}, now), domain.ErrDuplicateAttribute, "descendant code")

	require.ErrorIs(t, tr.phones.DefineAttribute(domain.AttributeDefinition{}, inherited, nil, now), domain.ErrInvalidAttributeCode)

	require.NoError(t, tr.phones.RemoveAttribute("nfc", now))
	require.ErrorIs(t, tr.phones.RemoveAttribute("nfc", now), domain.ErrAttributeNotFound)
	assert.Equal(t, []string{"catalog.category_attribute_removed.v1"}, names(tr.phones.PullEvents()))
	assert.Len(t, tr.phones.Attributes(), 3)
}

func TestClassify(t *testing.T) {
	tr := newTree(t)
	class := tr.phonesClass(t)
	assert.Equal(t, tr.phones.ID(), class.CategoryID())
	assert.Equal(t, tr.phones.Path(), class.Path())

	codes := []string{}
	for _, d := range class.Schema().Definitions() {
		codes = append(codes, d.Code())
	}
	assert.Equal(t, []string{"brand_country", "warranty_months", "color", "memory_gb", "nfc", "model"}, codes)

	_, err := domain.Classify(nil)
	require.ErrorIs(t, err, domain.ErrCategoryNotFound)
	_, err = domain.Classify([]*domain.Category{tr.root, tr.phones})
	require.ErrorIs(t, err, domain.ErrBrokenCategoryChain)
	_, err = domain.Classify([]*domain.Category{tr.electronics})
	require.ErrorIs(t, err, domain.ErrBrokenCategoryChain)
}

func TestCategory_SnapshotRoundTrip(t *testing.T) {
	tr := newTree(t)
	tr.phones.AdvanceVersion()
	restored, err := domain.RehydrateCategory(tr.phones.Snapshot())
	require.NoError(t, err)
	assert.Equal(t, tr.phones.Snapshot(), restored.Snapshot())
	assert.Equal(t, 1, restored.Version())

	root, err := domain.RehydrateCategory(tr.root.Snapshot())
	require.NoError(t, err)
	assert.True(t, root.ParentID().IsZero())

	for _, mutate := range []func(*domain.CategorySnapshot){
		func(s *domain.CategorySnapshot) { s.ID = "bad" },
		func(s *domain.CategorySnapshot) { s.ParentID = "bad" },
		func(s *domain.CategorySnapshot) { s.Ancestors = []string{"bad"} },
		func(s *domain.CategorySnapshot) { s.Attributes = []domain.AttributeSpec{{Code: "Bad"}} },
	} {
		snap := tr.phones.Snapshot()
		mutate(&snap)
		_, err := domain.RehydrateCategory(snap)
		require.Error(t, err)
	}
}

func TestSchema(t *testing.T) {
	a := attr(t, domain.AttributeSpec{Code: "a", Name: "A", Type: "string", Required: true})
	b := attr(t, domain.AttributeSpec{Code: "b", Name: "B", Type: "number"})
	_, err := domain.NewSchema(a, a)
	require.ErrorIs(t, err, domain.ErrDuplicateAttribute)

	schema, err := domain.NewSchema(a, b)
	require.NoError(t, err)
	assert.True(t, schema.Has("a"))
	assert.False(t, schema.Has("c"))

	_, err = schema.ParseValues(map[string]string{"b": "x", "c": "1"})
	require.ErrorIs(t, err, domain.ErrInvalidAttributes)
	var fielded interface {
		Fields() []kernel.FieldViolation
	}
	require.ErrorAs(t, err, &fielded)
	require.Len(t, fielded.Fields(), 2)
	assert.Equal(t, "attributes.b", fielded.Fields()[0].Field)
	assert.Equal(t, "UNKNOWN_ATTRIBUTE", fielded.Fields()[1].Code)

	values, err := schema.ParseValues(map[string]string{"b": "2"})
	require.NoError(t, err)
	missing := schema.MissingRequired(values)
	require.Len(t, missing, 1)
	assert.Equal(t, "attributes.a", missing[0].Field)

	entries := schema.Entries(values)
	require.Len(t, entries, 1)
	assert.Equal(t, "b", entries[0].Definition.Code())
}
