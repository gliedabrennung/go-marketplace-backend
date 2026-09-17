package command

import (
	"context"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

type AttachDocument struct {
	Actor     auth.Principal
	SellerID  string
	Kind      string
	ObjectKey string
}

type AttachDocumentHandler struct {
	unit Unit
}

func NewAttachDocumentHandler(unit Unit) *AttachDocumentHandler {
	return &AttachDocumentHandler{unit: unit}
}

func (h *AttachDocumentHandler) Handle(ctx context.Context, cmd AttachDocument) (struct{}, error) {
	now := h.unit.clock.Now()
	document, err := domain.NewDocument(cmd.Kind, cmd.ObjectKey, now)
	if err != nil {
		return struct{}{}, err
	}
	return struct{}{}, h.unit.memberAction(ctx, cmd.Actor, cmd.SellerID, func(s *domain.Seller, actor kernel.UserID, now time.Time) error {
		return s.AttachDocument(actor, document, now)
	})
}
