package command

import (
	"context"
	"errors"

	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type ConfirmEmail struct {
	Token string
}

type ConfirmEmailHandler struct {
	uow   application.UnitOfWork
	clock application.Clock
}

func NewConfirmEmailHandler(uow application.UnitOfWork, clock application.Clock) *ConfirmEmailHandler {
	return &ConfirmEmailHandler{uow: uow, clock: clock}
}

func (h *ConfirmEmailHandler) Handle(ctx context.Context, cmd ConfirmEmail) (struct{}, error) {
	id, secret, err := decodeConfirmationToken(cmd.Token)
	if err != nil {
		return struct{}{}, err
	}
	now := h.clock.Now()

	var (
		userID    kernel.UserID
		verifyErr error
	)
	err = h.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		challenge, err := repos.Challenges().FindByID(ctx, id)
		if errors.Is(err, domain.ErrChallengeNotFound) {
			verifyErr = domain.ErrInvalidConfirmationToken
			return nil
		}
		if err != nil {
			return err
		}
		if challenge.Purpose() != domain.PurposeEmailConfirmation {
			verifyErr = domain.ErrInvalidConfirmationToken
			return nil
		}
		verifyErr = challenge.Verify(secret, now)
		userID = challenge.UserID()
		return repos.Challenges().Save(ctx, challenge)
	})
	if err != nil {
		return struct{}{}, err
	}
	if verifyErr != nil {
		return struct{}{}, verifyErr
	}

	err = h.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		user, err := repos.Users().FindByID(ctx, userID)
		if err != nil {
			return err
		}
		if err := user.ConfirmEmail(now); err != nil {
			return err
		}
		return repos.Users().Save(ctx, user)
	})
	return struct{}{}, err
}
