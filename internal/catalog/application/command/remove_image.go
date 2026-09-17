package command

import (
	"context"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

type RemoveImage struct {
	Actor     auth.Principal
	ProductID string
	ImageID   string
}

type RemoveImageHandler struct {
	base Base
}

func NewRemoveImageHandler(base Base) *RemoveImageHandler {
	return &RemoveImageHandler{base: base}
}

func (h *RemoveImageHandler) Handle(ctx context.Context, cmd RemoveImage) (struct{}, error) {
	imageID, err := domain.ParseImageID(cmd.ImageID)
	if err != nil {
		return struct{}{}, domain.ErrImageNotFound
	}
	return struct{}{}, h.base.authorAction(ctx, cmd.Actor, cmd.ProductID, false,
		func(p *domain.Product, author kernel.SellerID, _ domain.Classification, now time.Time) error {
			return p.RemoveImage(author, imageID, now)
		})
}
