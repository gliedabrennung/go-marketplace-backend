package breaker

import (
	"context"
	"errors"
	"sync"
	"time"
)

var ErrOpen = errors.New("circuit breaker is open")

type State string

const (
	StateClosed   State = "closed"
	StateOpen     State = "open"
	StateHalfOpen State = "half_open"
)

type Settings struct {
	Failures int
	Cooldown time.Duration
	Now      func() time.Time
	Ignore   func(error) bool
	OnChange func(State)
}

type Breaker struct {
	mu       sync.Mutex
	settings Settings
	state    State
	failures int
	openedAt time.Time
	probing  bool
}

func New(settings Settings) *Breaker {
	if settings.Failures <= 0 {
		settings.Failures = 5
	}
	if settings.Cooldown <= 0 {
		settings.Cooldown = 30 * time.Second
	}
	if settings.Now == nil {
		settings.Now = time.Now
	}
	if settings.Ignore == nil {
		settings.Ignore = func(error) bool { return false }
	}
	if settings.OnChange == nil {
		settings.OnChange = func(State) {}
	}
	return &Breaker{settings: settings, state: StateClosed}
}

func (b *Breaker) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	if err := b.acquire(); err != nil {
		return err
	}
	err := fn(ctx)
	b.release(err)
	return err
}

func (b *Breaker) State() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

func (b *Breaker) acquire() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case StateOpen:
		if b.settings.Now().Sub(b.openedAt) < b.settings.Cooldown {
			return ErrOpen
		}
		b.probing = true
		b.move(StateHalfOpen)
	case StateHalfOpen:
		if b.probing {
			return ErrOpen
		}
		b.probing = true
	}
	return nil
}

func (b *Breaker) release(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.probing = false
	if err == nil || b.settings.Ignore(err) {
		b.failures = 0
		b.move(StateClosed)
		return
	}
	b.failures++
	if b.state == StateHalfOpen || b.failures >= b.settings.Failures {
		b.openedAt = b.settings.Now()
		b.move(StateOpen)
	}
}

func (b *Breaker) move(state State) {
	if b.state == state {
		return
	}
	b.state = state
	b.settings.OnChange(state)
}
