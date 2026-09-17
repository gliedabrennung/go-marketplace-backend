package command

import (
	"context"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

type SubmitProduct struct {
	Actor     auth.Principal
	ProductID string
}

type SubmitProductHandler struct {
	base Base
}

func NewSubmitProductHandler(base Base) *SubmitProductHandler {
	return &SubmitProductHandler{base: base}
}

func (h *SubmitProductHandler) Handle(ctx context.Context, cmd SubmitProduct) (struct{}, error) {
	return struct{}{}, h.base.authorAction(ctx, cmd.Actor, cmd.ProductID, true, (*domain.Product).SubmitForModeration)
}
