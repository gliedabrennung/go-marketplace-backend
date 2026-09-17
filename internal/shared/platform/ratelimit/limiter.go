package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type Limit struct {
	Name   string
	Max    int
	Window time.Duration
}

type Decision struct {
	Allowed    bool
	RetryAfter time.Duration
}

type ExceededError struct {
	limit      string
	retryAfter time.Duration
}

func (e *ExceededError) Error() string {
	return fmt.Sprintf("rate limit %s exceeded, retry after %s", e.limit, e.retryAfter)
}

func (e *ExceededError) Kind() kernel.ErrorKind { return kernel.KindRateLimited }

func (e *ExceededError) Code() string { return "RATE_LIMIT_EXCEEDED" }

func (e *ExceededError) Message() string { return "too many requests" }

func (e *ExceededError) RetryAfter() time.Duration { return e.retryAfter }

func (d Decision) Err(limit Limit) error {
	if d.Allowed {
		return nil
	}
	return &ExceededError{limit: limit.Name, retryAfter: d.RetryAfter}
}

var slidingWindow = redis.NewScript(`
local key = KEYS[1]
local now = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local limit = tonumber(ARGV[3])
local member = ARGV[4]
redis.call('ZREMRANGEBYSCORE', key, 0, now - window)
local count = redis.call('ZCARD', key)
if count >= limit then
  local oldest = redis.call('ZRANGE', key, 0, 0, 'WITHSCORES')
  local retry = window
  if oldest[2] then
    retry = window - (now - tonumber(oldest[2]))
  end
  return {0, retry}
end
redis.call('ZADD', key, now, member)
redis.call('PEXPIRE', key, window)
return {1, 0}
`)

type Limiter struct {
	rdb    redis.UniversalClient
	prefix string
	now    func() time.Time
}

func NewLimiter(rdb redis.UniversalClient, prefix string) *Limiter {
	return &Limiter{rdb: rdb, prefix: prefix, now: time.Now}
}

func (l *Limiter) Allow(ctx context.Context, limit Limit, subject string) (Decision, error) {
	key := l.prefix + ":" + limit.Name + ":" + subject
	now := l.now().UnixMilli()
	res, err := slidingWindow.Run(ctx, l.rdb, []string{key},
		now, limit.Window.Milliseconds(), limit.Max, fmt.Sprintf("%d-%s", now, kernel.NewID[struct{}]().String()),
	).Int64Slice()
	if err != nil {
		return Decision{}, fmt.Errorf("rate limit %s: %w", limit.Name, err)
	}
	if len(res) != 2 {
		return Decision{}, fmt.Errorf("rate limit %s: unexpected script result %v", limit.Name, res)
	}
	return Decision{Allowed: res[0] == 1, RetryAfter: time.Duration(res[1]) * time.Millisecond}, nil
}

func (l *Limiter) Reset(ctx context.Context, limit Limit, subject string) error {
	if err := l.rdb.Del(ctx, l.prefix+":"+limit.Name+":"+subject).Err(); err != nil {
		return fmt.Errorf("reset rate limit %s: %w", limit.Name, err)
	}
	return nil
}
