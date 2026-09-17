package command

import (
	"context"
	"errors"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
)

const (
	smallThumbnailSide = 256
	largeThumbnailSide = 1024
)

type ProcessImage struct {
	ProductID string
	ImageID   string
}

type ProcessImageHandler struct {
	base     Base
	renderer application.ThumbnailRenderer
}

func NewProcessImageHandler(base Base, renderer application.ThumbnailRenderer) *ProcessImageHandler {
	return &ProcessImageHandler{base: base, renderer: renderer}
}

func (h *ProcessImageHandler) Handle(ctx context.Context, cmd ProcessImage) (struct{}, error) {
	productID, err := parseProductID(cmd.ProductID)
	if err != nil {
		return struct{}{}, err
	}
	imageID, err := domain.ParseImageID(cmd.ImageID)
	if err != nil {
		return struct{}{}, domain.ErrImageNotFound
	}
	pending, err := h.pending(ctx, productID, imageID)
	if err != nil || !pending {
		return struct{}{}, err
	}

	targets := []application.Thumbnail{
		{Key: domain.ImageObjectKey(productID, imageID, domain.ImageVariantSmall), MaxSide: smallThumbnailSide},
		{Key: domain.ImageObjectKey(productID, imageID, domain.ImageVariantLarge), MaxSide: largeThumbnailSide},
	}
	if err := h.renderer.Render(ctx, domain.ImageObjectKey(productID, imageID, domain.ImageVariantOriginal), targets); err != nil {
		return struct{}{}, err
	}

	now := h.base.clock.Now()
	err = h.base.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		product, err := repos.Products().FindByID(ctx, productID)
		if err != nil {
			return err
		}
		if err := product.MarkImageProcessed(imageID, now); err != nil {
			return err
		}
		return repos.Products().Save(ctx, product)
	})
	if errors.Is(err, domain.ErrImageNotFound) {
		return struct{}{}, nil
	}
	return struct{}{}, err
}

func (h *ProcessImageHandler) pending(ctx context.Context, productID domain.ProductID, imageID domain.ImageID) (bool, error) {
	pending := false
	err := h.base.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		product, err := repos.Products().FindByID(ctx, productID)
		if err != nil {
			return err
		}
		image, err := product.Image(imageID)
		if err != nil {
			return err
		}
		pending = image.Status() == domain.ImageUploaded
		return nil
	})
	if errors.Is(err, domain.ErrImageNotFound) {
		return false, nil
	}
	return pending, err
}
