package command

import (
	"context"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

type UpdateProduct struct {
	Actor       auth.Principal
	ProductID   string
	Title       string
	Description string
	Brand       string
	Attributes  map[string]string
}

type UpdateProductHandler struct {
	base Base
}

func NewUpdateProductHandler(base Base) *UpdateProductHandler {
	return &UpdateProductHandler{base: base}
}

func (h *UpdateProductHandler) Handle(ctx context.Context, cmd UpdateProduct) (struct{}, error) {
	content := domain.ProductContent{Title: cmd.Title, Description: cmd.Description, Brand: cmd.Brand, Attributes: cmd.Attributes}
	return struct{}{}, h.base.authorAction(ctx, cmd.Actor, cmd.ProductID, true,
		func(p *domain.Product, author kernel.SellerID, class domain.Classification, now time.Time) error {
			return p.UpdateContent(author, class, content, now)
		})
}
