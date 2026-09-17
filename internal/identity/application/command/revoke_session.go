package command

import (
	"context"

	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

type RevokeSession struct {
	Actor     auth.Principal
	SessionID string
}

type RevokeSessionHandler struct {
	uow   application.UnitOfWork
	clock application.Clock
}

func NewRevokeSessionHandler(uow application.UnitOfWork, clock application.Clock) *RevokeSessionHandler {
	return &RevokeSessionHandler{uow: uow, clock: clock}
}

func (h *RevokeSessionHandler) Handle(ctx context.Context, cmd RevokeSession) (struct{}, error) {
	actor, err := actorID(cmd.Actor)
	if err != nil {
		return struct{}{}, err
	}
	id, err := domain.ParseSessionID(cmd.SessionID)
	if err != nil {
		return struct{}{}, domain.ErrSessionNotFound
	}
	now := h.clock.Now()

	err = h.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		session, err := repos.Sessions().FindByID(ctx, id)
		if err != nil {
			return err
		}
		owner := session.BelongsTo(actor)
		if !owner && !api.Can(cmd.Actor, api.PermSessionsRevokeAny) {
			return domain.ErrSessionNotFound
		}

		reason := domain.RevokedByOwner
		switch {
		case !owner:
			reason = domain.RevokedByAdmin
		case cmd.SessionID == cmd.Actor.SessionID:
			reason = domain.RevokedBySignOut
		}
		session.Revoke(reason, now)
		if err := repos.Sessions().Save(ctx, session); err != nil {
			return err
		}
		if owner {
			return nil
		}
		return repos.Audit().Record(ctx, application.AuditEntry{
			ActorID:    cmd.Actor.UserID,
			ActorRoles: cmd.Actor.Roles,
			Action:     "identity.session.revoke",
			ObjectType: "identity.session",
			ObjectID:   session.ID().String(),
			Details:    map[string]string{"user_id": session.UserID().String()},
			OccurredAt: now,
		})
	})
	return struct{}{}, err
}
