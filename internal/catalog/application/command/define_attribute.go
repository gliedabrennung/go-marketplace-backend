package command

import (
	"context"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

type DefineAttribute struct {
	Actor      auth.Principal
	CategoryID string
	Code       string
	Name       string
	Type       string
	Required   bool
	Filterable bool
	Options    []string
	Unit       string
}

type DefineAttributeHandler struct {
	base Base
}

func NewDefineAttributeHandler(base Base) *DefineAttributeHandler {
	return &DefineAttributeHandler{base: base}
}

func (h *DefineAttributeHandler) Handle(ctx context.Context, cmd DefineAttribute) (struct{}, error) {
	def, err := domain.NewAttributeDefinition(domain.AttributeSpec{
		Code: cmd.Code, Name: cmd.Name, Type: cmd.Type, Required: cmd.Required,
		Filterable: cmd.Filterable, Options: cmd.Options, Unit: cmd.Unit,
	})
	if err != nil {
		return struct{}{}, err
	}
	return struct{}{}, h.base.categoryAction(ctx, cmd.Actor, cmd.CategoryID, "catalog.category.define_attribute",
		map[string]string{"code": def.Code(), "type": string(def.Type())},
		func(ctx context.Context, repos application.Repositories, c *domain.Category) error {
			inherited := domain.Schema{}
			if !c.ParentID().IsZero() {
				ancestors, err := classification(ctx, repos, c.ParentID())
				if err != nil {
					return err
				}
				inherited = ancestors.Schema()
			}
			descendants, err := repos.Categories().DescendantAttributeCodes(ctx, c.ID())
			if err != nil {
				return err
			}
			return c.DefineAttribute(def, inherited, descendants, h.base.clock.Now())
		})
}
