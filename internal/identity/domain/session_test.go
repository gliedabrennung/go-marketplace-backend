package domain_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

const refreshTTL = 30 * 24 * time.Hour

func startSession(t *testing.T, token string) *domain.Session {
	t.Helper()
	s, err := domain.StartSession(domain.NewSessionID(), kernel.NewUserID(), domain.NewDevice("iPhone", "ua", "10.0.0.1"), token, refreshTTL, now)
	require.NoError(t, err)
	return s
}

func TestStartSession(t *testing.T) {
	userID := kernel.NewUserID()
	s, err := domain.StartSession(domain.NewSessionID(), userID, domain.NewDevice("Pixel", "okhttp", "10.0.0.2"), "refresh-1", refreshTTL, now)
	require.NoError(t, err)

	assert.True(t, s.BelongsTo(userID))
	assert.False(t, s.BelongsTo(kernel.NewUserID()))
	assert.True(t, s.IsActive(now))
	assert.Equal(t, domain.SessionStatusActive, s.Status())
	assert.Equal(t, now.Add(refreshTTL), s.ExpiresAt())
	assert.Equal(t, domain.DigestRefreshToken("refresh-1"), s.RefreshDigest())
	assert.Equal(t, "Pixel", s.Device().Name)
	assert.Equal(t, userID, s.UserID())
	assert.Equal(t, []string{"identity.session_started.v1"}, eventNames(s.PullEvents()))
}

func TestStartSession_Validation(t *testing.T) {
	device := domain.NewDevice("", "", "")
	_, err := domain.StartSession(domain.SessionID{}, kernel.NewUserID(), device, "t", refreshTTL, now)
	require.ErrorIs(t, err, kernel.ErrInvalidID)
	_, err = domain.StartSession(domain.NewSessionID(), kernel.UserID{}, device, "t", refreshTTL, now)
	require.ErrorIs(t, err, kernel.ErrInvalidID)
	_, err = domain.StartSession(domain.NewSessionID(), kernel.NewUserID(), device, "", refreshTTL, now)
	require.ErrorIs(t, err, domain.ErrInvalidSecret)
	_, err = domain.StartSession(domain.NewSessionID(), kernel.NewUserID(), device, "t", 0, now)
	require.ErrorIs(t, err, domain.ErrInvalidTTL)
}

func TestSession_RotateSlidesExpiry(t *testing.T) {
	s := startSession(t, "refresh-1")
	later := now.Add(10 * 24 * time.Hour)

	require.NoError(t, s.Rotate("refresh-1", "refresh-2", refreshTTL, later))
	assert.Equal(t, domain.DigestRefreshToken("refresh-2"), s.RefreshDigest())
	assert.Equal(t, later.Add(refreshTTL), s.ExpiresAt())
	assert.True(t, s.IsActive(later))
}

func TestSession_ReusedTokenRevokesWholeSession(t *testing.T) {
	s := startSession(t, "refresh-1")
	s.PullEvents()
	require.NoError(t, s.Rotate("refresh-1", "refresh-2", refreshTTL, now))

	require.ErrorIs(t, s.Rotate("refresh-1", "refresh-3", refreshTTL, now), domain.ErrRefreshTokenReused)
	assert.Equal(t, domain.SessionStatusRevoked, s.Status())
	assert.Equal(t, domain.RevokedByReuse, s.RevokeReason())
	assert.False(t, s.IsActive(now))

	events := s.PullEvents()
	require.Len(t, events, 1)
	assert.Equal(t, domain.RevokedByReuse, events[0].(domain.SessionRevoked).Reason)

	require.ErrorIs(t, s.Rotate("refresh-2", "refresh-3", refreshTTL, now), domain.ErrSessionRevoked)
}

func TestSession_RotateRules(t *testing.T) {
	s := startSession(t, "refresh-1")
	require.ErrorIs(t, s.Rotate("refresh-1", "", refreshTTL, now), domain.ErrInvalidSecret)
	require.ErrorIs(t, s.Rotate("refresh-1", "next", 0, now), domain.ErrInvalidTTL)
	require.ErrorIs(t, s.Rotate("refresh-1", "next", refreshTTL, now.Add(refreshTTL)), domain.ErrSessionExpired)
	assert.False(t, s.IsActive(now.Add(refreshTTL)))
}

func TestSession_RevokeIsIdempotent(t *testing.T) {
	s := startSession(t, "refresh-1")
	s.PullEvents()
	s.Revoke(domain.RevokedBySignOut, now)
	s.Revoke(domain.RevokedByAdmin, now)
	assert.Equal(t, domain.RevokedBySignOut, s.RevokeReason())
	assert.Len(t, s.PullEvents(), 1)
}

func TestNewDevice_Truncates(t *testing.T) {
	d := domain.NewDevice(strings.Repeat("я", 150), strings.Repeat("u", 600), strings.Repeat("1", 70))
	assert.Equal(t, 100, len([]rune(d.Name)))
	assert.Len(t, d.UserAgent, 512)
	assert.Len(t, d.IP, 64)
}

func TestSession_SnapshotRoundTrip(t *testing.T) {
	s := startSession(t, "refresh-1")
	s.Revoke(domain.RevokedByOwner, now)
	s.AdvanceVersion()

	restored, err := domain.RehydrateSession(s.Snapshot())
	require.NoError(t, err)
	assert.Equal(t, s.Snapshot(), restored.Snapshot())
	assert.Equal(t, s.ID(), restored.ID())
	assert.Equal(t, 1, restored.Version())
	assert.Empty(t, restored.PullEvents())

	broken := s.Snapshot()
	broken.ID = "x"
	_, err = domain.RehydrateSession(broken)
	require.Error(t, err)

	broken = s.Snapshot()
	broken.UserID = "x"
	_, err = domain.RehydrateSession(broken)
	require.Error(t, err)

	parsed, err := domain.ParseSessionID(s.ID().String())
	require.NoError(t, err)
	assert.Equal(t, s.ID(), parsed)
}
