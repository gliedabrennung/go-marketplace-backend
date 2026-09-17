package command

import (
	"context"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

type RenameCategory struct {
	Actor      auth.Principal
	CategoryID string
	Name       string
	Slug       string
}

type RenameCategoryHandler struct {
	base Base
}

func NewRenameCategoryHandler(base Base) *RenameCategoryHandler {
	return &RenameCategoryHandler{base: base}
}

func (h *RenameCategoryHandler) Handle(ctx context.Context, cmd RenameCategory) (struct{}, error) {
	return struct{}{}, h.base.categoryAction(ctx, cmd.Actor, cmd.CategoryID, "catalog.category.rename",
		map[string]string{"name": cmd.Name, "slug": cmd.Slug},
		func(_ context.Context, _ application.Repositories, c *domain.Category) error {
			return c.Rename(cmd.Name, cmd.Slug, h.base.clock.Now())
		})
}
