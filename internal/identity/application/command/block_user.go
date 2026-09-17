package command

import (
	"context"

	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

type BlockUser struct {
	Actor  auth.Principal
	UserID string
	Reason string
}

type BlockUserHandler struct {
	uow   application.UnitOfWork
	clock application.Clock
}

func NewBlockUserHandler(uow application.UnitOfWork, clock application.Clock) *BlockUserHandler {
	return &BlockUserHandler{uow: uow, clock: clock}
}

func (h *BlockUserHandler) Handle(ctx context.Context, cmd BlockUser) (struct{}, error) {
	if err := api.Authorize(cmd.Actor, api.PermUsersBlock); err != nil {
		return struct{}{}, err
	}
	actor, err := actorID(cmd.Actor)
	if err != nil {
		return struct{}{}, err
	}
	target, err := kernel.ParseUserID(cmd.UserID)
	if err != nil {
		return struct{}{}, domain.ErrUserNotFound
	}
	now := h.clock.Now()

	err = h.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		user, err := repos.Users().FindByID(ctx, target)
		if err != nil {
			return err
		}
		if err := user.Block(cmd.Reason, actor, now); err != nil {
			return err
		}
		if err := repos.Users().Save(ctx, user); err != nil {
			return err
		}
		return repos.Audit().Record(ctx, audit(cmd.Actor, "identity.user.block", target.String(), map[string]string{"reason": user.BlockReason()}, now))
	})
	return struct{}{}, err
}
