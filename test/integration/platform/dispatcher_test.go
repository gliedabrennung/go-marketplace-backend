//go:build integration

package platform_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/inbox"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/outbox"
	"github.com/gliedabrennung/go-marketplace-backend/test/testdb"
)

type deliveries struct {
	mu   sync.Mutex
	seen map[string][]int
}

func (d *deliveries) record(consumer string, msg outbox.Message) {
	var payload struct {
		Value int `json:"value"`
	}
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		panic(err)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.seen[consumer] = append(d.seen[consumer], payload.Value)
}

func (d *deliveries) of(consumer string) []int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]int(nil), d.seen[consumer]...)
}

func TestDispatcher_RetriesFailedSubscriberWithoutDuplicatesAndKeepsAggregateOrder(t *testing.T) {
	pool := testdb.Pool(t)
	testdb.Truncate(t, pool, "platform.outbox", "platform.processed_messages")
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	now := time.Now().UTC()
	writeEvents(ctx, t, pool, newWriter(),
		thingHappened{id: "agg-1", value: 1, at: now},
		thingHappened{id: "agg-1", value: 2, at: now},
		thingHappened{id: "agg-2", value: 3, at: now},
	)

	got := &deliveries{seen: map[string][]int{}}
	failNext := true
	var mu sync.Mutex
	subscriptions := []outbox.Subscription{
		{
			Consumer:   "stable",
			EventNames: []string{"sample.thing_happened.v1"},
			Handle: func(_ context.Context, msg outbox.Message) error {
				got.record("stable", msg)
				return nil
			},
		},
		{
			Consumer:   "flaky",
			EventNames: []string{"sample.thing_happened.v1"},
			Handle: func(_ context.Context, msg outbox.Message) error {
				mu.Lock()
				fail := failNext && msg.AggregateID == "agg-1"
				if fail {
					failNext = false
				}
				mu.Unlock()
				if fail {
					return errors.New("temporary failure")
				}
				got.record("flaky", msg)
				return nil
			},
		},
		{
			Consumer:   "unrelated",
			EventNames: []string{"sample.other.v1"},
			Handle: func(context.Context, outbox.Message) error {
				return errors.New("must not be called")
			},
		},
	}

	dispatcher := outbox.NewDispatcher(inbox.NewGuard(pool), log, prometheus.NewRegistry(), subscriptions...)
	relay := outbox.NewRelay(pool, "platform", "outbox", dispatcher, outbox.DefaultRelayConfig(), log, outbox.NewRelayMetrics(prometheus.NewRegistry()))

	n, err := relay.ProcessBatch(ctx)
	require.NoError(t, err)
	assert.Equal(t, 3, n)
	assert.Equal(t, []int{1, 3}, got.of("stable"))
	assert.Equal(t, []int{3}, got.of("flaky"))

	var pending int
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM platform.outbox WHERE published_at IS NULL AND aggregate_id = 'agg-1' AND attempts = 1").Scan(&pending))
	assert.Equal(t, 2, pending, "failed event and the later event of the same aggregate wait for retry")

	_, err = pool.Exec(ctx, "UPDATE platform.outbox SET next_attempt_at = now() WHERE published_at IS NULL")
	require.NoError(t, err)

	n, err = relay.ProcessBatch(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	assert.Equal(t, []int{1, 3, 2}, got.of("stable"), "already processed delivery is not repeated")
	assert.Equal(t, []int{3, 1, 2}, got.of("flaky"), "aggregate order is preserved for the retried subscriber")

	var unpublished int
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM platform.outbox WHERE published_at IS NULL").Scan(&unpublished))
	assert.Zero(t, unpublished)
}
