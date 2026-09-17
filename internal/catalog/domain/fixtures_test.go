package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

var now = time.Date(2026, 9, 16, 10, 0, 0, 0, time.UTC)

func attr(t *testing.T, spec domain.AttributeSpec) domain.AttributeDefinition {
	t.Helper()
	def, err := domain.NewAttributeDefinition(spec)
	require.NoError(t, err)
	return def
}

type tree struct {
	root        *domain.Category
	electronics *domain.Category
	phones      *domain.Category
}

func newTree(t *testing.T) tree {
	t.Helper()
	root, err := domain.CreateCategory(domain.NewCategoryID(), "Все товары", "all", nil, now)
	require.NoError(t, err)
	require.NoError(t, root.DefineAttribute(attr(t, domain.AttributeSpec{Code: "brand_country", Name: "Страна бренда", Type: "string"}), domain.Schema{}, nil, now))

	electronics, err := domain.CreateCategory(domain.NewCategoryID(), "Электроника", "electronics", root, now)
	require.NoError(t, err)
	inherited := classify(t, root).Schema()
	require.NoError(t, electronics.DefineAttribute(attr(t, domain.AttributeSpec{Code: "warranty_months", Name: "Гарантия", Type: "number", Required: true}), inherited, nil, now))

	phones, err := domain.CreateCategory(domain.NewCategoryID(), "Смартфоны", "smartphones", electronics, now)
	require.NoError(t, err)
	inherited = classify(t, root, electronics).Schema()
	require.NoError(t, phones.DefineAttribute(attr(t, domain.AttributeSpec{Code: "color", Name: "Цвет", Type: "enum", Filterable: true, Options: []string{"black", "white", "red"}}), inherited, nil, now))
	require.NoError(t, phones.DefineAttribute(attr(t, domain.AttributeSpec{Code: "memory_gb", Name: "Память", Type: "unit", Unit: "GB", Filterable: true, Required: true}), inherited, nil, now))
	require.NoError(t, phones.DefineAttribute(attr(t, domain.AttributeSpec{Code: "nfc", Name: "NFC", Type: "boolean", Filterable: true}), inherited, nil, now))
	require.NoError(t, phones.DefineAttribute(attr(t, domain.AttributeSpec{Code: "model", Name: "Модель", Type: "string"}), inherited, nil, now))

	for _, c := range []*domain.Category{root, electronics, phones} {
		c.PullEvents()
	}
	return tree{root: root, electronics: electronics, phones: phones}
}

func classify(t *testing.T, chain ...*domain.Category) domain.Classification {
	t.Helper()
	c, err := domain.Classify(chain)
	require.NoError(t, err)
	return c
}

func (tr tree) phonesClass(t *testing.T) domain.Classification {
	return classify(t, tr.root, tr.electronics, tr.phones)
}

func phoneContent() domain.ProductContent {
	return domain.ProductContent{
		Title:       "Смартфон Nova X",
		Description: "Флагман с отличной камерой",
		Brand:       "Nova",
		Attributes:  map[string]string{"color": "black", "memory_gb": "256", "nfc": "true", "warranty_months": "12", "model": "X1"},
	}
}

func draftProduct(t *testing.T, tr tree, seller kernel.SellerID) *domain.Product {
	t.Helper()
	p, err := domain.CreateProduct(domain.NewProductID(), seller, tr.phonesClass(t), phoneContent(), now)
	require.NoError(t, err)
	p.PullEvents()
	return p
}

func publishedProduct(t *testing.T, tr tree, seller kernel.SellerID) *domain.Product {
	t.Helper()
	p := draftProduct(t, tr, seller)
	require.NoError(t, p.SubmitForModeration(seller, tr.phonesClass(t), now))
	require.NoError(t, p.Publish(kernel.NewUserID(), tr.phonesClass(t), now))
	p.PullEvents()
	return p
}

func names(events []kernel.DomainEvent) []string {
	out := make([]string, len(events))
	for i, e := range events {
		out[i] = e.EventName()
	}
	return out
}
