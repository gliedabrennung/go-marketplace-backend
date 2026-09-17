package domain

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type challengeTag struct{}

type ChallengeID = kernel.ID[challengeTag]

func NewChallengeID() ChallengeID { return kernel.NewID[challengeTag]() }

func ParseChallengeID(s string) (ChallengeID, error) { return kernel.ParseID[challengeTag](s) }

type ChallengePurpose string

const PurposeEmailConfirmation ChallengePurpose = "email_confirmation"

type ChallengeStatus string

const (
	ChallengePending   ChallengeStatus = "pending"
	ChallengeVerified  ChallengeStatus = "verified"
	ChallengeExhausted ChallengeStatus = "exhausted"
)

type ChallengePolicy struct {
	TTL         time.Duration
	MaxAttempts int
}

func EmailConfirmationPolicy() ChallengePolicy {
	return ChallengePolicy{TTL: 24 * time.Hour, MaxAttempts: 5}
}

type Challenge struct {
	id           ChallengeID
	purpose      ChallengePurpose
	target       string
	userID       kernel.UserID
	secretDigest string
	status       ChallengeStatus
	attempts     int
	maxAttempts  int
	expiresAt    time.Time
	createdAt    time.Time
	updatedAt    time.Time
	version      int
}

func IssueEmailConfirmationChallenge(id ChallengeID, userID kernel.UserID, email Email, secret string, policy ChallengePolicy, now time.Time) (*Challenge, error) {
	if id.IsZero() || userID.IsZero() {
		return nil, kernel.ErrInvalidID
	}
	if email.IsZero() {
		return nil, ErrInvalidEmail
	}
	if secret == "" {
		return nil, ErrInvalidSecret
	}
	if policy.TTL <= 0 || policy.MaxAttempts <= 0 {
		return nil, ErrInvalidTTL
	}
	return &Challenge{
		id:           id,
		purpose:      PurposeEmailConfirmation,
		target:       email.String(),
		userID:       userID,
		secretDigest: DigestChallengeSecret(id, secret),
		status:       ChallengePending,
		maxAttempts:  policy.MaxAttempts,
		expiresAt:    now.Add(policy.TTL),
		createdAt:    now,
		updatedAt:    now,
	}, nil
}

func DigestChallengeSecret(id ChallengeID, secret string) string {
	sum := sha256.Sum256([]byte(id.String() + ":" + secret))
	return hex.EncodeToString(sum[:])
}

func (c *Challenge) Verify(secret string, now time.Time) error {
	switch c.status {
	case ChallengeVerified:
		return ErrChallengeAlreadyUsed
	case ChallengeExhausted:
		return ErrChallengeExhausted
	}
	if !now.Before(c.expiresAt) {
		return ErrChallengeExpired
	}
	c.attempts++
	c.updatedAt = now
	presented := DigestChallengeSecret(c.id, secret)
	if subtle.ConstantTimeCompare([]byte(presented), []byte(c.secretDigest)) != 1 {
		if c.attempts >= c.maxAttempts {
			c.status = ChallengeExhausted
		}
		return ErrInvalidConfirmationToken
	}
	c.status = ChallengeVerified
	return nil
}

func (c *Challenge) ID() ChallengeID { return c.id }

func (c *Challenge) Purpose() ChallengePurpose { return c.purpose }

func (c *Challenge) Target() string { return c.target }

func (c *Challenge) UserID() kernel.UserID { return c.userID }

func (c *Challenge) Status() ChallengeStatus { return c.status }

func (c *Challenge) Attempts() int { return c.attempts }

func (c *Challenge) ExpiresAt() time.Time { return c.expiresAt }

func (c *Challenge) Version() int { return c.version }

func (c *Challenge) AdvanceVersion() { c.version++ }

type ChallengeSnapshot struct {
	ID           string
	Purpose      string
	Target       string
	UserID       string
	SecretDigest string
	Status       string
	Attempts     int
	MaxAttempts  int
	ExpiresAt    time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
	Version      int
}

func (c *Challenge) Snapshot() ChallengeSnapshot {
	return ChallengeSnapshot{
		ID:           c.id.String(),
		Purpose:      string(c.purpose),
		Target:       c.target,
		UserID:       c.userID.String(),
		SecretDigest: c.secretDigest,
		Status:       string(c.status),
		Attempts:     c.attempts,
		MaxAttempts:  c.maxAttempts,
		ExpiresAt:    c.expiresAt,
		CreatedAt:    c.createdAt,
		UpdatedAt:    c.updatedAt,
		Version:      c.version,
	}
}

func RehydrateChallenge(s ChallengeSnapshot) (*Challenge, error) {
	id, err := ParseChallengeID(s.ID)
	if err != nil {
		return nil, fmt.Errorf("rehydrate challenge: %w", err)
	}
	userID, err := kernel.ParseUserID(s.UserID)
	if err != nil {
		return nil, fmt.Errorf("rehydrate challenge %s user: %w", s.ID, err)
	}
	return &Challenge{
		id:           id,
		purpose:      ChallengePurpose(s.Purpose),
		target:       s.Target,
		userID:       userID,
		secretDigest: s.SecretDigest,
		status:       ChallengeStatus(s.Status),
		attempts:     s.Attempts,
		maxAttempts:  s.MaxAttempts,
		expiresAt:    s.ExpiresAt,
		createdAt:    s.CreatedAt,
		updatedAt:    s.UpdatedAt,
		version:      s.Version,
	}, nil
}
