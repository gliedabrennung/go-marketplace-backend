package command

import (
	"context"

	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

type GrantRole struct {
	Actor  auth.Principal
	UserID string
	Role   string
}

type RevokeRole struct {
	Actor  auth.Principal
	UserID string
	Role   string
}

type GrantRoleHandler struct {
	uow   application.UnitOfWork
	clock application.Clock
}

func NewGrantRoleHandler(uow application.UnitOfWork, clock application.Clock) *GrantRoleHandler {
	return &GrantRoleHandler{uow: uow, clock: clock}
}

func (h *GrantRoleHandler) Handle(ctx context.Context, cmd GrantRole) (struct{}, error) {
	return struct{}{}, changeRole(ctx, h.uow, h.clock, cmd.Actor, cmd.UserID, cmd.Role, "identity.user.grant_role",
		func(u *domain.User, role domain.Role, by kernel.UserID, c application.Clock) error {
			return u.GrantRole(role, by, c.Now())
		})
}

type RevokeRoleHandler struct {
	uow   application.UnitOfWork
	clock application.Clock
}

func NewRevokeRoleHandler(uow application.UnitOfWork, clock application.Clock) *RevokeRoleHandler {
	return &RevokeRoleHandler{uow: uow, clock: clock}
}

func (h *RevokeRoleHandler) Handle(ctx context.Context, cmd RevokeRole) (struct{}, error) {
	return struct{}{}, changeRole(ctx, h.uow, h.clock, cmd.Actor, cmd.UserID, cmd.Role, "identity.user.revoke_role",
		func(u *domain.User, role domain.Role, by kernel.UserID, c application.Clock) error {
			return u.RevokeRole(role, by, c.Now())
		})
}

type roleChange func(u *domain.User, role domain.Role, by kernel.UserID, c application.Clock) error

func changeRole(ctx context.Context, uow application.UnitOfWork, clock application.Clock, actorPrincipal auth.Principal, userID, roleName, action string, apply roleChange) error {
	if err := api.Authorize(actorPrincipal, api.PermUsersManageRoles); err != nil {
		return err
	}
	actor, err := actorID(actorPrincipal)
	if err != nil {
		return err
	}
	role, err := domain.ParseRole(roleName)
	if err != nil {
		return err
	}
	target, err := kernel.ParseUserID(userID)
	if err != nil {
		return domain.ErrUserNotFound
	}

	return uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		user, err := repos.Users().FindByID(ctx, target)
		if err != nil {
			return err
		}
		if err := apply(user, role, actor, clock); err != nil {
			return err
		}
		if err := repos.Users().Save(ctx, user); err != nil {
			return err
		}
		return repos.Audit().Record(ctx, audit(actorPrincipal, action, target.String(), map[string]string{"role": role.String()}, clock.Now()))
	})
}
