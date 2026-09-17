package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

func TestProduct_ImageUploadFlow(t *testing.T) {
	tr := newTree(t)
	seller := kernel.NewSellerID()
	p := draftProduct(t, tr, seller)
	first, second := domain.NewImageID(), domain.NewImageID()

	img, err := p.RequestImageUpload(seller, first, "image/jpeg", 5000, now)
	require.NoError(t, err)
	assert.Equal(t, domain.ImageAwaitingUpload, img.Status())
	assert.Equal(t, first, img.ID())
	assert.Equal(t, "image/jpeg", img.ContentType())
	assert.Equal(t, int64(5000), img.Size())
	_, err = p.RequestImageUpload(seller, second, "image/webp", 7000, now)
	require.NoError(t, err)
	assert.Empty(t, p.CoverKey())

	require.ErrorIs(t, p.MarkImageProcessed(first, now), domain.ErrImageNotUploaded)
	require.ErrorIs(t, p.ConfirmImageUpload(seller, first, domain.ImageProbe{ContentType: "image/png", Size: 5000, Width: 10, Height: 10}, now), domain.ErrImageMismatch)
	require.ErrorIs(t, p.ConfirmImageUpload(seller, first, domain.ImageProbe{ContentType: "image/jpeg", Size: 4999, Width: 10, Height: 10}, now), domain.ErrImageMismatch)
	require.ErrorIs(t, p.ConfirmImageUpload(seller, first, domain.ImageProbe{ContentType: "image/jpeg", Size: 5000}, now), domain.ErrImageMismatch)
	require.ErrorIs(t, p.ConfirmImageUpload(seller, domain.NewImageID(), domain.ImageProbe{}, now), domain.ErrImageNotFound)

	probe := domain.ImageProbe{ContentType: "image/jpeg", Size: 5000, Width: 1200, Height: 900}
	require.NoError(t, p.ConfirmImageUpload(seller, first, probe, now))
	require.ErrorIs(t, p.ConfirmImageUpload(seller, first, probe, now), domain.ErrImageAlreadyUploaded)
	stored, err := p.Image(first)
	require.NoError(t, err)
	assert.Equal(t, 1200, stored.Width())
	assert.Equal(t, 900, stored.Height())
	assert.Equal(t, domain.ImageUploaded, stored.Status())

	events := p.PullEvents()
	require.Len(t, events, 1)
	uploaded := events[0].(domain.ProductImageUploaded)
	assert.Equal(t, domain.ImageObjectKey(p.ID(), first, domain.ImageVariantOriginal), uploaded.ObjectKey)

	require.NoError(t, p.MarkImageProcessed(first, now))
	require.NoError(t, p.MarkImageProcessed(first, now))
	cover := domain.ImageObjectKey(p.ID(), first, domain.ImageVariantSmall)
	assert.Equal(t, cover, p.CoverKey())
	processed := p.PullEvents()
	require.Len(t, processed, 1)
	assert.Equal(t, cover, processed[0].(domain.ProductImageProcessed).CoverKey)
	assert.False(t, processed[0].(domain.ProductImageProcessed).Published)
	_, err = p.Image(domain.NewImageID())
	require.ErrorIs(t, err, domain.ErrImageNotFound)
}

func TestProduct_ImageRules(t *testing.T) {
	tr := newTree(t)
	seller := kernel.NewSellerID()
	p := draftProduct(t, tr, seller)

	_, err := p.RequestImageUpload(seller, domain.NewImageID(), "image/gif", 10, now)
	require.ErrorIs(t, err, domain.ErrUnsupportedImageType)
	_, err = p.RequestImageUpload(seller, domain.NewImageID(), "image/png", 0, now)
	require.ErrorIs(t, err, domain.ErrInvalidImageSize)
	_, err = p.RequestImageUpload(seller, domain.NewImageID(), "image/png", domain.MaxImageBytes+1, now)
	require.ErrorIs(t, err, domain.ErrInvalidImageSize)
	_, err = p.RequestImageUpload(seller, domain.ImageID{}, "image/png", 10, now)
	require.ErrorIs(t, err, kernel.ErrInvalidID)

	ids := make([]domain.ImageID, 0, domain.MaxImages)
	for range domain.MaxImages {
		id := domain.NewImageID()
		_, err := p.RequestImageUpload(seller, id, "image/png", 10, now)
		require.NoError(t, err)
		ids = append(ids, id)
	}
	_, err = p.RequestImageUpload(seller, domain.NewImageID(), "image/png", 10, now)
	require.ErrorIs(t, err, domain.ErrTooManyImages)

	reversed := make([]domain.ImageID, len(ids))
	for i, id := range ids {
		reversed[len(ids)-1-i] = id
	}
	require.NoError(t, p.ReorderImages(seller, reversed, now))
	assert.Equal(t, reversed[0], p.Images()[0].ID())
	require.ErrorIs(t, p.ReorderImages(seller, reversed[:3], now), domain.ErrInvalidImageOrder)
	duplicated := append([]domain.ImageID{reversed[1]}, reversed[1:]...)
	require.ErrorIs(t, p.ReorderImages(seller, duplicated, now), domain.ErrInvalidImageOrder)

	require.NoError(t, p.RemoveImage(seller, ids[0], now))
	require.ErrorIs(t, p.RemoveImage(seller, ids[0], now), domain.ErrImageNotFound)
	assert.Len(t, p.Images(), domain.MaxImages-1)
}

func TestProduct_ImagesLockedAfterSubmission(t *testing.T) {
	tr := newTree(t)
	seller := kernel.NewSellerID()
	p := draftProduct(t, tr, seller)
	img := domain.NewImageID()
	_, err := p.RequestImageUpload(seller, img, "image/png", 10, now)
	require.NoError(t, err)
	require.NoError(t, p.ConfirmImageUpload(seller, img, domain.ImageProbe{ContentType: "image/png", Size: 10, Width: 1, Height: 1}, now))
	require.NoError(t, p.SubmitForModeration(seller, tr.phonesClass(t), now))
	require.NoError(t, p.Publish(kernel.NewUserID(), tr.phonesClass(t), now))
	p.PullEvents()

	_, err = p.RequestImageUpload(seller, domain.NewImageID(), "image/png", 10, now)
	require.ErrorIs(t, err, domain.ErrProductLocked)
	require.ErrorIs(t, p.RemoveImage(seller, img, now), domain.ErrProductLocked)
	require.ErrorIs(t, p.ReorderImages(seller, []domain.ImageID{img}, now), domain.ErrProductLocked)
	require.ErrorIs(t, p.ConfirmImageUpload(seller, img, domain.ImageProbe{}, now), domain.ErrProductLocked)

	require.NoError(t, p.MarkImageProcessed(img, now), "thumbnails are attached by the system after publication")
	events := p.PullEvents()
	require.Len(t, events, 1)
	assert.True(t, events[0].(domain.ProductImageProcessed).Published)
}
