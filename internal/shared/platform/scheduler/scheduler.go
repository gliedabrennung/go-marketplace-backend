package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/postgres"
)

type Job struct {
	Name     string
	Interval time.Duration
	Run      func(ctx context.Context) error
}

type Scheduler struct {
	jobs []Job
	log  *slog.Logger
}

func New(log *slog.Logger, jobs ...Job) *Scheduler {
	return &Scheduler{jobs: jobs, log: log}
}

func (s *Scheduler) Run(ctx context.Context) error {
	var wg sync.WaitGroup
	for _, job := range s.jobs {
		wg.Go(func() { s.loop(ctx, job) })
	}
	wg.Wait()
	return nil
}

func (s *Scheduler) loop(ctx context.Context, job Job) {
	ticker := time.NewTicker(job.Interval)
	defer ticker.Stop()
	for {
		start := time.Now()
		if err := job.Run(ctx); err != nil && ctx.Err() == nil {
			s.log.ErrorContext(ctx, "scheduled job failed", "job", job.Name, "err", err)
		} else {
			s.log.DebugContext(ctx, "scheduled job completed", "job", job.Name, "duration_ms", time.Since(start).Milliseconds())
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func Exclusive(pool *pgxpool.Pool, name string, fn func(ctx context.Context) error) func(ctx context.Context) error {
	key := postgres.AdvisoryLockKey("job:" + name)
	return func(ctx context.Context) error {
		conn, err := pool.Acquire(ctx)
		if err != nil {
			return fmt.Errorf("acquire connection for job %s: %w", name, err)
		}
		defer conn.Release()

		var locked bool
		if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", key).Scan(&locked); err != nil {
			return fmt.Errorf("lock job %s: %w", name, err)
		}
		if !locked {
			return nil
		}
		runErr := fn(ctx)
		if _, err := conn.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", key); err != nil {
			unlockErr := fmt.Errorf("unlock job %s: %w", name, err)
			if closeErr := conn.Conn().Close(context.WithoutCancel(ctx)); closeErr != nil {
				unlockErr = errors.Join(unlockErr, fmt.Errorf("close locked connection: %w", closeErr))
			}
			return errors.Join(runErr, unlockErr)
		}
		return runErr
	}
}
