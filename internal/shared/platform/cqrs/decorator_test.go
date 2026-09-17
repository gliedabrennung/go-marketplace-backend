package cqrs_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/cqrs"
)

type PlaceSample struct{ Fail error }

type recordingLog struct {
	mu     sync.Mutex
	levels []string
}

func (l *recordingLog) add(level string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.levels = append(l.levels, level)
}

func (l *recordingLog) DebugContext(context.Context, string, ...any) { l.add("debug") }
func (l *recordingLog) WarnContext(context.Context, string, ...any)  { l.add("warn") }
func (l *recordingLog) ErrorContext(context.Context, string, ...any) { l.add("error") }

type recordingMetrics struct {
	name string
	ok   bool
}

func (m *recordingMetrics) ObserveCommand(name string, _ time.Duration, ok bool) {
	m.name, m.ok = name, ok
}

func TestDecorate(t *testing.T) {
	inner := cqrs.HandlerFunc[PlaceSample, string](func(_ context.Context, cmd PlaceSample) (string, error) {
		if cmd.Fail != nil {
			return "", cmd.Fail
		}
		return "done", nil
	})

	cases := []struct {
		name      string
		fail      error
		wantLevel string
		wantOK    bool
	}{
		{name: "success", wantLevel: "debug", wantOK: true},
		{name: "business rejection", fail: kernel.BusinessRule("RULE", "rule"), wantLevel: "warn", wantOK: true},
		{name: "internal failure", fail: errors.New("db down"), wantLevel: "error", wantOK: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			log := &recordingLog{}
			m := &recordingMetrics{}
			h := cqrs.Decorate("ordering", inner, log, m)

			res, err := h.Handle(context.Background(), PlaceSample{Fail: tc.fail})
			if tc.fail == nil {
				require.NoError(t, err)
				assert.Equal(t, "done", res)
			} else {
				require.ErrorIs(t, err, tc.fail)
			}
			assert.Equal(t, "ordering.PlaceSample", m.name)
			assert.Equal(t, tc.wantOK, m.ok)
			assert.Equal(t, tc.wantLevel, log.levels[len(log.levels)-1])
		})
	}
}
