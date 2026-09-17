package command

import (
	"context"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

type ReorderImages struct {
	Actor     auth.Principal
	ProductID string
	ImageIDs  []string
}

type ReorderImagesHandler struct {
	base Base
}

func NewReorderImagesHandler(base Base) *ReorderImagesHandler {
	return &ReorderImagesHandler{base: base}
}

func (h *ReorderImagesHandler) Handle(ctx context.Context, cmd ReorderImages) (struct{}, error) {
	order := make([]domain.ImageID, 0, len(cmd.ImageIDs))
	for _, raw := range cmd.ImageIDs {
		id, err := domain.ParseImageID(raw)
		if err != nil {
			return struct{}{}, domain.ErrInvalidImageOrder
		}
		order = append(order, id)
	}
	return struct{}{}, h.base.authorAction(ctx, cmd.Actor, cmd.ProductID, false,
		func(p *domain.Product, author kernel.SellerID, _ domain.Classification, now time.Time) error {
			return p.ReorderImages(author, order, now)
		})
}
