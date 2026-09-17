package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

func TestRegisterWithEmail_CreatesPendingBuyer(t *testing.T) {
	id := kernel.NewUserID()
	u, err := domain.RegisterWithEmail(id, email(t), hash(t), now)
	require.NoError(t, err)

	assert.Equal(t, id, u.ID())
	assert.Equal(t, domain.UserStatusPending, u.Status())
	assert.False(t, u.EmailVerified())
	assert.Equal(t, "buyer@example.kz", u.Email().String())
	assert.Equal(t, "encoded-hash", u.PasswordHash().String())
	assert.Equal(t, []domain.Role{domain.RoleBuyer}, u.Roles())
	assert.Equal(t, now, u.CreatedAt())
	require.ErrorIs(t, u.EnsureCanSignIn(), domain.ErrEmailNotConfirmed)

	events := u.PullEvents()
	require.Len(t, events, 1)
	registered := events[0].(domain.UserRegistered)
	assert.Equal(t, id.String(), registered.AggregateID())
	assert.Equal(t, now, registered.OccurredAt())
	assert.Equal(t, "identity.user_registered.v1", registered.EventName())
	assert.Empty(t, u.PullEvents())
}

func TestRegisterWithEmail_Validation(t *testing.T) {
	_, err := domain.RegisterWithEmail(kernel.UserID{}, email(t), hash(t), now)
	require.ErrorIs(t, err, kernel.ErrInvalidID)
	_, err = domain.RegisterWithEmail(kernel.NewUserID(), domain.Email{}, hash(t), now)
	require.ErrorIs(t, err, domain.ErrInvalidEmail)
	_, err = domain.RegisterWithEmail(kernel.NewUserID(), email(t), domain.PasswordHash{}, now)
	require.ErrorIs(t, err, domain.ErrInvalidPasswordHash)
}

func TestUser_ConfirmEmailActivates(t *testing.T) {
	u := pendingUser(t)
	require.NoError(t, u.ConfirmEmail(now.Add(time.Minute)))

	assert.True(t, u.EmailVerified())
	assert.Equal(t, domain.UserStatusActive, u.Status())
	assert.NoError(t, u.EnsureCanSignIn())
	assert.Equal(t, []string{"identity.user_email_confirmed.v1"}, eventNames(u.PullEvents()))

	require.ErrorIs(t, u.ConfirmEmail(now), domain.ErrEmailAlreadyConfirmed)
}

func TestUser_ConfirmEmailKeepsBlockedStatus(t *testing.T) {
	u := pendingUser(t)
	require.NoError(t, u.Block("fraud", kernel.NewUserID(), now))
	require.NoError(t, u.ConfirmEmail(now))
	assert.Equal(t, domain.UserStatusBlocked, u.Status())
}

func TestUser_BlockedUserCannotSignIn(t *testing.T) {
	u := activeUser(t)
	admin := kernel.NewUserID()
	require.NoError(t, u.Block("  chargeback fraud ", admin, now))

	assert.Equal(t, domain.UserStatusBlocked, u.Status())
	assert.Equal(t, "chargeback fraud", u.BlockReason())
	require.ErrorIs(t, u.EnsureCanSignIn(), domain.ErrUserBlocked)

	events := u.PullEvents()
	require.Len(t, events, 1)
	blocked := events[0].(domain.UserBlocked)
	assert.Equal(t, admin, blocked.BlockedBy)
	assert.Equal(t, "chargeback fraud", blocked.Reason)
}

func TestUser_BlockRules(t *testing.T) {
	u := activeUser(t)
	require.ErrorIs(t, u.Block(" ", kernel.NewUserID(), now), domain.ErrReasonRequired)
	require.ErrorIs(t, u.Block("self", u.ID(), now), domain.ErrCannotBlockSelf)
	require.NoError(t, u.Block("fraud", kernel.NewUserID(), now))
	require.ErrorIs(t, u.Block("again", kernel.NewUserID(), now), domain.ErrUserAlreadyBlocked)
}

func TestUser_UnblockRestoresStatus(t *testing.T) {
	verified := activeUser(t)
	require.ErrorIs(t, verified.Unblock(kernel.NewUserID(), now), domain.ErrUserNotBlocked)
	require.NoError(t, verified.Block("fraud", kernel.NewUserID(), now))
	require.NoError(t, verified.Unblock(kernel.NewUserID(), now))
	assert.Equal(t, domain.UserStatusActive, verified.Status())
	assert.Empty(t, verified.BlockReason())
	assert.Equal(t, []string{"identity.user_blocked.v1", "identity.user_unblocked.v1"}, eventNames(verified.PullEvents()))

	pending := pendingUser(t)
	require.NoError(t, pending.Block("spam", kernel.NewUserID(), now))
	require.NoError(t, pending.Unblock(kernel.NewUserID(), now))
	assert.Equal(t, domain.UserStatusPending, pending.Status())
}

func TestUser_Roles(t *testing.T) {
	u := activeUser(t)
	admin := kernel.NewUserID()

	require.NoError(t, u.GrantRole(domain.RoleSupportAgent, admin, now))
	require.NoError(t, u.GrantRole(domain.RoleSupportAgent, admin, now))
	assert.True(t, u.HasRole(domain.RoleSupportAgent))
	assert.Equal(t, []string{"buyer", "support_agent"}, u.RoleNames())

	require.ErrorIs(t, u.GrantRole("root", admin, now), domain.ErrInvalidRole)
	require.ErrorIs(t, u.RevokeRole(domain.RoleBuyer, admin, now), domain.ErrBaseRoleRequired)

	require.NoError(t, u.RevokeRole(domain.RoleSupportAgent, admin, now))
	require.NoError(t, u.RevokeRole(domain.RoleSupportAgent, admin, now))
	assert.False(t, u.HasRole(domain.RoleSupportAgent))

	assert.Equal(t, []string{"identity.user_role_granted.v1", "identity.user_role_revoked.v1"}, eventNames(u.PullEvents()))
}

func TestUser_RolesAreCopied(t *testing.T) {
	u := activeUser(t)
	roles := u.Roles()
	roles[0] = domain.RolePlatformAdmin
	assert.False(t, u.HasRole(domain.RolePlatformAdmin))
}

func TestUser_SnapshotRoundTrip(t *testing.T) {
	u := activeUser(t)
	require.NoError(t, u.GrantRole(domain.RoleContentModerator, kernel.NewUserID(), now))
	u.AdvanceVersion()

	restored, err := domain.RehydrateUser(u.Snapshot())
	require.NoError(t, err)
	assert.Equal(t, u.Snapshot(), restored.Snapshot())
	assert.Equal(t, 1, restored.Version())
	assert.Empty(t, restored.PullEvents())
}

func TestRehydrateUser_RejectsCorruptedData(t *testing.T) {
	base := activeUser(t).Snapshot()

	broken := base
	broken.ID = "nope"
	_, err := domain.RehydrateUser(broken)
	require.Error(t, err)

	broken = base
	broken.Email = "not-an-email"
	_, err = domain.RehydrateUser(broken)
	require.Error(t, err)

	broken = base
	broken.Roles = []string{"superuser"}
	_, err = domain.RehydrateUser(broken)
	require.Error(t, err)
}
