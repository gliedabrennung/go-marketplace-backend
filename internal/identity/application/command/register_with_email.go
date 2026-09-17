package command

import (
	"context"
	"errors"
	"fmt"

	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type RegisterWithEmail struct {
	Email    string
	Password string
}

type RegisterWithEmailResult struct {
	UserID string
}

type RegisterWithEmailHandler struct {
	uow           application.UnitOfWork
	hasher        application.PasswordHasher
	confirmations *EmailConfirmations
	limiter       application.AttemptLimiter
	clock         application.Clock
}

func NewRegisterWithEmailHandler(
	uow application.UnitOfWork,
	hasher application.PasswordHasher,
	confirmations *EmailConfirmations,
	limiter application.AttemptLimiter,
	clock application.Clock,
) *RegisterWithEmailHandler {
	return &RegisterWithEmailHandler{uow: uow, hasher: hasher, confirmations: confirmations, limiter: limiter, clock: clock}
}

func (h *RegisterWithEmailHandler) Handle(ctx context.Context, cmd RegisterWithEmail) (RegisterWithEmailResult, error) {
	email, err := domain.NewEmail(cmd.Email)
	if err != nil {
		return RegisterWithEmailResult{}, err
	}
	password, err := domain.NewPassword(cmd.Password)
	if err != nil {
		return RegisterWithEmailResult{}, err
	}
	if err := h.limiter.Allow(ctx, application.ActionConfirmationRequest, email.String()); err != nil {
		return RegisterWithEmailResult{}, err
	}

	hash, err := h.hasher.Hash(password)
	if err != nil {
		return RegisterWithEmailResult{}, fmt.Errorf("hash password: %w", err)
	}
	now := h.clock.Now()
	user, err := domain.RegisterWithEmail(kernel.NewUserID(), email, hash, now)
	if err != nil {
		return RegisterWithEmailResult{}, err
	}

	err = h.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		_, err := repos.Users().FindByVerifiedEmail(ctx, email)
		switch {
		case err == nil:
			return domain.ErrEmailTaken
		case !errors.Is(err, domain.ErrUserNotFound):
			return err
		}
		return repos.Users().Save(ctx, user)
	})
	if err != nil {
		return RegisterWithEmailResult{}, err
	}

	if err := h.confirmations.Issue(ctx, user, now); err != nil {
		return RegisterWithEmailResult{}, err
	}
	return RegisterWithEmailResult{UserID: user.ID().String()}, nil
}
