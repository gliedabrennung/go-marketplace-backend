package command

import (
	"context"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

type CreateProduct struct {
	Actor       auth.Principal
	SellerID    string
	CategoryID  string
	Title       string
	Description string
	Brand       string
	Attributes  map[string]string
}

type CreateProductResult struct {
	ProductID string
}

type CreateProductHandler struct {
	base Base
}

func NewCreateProductHandler(base Base) *CreateProductHandler {
	return &CreateProductHandler{base: base}
}

func (h *CreateProductHandler) Handle(ctx context.Context, cmd CreateProduct) (CreateProductResult, error) {
	author, err := h.base.requireMember(ctx, cmd.Actor, cmd.SellerID)
	if err != nil {
		return CreateProductResult{}, err
	}
	categoryID, err := parseCategoryID(cmd.CategoryID)
	if err != nil {
		return CreateProductResult{}, err
	}
	content := domain.ProductContent{Title: cmd.Title, Description: cmd.Description, Brand: cmd.Brand, Attributes: cmd.Attributes}
	now := h.base.clock.Now()

	var created *domain.Product
	err = h.base.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		class, err := classification(ctx, repos, categoryID)
		if err != nil {
			return err
		}
		product, err := domain.CreateProduct(domain.NewProductID(), author, class, content, now)
		if err != nil {
			return err
		}
		created = product
		return repos.Products().Save(ctx, product)
	})
	if err != nil {
		return CreateProductResult{}, err
	}
	return CreateProductResult{ProductID: created.ID().String()}, nil
}
