package query

import (
	"context"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
)

type ListCategories struct{}

type ListCategoriesHandler struct {
	reader ReadModel
}

func NewListCategoriesHandler(reader ReadModel) *ListCategoriesHandler {
	return &ListCategoriesHandler{reader: reader}
}

func (h *ListCategoriesHandler) Handle(ctx context.Context, _ ListCategories) ([]CategoryNode, error) {
	return h.reader.CategoryTree(ctx)
}

type GetCategory struct {
	CategoryID string
}

type GetCategoryHandler struct {
	reader ReadModel
}

func NewGetCategoryHandler(reader ReadModel) *GetCategoryHandler {
	return &GetCategoryHandler{reader: reader}
}

func (h *GetCategoryHandler) Handle(ctx context.Context, q GetCategory) (CategoryView, error) {
	if _, err := domain.ParseCategoryID(q.CategoryID); err != nil {
		return CategoryView{}, domain.ErrCategoryNotFound
	}
	return h.reader.Category(ctx, q.CategoryID)
}
