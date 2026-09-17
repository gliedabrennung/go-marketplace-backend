package command

import (
	"context"

	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

type UnblockUser struct {
	Actor  auth.Principal
	UserID string
}

type UnblockUserHandler struct {
	uow   application.UnitOfWork
	clock application.Clock
}

func NewUnblockUserHandler(uow application.UnitOfWork, clock application.Clock) *UnblockUserHandler {
	return &UnblockUserHandler{uow: uow, clock: clock}
}

func (h *UnblockUserHandler) Handle(ctx context.Context, cmd UnblockUser) (struct{}, error) {
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
		if err := user.Unblock(actor, now); err != nil {
			return err
		}
		if err := repos.Users().Save(ctx, user); err != nil {
			return err
		}
		return repos.Audit().Record(ctx, audit(cmd.Actor, "identity.user.unblock", target.String(), nil, now))
	})
	return struct{}{}, err
}
