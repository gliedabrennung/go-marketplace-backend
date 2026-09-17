package throttle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/ratelimit"
)

type windowLimiter interface {
	Allow(ctx context.Context, limit ratelimit.Limit, subject string) (ratelimit.Decision, error)
}

type AttemptLimiter struct {
	limiter windowLimiter
	limits  map[application.LimitedAction]ratelimit.Limit
}

func DefaultLimits() map[application.LimitedAction]ratelimit.Limit {
	return map[application.LimitedAction]ratelimit.Limit{
		application.ActionConfirmationRequest: {Name: "identity.confirmation_request", Max: 3, Window: 10 * time.Minute},
		application.ActionSignIn:              {Name: "identity.sign_in", Max: 5, Window: 15 * time.Minute},
	}
}

func NewAttemptLimiter(limiter windowLimiter, limits map[application.LimitedAction]ratelimit.Limit) *AttemptLimiter {
	return &AttemptLimiter{limiter: limiter, limits: limits}
}

func (a *AttemptLimiter) Allow(ctx context.Context, action application.LimitedAction, subject string) error {
	limit, ok := a.limits[action]
	if !ok {
		return fmt.Errorf("no rate limit configured for action %q", action)
	}
	sum := sha256.Sum256([]byte(subject))
	decision, err := a.limiter.Allow(ctx, limit, hex.EncodeToString(sum[:16]))
	if err != nil {
		return fmt.Errorf("check %s limit: %w", action, err)
	}
	return decision.Err(limit)
}
