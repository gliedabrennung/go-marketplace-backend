//go:build integration

package platform_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/outbox"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/postgres"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/reqctx"
	"github.com/gliedabrennung/go-marketplace-backend/test/testdb"
)

type thingHappened struct {
	id    string
	value int
	at    time.Time
}

func (e thingHappened) EventName() string     { return "sample.thing_happened.v1" }
func (e thingHappened) AggregateID() string   { return e.id }
func (e thingHappened) OccurredAt() time.Time { return e.at }

type fakePublisher struct {
	mu   sync.Mutex
	fail error
	got  []outbox.Message
}

func (p *fakePublisher) Publish(_ context.Context, msgs []outbox.Message) []error {
	p.mu.Lock()
	defer p.mu.Unlock()
	errs := make([]error, len(msgs))
	for i, m := range msgs {
		if p.fail != nil {
			errs[i] = p.fail
			continue
		}
		p.got = append(p.got, m)
	}
	return errs
}

func newWriter() *outbox.Writer {
	codec := outbox.NewCodec()
	outbox.Register(codec, func(e thingHappened) any {
		return map[string]any{"id": e.id, "value": e.value}
	})
	return outbox.NewWriter("platform", "outbox", codec)
}

func writeEvents(ctx context.Context, t *testing.T, pool *pgxpool.Pool, w *outbox.Writer, events ...kernel.DomainEvent) {
	t.Helper()
	err := postgres.InTx(ctx, pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		return w.Write(ctx, tx, events)
	})
	require.NoError(t, err)
}

func newRelay(t *testing.T, pub outbox.Publisher, cfg outbox.RelayConfig) *outbox.Relay {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return outbox.NewRelay(testdb.Pool(t), "platform", "outbox", pub, cfg, log, outbox.NewRelayMetrics(prometheus.NewRegistry()))
}

func TestOutbox_RelayPublishesInOrderAndMarksPublished(t *testing.T) {
	pool := testdb.Pool(t)
	testdb.Truncate(t, pool, "platform.outbox")
	ctx := reqctx.WithRequestID(context.Background(), "req-42")

	now := time.Now().UTC()
	writeEvents(ctx, t, pool, newWriter(),
		thingHappened{id: "agg-1", value: 1, at: now},
		thingHappened{id: "agg-1", value: 2, at: now},
		thingHappened{id: "agg-2", value: 3, at: now},
	)

	pub := &fakePublisher{}
	cfg := outbox.DefaultRelayConfig()
	n, err := newRelay(t, pub, cfg).ProcessBatch(ctx)
	require.NoError(t, err)
	assert.Equal(t, 3, n)

	require.Len(t, pub.got, 3)
	assert.JSONEq(t, `{"id":"agg-1","value":1}`, string(pub.got[0].Payload))
	assert.JSONEq(t, `{"id":"agg-1","value":2}`, string(pub.got[1].Payload))
	assert.Equal(t, "sample.thing_happened.v1", pub.got[0].EventName)
	assert.Equal(t, "req-42", pub.got[0].Metadata[outbox.MetaRequestID])
	assert.NotEmpty(t, pub.got[0].Metadata[outbox.MetaOccurredAt])

	var unpublished int
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM platform.outbox WHERE published_at IS NULL").Scan(&unpublished))
	assert.Zero(t, unpublished)

	n, err = newRelay(t, pub, cfg).ProcessBatch(ctx)
	require.NoError(t, err)
	assert.Zero(t, n)
}

func TestOutbox_RelayBacksOffOnFailure(t *testing.T) {
	pool := testdb.Pool(t)
	testdb.Truncate(t, pool, "platform.outbox")
	ctx := context.Background()

	writeEvents(ctx, t, pool, newWriter(), thingHappened{id: "agg-1", value: 1, at: time.Now()})

	pub := &fakePublisher{fail: errors.New("broker unavailable")}
	cfg := outbox.DefaultRelayConfig()
	cfg.BaseBackoff = time.Hour
	relay := newRelay(t, pub, cfg)

	n, err := relay.ProcessBatch(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	var (
		attempts  int
		lastError string
		delayed   bool
	)
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT attempts, last_error, next_attempt_at > now() + interval '50 minutes' FROM platform.outbox",
	).Scan(&attempts, &lastError, &delayed))
	assert.Equal(t, 1, attempts)
	assert.Equal(t, "broker unavailable", lastError)
	assert.True(t, delayed)

	n, err = relay.ProcessBatch(ctx)
	require.NoError(t, err)
	assert.Zero(t, n, "message must not be retried before backoff elapses")
}

func TestOutbox_RelaySkipsExhaustedMessages(t *testing.T) {
	pool := testdb.Pool(t)
	testdb.Truncate(t, pool, "platform.outbox")
	ctx := context.Background()

	writeEvents(ctx, t, pool, newWriter(), thingHappened{id: "agg-1", value: 1, at: time.Now()})
	_, err := pool.Exec(ctx, "UPDATE platform.outbox SET attempts = 10")
	require.NoError(t, err)

	pub := &fakePublisher{}
	n, err := newRelay(t, pub, outbox.DefaultRelayConfig()).ProcessBatch(ctx)
	require.NoError(t, err)
	assert.Zero(t, n)
	assert.Empty(t, pub.got)
}

func TestOutbox_WriteIsRolledBackWithTransaction(t *testing.T) {
	pool := testdb.Pool(t)
	testdb.Truncate(t, pool, "platform.outbox")
	ctx := context.Background()

	errBusiness := errors.New("aggregate save failed")
	err := postgres.InTx(ctx, pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		if err := newWriter().Write(ctx, tx, []kernel.DomainEvent{thingHappened{id: "agg-1", at: time.Now()}}); err != nil {
			return err
		}
		return errBusiness
	})
	require.ErrorIs(t, err, errBusiness)

	var count int
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM platform.outbox").Scan(&count))
	assert.Zero(t, count)
}
