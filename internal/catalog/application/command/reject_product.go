package command

import (
	"context"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

type RejectProduct struct {
	Actor     auth.Principal
	ProductID string
	Reason    string
}

type RejectProductHandler struct {
	base Base
}

func NewRejectProductHandler(base Base) *RejectProductHandler {
	return &RejectProductHandler{base: base}
}

func (h *RejectProductHandler) Handle(ctx context.Context, cmd RejectProduct) (struct{}, error) {
	return struct{}{}, h.base.moderationAction(ctx, cmd.Actor, cmd.ProductID, "catalog.product.reject",
		map[string]string{"reason": cmd.Reason},
		func(p *domain.Product, moderator kernel.UserID, _ domain.Classification, now time.Time) error {
			return p.Reject(moderator, cmd.Reason, now)
		})
}
