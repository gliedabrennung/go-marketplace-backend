package command

import (
	"context"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

type RemoveAttribute struct {
	Actor      auth.Principal
	CategoryID string
	Code       string
}

type RemoveAttributeHandler struct {
	base Base
}

func NewRemoveAttributeHandler(base Base) *RemoveAttributeHandler {
	return &RemoveAttributeHandler{base: base}
}

func (h *RemoveAttributeHandler) Handle(ctx context.Context, cmd RemoveAttribute) (struct{}, error) {
	return struct{}{}, h.base.categoryAction(ctx, cmd.Actor, cmd.CategoryID, "catalog.category.remove_attribute",
		map[string]string{"code": cmd.Code},
		func(_ context.Context, _ application.Repositories, c *domain.Category) error {
			return c.RemoveAttribute(cmd.Code, h.base.clock.Now())
		})
}
