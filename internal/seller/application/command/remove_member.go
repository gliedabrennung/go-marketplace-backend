package command

import (
	"context"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

type RemoveMember struct {
	Actor    auth.Principal
	SellerID string
	UserID   string
}

type RemoveMemberHandler struct {
	unit Unit
}

func NewRemoveMemberHandler(unit Unit) *RemoveMemberHandler {
	return &RemoveMemberHandler{unit: unit}
}

func (h *RemoveMemberHandler) Handle(ctx context.Context, cmd RemoveMember) (struct{}, error) {
	member, err := kernel.ParseUserID(cmd.UserID)
	if err != nil {
		return struct{}{}, domain.ErrMemberNotFound
	}
	return struct{}{}, h.unit.memberAction(ctx, cmd.Actor, cmd.SellerID, func(s *domain.Seller, actor kernel.UserID, now time.Time) error {
		return s.RemoveMember(actor, member, now)
	})
}
