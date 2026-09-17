package command_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application/command"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
	sellerapi "github.com/gliedabrennung/go-marketplace-backend/internal/seller/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/pagination"
)

func TestProducts_ModerationLifecycle(t *testing.T) {
	e := newEnv(t)
	tr := e.tree(t)
	owner, seller := e.seller()
	moderator := user("content_moderator")

	_, err := e.createProduct.Handle(ctx, productCommand(user(), seller, tr.phones, phone("black")))
	require.ErrorIs(t, err, sellerapi.ErrSellerNotFound)
	_, err = e.createProduct.Handle(ctx, productCommand(auth.Principal{}, seller, tr.phones, phone("black")))
	require.ErrorIs(t, err, auth.ErrUnauthenticated)
	_, err = e.createProduct.Handle(ctx, productCommand(owner, "bad", tr.phones, phone("black")))
	require.ErrorIs(t, err, sellerapi.ErrSellerNotFound)
	_, err = e.createProduct.Handle(ctx, productCommand(owner, seller, "bad", phone("black")))
	require.ErrorIs(t, err, domain.ErrCategoryNotFound)
	_, err = e.createProduct.Handle(ctx, productCommand(owner, seller, tr.phones, map[string]string{"color": "green"}))
	require.ErrorIs(t, err, domain.ErrInvalidAttributes)

	id := e.draft(t, owner, seller, tr.phones, map[string]string{"color": "black"})
	_, err = e.submitProduct.Handle(ctx, command.SubmitProduct{Actor: owner, ProductID: id})
	require.ErrorIs(t, err, domain.ErrProductIncomplete)

	update := command.UpdateProduct{Actor: owner, ProductID: id, Title: "Смартфон Nova X Pro", Brand: "Nova", Attributes: phone("black")}
	_, err = e.updateProduct.Handle(ctx, command.UpdateProduct{Actor: user(), ProductID: id, Title: update.Title, Attributes: update.Attributes})
	require.ErrorIs(t, err, domain.ErrProductNotFound)
	_, err = e.updateProduct.Handle(ctx, command.UpdateProduct{Actor: owner, ProductID: "bad", Title: update.Title})
	require.ErrorIs(t, err, domain.ErrProductNotFound)
	_, err = e.updateProduct.Handle(ctx, update)
	require.NoError(t, err)
	_, err = e.submitProduct.Handle(ctx, command.SubmitProduct{Actor: owner, ProductID: id})
	require.NoError(t, err)

	_, err = e.getProduct.Handle(ctx, query.GetProduct{ProductID: id})
	require.ErrorIs(t, err, domain.ErrProductNotFound)
	_, err = e.getProduct.Handle(ctx, query.GetProduct{Actor: user(), ProductID: id})
	require.ErrorIs(t, err, domain.ErrProductNotFound)
	view, err := e.getProduct.Handle(ctx, query.GetProduct{Actor: owner, ProductID: id})
	require.NoError(t, err)
	assert.Equal(t, "on_moderation", view.Status)

	queue, err := e.moderationQueue.Handle(ctx, query.ListModerationQueue{Actor: moderator})
	require.NoError(t, err)
	require.Len(t, queue.Items, 1)
	assert.Equal(t, id, queue.Items[0].ID)
	_, err = e.moderationQueue.Handle(ctx, query.ListModerationQueue{Actor: owner})
	require.ErrorIs(t, err, auth.ErrForbidden)
	_, err = e.moderationQueue.Handle(ctx, query.ListModerationQueue{Actor: moderator, Cursor: "broken"})
	require.ErrorIs(t, err, pagination.ErrInvalidCursor)

	_, err = e.rejectProduct.Handle(ctx, command.RejectProduct{Actor: owner, ProductID: id, Reason: "bad photos"})
	require.ErrorIs(t, err, auth.ErrForbidden)
	_, err = e.rejectProduct.Handle(ctx, command.RejectProduct{Actor: moderator, ProductID: id})
	require.ErrorIs(t, err, domain.ErrReasonRequired)
	_, err = e.rejectProduct.Handle(ctx, command.RejectProduct{Actor: moderator, ProductID: "bad", Reason: "x"})
	require.ErrorIs(t, err, domain.ErrProductNotFound)
	_, err = e.rejectProduct.Handle(ctx, command.RejectProduct{Actor: moderator, ProductID: id, Reason: "bad photos"})
	require.NoError(t, err)
	view, err = e.getProduct.Handle(ctx, query.GetProduct{Actor: owner, ProductID: id})
	require.NoError(t, err)
	assert.Equal(t, "rejected", view.Status)
	assert.Equal(t, "bad photos", view.RejectionReason)

	_, err = e.submitProduct.Handle(ctx, command.SubmitProduct{Actor: owner, ProductID: id})
	require.NoError(t, err)
	e.clock.Advance(time.Minute)
	_, err = e.publishProduct.Handle(ctx, command.PublishProduct{Actor: moderator, ProductID: id})
	require.NoError(t, err)

	public, err := e.getProduct.Handle(ctx, query.GetProduct{ProductID: id})
	require.NoError(t, err)
	assert.Equal(t, "published", public.Status)
	assert.Equal(t, "Смартфон Nova X Pro", public.Title)
	assert.False(t, public.PublishedAt.IsZero())
	require.NotEmpty(t, public.Attributes)
	assert.Equal(t, "warranty_months", public.Attributes[0].Code)
	assert.Equal(t, "Гарантия", public.Attributes[0].Name)
	assert.Nil(t, public.Variants)

	_, err = e.updateProduct.Handle(ctx, update)
	require.ErrorIs(t, err, domain.ErrProductLocked)
	_, err = e.getProduct.Handle(ctx, query.GetProduct{ProductID: "bad"})
	require.ErrorIs(t, err, domain.ErrProductNotFound)

	assert.Equal(t, []string{"catalog.product.reject", "catalog.product.publish"}, e.actions()[len(e.actions())-2:])
}

func TestProducts_SellerListing(t *testing.T) {
	e := newEnv(t)
	tr := e.tree(t)
	owner, seller := e.seller()
	first := e.draft(t, owner, seller, tr.phones, phone("black"))
	second := e.draft(t, owner, seller, tr.phones, phone("white"))
	third := e.published(t, owner, seller, tr.phones, "red")

	page, err := e.sellerProducts.Handle(ctx, query.ListSellerProducts{Actor: owner, SellerID: seller, Limit: 2})
	require.NoError(t, err)
	require.Len(t, page.Items, 2)
	assert.True(t, page.HasMore)
	assert.Equal(t, []string{third, second}, []string{page.Items[0].ID, page.Items[1].ID})

	page, err = e.sellerProducts.Handle(ctx, query.ListSellerProducts{Actor: owner, SellerID: seller, Limit: 2, Cursor: page.NextCursor})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.False(t, page.HasMore)
	assert.Equal(t, first, page.Items[0].ID)

	page, err = e.sellerProducts.Handle(ctx, query.ListSellerProducts{Actor: owner, SellerID: seller, Status: "published"})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.Equal(t, third, page.Items[0].ID)

	_, err = e.sellerProducts.Handle(ctx, query.ListSellerProducts{Actor: owner, SellerID: seller, Status: "deleted"})
	require.ErrorIs(t, err, query.ErrUnknownStatus)
	_, err = e.sellerProducts.Handle(ctx, query.ListSellerProducts{Actor: user(), SellerID: seller})
	require.ErrorIs(t, err, sellerapi.ErrSellerNotFound)
	_, err = e.sellerProducts.Handle(ctx, query.ListSellerProducts{Actor: owner, SellerID: "bad"})
	require.ErrorIs(t, err, sellerapi.ErrSellerNotFound)
	_, err = e.sellerProducts.Handle(ctx, query.ListSellerProducts{SellerID: seller})
	require.ErrorIs(t, err, auth.ErrUnauthenticated)
	_, err = e.sellerProducts.Handle(ctx, query.ListSellerProducts{Actor: owner, SellerID: seller, Cursor: "%%%"})
	require.ErrorIs(t, err, pagination.ErrInvalidCursor)
}
