package command

import (
	"context"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
	identity "github.com/gliedabrennung/go-marketplace-backend/internal/identity/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

type CreateCategory struct {
	Actor    auth.Principal
	ParentID string
	Name     string
	Slug     string
}

type CreateCategoryResult struct {
	CategoryID string
}

type CreateCategoryHandler struct {
	base Base
}

func NewCreateCategoryHandler(base Base) *CreateCategoryHandler {
	return &CreateCategoryHandler{base: base}
}

func (h *CreateCategoryHandler) Handle(ctx context.Context, cmd CreateCategory) (CreateCategoryResult, error) {
	if _, err := h.base.requireStaff(cmd.Actor, identity.PermCategoriesManage); err != nil {
		return CreateCategoryResult{}, err
	}
	now := h.base.clock.Now()
	var created *domain.Category
	err := h.base.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		var parent *domain.Category
		if cmd.ParentID != "" {
			parentID, err := parseCategoryID(cmd.ParentID)
			if err != nil {
				return err
			}
			if parent, err = repos.Categories().FindByID(ctx, parentID); err != nil {
				return err
			}
		}
		category, err := domain.CreateCategory(domain.NewCategoryID(), cmd.Name, cmd.Slug, parent, now)
		if err != nil {
			return err
		}
		if err := repos.Categories().Save(ctx, category); err != nil {
			return err
		}
		created = category
		return repos.Audit().Record(ctx, audit(cmd.Actor, "catalog.category.create", "category", category.ID().String(),
			map[string]string{"name": category.Name(), "slug": category.Slug()}, now))
	})
	if err != nil {
		return CreateCategoryResult{}, err
	}
	return CreateCategoryResult{CategoryID: created.ID().String()}, nil
}
