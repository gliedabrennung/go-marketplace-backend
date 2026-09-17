package command_test

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/application/command"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/domain"
)

func TestRegisterWithEmail_SendsConfirmation(t *testing.T) {
	e := newEnv(t)
	res, err := e.registerEmail.Handle(ctx, command.RegisterWithEmail{Email: " Buyer@Example.kz", Password: testPassword})
	require.NoError(t, err)

	assert.NotEmpty(t, res.UserID)
	assert.Contains(t, e.sender.links[testEmail], ".token-")
	profile, err := e.profile.Handle(ctx, profileQuery(res.UserID))
	require.NoError(t, err)
	assert.Equal(t, string(domain.UserStatusPending), profile.Status)
	assert.False(t, profile.EmailVerified)
	assert.Equal(t, testEmail, profile.Email)
}

func TestRegisterWithEmail_Validation(t *testing.T) {
	e := newEnv(t)
	_, err := e.registerEmail.Handle(ctx, command.RegisterWithEmail{Email: "nope", Password: testPassword})
	require.ErrorIs(t, err, domain.ErrInvalidEmail)
	_, err = e.registerEmail.Handle(ctx, command.RegisterWithEmail{Email: testEmail, Password: "short"})
	require.ErrorIs(t, err, domain.ErrWeakPassword)

	e.limiter.deny[application.ActionConfirmationRequest] = true
	_, err = e.registerEmail.Handle(ctx, command.RegisterWithEmail{Email: testEmail, Password: testPassword})
	require.ErrorIs(t, err, errLimited)
	assert.Empty(t, e.sender.links)
}

func TestRegisterWithEmail_SenderFailure(t *testing.T) {
	e := newEnv(t)
	e.sender.err = errors.New("smtp down")
	_, err := e.registerEmail.Handle(ctx, command.RegisterWithEmail{Email: testEmail, Password: testPassword})
	require.ErrorContains(t, err, "smtp down")
}

func TestRegisterWithEmail_VerifiedEmailIsTaken(t *testing.T) {
	e := newEnv(t)
	e.register(t, testEmail)
	_, err := e.registerEmail.Handle(ctx, command.RegisterWithEmail{Email: testEmail, Password: testPassword})
	require.ErrorIs(t, err, domain.ErrEmailTaken)
}

func TestRegisterWithEmail_UnconfirmedUserCanRegisterAgain(t *testing.T) {
	e := newEnv(t)
	first, err := e.registerEmail.Handle(ctx, command.RegisterWithEmail{Email: testEmail, Password: testPassword})
	require.NoError(t, err)
	firstLink := e.sender.links[testEmail]

	second, err := e.registerEmail.Handle(ctx, command.RegisterWithEmail{Email: testEmail, Password: "another secret 7"})
	require.NoError(t, err)
	assert.NotEqual(t, first.UserID, second.UserID)

	_, err = e.confirmEmail.Handle(ctx, command.ConfirmEmail{Token: e.sender.links[testEmail]})
	require.NoError(t, err)
	_, err = e.confirmEmail.Handle(ctx, command.ConfirmEmail{Token: firstLink})
	require.ErrorIs(t, err, domain.ErrEmailTaken, "the first confirmed account owns the email")

	_, err = e.signInEmail.Handle(ctx, command.SignInWithEmail{Email: testEmail, Password: "another secret 7"})
	require.NoError(t, err)
}

func TestConfirmEmail_ActivatesUser(t *testing.T) {
	e := newEnv(t)
	userID := e.register(t, testEmail)

	profile, err := e.profile.Handle(ctx, profileQuery(userID))
	require.NoError(t, err)
	assert.Equal(t, string(domain.UserStatusActive), profile.Status)
	assert.True(t, profile.EmailVerified)

	_, err = e.confirmEmail.Handle(ctx, command.ConfirmEmail{Token: e.sender.links[testEmail]})
	require.ErrorIs(t, err, domain.ErrChallengeAlreadyUsed)
}

func TestConfirmEmail_InvalidTokens(t *testing.T) {
	e := newEnv(t)
	_, err := e.registerEmail.Handle(ctx, command.RegisterWithEmail{Email: testEmail, Password: testPassword})
	require.NoError(t, err)
	link := e.sender.links[testEmail]

	for _, token := range []string{"", "garbage", "not-a-uuid.secret", domain.NewChallengeID().String() + ".secret", link + "x"} {
		_, err := e.confirmEmail.Handle(ctx, command.ConfirmEmail{Token: token})
		require.ErrorIs(t, err, domain.ErrInvalidConfirmationToken, token)
	}
}

func TestConfirmEmail_ExpiredLink(t *testing.T) {
	e := newEnv(t)
	_, err := e.registerEmail.Handle(ctx, command.RegisterWithEmail{Email: testEmail, Password: testPassword})
	require.NoError(t, err)
	e.clock.Advance(25 * time.Hour)
	_, err = e.confirmEmail.Handle(ctx, command.ConfirmEmail{Token: e.sender.links[testEmail]})
	require.ErrorIs(t, err, domain.ErrChallengeExpired)
}

func TestSignInWithEmail(t *testing.T) {
	e := newEnv(t)
	userID := e.register(t, testEmail)

	tokens, err := e.signInEmail.Handle(ctx, command.SignInWithEmail{Email: testEmail, Password: testPassword, DeviceName: "Chrome"})
	require.NoError(t, err)
	assert.Equal(t, userID, tokens.UserID)
	assert.NotEmpty(t, tokens.SessionID)
	assert.Equal(t, "access:"+tokens.UserID+":"+tokens.SessionID+":buyer", tokens.AccessToken)
	assert.Equal(t, e.clock.Now().Add(30*24*time.Hour), tokens.RefreshExpiresAt)
	assert.Equal(t, []string{
		"identity.user_registered.v1", "identity.user_email_confirmed.v1", "identity.session_started.v1",
	}, eventNames(e.store.Events()))

	_, err = e.signInEmail.Handle(ctx, command.SignInWithEmail{Email: testEmail, Password: "wrong password 1"})
	require.ErrorIs(t, err, domain.ErrInvalidCredentials)
	_, err = e.signInEmail.Handle(ctx, command.SignInWithEmail{Email: "ghost@example.kz", Password: testPassword})
	require.ErrorIs(t, err, domain.ErrInvalidCredentials)
	_, err = e.signInEmail.Handle(ctx, command.SignInWithEmail{Email: "not-an-email", Password: testPassword})
	require.ErrorIs(t, err, domain.ErrInvalidCredentials)
}

func TestSignInWithEmail_UnconfirmedUserCannotSignIn(t *testing.T) {
	e := newEnv(t)
	_, err := e.registerEmail.Handle(ctx, command.RegisterWithEmail{Email: testEmail, Password: testPassword})
	require.NoError(t, err)
	_, err = e.signInEmail.Handle(ctx, command.SignInWithEmail{Email: testEmail, Password: testPassword})
	require.ErrorIs(t, err, domain.ErrInvalidCredentials)
}

func TestSignInWithEmail_RateLimitedAndBlocked(t *testing.T) {
	e := newEnv(t)
	userID := e.register(t, testEmail)
	_, err := e.block.Handle(ctx, command.BlockUser{Actor: e.admin(t), UserID: userID, Reason: "fraud"})
	require.NoError(t, err)

	_, err = e.signInEmail.Handle(ctx, command.SignInWithEmail{Email: testEmail, Password: testPassword})
	require.ErrorIs(t, err, domain.ErrUserBlocked)

	e.limiter.deny[application.ActionSignIn] = true
	_, err = e.signInEmail.Handle(ctx, command.SignInWithEmail{Email: testEmail, Password: testPassword})
	require.ErrorIs(t, err, errLimited)
}
