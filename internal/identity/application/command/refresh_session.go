package command

import (
	"context"
	"errors"
	"fmt"

	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/domain"
)

type RefreshSession struct {
	RefreshToken string
}

type RefreshSessionHandler struct {
	uow      application.UnitOfWork
	secrets  application.SecretGenerator
	clock    application.Clock
	sessions *SessionStarter
}

func NewRefreshSessionHandler(uow application.UnitOfWork, secrets application.SecretGenerator, clock application.Clock, sessions *SessionStarter) *RefreshSessionHandler {
	return &RefreshSessionHandler{uow: uow, secrets: secrets, clock: clock, sessions: sessions}
}

func (h *RefreshSessionHandler) Handle(ctx context.Context, cmd RefreshSession) (application.AuthTokens, error) {
	if cmd.RefreshToken == "" {
		return application.AuthTokens{}, domain.ErrInvalidRefresh
	}
	next, err := h.secrets.Token()
	if err != nil {
		return application.AuthTokens{}, fmt.Errorf("generate refresh token: %w", err)
	}
	now := h.clock.Now()

	var (
		user    *domain.User
		session *domain.Session
		ruleErr error
	)
	err = h.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		found, err := repos.Sessions().FindByRefreshDigest(ctx, domain.DigestRefreshToken(cmd.RefreshToken))
		if errors.Is(err, domain.ErrSessionNotFound) {
			ruleErr = domain.ErrInvalidRefresh
			return nil
		}
		if err != nil {
			return err
		}
		owner, err := repos.Users().FindByID(ctx, found.UserID())
		if err != nil {
			return err
		}
		if signInErr := owner.EnsureCanSignIn(); signInErr != nil {
			found.Revoke(domain.RevokedByBlock, now)
			ruleErr = signInErr
		} else {
			ruleErr = found.Rotate(cmd.RefreshToken, next, h.sessions.RefreshTTL(), now)
		}
		user, session = owner, found
		return repos.Sessions().Save(ctx, found)
	})
	if err != nil {
		return application.AuthTokens{}, err
	}
	if ruleErr != nil {
		return application.AuthTokens{}, ruleErr
	}
	return h.sessions.Issue(user, session, next, now)
}
