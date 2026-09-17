package command_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/application/command"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

func TestBlockUser_RequiresPermission(t *testing.T) {
	e := newEnv(t)
	target := e.signIn(t, testEmail)
	buyer := e.principal(e.signIn(t, otherEmail))

	_, err := e.block.Handle(ctx, command.BlockUser{Actor: buyer, UserID: target.UserID, Reason: "spite"})
	require.ErrorIs(t, err, auth.ErrForbidden)
	_, err = e.block.Handle(ctx, command.BlockUser{Actor: auth.Principal{}, UserID: target.UserID, Reason: "x"})
	require.ErrorIs(t, err, auth.ErrUnauthenticated)
}

func TestBlockAndUnblockUser(t *testing.T) {
	e := newEnv(t)
	target := e.signIn(t, testEmail)
	admin := e.admin(t)

	_, err := e.block.Handle(ctx, command.BlockUser{Actor: admin, UserID: target.UserID, Reason: "chargebacks"})
	require.NoError(t, err)
	profile, err := e.profile.Handle(ctx, profileQuery(target.UserID))
	require.NoError(t, err)
	assert.Equal(t, string(domain.UserStatusBlocked), profile.Status)

	_, err = e.unblock.Handle(ctx, command.UnblockUser{Actor: admin, UserID: target.UserID})
	require.NoError(t, err)
	profile, err = e.profile.Handle(ctx, profileQuery(target.UserID))
	require.NoError(t, err)
	assert.Equal(t, string(domain.UserStatusActive), profile.Status)

	entries := e.store.AuditEntries()
	require.Len(t, entries, 2)
	assert.Equal(t, "identity.user.block", entries[0].Action)
	assert.Equal(t, "chargebacks", entries[0].Details["reason"])
	assert.Equal(t, "identity.user.unblock", entries[1].Action)
}

func TestBlockUser_Errors(t *testing.T) {
	e := newEnv(t)
	admin := e.admin(t)

	_, err := e.block.Handle(ctx, command.BlockUser{Actor: admin, UserID: "bad", Reason: "x"})
	require.ErrorIs(t, err, domain.ErrUserNotFound)
	_, err = e.block.Handle(ctx, command.BlockUser{Actor: admin, UserID: domain.NewSessionID().String(), Reason: "x"})
	require.ErrorIs(t, err, domain.ErrUserNotFound)
	_, err = e.block.Handle(ctx, command.BlockUser{Actor: admin, UserID: admin.UserID, Reason: "x"})
	require.ErrorIs(t, err, domain.ErrCannotBlockSelf)

	_, err = e.unblock.Handle(ctx, command.UnblockUser{Actor: admin, UserID: admin.UserID})
	require.ErrorIs(t, err, domain.ErrUserNotBlocked)
	_, err = e.unblock.Handle(ctx, command.UnblockUser{Actor: e.principal(e.signIn(t, testEmail)), UserID: admin.UserID})
	require.ErrorIs(t, err, auth.ErrForbidden)
	_, err = e.unblock.Handle(ctx, command.UnblockUser{Actor: admin, UserID: "bad"})
	require.ErrorIs(t, err, domain.ErrUserNotFound)
	_, err = e.block.Handle(ctx, command.BlockUser{Actor: auth.Principal{UserID: "bad", Roles: []string{"platform_admin"}}, UserID: admin.UserID, Reason: "x"})
	require.ErrorIs(t, err, auth.ErrInvalidToken)
}

func TestGrantAndRevokeRole(t *testing.T) {
	e := newEnv(t)
	target := e.signIn(t, testEmail)
	admin := e.admin(t)

	_, err := e.grantRole.Handle(ctx, command.GrantRole{Actor: admin, UserID: target.UserID, Role: "content_moderator"})
	require.NoError(t, err)
	profile, err := e.profile.Handle(ctx, profileQuery(target.UserID))
	require.NoError(t, err)
	assert.Equal(t, []string{"buyer", "content_moderator"}, profile.Roles)

	_, err = e.revokeRole.Handle(ctx, command.RevokeRole{Actor: admin, UserID: target.UserID, Role: "content_moderator"})
	require.NoError(t, err)
	profile, err = e.profile.Handle(ctx, profileQuery(target.UserID))
	require.NoError(t, err)
	assert.Equal(t, []string{"buyer"}, profile.Roles)

	_, err = e.revokeRole.Handle(ctx, command.RevokeRole{Actor: admin, UserID: target.UserID, Role: "buyer"})
	require.ErrorIs(t, err, domain.ErrBaseRoleRequired)
	_, err = e.grantRole.Handle(ctx, command.GrantRole{Actor: admin, UserID: target.UserID, Role: "god"})
	require.ErrorIs(t, err, domain.ErrInvalidRole)
	_, err = e.grantRole.Handle(ctx, command.GrantRole{Actor: admin, UserID: "bad", Role: "buyer"})
	require.ErrorIs(t, err, domain.ErrUserNotFound)
	_, err = e.grantRole.Handle(ctx, command.GrantRole{Actor: e.principal(target), UserID: target.UserID, Role: "platform_admin"})
	require.ErrorIs(t, err, auth.ErrForbidden)

	assert.Len(t, e.store.AuditEntries(), 2)
}
