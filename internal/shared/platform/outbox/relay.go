package outbox

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/postgres"
)

type Publisher interface {
	Publish(ctx context.Context, msgs []Message) []error
}

type RelayConfig struct {
	BatchSize      int
	PollInterval   time.Duration
	MaxAttempts    int
	BaseBackoff    time.Duration
	MaxBackoff     time.Duration
	PublishTimeout time.Duration
}

func DefaultRelayConfig() RelayConfig {
	return RelayConfig{
		BatchSize:      100,
		PollInterval:   500 * time.Millisecond,
		MaxAttempts:    10,
		BaseBackoff:    time.Second,
		MaxBackoff:     5 * time.Minute,
		PublishTimeout: 30 * time.Second,
	}
}

type RelayMetrics struct {
	lag       prometheus.Gauge
	stuck     prometheus.Gauge
	published prometheus.Counter
	failed    prometheus.Counter
}

func NewRelayMetrics(reg prometheus.Registerer) *RelayMetrics {
	m := &RelayMetrics{
		lag: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "outbox_lag_seconds",
			Help: "Age of the oldest unpublished outbox record.",
		}),
		stuck: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "outbox_stuck_messages",
			Help: "Outbox records that exhausted publish attempts and require manual review.",
		}),
		published: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "outbox_published_total",
			Help: "Outbox records published to the broker.",
		}),
		failed: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "outbox_publish_failures_total",
			Help: "Failed outbox publish attempts.",
		}),
	}
	reg.MustRegister(m.lag, m.stuck, m.published, m.failed)
	return m
}

type Relay struct {
	pool    *pgxpool.Pool
	table   string
	lockKey int64
	pub     Publisher
	cfg     RelayConfig
	log     *slog.Logger
	metrics *RelayMetrics
}

func NewRelay(pool *pgxpool.Pool, schema, table string, pub Publisher, cfg RelayConfig, log *slog.Logger, metrics *RelayMetrics) *Relay {
	return &Relay{
		pool:    pool,
		table:   pgx.Identifier{schema, table}.Sanitize(),
		lockKey: postgres.AdvisoryLockKey("outbox-dispatch:" + schema + "." + table),
		pub:     pub,
		cfg:     cfg,
		log:     log,
		metrics: metrics,
	}
}

func (r *Relay) Run(ctx context.Context) error {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
		}

		n, err := r.ProcessBatch(ctx)
		if err != nil && ctx.Err() == nil {
			r.log.ErrorContext(ctx, "outbox relay batch failed", "err", err)
		}
		if gErr := r.refreshGauges(ctx); gErr != nil && ctx.Err() == nil {
			r.log.WarnContext(ctx, "outbox gauges refresh failed", "err", gErr)
		}

		next := r.cfg.PollInterval
		if err == nil && n == r.cfg.BatchSize {
			next = 0
		}
		timer.Reset(next)
	}
}

func (r *Relay) ProcessBatch(ctx context.Context) (int, error) {
	processed := 0
	err := postgres.InTx(ctx, r.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		var locked bool
		if err := tx.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock($1)", r.lockKey).Scan(&locked); err != nil {
			return fmt.Errorf("acquire relay lock: %w", err)
		}
		if !locked {
			return nil
		}

		msgs, err := r.fetch(ctx, tx)
		if err != nil || len(msgs) == 0 {
			return err
		}
		processed = len(msgs)

		pubCtx, cancel := context.WithTimeout(ctx, r.cfg.PublishTimeout)
		errs := r.pub.Publish(pubCtx, msgs)
		cancel()
		if len(errs) != len(msgs) {
			return fmt.Errorf("publisher returned %d results for %d messages", len(errs), len(msgs))
		}
		return r.record(ctx, tx, msgs, errs)
	})
	return processed, err
}

func (r *Relay) fetch(ctx context.Context, tx pgx.Tx) ([]Message, error) {
	rows, err := tx.Query(ctx,
		"SELECT id, aggregate_id, event_name, payload, metadata, created_at, attempts FROM "+r.table+
			" WHERE published_at IS NULL AND attempts < $1 AND next_attempt_at <= now()"+
			" ORDER BY id LIMIT $2 FOR UPDATE SKIP LOCKED",
		r.cfg.MaxAttempts, r.cfg.BatchSize,
	)
	if err != nil {
		return nil, fmt.Errorf("select outbox: %w", err)
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		var (
			m    Message
			meta []byte
		)
		if err := rows.Scan(&m.ID, &m.AggregateID, &m.EventName, &m.Payload, &meta, &m.CreatedAt, &m.Attempts); err != nil {
			return nil, fmt.Errorf("scan outbox: %w", err)
		}
		if err := json.Unmarshal(meta, &m.Metadata); err != nil {
			return nil, fmt.Errorf("decode outbox metadata %d: %w", m.ID, err)
		}
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate outbox: %w", err)
	}
	return msgs, nil
}

func (r *Relay) record(ctx context.Context, tx pgx.Tx, msgs []Message, errs []error) error {
	published := make([]int64, 0, len(msgs))
	for i, m := range msgs {
		if errs[i] == nil {
			published = append(published, m.ID)
			continue
		}
		attempts := m.Attempts + 1
		delay := r.backoff(attempts)
		r.metrics.failed.Inc()
		if attempts >= r.cfg.MaxAttempts {
			r.log.ErrorContext(ctx, "outbox message exhausted publish attempts",
				"outbox_id", m.ID, "event_name", m.EventName, "aggregate_id", m.AggregateID, "err", errs[i])
		}
		_, err := tx.Exec(ctx,
			"UPDATE "+r.table+" SET attempts = $2, last_error = $3, next_attempt_at = now() + make_interval(secs => $4) WHERE id = $1",
			m.ID, attempts, errs[i].Error(), delay.Seconds(),
		)
		if err != nil {
			return fmt.Errorf("record outbox failure %d: %w", m.ID, err)
		}
	}
	if len(published) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, "UPDATE "+r.table+" SET published_at = now() WHERE id = ANY($1)", published); err != nil {
		return fmt.Errorf("mark outbox published: %w", err)
	}
	r.metrics.published.Add(float64(len(published)))
	return nil
}

func (r *Relay) backoff(attempt int) time.Duration {
	d := r.cfg.BaseBackoff
	for i := 1; i < attempt; i++ {
		d *= 2
		if d >= r.cfg.MaxBackoff {
			return r.cfg.MaxBackoff
		}
	}
	return d
}

func (r *Relay) refreshGauges(ctx context.Context) error {
	var (
		lag   float64
		stuck int64
	)
	err := r.pool.QueryRow(ctx,
		"SELECT COALESCE(EXTRACT(EPOCH FROM now() - min(created_at)), 0)::float8,"+
			" count(*) FILTER (WHERE attempts >= $1) FROM "+r.table+" WHERE published_at IS NULL",
		r.cfg.MaxAttempts,
	).Scan(&lag, &stuck)
	if err != nil {
		return fmt.Errorf("query outbox gauges: %w", err)
	}
	r.metrics.lag.Set(lag)
	r.metrics.stuck.Set(float64(stuck))
	return nil
}

func DeletePublished(ctx context.Context, q postgres.Querier, schema, table string, before time.Time) (int64, error) {
	tag, err := q.Exec(ctx,
		"DELETE FROM "+pgx.Identifier{schema, table}.Sanitize()+" WHERE published_at IS NOT NULL AND published_at < $1",
		before,
	)
	if err != nil {
		return 0, fmt.Errorf("delete published outbox: %w", err)
	}
	return tag.RowsAffected(), nil
}
