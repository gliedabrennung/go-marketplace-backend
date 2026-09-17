package throttle

import (
	"context"
	"fmt"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/ratelimit"
)

var ImportLimit = ratelimit.Limit{Name: "catalog.offer_import", Max: 10, Window: time.Hour}

type windowLimiter interface {
	Allow(ctx context.Context, limit ratelimit.Limit, subject string) (ratelimit.Decision, error)
}

type ImportLimiter struct {
	limiter windowLimiter
	limit   ratelimit.Limit
}

func NewImportLimiter(limiter windowLimiter, limit ratelimit.Limit) *ImportLimiter {
	return &ImportLimiter{limiter: limiter, limit: limit}
}

func (l *ImportLimiter) Allow(ctx context.Context, sellerID string) error {
	decision, err := l.limiter.Allow(ctx, l.limit, sellerID)
	if err != nil {
		return fmt.Errorf("check import limit: %w", err)
	}
	return decision.Err(l.limit)
}
