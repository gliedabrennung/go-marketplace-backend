package domain

import (
	"context"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type UserRepository interface {
	FindByID(ctx context.Context, id kernel.UserID) (*User, error)
	FindByVerifiedEmail(ctx context.Context, email Email) (*User, error)
	Save(ctx context.Context, u *User) error
}

type ChallengeRepository interface {
	FindByID(ctx context.Context, id ChallengeID) (*Challenge, error)
	Save(ctx context.Context, c *Challenge) error
}

type SessionRepository interface {
	FindByID(ctx context.Context, id SessionID) (*Session, error)
	FindByRefreshDigest(ctx context.Context, digest string) (*Session, error)
	Save(ctx context.Context, s *Session) error
}
