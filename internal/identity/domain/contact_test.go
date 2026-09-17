package domain_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/domain"
)

func TestNewEmail(t *testing.T) {
	e, err := domain.NewEmail("  Buyer.One@Example.KZ ")
	require.NoError(t, err)
	assert.Equal(t, "buyer.one@example.kz", e.String())
	assert.False(t, e.IsZero())

	invalid := []string{
		"",
		"plain",
		"a@localhost",
		"a@example.",
		"Name <a@example.com>",
		strings.Repeat("a", 65) + "@example.com",
		"a@" + strings.Repeat("b", 250) + ".com",
	}
	for _, raw := range invalid {
		_, err := domain.NewEmail(raw)
		require.ErrorIs(t, err, domain.ErrInvalidEmail, raw)
	}
}

func TestNewPassword(t *testing.T) {
	p, err := domain.NewPassword("correct horse 1")
	require.NoError(t, err)
	assert.Equal(t, "correct horse 1", p.Reveal())
	assert.Equal(t, "***", p.String())

	for _, raw := range []string{"short1", "onlyletters", "1234567890", strings.Repeat("a1", 65)} {
		_, err := domain.NewPassword(raw)
		require.ErrorIs(t, err, domain.ErrWeakPassword, raw)
	}
}

func TestNewPasswordHash(t *testing.T) {
	_, err := domain.NewPasswordHash("")
	require.ErrorIs(t, err, domain.ErrInvalidPasswordHash)
	h, err := domain.NewPasswordHash("$argon2id$v=19$...")
	require.NoError(t, err)
	assert.False(t, h.IsZero())
	assert.Equal(t, "$argon2id$v=19$...", h.String())
}

func TestParseRole(t *testing.T) {
	for _, r := range []string{"buyer", "content_moderator", "support_agent", "platform_admin"} {
		role, err := domain.ParseRole(r)
		require.NoError(t, err)
		assert.Equal(t, r, role.String())
	}
	_, err := domain.ParseRole("seller_admin")
	require.ErrorIs(t, err, domain.ErrInvalidRole)
}
