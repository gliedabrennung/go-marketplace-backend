package command

import (
	"context"
	"errors"
	"fmt"

	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/domain"
)

type SignInWithEmail struct {
	Email      string
	Password   string
	DeviceName string
	UserAgent  string
	IP         string
}

type SignInWithEmailHandler struct {
	uow      application.UnitOfWork
	hasher   application.PasswordHasher
	limiter  application.AttemptLimiter
	clock    application.Clock
	sessions *SessionStarter
}

func NewSignInWithEmailHandler(
	uow application.UnitOfWork,
	hasher application.PasswordHasher,
	limiter application.AttemptLimiter,
	clock application.Clock,
	sessions *SessionStarter,
) *SignInWithEmailHandler {
	return &SignInWithEmailHandler{uow: uow, hasher: hasher, limiter: limiter, clock: clock, sessions: sessions}
}

func (h *SignInWithEmailHandler) Handle(ctx context.Context, cmd SignInWithEmail) (application.AuthTokens, error) {
	email, err := domain.NewEmail(cmd.Email)
	if err != nil {
		return application.AuthTokens{}, domain.ErrInvalidCredentials
	}
	if err := h.limiter.Allow(ctx, application.ActionSignIn, email.String()); err != nil {
		return application.AuthTokens{}, err
	}

	var user *domain.User
	err = h.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		found, err := repos.Users().FindByVerifiedEmail(ctx, email)
		if errors.Is(err, domain.ErrUserNotFound) {
			return nil
		}
		user = found
		return err
	})
	if err != nil {
		return application.AuthTokens{}, err
	}

	var hash domain.PasswordHash
	if user != nil {
		hash = user.PasswordHash()
	}
	ok, err := h.hasher.Verify(hash, cmd.Password)
	if err != nil {
		return application.AuthTokens{}, fmt.Errorf("verify password: %w", err)
	}
	if user == nil || !ok {
		return application.AuthTokens{}, domain.ErrInvalidCredentials
	}
	if err := user.EnsureCanSignIn(); err != nil {
		return application.AuthTokens{}, err
	}

	now := h.clock.Now()
	return h.sessions.Start(ctx, user, domain.NewDevice(cmd.DeviceName, cmd.UserAgent, cmd.IP), now)
}
