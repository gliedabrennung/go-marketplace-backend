package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/outbox"
	platform "github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/postgres"
)

const importColumns = `id::text, seller_id::text, requested_by::text, format, object_key, status,
	total_rows, succeeded_rows, failed_rows, errors, failure_reason, report_key,
	created_at, started_at, finished_at, updated_at, version`

type importRepository struct {
	q      platform.Querier
	events *outbox.Writer
}

func (r importRepository) FindByID(ctx context.Context, id domain.ImportJobID) (*domain.ImportJob, error) {
	snap, err := loadImportJob(ctx, r.q, id.String())
	if err != nil {
		return nil, err
	}
	return domain.RehydrateImportJob(snap)
}

func (r importRepository) Save(ctx context.Context, job *domain.ImportJob) error {
	snap := job.Snapshot()
	errorsJSON, err := json.Marshal(snap.Errors)
	if err != nil {
		return fmt.Errorf("encode import errors: %w", err)
	}
	if snap.Errors == nil {
		errorsJSON = []byte("[]")
	}
	if snap.Version == 0 {
		err = exec(ctx, r.q, "insert import job", `
			INSERT INTO catalog.import_jobs (id, seller_id, requested_by, format, object_key, status,
				total_rows, succeeded_rows, failed_rows, errors, failure_reason, report_key,
				created_at, started_at, finished_at, updated_at, version)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, 1)`,
			snap.ID, snap.SellerID, snap.RequestedBy, snap.Format, snap.ObjectKey, snap.Status,
			snap.TotalRows, snap.SucceededRows, snap.FailedRows, errorsJSON, nullable(snap.FailureReason), nullable(snap.ReportKey),
			utc(snap.CreatedAt), optionalTime(snap.StartedAt), optionalTime(snap.FinishedAt), utc(snap.UpdatedAt))
	} else {
		err = update(ctx, r.q, "update import job", `
			UPDATE catalog.import_jobs
			SET status = $2, total_rows = $3, succeeded_rows = $4, failed_rows = $5, errors = $6,
				failure_reason = $7, report_key = $8, started_at = $9, finished_at = $10, updated_at = $11, version = version + 1
			WHERE id = $1 AND version = $12`,
			snap.ID, snap.Status, snap.TotalRows, snap.SucceededRows, snap.FailedRows, errorsJSON,
			nullable(snap.FailureReason), nullable(snap.ReportKey), optionalTime(snap.StartedAt), optionalTime(snap.FinishedAt),
			utc(snap.UpdatedAt), snap.Version)
	}
	if err != nil {
		return err
	}
	job.AdvanceVersion()
	return r.events.Write(ctx, r.q, job.PullEvents())
}

func loadImportJob(ctx context.Context, q platform.Querier, id string) (domain.ImportJobSnapshot, error) {
	snap, err := scanImportJob(q.QueryRow(ctx, `SELECT `+importColumns+` FROM catalog.import_jobs WHERE id = $1`, id))
	if err != nil {
		return domain.ImportJobSnapshot{}, notFound(err, domain.ErrImportJobNotFound)
	}
	return snap, nil
}

func scanImportJob(row pgx.Row) (domain.ImportJobSnapshot, error) {
	var (
		snap              domain.ImportJobSnapshot
		failureReason     *string
		reportKey         *string
		started, finished *time.Time
		rowErrors         []domain.ImportRowError
	)
	err := row.Scan(&snap.ID, &snap.SellerID, &snap.RequestedBy, &snap.Format, &snap.ObjectKey, &snap.Status,
		&snap.TotalRows, &snap.SucceededRows, &snap.FailedRows, &rowErrors, &failureReason, &reportKey,
		&snap.CreatedAt, &started, &finished, &snap.UpdatedAt, &snap.Version)
	if err != nil {
		return domain.ImportJobSnapshot{}, err
	}
	snap.Errors = rowErrors
	snap.FailureReason, snap.ReportKey = deref(failureReason), deref(reportKey)
	snap.CreatedAt, snap.UpdatedAt = utc(snap.CreatedAt), utc(snap.UpdatedAt)
	snap.StartedAt, snap.FinishedAt = moment(started), moment(finished)
	return snap, nil
}
