package command_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/application/command"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

func TestRefreshSession_RotatesToken(t *testing.T) {
	e := newEnv(t)
	tokens := e.signIn(t, testEmail)
	e.clock.Advance(20 * time.Minute)

	next, err := e.refresh.Handle(ctx, command.RefreshSession{RefreshToken: tokens.RefreshToken})
	require.NoError(t, err)
	assert.Equal(t, tokens.SessionID, next.SessionID)
	assert.Equal(t, tokens.UserID, next.UserID)
	assert.NotEqual(t, tokens.RefreshToken, next.RefreshToken)
	assert.Equal(t, e.clock.Now().Add(30*24*time.Hour), next.RefreshExpiresAt)

	again, err := e.refresh.Handle(ctx, command.RefreshSession{RefreshToken: next.RefreshToken})
	require.NoError(t, err)
	assert.NotEqual(t, next.RefreshToken, again.RefreshToken)
}

func TestRefreshSession_ReuseRevokesChain(t *testing.T) {
	e := newEnv(t)
	tokens := e.signIn(t, testEmail)
	next, err := e.refresh.Handle(ctx, command.RefreshSession{RefreshToken: tokens.RefreshToken})
	require.NoError(t, err)

	_, err = e.refresh.Handle(ctx, command.RefreshSession{RefreshToken: tokens.RefreshToken})
	require.ErrorIs(t, err, domain.ErrRefreshTokenReused)

	_, err = e.refresh.Handle(ctx, command.RefreshSession{RefreshToken: next.RefreshToken})
	require.ErrorIs(t, err, domain.ErrSessionRevoked, "revoked session cannot be restored")
}

func TestRefreshSession_InvalidToken(t *testing.T) {
	e := newEnv(t)
	_, err := e.refresh.Handle(ctx, command.RefreshSession{})
	require.ErrorIs(t, err, domain.ErrInvalidRefresh)
	_, err = e.refresh.Handle(ctx, command.RefreshSession{RefreshToken: "unknown"})
	require.ErrorIs(t, err, domain.ErrInvalidRefresh)
}

func TestRefreshSession_BlockedUserLosesSession(t *testing.T) {
	e := newEnv(t)
	tokens := e.signIn(t, testEmail)
	_, err := e.block.Handle(ctx, command.BlockUser{Actor: e.admin(t), UserID: tokens.UserID, Reason: "fraud"})
	require.NoError(t, err)

	_, err = e.refresh.Handle(ctx, command.RefreshSession{RefreshToken: tokens.RefreshToken})
	require.ErrorIs(t, err, domain.ErrUserBlocked)

	id, err := domain.ParseSessionID(tokens.SessionID)
	require.NoError(t, err)
	session, err := e.store.Sessions().FindByID(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, domain.RevokedByBlock, session.RevokeReason())
}

func TestRevokeSession_OwnerSignsOut(t *testing.T) {
	e := newEnv(t)
	tokens := e.signIn(t, testEmail)
	actor := e.principal(tokens)

	_, err := e.revokeSession.Handle(ctx, command.RevokeSession{Actor: actor, SessionID: tokens.SessionID})
	require.NoError(t, err)

	id, err := domain.ParseSessionID(tokens.SessionID)
	require.NoError(t, err)
	session, err := e.store.Sessions().FindByID(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, domain.RevokedBySignOut, session.RevokeReason())

	_, err = e.refresh.Handle(ctx, command.RefreshSession{RefreshToken: tokens.RefreshToken})
	require.ErrorIs(t, err, domain.ErrSessionRevoked)
	assert.Empty(t, e.store.AuditEntries())
}

func TestRevokeSession_OwnerRevokesOtherDevice(t *testing.T) {
	e := newEnv(t)
	phone := e.signIn(t, testEmail)
	laptop := e.signIn(t, testEmail)

	_, err := e.revokeSession.Handle(ctx, command.RevokeSession{Actor: e.principal(phone), SessionID: laptop.SessionID})
	require.NoError(t, err)

	id, err := domain.ParseSessionID(laptop.SessionID)
	require.NoError(t, err)
	session, err := e.store.Sessions().FindByID(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, domain.RevokedByOwner, session.RevokeReason())
}

func TestRevokeSession_ForeignSessionIsHidden(t *testing.T) {
	e := newEnv(t)
	victim := e.signIn(t, testEmail)
	attacker := e.signIn(t, otherEmail)

	_, err := e.revokeSession.Handle(ctx, command.RevokeSession{Actor: e.principal(attacker), SessionID: victim.SessionID})
	require.ErrorIs(t, err, domain.ErrSessionNotFound)

	_, err = e.revokeSession.Handle(ctx, command.RevokeSession{Actor: e.principal(attacker), SessionID: "bad"})
	require.ErrorIs(t, err, domain.ErrSessionNotFound)

	_, err = e.revokeSession.Handle(ctx, command.RevokeSession{Actor: auth.Principal{}, SessionID: victim.SessionID})
	require.ErrorIs(t, err, auth.ErrUnauthenticated)
}

func TestRevokeSession_SupportAgentIsAudited(t *testing.T) {
	e := newEnv(t)
	victim := e.signIn(t, testEmail)
	agent := e.principal(e.signIn(t, otherEmail), "support_agent")

	_, err := e.revokeSession.Handle(ctx, command.RevokeSession{Actor: agent, SessionID: victim.SessionID})
	require.NoError(t, err)

	entries := e.store.AuditEntries()
	require.Len(t, entries, 1)
	assert.Equal(t, "identity.session.revoke", entries[0].Action)
	assert.Equal(t, victim.SessionID, entries[0].ObjectID)
	assert.Equal(t, agent.UserID, entries[0].ActorID)
}

func TestRevokeUserSessions(t *testing.T) {
	e := newEnv(t)
	first := e.signIn(t, testEmail)
	e.signIn(t, testEmail)
	other := e.signIn(t, otherEmail)

	res, err := e.revokeAll.Handle(ctx, command.RevokeUserSessions{UserID: first.UserID})
	require.NoError(t, err)
	assert.Equal(t, 2, res.Revoked)

	res, err = e.revokeAll.Handle(ctx, command.RevokeUserSessions{UserID: first.UserID})
	require.NoError(t, err)
	assert.Zero(t, res.Revoked)

	_, err = e.refresh.Handle(ctx, command.RefreshSession{RefreshToken: other.RefreshToken})
	require.NoError(t, err)
}

func TestListSessions(t *testing.T) {
	e := newEnv(t)
	var last string
	var current auth.Principal
	for range 3 {
		tokens := e.signIn(t, testEmail)
		current = e.principal(tokens)
		last = tokens.SessionID
		e.clock.Advance(time.Minute)
	}

	page, err := e.listSessions.Handle(ctx, query.ListSessions{Actor: current, Limit: 2})
	require.NoError(t, err)
	require.Len(t, page.Items, 2)
	assert.True(t, page.HasMore)
	assert.Equal(t, last, page.Items[0].ID)
	assert.True(t, page.Items[0].Current)
	assert.False(t, page.Items[1].Current)
	assert.Equal(t, "iPhone", page.Items[0].DeviceName)

	rest, err := e.listSessions.Handle(ctx, query.ListSessions{Actor: current, Limit: 2, Cursor: page.NextCursor})
	require.NoError(t, err)
	require.Len(t, rest.Items, 1)
	assert.False(t, rest.HasMore)

	_, err = e.listSessions.Handle(ctx, query.ListSessions{Actor: current, Cursor: "%%"})
	require.Error(t, err)
	_, err = e.listSessions.Handle(ctx, query.ListSessions{})
	require.ErrorIs(t, err, auth.ErrUnauthenticated)
}

func TestGetProfile(t *testing.T) {
	e := newEnv(t)
	tokens := e.signIn(t, testEmail)

	profile, err := e.profile.Handle(ctx, profileQuery(tokens.UserID))
	require.NoError(t, err)
	assert.Equal(t, testEmail, profile.Email)
	assert.True(t, profile.EmailVerified)
	assert.Equal(t, []string{"buyer"}, profile.Roles)

	_, err = e.profile.Handle(ctx, query.GetProfile{})
	require.ErrorIs(t, err, auth.ErrUnauthenticated)
	_, err = e.profile.Handle(ctx, profileQuery(domain.NewSessionID().String()))
	require.ErrorIs(t, err, domain.ErrUserNotFound)
}
