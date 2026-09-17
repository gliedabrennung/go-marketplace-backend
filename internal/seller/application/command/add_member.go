package command

import (
	"context"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

type AddMember struct {
	Actor    auth.Principal
	SellerID string
	UserID   string
	Role     string
}

type AddMemberHandler struct {
	unit Unit
}

func NewAddMemberHandler(unit Unit) *AddMemberHandler {
	return &AddMemberHandler{unit: unit}
}

func (h *AddMemberHandler) Handle(ctx context.Context, cmd AddMember) (struct{}, error) {
	member, err := kernel.ParseUserID(cmd.UserID)
	if err != nil {
		return struct{}{}, err
	}
	role, err := domain.ParseMemberRole(cmd.Role)
	if err != nil {
		return struct{}{}, err
	}
	return struct{}{}, h.unit.memberAction(ctx, cmd.Actor, cmd.SellerID, func(s *domain.Seller, actor kernel.UserID, now time.Time) error {
		return s.AddMember(actor, member, role, now)
	})
}
