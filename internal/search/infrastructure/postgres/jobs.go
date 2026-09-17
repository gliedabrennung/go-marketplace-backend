package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/gliedabrennung/go-marketplace-backend/internal/search/application"
	platform "github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/postgres"
)

type ReindexJobs struct {
	db platform.Querier
}

func NewReindexJobs(db platform.Querier) *ReindexJobs {
	return &ReindexJobs{db: db}
}

func (j *ReindexJobs) Create(ctx context.Context, job application.ReindexJob) error {
	_, err := j.db.Exec(ctx, `
		INSERT INTO search.reindex_jobs (id, status, requested_by, target, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $5)`,
		job.ID, job.Status, job.RequestedBy, job.Target, job.CreatedAt.UTC())
	if err != nil {
		return fmt.Errorf("create reindex job: %w", err)
	}
	return nil
}

func (j *ReindexJobs) Claim(ctx context.Context, at time.Time) (application.ReindexJob, bool, error) {
	job, err := scanJob(j.db.QueryRow(ctx, `
		UPDATE search.reindex_jobs SET status = 'running', started_at = $1, updated_at = $1
		WHERE id = (
			SELECT id FROM search.reindex_jobs WHERE status = 'pending'
			ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1
		)
		RETURNING `+jobColumns, at.UTC()))
	if errors.Is(err, pgx.ErrNoRows) {
		return application.ReindexJob{}, false, nil
	}
	if err != nil {
		return application.ReindexJob{}, false, fmt.Errorf("claim reindex job: %w", err)
	}
	return job, true, nil
}

func (j *ReindexJobs) Progress(ctx context.Context, jobID string, processed int, at time.Time) error {
	_, err := j.db.Exec(ctx,
		`UPDATE search.reindex_jobs SET processed = $2, updated_at = $3 WHERE id = $1`, jobID, processed, at.UTC())
	if err != nil {
		return fmt.Errorf("update reindex progress: %w", err)
	}
	return nil
}

func (j *ReindexJobs) Finish(ctx context.Context, jobID, status, reason string, at time.Time) error {
	var failure *string
	if reason != "" {
		failure = &reason
	}
	_, err := j.db.Exec(ctx, `
		UPDATE search.reindex_jobs SET status = $2, failure_reason = $3, finished_at = $4, updated_at = $4
		WHERE id = $1`, jobID, status, failure, at.UTC())
	if err != nil {
		return fmt.Errorf("finish reindex job: %w", err)
	}
	return nil
}

func (j *ReindexJobs) Get(ctx context.Context, jobID string) (application.ReindexJob, error) {
	job, err := scanJob(j.db.QueryRow(ctx, `SELECT `+jobColumns+` FROM search.reindex_jobs WHERE id = $1`, jobID))
	if errors.Is(err, pgx.ErrNoRows) {
		return application.ReindexJob{}, application.ErrReindexJobNotFound
	}
	if err != nil {
		return application.ReindexJob{}, fmt.Errorf("select reindex job: %w", err)
	}
	return job, nil
}

func (j *ReindexJobs) HasUnfinished(ctx context.Context) (bool, error) {
	var exists bool
	err := j.db.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM search.reindex_jobs WHERE status IN ('pending', 'running'))`).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check reindex jobs: %w", err)
	}
	return exists, nil
}

const jobColumns = `id::text, status, requested_by::text, target, processed,
	COALESCE(failure_reason, ''), created_at, started_at, finished_at`

func scanJob(row pgx.Row) (application.ReindexJob, error) {
	var (
		job               application.ReindexJob
		started, finished *time.Time
	)
	err := row.Scan(&job.ID, &job.Status, &job.RequestedBy, &job.Target, &job.Processed,
		&job.FailureReason, &job.CreatedAt, &started, &finished)
	if err != nil {
		return application.ReindexJob{}, err
	}
	job.CreatedAt = job.CreatedAt.UTC()
	if started != nil {
		job.StartedAt = started.UTC()
	}
	if finished != nil {
		job.FinishedAt = finished.UTC()
	}
	return job, nil
}
