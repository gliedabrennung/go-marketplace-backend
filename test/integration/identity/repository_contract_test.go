//go:build integration

package identity_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/infrastructure/memory"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/infrastructure/postgres"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/test/testdb"
)

var identityTables = []string{
	"identity.refresh_tokens", "identity.sessions", "identity.challenges", "identity.users",
	"platform.outbox", "platform.idempotency_keys", "platform.processed_messages",
}

func implementations() map[string]func(t *testing.T) application.Repositories {
	return map[string]func(t *testing.T) application.Repositories{
		"memory": func(*testing.T) application.Repositories {
			return memory.NewStore()
		},
		"postgres": func(t *testing.T) application.Repositories {
			pool := testdb.Pool(t)
			testdb.Truncate(t, pool, identityTables...)
			return postgres.NewRepositories(pool, postgres.NewOutboxWriter())
		},
	}
}

func instant() time.Time {
	return time.Now().UTC().Truncate(time.Microsecond)
}

func newUser(t *testing.T, raw string) *domain.User {
	t.Helper()
	email, err := domain.NewEmail(raw)
	require.NoError(t, err)
	hash, err := domain.NewPasswordHash("encoded-hash")
	require.NoError(t, err)
	u, err := domain.RegisterWithEmail(kernel.NewUserID(), email, hash, instant())
	require.NoError(t, err)
	return u
}

func TestUserRepositoryContract(t *testing.T) {
	ctx := context.Background()
	for name, factory := range implementations() {
		t.Run(name, func(t *testing.T) {
			t.Run("FindByID returns ErrUserNotFound for missing user", func(t *testing.T) {
				_, err := factory(t).Users().FindByID(ctx, kernel.NewUserID())
				require.ErrorIs(t, err, domain.ErrUserNotFound)
			})

			t.Run("Save inserts, increments version and round-trips state", func(t *testing.T) {
				repos := factory(t)
				u := newUser(t, "buyer@example.kz")
				require.NoError(t, u.ConfirmEmail(instant()))
				require.NoError(t, u.GrantRole(domain.RoleSupportAgent, kernel.NewUserID(), instant()))
				require.NoError(t, repos.Users().Save(ctx, u))
				assert.Equal(t, 1, u.Version())
				assert.Empty(t, u.PullEvents())

				loaded, err := repos.Users().FindByID(ctx, u.ID())
				require.NoError(t, err)
				assert.Equal(t, u.Snapshot(), loaded.Snapshot())

				byEmail, err := repos.Users().FindByVerifiedEmail(ctx, u.Email())
				require.NoError(t, err)
				assert.Equal(t, u.ID(), byEmail.ID())

				require.NoError(t, loaded.Block("fraud", kernel.NewUserID(), instant()))
				require.NoError(t, repos.Users().Save(ctx, loaded))
				assert.Equal(t, 2, loaded.Version())
			})

			t.Run("Save with stale version returns ErrConcurrentModification", func(t *testing.T) {
				repos := factory(t)
				u := newUser(t, "buyer@example.kz")
				stale, err := domain.RehydrateUser(u.Snapshot())
				require.NoError(t, err)
				require.NoError(t, repos.Users().Save(ctx, u))
				require.ErrorIs(t, repos.Users().Save(ctx, stale), kernel.ErrConcurrentModification)

				first, err := repos.Users().FindByID(ctx, u.ID())
				require.NoError(t, err)
				second, err := repos.Users().FindByID(ctx, u.ID())
				require.NoError(t, err)
				require.NoError(t, first.Block("a", kernel.NewUserID(), instant()))
				require.NoError(t, second.GrantRole(domain.RoleSupportAgent, kernel.NewUserID(), instant()))
				require.NoError(t, repos.Users().Save(ctx, first))
				require.ErrorIs(t, repos.Users().Save(ctx, second), kernel.ErrConcurrentModification)
			})

			t.Run("verified email is unique, unverified duplicates are allowed", func(t *testing.T) {
				repos := factory(t)
				first := newUser(t, "buyer@example.kz")
				second := newUser(t, "buyer@example.kz")
				require.NoError(t, repos.Users().Save(ctx, first))
				require.NoError(t, repos.Users().Save(ctx, second))

				_, err := repos.Users().FindByVerifiedEmail(ctx, first.Email())
				require.ErrorIs(t, err, domain.ErrUserNotFound)

				require.NoError(t, first.ConfirmEmail(instant()))
				require.NoError(t, repos.Users().Save(ctx, first))
				require.NoError(t, second.ConfirmEmail(instant()))
				require.ErrorIs(t, repos.Users().Save(ctx, second), domain.ErrEmailTaken)

				found, err := repos.Users().FindByVerifiedEmail(ctx, first.Email())
				require.NoError(t, err)
				assert.Equal(t, first.ID(), found.ID())
			})
		})
	}
}

func TestSessionRepositoryContract(t *testing.T) {
	ctx := context.Background()
	for name, factory := range implementations() {
		t.Run(name, func(t *testing.T) {
			t.Run("lookups return ErrSessionNotFound", func(t *testing.T) {
				repos := factory(t)
				_, err := repos.Sessions().FindByID(ctx, domain.NewSessionID())
				require.ErrorIs(t, err, domain.ErrSessionNotFound)
				_, err = repos.Sessions().FindByRefreshDigest(ctx, domain.DigestRefreshToken("nope"))
				require.ErrorIs(t, err, domain.ErrSessionNotFound)
			})

			t.Run("rotated tokens remain resolvable for reuse detection", func(t *testing.T) {
				repos := factory(t)
				now := instant()
				s, err := domain.StartSession(domain.NewSessionID(), kernel.NewUserID(), domain.NewDevice("Chrome", "ua", "10.0.0.1"), "r1", time.Hour, now)
				require.NoError(t, err)
				require.NoError(t, repos.Sessions().Save(ctx, s))
				require.NoError(t, s.Rotate("r1", "r2", time.Hour, now))
				require.NoError(t, repos.Sessions().Save(ctx, s))
				assert.Equal(t, 2, s.Version())

				byOld, err := repos.Sessions().FindByRefreshDigest(ctx, domain.DigestRefreshToken("r1"))
				require.NoError(t, err)
				byNew, err := repos.Sessions().FindByRefreshDigest(ctx, domain.DigestRefreshToken("r2"))
				require.NoError(t, err)
				assert.Equal(t, s.ID(), byOld.ID())
				assert.Equal(t, s.Snapshot(), byNew.Snapshot())

				require.ErrorIs(t, byOld.Rotate("r1", "r3", time.Hour, now), domain.ErrRefreshTokenReused)
				require.NoError(t, repos.Sessions().Save(ctx, byOld))
				require.ErrorIs(t, repos.Sessions().Save(ctx, byNew), kernel.ErrConcurrentModification)

				loaded, err := repos.Sessions().FindByID(ctx, s.ID())
				require.NoError(t, err)
				assert.Equal(t, domain.SessionStatusRevoked, loaded.Status())
				assert.Equal(t, domain.RevokedByReuse, loaded.RevokeReason())
			})
		})
	}
}

func TestChallengeRepositoryContract(t *testing.T) {
	ctx := context.Background()
	for name, factory := range implementations() {
		t.Run(name, func(t *testing.T) {
			repos := factory(t)
			now := instant()
			email, err := domain.NewEmail("buyer@example.kz")
			require.NoError(t, err)

			_, err = repos.Challenges().FindByID(ctx, domain.NewChallengeID())
			require.ErrorIs(t, err, domain.ErrChallengeNotFound)

			c, err := domain.IssueEmailConfirmationChallenge(domain.NewChallengeID(), kernel.NewUserID(), email, "secret", domain.EmailConfirmationPolicy(), now)
			require.NoError(t, err)
			stale, err := domain.RehydrateChallenge(c.Snapshot())
			require.NoError(t, err)
			require.NoError(t, repos.Challenges().Save(ctx, c))
			require.ErrorIs(t, repos.Challenges().Save(ctx, stale), kernel.ErrConcurrentModification)

			loaded, err := repos.Challenges().FindByID(ctx, c.ID())
			require.NoError(t, err)
			assert.Equal(t, c.Snapshot(), loaded.Snapshot())

			require.ErrorIs(t, loaded.Verify("guess", now.Add(time.Minute)), domain.ErrInvalidConfirmationToken)
			require.NoError(t, repos.Challenges().Save(ctx, loaded))
			require.NoError(t, loaded.Verify("secret", now.Add(time.Minute)))
			require.NoError(t, repos.Challenges().Save(ctx, loaded))

			final, err := repos.Challenges().FindByID(ctx, c.ID())
			require.NoError(t, err)
			assert.Equal(t, domain.ChallengeVerified, final.Status())
			assert.Equal(t, 2, final.Attempts())
			assert.Equal(t, 3, final.Version())
			require.ErrorIs(t, repos.Challenges().Save(ctx, c), kernel.ErrConcurrentModification)
		})
	}
}
