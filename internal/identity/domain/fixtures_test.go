package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

var now = time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)

func email(t *testing.T) domain.Email {
	t.Helper()
	e, err := domain.NewEmail("buyer@example.kz")
	require.NoError(t, err)
	return e
}

func hash(t *testing.T) domain.PasswordHash {
	t.Helper()
	h, err := domain.NewPasswordHash("encoded-hash")
	require.NoError(t, err)
	return h
}

func pendingUser(t *testing.T) *domain.User {
	t.Helper()
	u, err := domain.RegisterWithEmail(kernel.NewUserID(), email(t), hash(t), now)
	require.NoError(t, err)
	u.PullEvents()
	return u
}

func activeUser(t *testing.T) *domain.User {
	t.Helper()
	u := pendingUser(t)
	require.NoError(t, u.ConfirmEmail(now))
	u.PullEvents()
	return u
}

func eventNames(events []kernel.DomainEvent) []string {
	out := make([]string, len(events))
	for i, e := range events {
		out[i] = e.EventName()
	}
	return out
}
