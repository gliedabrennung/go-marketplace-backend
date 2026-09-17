package command

import (
	"context"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

const imageProbeBytes = 512 << 10

type ConfirmImageUpload struct {
	Actor     auth.Principal
	ProductID string
	ImageID   string
}

type ConfirmImageUploadHandler struct {
	base    Base
	storage application.ObjectStorage
	prober  application.ImageProber
}

func NewConfirmImageUploadHandler(base Base, storage application.ObjectStorage, prober application.ImageProber) *ConfirmImageUploadHandler {
	return &ConfirmImageUploadHandler{base: base, storage: storage, prober: prober}
}

func (h *ConfirmImageUploadHandler) Handle(ctx context.Context, cmd ConfirmImageUpload) (struct{}, error) {
	productID, err := parseProductID(cmd.ProductID)
	if err != nil {
		return struct{}{}, err
	}
	imageID, err := domain.ParseImageID(cmd.ImageID)
	if err != nil {
		return struct{}{}, domain.ErrImageNotFound
	}
	author, err := h.base.productAuthor(ctx, cmd.Actor, productID)
	if err != nil {
		return struct{}{}, err
	}

	key := domain.ImageObjectKey(productID, imageID, domain.ImageVariantOriginal)
	info, err := h.storage.Head(ctx, key)
	if err != nil {
		return struct{}{}, err
	}
	header, err := h.storage.ReadPrefix(ctx, key, imageProbeBytes)
	if err != nil {
		return struct{}{}, err
	}
	image, err := h.prober.Probe(header)
	if err != nil {
		return struct{}{}, application.ErrInvalidImage
	}
	probe := domain.ImageProbe{ContentType: image.ContentType, Size: info.Size, Width: image.Width, Height: image.Height}

	return struct{}{}, h.base.withProduct(ctx, productID, author, false,
		func(p *domain.Product, author kernel.SellerID, _ domain.Classification, now time.Time) error {
			return p.ConfirmImageUpload(author, imageID, probe, now)
		})
}
