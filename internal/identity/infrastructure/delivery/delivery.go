package delivery

import (
	"context"
	"log/slog"

	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

var ErrDeliveryUnavailable = kernel.NewError(kernel.KindInternal, "IDENTITY_DELIVERY_UNAVAILABLE", "email delivery provider is not configured")

type LogSender struct {
	log           *slog.Logger
	revealSecrets bool
}

func NewLogSender(log *slog.Logger, revealSecrets bool) *LogSender {
	return &LogSender{log: log, revealSecrets: revealSecrets}
}

func (s *LogSender) SendEmailConfirmation(ctx context.Context, email domain.Email, token string) error {
	if !s.revealSecrets {
		s.log.ErrorContext(ctx, "email confirmation delivery is not configured", "email", email.String())
		return ErrDeliveryUnavailable
	}
	s.log.WarnContext(ctx, "development delivery of email confirmation", "email", email.String(), "dev_token", token)
	return nil
}
