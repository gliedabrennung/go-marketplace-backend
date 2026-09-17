package throttle_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/infrastructure/throttle"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/ratelimit"
)

type recordingLimiter struct {
	subjects []string
	decision ratelimit.Decision
	err      error
}

func (l *recordingLimiter) Allow(_ context.Context, _ ratelimit.Limit, subject string) (ratelimit.Decision, error) {
	l.subjects = append(l.subjects, subject)
	return l.decision, l.err
}

func TestAttemptLimiter(t *testing.T) {
	inner := &recordingLimiter{decision: ratelimit.Decision{Allowed: true}}
	limiter := throttle.NewAttemptLimiter(inner, throttle.DefaultLimits())

	require.NoError(t, limiter.Allow(context.Background(), application.ActionSignIn, "buyer@example.kz"))
	require.Len(t, inner.subjects, 1)
	assert.Len(t, inner.subjects[0], 32)
	assert.False(t, strings.Contains(inner.subjects[0], "buyer"), "subject must not contain personal data")

	inner.decision = ratelimit.Decision{Allowed: false, RetryAfter: time.Minute}
	err := limiter.Allow(context.Background(), application.ActionConfirmationRequest, "buyer@example.kz")
	require.Error(t, err)
	assert.Equal(t, kernel.KindRateLimited, kernel.KindOf(err))

	inner.err = errors.New("redis down")
	require.ErrorContains(t, limiter.Allow(context.Background(), application.ActionSignIn, "x"), "redis down")

	require.Error(t, limiter.Allow(context.Background(), "unknown", "x"))
}
