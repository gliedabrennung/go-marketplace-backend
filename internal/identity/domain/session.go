package domain

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type sessionTag struct{}

type SessionID = kernel.ID[sessionTag]

func NewSessionID() SessionID { return kernel.NewID[sessionTag]() }

func ParseSessionID(s string) (SessionID, error) { return kernel.ParseID[sessionTag](s) }

type SessionStatus string

const (
	SessionStatusActive  SessionStatus = "active"
	SessionStatusRevoked SessionStatus = "revoked"
)

type RevokeReason string

const (
	RevokedBySignOut RevokeReason = "sign_out"
	RevokedByOwner   RevokeReason = "revoked_by_owner"
	RevokedByAdmin   RevokeReason = "revoked_by_admin"
	RevokedByReuse   RevokeReason = "token_reuse"
	RevokedByBlock   RevokeReason = "user_blocked"
)

type Device struct {
	Name      string
	UserAgent string
	IP        string
}

func NewDevice(name, userAgent, ip string) Device {
	return Device{
		Name:      truncate(name, 100),
		UserAgent: truncate(userAgent, 512),
		IP:        truncate(ip, 64),
	}
}

func truncate(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	return string([]rune(s)[:limit])
}

type Session struct {
	id            SessionID
	userID        kernel.UserID
	device        Device
	refreshDigest string
	status        SessionStatus
	createdAt     time.Time
	lastUsedAt    time.Time
	expiresAt     time.Time
	revokedAt     time.Time
	revokeReason  RevokeReason
	version       int

	events kernel.EventBuffer
}

func DigestRefreshToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func StartSession(id SessionID, userID kernel.UserID, device Device, refreshToken string, ttl time.Duration, now time.Time) (*Session, error) {
	if id.IsZero() || userID.IsZero() {
		return nil, kernel.ErrInvalidID
	}
	if refreshToken == "" {
		return nil, ErrInvalidSecret
	}
	if ttl <= 0 {
		return nil, ErrInvalidTTL
	}
	s := &Session{
		id:            id,
		userID:        userID,
		device:        device,
		refreshDigest: DigestRefreshToken(refreshToken),
		status:        SessionStatusActive,
		createdAt:     now,
		lastUsedAt:    now,
		expiresAt:     now.Add(ttl),
	}
	s.events.Record(SessionStarted{SessionID: id, UserID: userID, At: now})
	return s, nil
}

func (s *Session) Rotate(presented, next string, ttl time.Duration, now time.Time) error {
	if s.status == SessionStatusRevoked {
		return ErrSessionRevoked
	}
	if !now.Before(s.expiresAt) {
		return ErrSessionExpired
	}
	if next == "" {
		return ErrInvalidSecret
	}
	if ttl <= 0 {
		return ErrInvalidTTL
	}
	if subtle.ConstantTimeCompare([]byte(DigestRefreshToken(presented)), []byte(s.refreshDigest)) != 1 {
		s.revoke(RevokedByReuse, now)
		return ErrRefreshTokenReused
	}
	s.refreshDigest = DigestRefreshToken(next)
	s.lastUsedAt = now
	s.expiresAt = now.Add(ttl)
	return nil
}

func (s *Session) Revoke(reason RevokeReason, now time.Time) {
	if s.status == SessionStatusRevoked {
		return
	}
	s.revoke(reason, now)
}

func (s *Session) revoke(reason RevokeReason, now time.Time) {
	s.status = SessionStatusRevoked
	s.revokedAt = now
	s.revokeReason = reason
	s.events.Record(SessionRevoked{SessionID: s.id, UserID: s.userID, Reason: reason, At: now})
}

func (s *Session) BelongsTo(userID kernel.UserID) bool { return s.userID == userID }

func (s *Session) IsActive(now time.Time) bool {
	return s.status == SessionStatusActive && now.Before(s.expiresAt)
}

func (s *Session) ID() SessionID { return s.id }

func (s *Session) UserID() kernel.UserID { return s.userID }

func (s *Session) Device() Device { return s.device }

func (s *Session) RefreshDigest() string { return s.refreshDigest }

func (s *Session) Status() SessionStatus { return s.status }

func (s *Session) RevokeReason() RevokeReason { return s.revokeReason }

func (s *Session) ExpiresAt() time.Time { return s.expiresAt }

func (s *Session) Version() int { return s.version }

func (s *Session) AdvanceVersion() { s.version++ }

func (s *Session) PullEvents() []kernel.DomainEvent { return s.events.Pull() }

type SessionSnapshot struct {
	ID            string
	UserID        string
	DeviceName    string
	UserAgent     string
	IP            string
	RefreshDigest string
	Status        string
	CreatedAt     time.Time
	LastUsedAt    time.Time
	ExpiresAt     time.Time
	RevokedAt     time.Time
	RevokeReason  string
	Version       int
}

func (s *Session) Snapshot() SessionSnapshot {
	return SessionSnapshot{
		ID:            s.id.String(),
		UserID:        s.userID.String(),
		DeviceName:    s.device.Name,
		UserAgent:     s.device.UserAgent,
		IP:            s.device.IP,
		RefreshDigest: s.refreshDigest,
		Status:        string(s.status),
		CreatedAt:     s.createdAt,
		LastUsedAt:    s.lastUsedAt,
		ExpiresAt:     s.expiresAt,
		RevokedAt:     s.revokedAt,
		RevokeReason:  string(s.revokeReason),
		Version:       s.version,
	}
}

func RehydrateSession(snap SessionSnapshot) (*Session, error) {
	id, err := ParseSessionID(snap.ID)
	if err != nil {
		return nil, fmt.Errorf("rehydrate session: %w", err)
	}
	userID, err := kernel.ParseUserID(snap.UserID)
	if err != nil {
		return nil, fmt.Errorf("rehydrate session %s user: %w", snap.ID, err)
	}
	return &Session{
		id:            id,
		userID:        userID,
		device:        Device{Name: snap.DeviceName, UserAgent: snap.UserAgent, IP: snap.IP},
		refreshDigest: snap.RefreshDigest,
		status:        SessionStatus(snap.Status),
		createdAt:     snap.CreatedAt,
		lastUsedAt:    snap.LastUsedAt,
		expiresAt:     snap.ExpiresAt,
		revokedAt:     snap.RevokedAt,
		revokeReason:  RevokeReason(snap.RevokeReason),
		version:       snap.Version,
	}, nil
}
