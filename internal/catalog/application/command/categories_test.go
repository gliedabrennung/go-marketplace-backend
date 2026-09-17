package command_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application/command"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

func TestCategories_TreeAndEffectiveSchema(t *testing.T) {
	e := newEnv(t)
	tr := e.tree(t)

	nodes, err := e.listCategories.Handle(ctx, query.ListCategories{})
	require.NoError(t, err)
	slugs := []string{}
	for _, n := range nodes {
		slugs = append(slugs, n.Slug)
	}
	assert.Equal(t, []string{"all", "electronics", "smartphones"}, slugs)

	view, err := e.getCategory.Handle(ctx, query.GetCategory{CategoryID: tr.phones})
	require.NoError(t, err)
	assert.Equal(t, "Смартфоны", view.Name)
	assert.Equal(t, tr.electronics, view.ParentID)
	require.Len(t, view.Path, 3)
	assert.Equal(t, 3, view.Path[2].Depth)
	codes := []string{}
	for _, a := range view.Attributes {
		codes = append(codes, a.Code)
	}
	assert.Equal(t, []string{"brand_country", "warranty_months", "color", "memory_gb", "nfc", "model"}, codes)
	assert.Equal(t, tr.root, view.Attributes[0].CategoryID)
	assert.Equal(t, []string{"black", "white", "red"}, view.Attributes[2].Options)
	assert.Equal(t, "GB", view.Attributes[3].Unit)

	_, err = e.getCategory.Handle(ctx, query.GetCategory{CategoryID: "bad"})
	require.ErrorIs(t, err, domain.ErrCategoryNotFound)
	_, err = e.getCategory.Handle(ctx, query.GetCategory{CategoryID: domain.NewCategoryID().String()})
	require.ErrorIs(t, err, domain.ErrCategoryNotFound)
}

func TestCategories_Management(t *testing.T) {
	e := newEnv(t)
	tr := e.tree(t)
	admin := user("platform_admin")

	_, err := e.createCategory.Handle(ctx, command.CreateCategory{Actor: user("content_moderator"), Name: "Книги", Slug: "books"})
	require.ErrorIs(t, err, auth.ErrForbidden)
	_, err = e.createCategory.Handle(ctx, command.CreateCategory{Actor: auth.Principal{}, Name: "Книги", Slug: "books"})
	require.ErrorIs(t, err, auth.ErrUnauthenticated)
	_, err = e.createCategory.Handle(ctx, command.CreateCategory{Actor: admin, ParentID: tr.root, Name: "Другая электроника", Slug: "electronics"})
	require.ErrorIs(t, err, domain.ErrCategorySlugTaken)
	_, err = e.createCategory.Handle(ctx, command.CreateCategory{Actor: admin, ParentID: "bad", Name: "Книги", Slug: "books"})
	require.ErrorIs(t, err, domain.ErrCategoryNotFound)
	_, err = e.createCategory.Handle(ctx, command.CreateCategory{Actor: admin, ParentID: tr.root, Name: "", Slug: "books"})
	require.ErrorIs(t, err, domain.ErrInvalidCategoryName)

	_, err = e.renameCategory.Handle(ctx, command.RenameCategory{Actor: admin, CategoryID: tr.phones, Name: "Телефоны", Slug: "phones"})
	require.NoError(t, err)
	_, err = e.renameCategory.Handle(ctx, command.RenameCategory{Actor: admin, CategoryID: "bad", Name: "Телефоны", Slug: "phones"})
	require.ErrorIs(t, err, domain.ErrCategoryNotFound)
	_, err = e.renameCategory.Handle(ctx, command.RenameCategory{Actor: user(), CategoryID: tr.phones, Name: "Телефоны", Slug: "phones"})
	require.ErrorIs(t, err, auth.ErrForbidden)
	view, err := e.getCategory.Handle(ctx, query.GetCategory{CategoryID: tr.phones})
	require.NoError(t, err)
	assert.Equal(t, "phones", view.Slug)

	_, err = e.defineAttribute.Handle(ctx, command.DefineAttribute{Actor: admin, CategoryID: tr.root, Code: "color", Name: "Цвет", Type: "string"})
	require.ErrorIs(t, err, domain.ErrDuplicateAttribute)
	_, err = e.defineAttribute.Handle(ctx, command.DefineAttribute{Actor: admin, CategoryID: tr.phones, Code: "brand_country", Name: "Страна", Type: "string"})
	require.ErrorIs(t, err, domain.ErrDuplicateAttribute)
	_, err = e.defineAttribute.Handle(ctx, command.DefineAttribute{Actor: admin, CategoryID: tr.phones, Code: "size", Name: "Размер", Type: "blob"})
	require.ErrorIs(t, err, domain.ErrInvalidAttributeType)

	_, err = e.removeAttribute.Handle(ctx, command.RemoveAttribute{Actor: admin, CategoryID: tr.phones, Code: "model"})
	require.NoError(t, err)
	_, err = e.removeAttribute.Handle(ctx, command.RemoveAttribute{Actor: admin, CategoryID: tr.phones, Code: "model"})
	require.ErrorIs(t, err, domain.ErrAttributeNotFound)

	actions := e.actions()
	assert.Contains(t, actions, "catalog.category.create")
	assert.Contains(t, actions, "catalog.category.define_attribute")
	assert.Equal(t, "catalog.category.remove_attribute", actions[len(actions)-1])
	assert.Equal(t, "catalog.category.rename", actions[len(actions)-2])
}
