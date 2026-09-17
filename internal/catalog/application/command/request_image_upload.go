package command

import (
	"context"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

type RequestImageUpload struct {
	Actor       auth.Principal
	ProductID   string
	ContentType string
	Size        int64
}

type RequestImageUploadResult struct {
	ImageID string
	Upload  application.UploadTarget
}

type RequestImageUploadHandler struct {
	base    Base
	storage application.ObjectStorage
}

func NewRequestImageUploadHandler(base Base, storage application.ObjectStorage) *RequestImageUploadHandler {
	return &RequestImageUploadHandler{base: base, storage: storage}
}

func (h *RequestImageUploadHandler) Handle(ctx context.Context, cmd RequestImageUpload) (RequestImageUploadResult, error) {
	productID, err := parseProductID(cmd.ProductID)
	if err != nil {
		return RequestImageUploadResult{}, err
	}
	author, err := h.base.productAuthor(ctx, cmd.Actor, productID)
	if err != nil {
		return RequestImageUploadResult{}, err
	}
	imageID := domain.NewImageID()
	err = h.base.withProduct(ctx, productID, author, false,
		func(p *domain.Product, author kernel.SellerID, _ domain.Classification, now time.Time) error {
			_, err := p.RequestImageUpload(author, imageID, cmd.ContentType, cmd.Size, now)
			return err
		})
	if err != nil {
		return RequestImageUploadResult{}, err
	}
	target, err := h.storage.PresignUpload(ctx, domain.ImageObjectKey(productID, imageID, domain.ImageVariantOriginal), cmd.ContentType, cmd.Size)
	if err != nil {
		return RequestImageUploadResult{}, err
	}
	return RequestImageUploadResult{ImageID: imageID.String(), Upload: target}, nil
}
