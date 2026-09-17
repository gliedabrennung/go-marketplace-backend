package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

func confirmation(t *testing.T, secret string) *domain.Challenge {
	t.Helper()
	c, err := domain.IssueEmailConfirmationChallenge(domain.NewChallengeID(), kernel.NewUserID(), email(t), secret, domain.EmailConfirmationPolicy(), now)
	require.NoError(t, err)
	return c
}

func TestIssueEmailConfirmationChallenge(t *testing.T) {
	userID := kernel.NewUserID()
	c, err := domain.IssueEmailConfirmationChallenge(domain.NewChallengeID(), userID, email(t), "token-secret", domain.EmailConfirmationPolicy(), now)
	require.NoError(t, err)

	assert.Equal(t, domain.PurposeEmailConfirmation, c.Purpose())
	assert.Equal(t, "buyer@example.kz", c.Target())
	assert.Equal(t, userID, c.UserID())
	assert.Equal(t, domain.ChallengePending, c.Status())
	assert.Equal(t, now.Add(24*time.Hour), c.ExpiresAt())
	assert.NotContains(t, c.Snapshot().SecretDigest, "token-secret")
}

func TestIssueEmailConfirmationChallenge_Validation(t *testing.T) {
	policy := domain.EmailConfirmationPolicy()
	_, err := domain.IssueEmailConfirmationChallenge(domain.ChallengeID{}, kernel.NewUserID(), email(t), "s", policy, now)
	require.ErrorIs(t, err, kernel.ErrInvalidID)
	_, err = domain.IssueEmailConfirmationChallenge(domain.NewChallengeID(), kernel.UserID{}, email(t), "s", policy, now)
	require.ErrorIs(t, err, kernel.ErrInvalidID)
	_, err = domain.IssueEmailConfirmationChallenge(domain.NewChallengeID(), kernel.NewUserID(), domain.Email{}, "s", policy, now)
	require.ErrorIs(t, err, domain.ErrInvalidEmail)
	_, err = domain.IssueEmailConfirmationChallenge(domain.NewChallengeID(), kernel.NewUserID(), email(t), "", policy, now)
	require.ErrorIs(t, err, domain.ErrInvalidSecret)
	_, err = domain.IssueEmailConfirmationChallenge(domain.NewChallengeID(), kernel.NewUserID(), email(t), "s", domain.ChallengePolicy{}, now)
	require.ErrorIs(t, err, domain.ErrInvalidTTL)
}

func TestChallenge_VerifySucceedsOnce(t *testing.T) {
	c := confirmation(t, "secret")
	require.NoError(t, c.Verify("secret", now.Add(23*time.Hour)))
	assert.Equal(t, domain.ChallengeVerified, c.Status())
	require.ErrorIs(t, c.Verify("secret", now.Add(time.Minute)), domain.ErrChallengeAlreadyUsed)
}

func TestChallenge_WrongSecretIsCountedAndExhausts(t *testing.T) {
	c := confirmation(t, "secret")
	for i := 1; i < domain.EmailConfirmationPolicy().MaxAttempts; i++ {
		require.ErrorIs(t, c.Verify("guess", now), domain.ErrInvalidConfirmationToken)
		assert.Equal(t, i, c.Attempts())
		assert.Equal(t, domain.ChallengePending, c.Status())
	}
	require.ErrorIs(t, c.Verify("guess", now), domain.ErrInvalidConfirmationToken)
	assert.Equal(t, domain.ChallengeExhausted, c.Status())
	require.ErrorIs(t, c.Verify("secret", now), domain.ErrChallengeExhausted)
}

func TestChallenge_ExpiredCannotBeVerified(t *testing.T) {
	c := confirmation(t, "secret")
	require.ErrorIs(t, c.Verify("secret", now.Add(24*time.Hour)), domain.ErrChallengeExpired)
	assert.Zero(t, c.Attempts())
}

func TestChallenge_SnapshotRoundTrip(t *testing.T) {
	c := confirmation(t, "s")
	require.ErrorIs(t, c.Verify("wrong", now), domain.ErrInvalidConfirmationToken)
	c.AdvanceVersion()

	restored, err := domain.RehydrateChallenge(c.Snapshot())
	require.NoError(t, err)
	assert.Equal(t, c.Snapshot(), restored.Snapshot())
	assert.Equal(t, c.ID(), restored.ID())
	assert.Equal(t, 1, restored.Version())
	require.NoError(t, restored.Verify("s", now))

	broken := c.Snapshot()
	broken.ID = "bad"
	_, err = domain.RehydrateChallenge(broken)
	require.Error(t, err)

	broken = c.Snapshot()
	broken.UserID = "bad"
	_, err = domain.RehydrateChallenge(broken)
	require.Error(t, err)

	parsed, err := domain.ParseChallengeID(c.ID().String())
	require.NoError(t, err)
	assert.Equal(t, c.ID(), parsed)
}
