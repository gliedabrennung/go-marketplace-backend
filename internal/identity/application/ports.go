package application

import (
	"context"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

type Clock interface {
	Now() time.Time
}

type PasswordHasher interface {
	Hash(password domain.Password) (domain.PasswordHash, error)
	Verify(hash domain.PasswordHash, password string) (bool, error)
}

type SecretGenerator interface {
	Token() (string, error)
}

type ConfirmationSender interface {
	SendEmailConfirmation(ctx context.Context, email domain.Email, token string) error
}

type LimitedAction string

const (
	ActionConfirmationRequest LimitedAction = "confirmation_request"
	ActionSignIn              LimitedAction = "sign_in"
)

type AttemptLimiter interface {
	Allow(ctx context.Context, action LimitedAction, subject string) error
}

type AccessTokenIssuer interface {
	Issue(p auth.Principal, now time.Time) (string, time.Time, error)
}

type AuditEntry struct {
	ActorID    string
	ActorRoles []string
	Action     string
	ObjectType string
	ObjectID   string
	Details    map[string]string
	OccurredAt time.Time
}

type AuditTrail interface {
	Record(ctx context.Context, e AuditEntry) error
}

type Repositories interface {
	Users() domain.UserRepository
	Sessions() domain.SessionRepository
	Challenges() domain.ChallengeRepository
	Audit() AuditTrail
}

type UnitOfWork interface {
	Do(ctx context.Context, fn func(ctx context.Context, repos Repositories) error) error
}

type Policy struct {
	EmailConfirmation domain.ChallengePolicy
	RefreshTTL        time.Duration
}

func DefaultPolicy() Policy {
	return Policy{
		EmailConfirmation: domain.EmailConfirmationPolicy(),
		RefreshTTL:        30 * 24 * time.Hour,
	}
}

type AuthTokens struct {
	UserID           string
	SessionID        string
	AccessToken      string
	AccessExpiresAt  time.Time
	RefreshToken     string
	RefreshExpiresAt time.Time
}
