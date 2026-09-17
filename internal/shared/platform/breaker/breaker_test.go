package breaker_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/breaker"
)

var (
	errBoom   = errors.New("boom")
	errReject = errors.New("rejected")
)

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func fail(context.Context) error { return errBoom }

func succeed(context.Context) error { return nil }

func TestBreakerOpensAfterConsecutiveFailures(t *testing.T) {
	clk := &fakeClock{now: time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)}
	b := breaker.New(breaker.Settings{Failures: 3, Cooldown: time.Minute, Now: clk.Now})
	ctx := context.Background()

	for range 2 {
		require.ErrorIs(t, b.Do(ctx, fail), errBoom)
	}
	require.NoError(t, b.Do(ctx, succeed))
	for range 3 {
		require.ErrorIs(t, b.Do(ctx, fail), errBoom)
	}
	assert.Equal(t, breaker.StateOpen, b.State())

	called := false
	err := b.Do(ctx, func(context.Context) error { called = true; return nil })
	require.ErrorIs(t, err, breaker.ErrOpen)
	assert.False(t, called)
}

func TestBreakerHalfOpenProbe(t *testing.T) {
	clk := &fakeClock{now: time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)}
	var states []breaker.State
	b := breaker.New(breaker.Settings{
		Failures: 1, Cooldown: time.Minute, Now: clk.Now,
		OnChange: func(s breaker.State) { states = append(states, s) },
	})
	ctx := context.Background()

	require.ErrorIs(t, b.Do(ctx, fail), errBoom)
	clk.Advance(time.Minute)
	require.ErrorIs(t, b.Do(ctx, fail), errBoom)
	assert.Equal(t, breaker.StateOpen, b.State())

	clk.Advance(time.Minute)
	release := make(chan struct{})
	started := make(chan struct{})
	done := make(chan error)
	go func() {
		done <- b.Do(ctx, func(context.Context) error {
			close(started)
			<-release
			return nil
		})
	}()
	<-started
	assert.Equal(t, breaker.StateHalfOpen, b.State())
	require.ErrorIs(t, b.Do(ctx, succeed), breaker.ErrOpen)
	close(release)
	require.NoError(t, <-done)
	assert.Equal(t, breaker.StateClosed, b.State())
	assert.Equal(t, []breaker.State{
		breaker.StateOpen, breaker.StateHalfOpen, breaker.StateOpen, breaker.StateHalfOpen, breaker.StateClosed,
	}, states)
}

func TestBreakerIgnoresBusinessErrors(t *testing.T) {
	b := breaker.New(breaker.Settings{Failures: 1, Ignore: func(err error) bool { return errors.Is(err, errReject) }})
	ctx := context.Background()
	for range 3 {
		require.ErrorIs(t, b.Do(ctx, func(context.Context) error { return errReject }), errReject)
	}
	assert.Equal(t, breaker.StateClosed, b.State())
}
