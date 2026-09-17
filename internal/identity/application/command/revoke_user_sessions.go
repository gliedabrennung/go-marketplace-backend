package command

import (
	"context"
	"errors"

	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/domain"
)

type RevokeUserSessions struct {
	UserID string
}

type RevokeUserSessionsResult struct {
	Revoked int
}

type activeSessionLister interface {
	ActiveSessionIDs(ctx context.Context, userID string) ([]string, error)
}

type RevokeUserSessionsHandler struct {
	uow      application.UnitOfWork
	sessions activeSessionLister
	clock    application.Clock
}

func NewRevokeUserSessionsHandler(uow application.UnitOfWork, sessions activeSessionLister, clock application.Clock) *RevokeUserSessionsHandler {
	return &RevokeUserSessionsHandler{uow: uow, sessions: sessions, clock: clock}
}

func (h *RevokeUserSessionsHandler) Handle(ctx context.Context, cmd RevokeUserSessions) (RevokeUserSessionsResult, error) {
	ids, err := h.sessions.ActiveSessionIDs(ctx, cmd.UserID)
	if err != nil {
		return RevokeUserSessionsResult{}, err
	}
	now := h.clock.Now()
	revoked := 0
	for _, raw := range ids {
		id, err := domain.ParseSessionID(raw)
		if err != nil {
			return RevokeUserSessionsResult{Revoked: revoked}, err
		}
		err = h.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
			session, err := repos.Sessions().FindByID(ctx, id)
			if err != nil {
				return err
			}
			session.Revoke(domain.RevokedByBlock, now)
			return repos.Sessions().Save(ctx, session)
		})
		if errors.Is(err, domain.ErrSessionNotFound) {
			continue
		}
		if err != nil {
			return RevokeUserSessionsResult{Revoked: revoked}, err
		}
		revoked++
	}
	return RevokeUserSessionsResult{Revoked: revoked}, nil
}
