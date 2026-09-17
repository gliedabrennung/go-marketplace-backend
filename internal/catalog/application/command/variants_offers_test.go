package command_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application/command"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
	sellerapi "github.com/gliedabrennung/go-marketplace-backend/internal/seller/api"
)

func TestVariants_Grouping(t *testing.T) {
	e := newEnv(t)
	tr := e.tree(t)
	owner, seller := e.seller()
	black := e.published(t, owner, seller, tr.phones, "black")
	white := e.published(t, owner, seller, tr.phones, "white")
	red := e.draft(t, owner, seller, tr.phones, phone("red"))
	duplicate := e.draft(t, owner, seller, tr.phones, phone("black"))

	_, err := e.createGroup.Handle(ctx, command.CreateVariantGroup{Actor: owner, SellerID: seller, CategoryID: tr.phones, Axes: []string{"nfc"}})
	require.ErrorIs(t, err, domain.ErrInvalidVariantAxes)
	_, err = e.createGroup.Handle(ctx, command.CreateVariantGroup{Actor: owner, SellerID: seller, CategoryID: "bad", Axes: []string{"color"}})
	require.ErrorIs(t, err, domain.ErrCategoryNotFound)
	_, err = e.createGroup.Handle(ctx, command.CreateVariantGroup{Actor: user(), SellerID: seller, CategoryID: tr.phones, Axes: []string{"color"}})
	require.ErrorIs(t, err, sellerapi.ErrSellerNotFound)

	group, err := e.createGroup.Handle(ctx, command.CreateVariantGroup{Actor: owner, SellerID: seller, CategoryID: tr.phones, Axes: []string{"color"}})
	require.NoError(t, err)
	other, err := e.createGroup.Handle(ctx, command.CreateVariantGroup{Actor: owner, SellerID: seller, CategoryID: tr.phones, Axes: []string{"model"}})
	require.NoError(t, err)

	for _, productID := range []string{black, white, red} {
		_, err = e.addMember.Handle(ctx, command.AddVariantMember{Actor: owner, GroupID: group.GroupID, ProductID: productID})
		require.NoError(t, err)
	}
	_, err = e.addMember.Handle(ctx, command.AddVariantMember{Actor: owner, GroupID: group.GroupID, ProductID: duplicate})
	require.ErrorIs(t, err, domain.ErrVariantConflict)
	_, err = e.addMember.Handle(ctx, command.AddVariantMember{Actor: owner, GroupID: other.GroupID, ProductID: black})
	require.ErrorIs(t, err, domain.ErrProductAlreadyGrouped)
	_, err = e.addMember.Handle(ctx, command.AddVariantMember{Actor: user(), GroupID: group.GroupID, ProductID: black})
	require.ErrorIs(t, err, domain.ErrVariantGroupNotFound)
	_, err = e.addMember.Handle(ctx, command.AddVariantMember{Actor: owner, GroupID: "bad", ProductID: black})
	require.ErrorIs(t, err, domain.ErrVariantGroupNotFound)
	_, err = e.addMember.Handle(ctx, command.AddVariantMember{Actor: owner, GroupID: group.GroupID, ProductID: "bad"})
	require.ErrorIs(t, err, domain.ErrProductNotFound)

	public, err := e.getProduct.Handle(ctx, query.GetProduct{ProductID: black})
	require.NoError(t, err)
	require.NotNil(t, public.Variants)
	assert.Equal(t, []string{"color"}, public.Variants.Axes)
	assert.Len(t, public.Variants.Members, 2)
	private, err := e.getProduct.Handle(ctx, query.GetProduct{Actor: user("content_moderator"), ProductID: black})
	require.NoError(t, err)
	assert.Len(t, private.Variants.Members, 3)

	_, err = e.removeMember.Handle(ctx, command.RemoveVariantMember{Actor: owner, GroupID: group.GroupID, ProductID: white})
	require.NoError(t, err)
	_, err = e.removeMember.Handle(ctx, command.RemoveVariantMember{Actor: owner, GroupID: group.GroupID, ProductID: white})
	require.ErrorIs(t, err, domain.ErrVariantMemberNotFound)
	_, err = e.removeMember.Handle(ctx, command.RemoveVariantMember{Actor: owner, GroupID: group.GroupID, ProductID: "bad"})
	require.ErrorIs(t, err, domain.ErrVariantMemberNotFound)
	_, err = e.addMember.Handle(ctx, command.AddVariantMember{Actor: owner, GroupID: other.GroupID, ProductID: white})
	require.NoError(t, err)
}

func TestOffers_Lifecycle(t *testing.T) {
	e := newEnv(t)
	tr := e.tree(t)
	owner, seller := e.seller()
	rivalOwner, rival := e.seller()
	product := e.published(t, owner, seller, tr.phones, "black")
	another := e.published(t, owner, seller, tr.phones, "white")
	draft := e.draft(t, owner, seller, tr.phones, phone("red"))

	offer := func(sellerOwnerID, sellerID, productID, sku string, price int64) command.CreateOffer {
		actor := owner
		if sellerOwnerID == rival {
			actor = rivalOwner
		}
		return command.CreateOffer{Actor: actor, SellerID: sellerID, ProductID: productID, SellerSKU: sku, Price: price, Condition: "new", ProcessingDays: 2}
	}

	_, err := e.createOffer.Handle(ctx, offer(seller, seller, draft, "SKU-0", 1000))
	require.ErrorIs(t, err, domain.ErrProductNotPublished)
	_, err = e.createOffer.Handle(ctx, offer(seller, seller, product, "SKU 1", 1000))
	require.ErrorIs(t, err, domain.ErrInvalidSellerSKU)
	_, err = e.createOffer.Handle(ctx, offer(seller, seller, product, "SKU-1", 0))
	require.ErrorIs(t, err, domain.ErrInvalidPrice)
	_, err = e.createOffer.Handle(ctx, offer(seller, seller, "bad", "SKU-1", 1000))
	require.ErrorIs(t, err, domain.ErrProductNotFound)
	_, err = e.createOffer.Handle(ctx, command.CreateOffer{Actor: user(), SellerID: seller, ProductID: product, SellerSKU: "SKU-1", Price: 1000, Condition: "new"})
	require.ErrorIs(t, err, sellerapi.ErrSellerNotFound)

	created, err := e.createOffer.Handle(ctx, offer(seller, seller, product, "SKU-1", 100000))
	require.NoError(t, err)
	_, err = e.createOffer.Handle(ctx, offer(seller, seller, product, "SKU-2", 100000))
	require.ErrorIs(t, err, domain.ErrOfferExists)
	_, err = e.createOffer.Handle(ctx, offer(seller, seller, another, "SKU-1", 100000))
	require.ErrorIs(t, err, domain.ErrSellerSKUTaken)
	rivalOffer, err := e.createOffer.Handle(ctx, offer(rival, rival, product, "SKU-1", 80000))
	require.NoError(t, err)

	_, err = e.updateOffer.Handle(ctx, command.UpdateOfferTerms{Actor: user(), OfferID: created.OfferID, Price: 90000, Condition: "new"})
	require.ErrorIs(t, err, domain.ErrOfferNotFound)
	_, err = e.updateOffer.Handle(ctx, command.UpdateOfferTerms{Actor: owner, OfferID: "bad", Price: 90000, Condition: "new"})
	require.ErrorIs(t, err, domain.ErrOfferNotFound)
	_, err = e.updateOffer.Handle(ctx, command.UpdateOfferTerms{Actor: owner, OfferID: created.OfferID, Price: 90000, Condition: "mint"})
	require.ErrorIs(t, err, domain.ErrInvalidCondition)
	_, err = e.updateOffer.Handle(ctx, command.UpdateOfferTerms{Actor: owner, OfferID: created.OfferID, Price: 70000, Currency: "KZT", Condition: "refurbished", ProcessingDays: 1})
	require.NoError(t, err)

	_, err = e.setOfferStatus.Handle(ctx, command.SetOfferStatus{Actor: owner, OfferID: created.OfferID, Status: "deleted"})
	require.ErrorIs(t, err, command.ErrInvalidOfferStatus)
	_, err = e.setOfferStatus.Handle(ctx, command.SetOfferStatus{Actor: owner, OfferID: created.OfferID, Status: "paused"})
	require.NoError(t, err)

	offers, err := e.productOffers.Handle(ctx, query.ListProductOffers{ProductID: product})
	require.NoError(t, err)
	require.Len(t, offers, 1)
	assert.Equal(t, rivalOffer.OfferID, offers[0].ID)

	_, err = e.setOfferStatus.Handle(ctx, command.SetOfferStatus{Actor: owner, OfferID: created.OfferID, Status: "active"})
	require.NoError(t, err)
	offers, err = e.productOffers.Handle(ctx, query.ListProductOffers{ProductID: product})
	require.NoError(t, err)
	require.Len(t, offers, 2)
	assert.Equal(t, created.OfferID, offers[0].ID)
	assert.Equal(t, int64(70000), offers[0].Price)
	assert.Equal(t, "KZT", offers[0].Currency)
	assert.Equal(t, "refurbished", offers[0].Condition)

	e.sellers.suspend(rival)
	offers, err = e.productOffers.Handle(ctx, query.ListProductOffers{ProductID: product})
	require.NoError(t, err)
	require.Len(t, offers, 1)
	_, err = e.setOfferStatus.Handle(ctx, command.SetOfferStatus{Actor: rivalOwner, OfferID: rivalOffer.OfferID, Status: "paused"})
	require.NoError(t, err)
	_, err = e.setOfferStatus.Handle(ctx, command.SetOfferStatus{Actor: rivalOwner, OfferID: rivalOffer.OfferID, Status: "active"})
	require.ErrorIs(t, err, application.ErrSellerInactive)
	_, err = e.createOffer.Handle(ctx, offer(rival, rival, another, "SKU-9", 1000))
	require.ErrorIs(t, err, application.ErrSellerInactive)

	_, err = e.setOfferStatus.Handle(ctx, command.SetOfferStatus{Actor: owner, OfferID: created.OfferID, Status: "archived"})
	require.NoError(t, err)
	_, err = e.updateOffer.Handle(ctx, command.UpdateOfferTerms{Actor: owner, OfferID: created.OfferID, Price: 1, Condition: "new"})
	require.ErrorIs(t, err, domain.ErrOfferArchived)
	_, err = e.createOffer.Handle(ctx, offer(seller, seller, product, "SKU-1", 65000))
	require.NoError(t, err)

	page, err := e.sellerOffers.Handle(ctx, query.ListSellerOffers{Actor: owner, SellerID: seller, Status: "archived"})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	page, err = e.sellerOffers.Handle(ctx, query.ListSellerOffers{Actor: owner, SellerID: seller})
	require.NoError(t, err)
	assert.Len(t, page.Items, 2)
	_, err = e.sellerOffers.Handle(ctx, query.ListSellerOffers{Actor: owner, SellerID: seller, Status: "gone"})
	require.ErrorIs(t, err, query.ErrUnknownStatus)
	_, err = e.sellerOffers.Handle(ctx, query.ListSellerOffers{Actor: rivalOwner, SellerID: seller})
	require.ErrorIs(t, err, sellerapi.ErrSellerNotFound)
	_, err = e.sellerOffers.Handle(ctx, query.ListSellerOffers{Actor: owner, SellerID: seller, Cursor: "x"})
	require.Error(t, err)

	_, err = e.productOffers.Handle(ctx, query.ListProductOffers{ProductID: draft})
	require.ErrorIs(t, err, domain.ErrProductNotFound)
	_, err = e.productOffers.Handle(ctx, query.ListProductOffers{ProductID: "bad"})
	require.ErrorIs(t, err, domain.ErrProductNotFound)
	_, err = e.productOffers.Handle(ctx, query.ListProductOffers{ProductID: domain.NewProductID().String()})
	require.ErrorIs(t, err, domain.ErrProductNotFound)
}
