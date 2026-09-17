package command_test

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application/command"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

func picture(width, height int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for x := range width {
		for y := range height {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 128, A: 255})
		}
	}
	return img
}

func pngBytes(t *testing.T, width, height int) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, picture(width, height)))
	return buf.Bytes()
}

func jpegBytes(t *testing.T, width, height int) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, jpeg.Encode(&buf, picture(width, height), nil))
	return buf.Bytes()
}

func originalKey(t *testing.T, productID, imageID string) string {
	t.Helper()
	return domain.ImageObjectKey(domain.ProductID(mustParse(t, domain.ParseProductID, productID)), mustParse(t, domain.ParseImageID, imageID), domain.ImageVariantOriginal)
}

func mustParse[T any](t *testing.T, parse func(string) (T, error), raw string) T {
	t.Helper()
	v, err := parse(raw)
	require.NoError(t, err)
	return v
}

func (e *env) upload(t *testing.T, owner auth.Principal, productID, contentType string, data []byte) string {
	t.Helper()
	res, err := e.requestImage.Handle(ctx, command.RequestImageUpload{Actor: owner, ProductID: productID, ContentType: contentType, Size: int64(len(data))})
	require.NoError(t, err)
	require.NoError(t, e.objects.Put(ctx, originalKey(t, productID, res.ImageID), contentType, data))
	return res.ImageID
}

func TestImages_UploadProcessAndView(t *testing.T) {
	e := newEnv(t)
	tr := e.tree(t)
	owner, seller := e.seller()
	id := e.draft(t, owner, seller, tr.phones, phone("black"))
	data := pngBytes(t, 1600, 800)

	res, err := e.requestImage.Handle(ctx, command.RequestImageUpload{Actor: owner, ProductID: id, ContentType: "image/png", Size: int64(len(data))})
	require.NoError(t, err)
	assert.Equal(t, "PUT", res.Upload.Method)
	assert.Contains(t, res.Upload.URL, res.ImageID)
	assert.Equal(t, "image/png", res.Upload.Headers["Content-Type"])

	_, err = e.confirmImage.Handle(ctx, command.ConfirmImageUpload{Actor: owner, ProductID: id, ImageID: res.ImageID})
	require.ErrorIs(t, err, application.ErrUploadMissing)
	require.NoError(t, e.objects.Put(ctx, originalKey(t, id, res.ImageID), "image/png", data))
	_, err = e.confirmImage.Handle(ctx, command.ConfirmImageUpload{Actor: owner, ProductID: id, ImageID: res.ImageID})
	require.NoError(t, err)

	_, err = e.processImage.Handle(ctx, command.ProcessImage{ProductID: id, ImageID: res.ImageID})
	require.NoError(t, err)
	_, err = e.processImage.Handle(ctx, command.ProcessImage{ProductID: id, ImageID: res.ImageID})
	require.NoError(t, err)

	pid := mustParse(t, domain.ParseProductID, id)
	iid := mustParse(t, domain.ParseImageID, res.ImageID)
	for variant, width := range map[string]int{domain.ImageVariantSmall: 256, domain.ImageVariantLarge: 1024} {
		thumb, ok := e.objects.Get(domain.ImageObjectKey(pid, iid, variant))
		require.True(t, ok, variant)
		cfg, err := jpeg.DecodeConfig(bytes.NewReader(thumb))
		require.NoError(t, err)
		assert.Equal(t, width, cfg.Width)
		assert.Equal(t, width/2, cfg.Height)
	}

	view, err := e.getProduct.Handle(ctx, query.GetProduct{Actor: owner, ProductID: id})
	require.NoError(t, err)
	require.Len(t, view.Images, 1)
	img := view.Images[0]
	assert.Equal(t, "processed", img.Status)
	assert.Equal(t, 1600, img.Width)
	assert.NotEmpty(t, img.OriginalURL)
	assert.NotEmpty(t, img.SmallURL)
	assert.NotEmpty(t, img.LargeURL)

	_, err = e.submitProduct.Handle(ctx, command.SubmitProduct{Actor: owner, ProductID: id})
	require.NoError(t, err)
	_, err = e.publishProduct.Handle(ctx, command.PublishProduct{Actor: user("content_moderator"), ProductID: id})
	require.NoError(t, err)
	public, err := e.getProduct.Handle(ctx, query.GetProduct{ProductID: id})
	require.NoError(t, err)
	require.Len(t, public.Images, 1)
	assert.Empty(t, public.Images[0].OriginalURL)
	assert.NotEmpty(t, public.Images[0].SmallURL)
}

func TestImages_Rejections(t *testing.T) {
	e := newEnv(t)
	tr := e.tree(t)
	owner, seller := e.seller()
	id := e.draft(t, owner, seller, tr.phones, phone("black"))

	_, err := e.requestImage.Handle(ctx, command.RequestImageUpload{Actor: owner, ProductID: id, ContentType: "image/gif", Size: 10})
	require.ErrorIs(t, err, domain.ErrUnsupportedImageType)
	_, err = e.requestImage.Handle(ctx, command.RequestImageUpload{Actor: user(), ProductID: id, ContentType: "image/png", Size: 10})
	require.ErrorIs(t, err, domain.ErrProductNotFound)
	_, err = e.requestImage.Handle(ctx, command.RequestImageUpload{Actor: owner, ProductID: "bad", ContentType: "image/png", Size: 10})
	require.ErrorIs(t, err, domain.ErrProductNotFound)

	garbage := e.upload(t, owner, id, "image/png", []byte("definitely not an image at all"))
	_, err = e.confirmImage.Handle(ctx, command.ConfirmImageUpload{Actor: owner, ProductID: id, ImageID: garbage})
	require.ErrorIs(t, err, application.ErrInvalidImage)

	mismatch := e.upload(t, owner, id, "image/png", jpegBytes(t, 40, 40))
	_, err = e.confirmImage.Handle(ctx, command.ConfirmImageUpload{Actor: owner, ProductID: id, ImageID: mismatch})
	require.ErrorIs(t, err, domain.ErrImageMismatch)

	_, err = e.confirmImage.Handle(ctx, command.ConfirmImageUpload{Actor: owner, ProductID: id, ImageID: "bad"})
	require.ErrorIs(t, err, domain.ErrImageNotFound)
	_, err = e.confirmImage.Handle(ctx, command.ConfirmImageUpload{Actor: owner, ProductID: "bad", ImageID: garbage})
	require.ErrorIs(t, err, domain.ErrProductNotFound)
	_, err = e.confirmImage.Handle(ctx, command.ConfirmImageUpload{Actor: user(), ProductID: id, ImageID: garbage})
	require.ErrorIs(t, err, domain.ErrProductNotFound)

	_, err = e.processImage.Handle(ctx, command.ProcessImage{ProductID: id, ImageID: garbage})
	require.NoError(t, err)
	_, err = e.processImage.Handle(ctx, command.ProcessImage{ProductID: id, ImageID: domain.NewImageID().String()})
	require.NoError(t, err)
	_, err = e.processImage.Handle(ctx, command.ProcessImage{ProductID: id, ImageID: "bad"})
	require.ErrorIs(t, err, domain.ErrImageNotFound)
	_, err = e.processImage.Handle(ctx, command.ProcessImage{ProductID: "bad", ImageID: garbage})
	require.ErrorIs(t, err, domain.ErrProductNotFound)

	_, err = e.reorderImages.Handle(ctx, command.ReorderImages{Actor: owner, ProductID: id, ImageIDs: []string{mismatch, garbage}})
	require.NoError(t, err)
	view, err := e.getProduct.Handle(ctx, query.GetProduct{Actor: owner, ProductID: id})
	require.NoError(t, err)
	require.Len(t, view.Images, 2)
	assert.Equal(t, mismatch, view.Images[0].ID)
	assert.Empty(t, view.Images[0].OriginalURL)

	_, err = e.reorderImages.Handle(ctx, command.ReorderImages{Actor: owner, ProductID: id, ImageIDs: []string{mismatch, "bad"}})
	require.ErrorIs(t, err, domain.ErrInvalidImageOrder)
	_, err = e.reorderImages.Handle(ctx, command.ReorderImages{Actor: owner, ProductID: id, ImageIDs: []string{mismatch}})
	require.ErrorIs(t, err, domain.ErrInvalidImageOrder)

	_, err = e.removeImage.Handle(ctx, command.RemoveImage{Actor: owner, ProductID: id, ImageID: garbage})
	require.NoError(t, err)
	_, err = e.removeImage.Handle(ctx, command.RemoveImage{Actor: owner, ProductID: id, ImageID: garbage})
	require.ErrorIs(t, err, domain.ErrImageNotFound)
	_, err = e.removeImage.Handle(ctx, command.RemoveImage{Actor: owner, ProductID: id, ImageID: "bad"})
	require.ErrorIs(t, err, domain.ErrImageNotFound)
}
