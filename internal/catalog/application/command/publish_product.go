package command

import (
	"context"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

type PublishProduct struct {
	Actor     auth.Principal
	ProductID string
}

type PublishProductHandler struct {
	base Base
}

func NewPublishProductHandler(base Base) *PublishProductHandler {
	return &PublishProductHandler{base: base}
}

func (h *PublishProductHandler) Handle(ctx context.Context, cmd PublishProduct) (struct{}, error) {
	return struct{}{}, h.base.moderationAction(ctx, cmd.Actor, cmd.ProductID, "catalog.product.publish", nil, (*domain.Product).Publish)
}
