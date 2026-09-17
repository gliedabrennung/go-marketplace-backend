package domain

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type ImportFormat string

const (
	ImportCSV  ImportFormat = "csv"
	ImportXLSX ImportFormat = "xlsx"
	ImportJSON ImportFormat = "json"
)

func ParseImportFormat(s string) (ImportFormat, error) {
	switch f := ImportFormat(s); f {
	case ImportCSV, ImportXLSX, ImportJSON:
		return f, nil
	default:
		return "", ErrInvalidImportFormat.WithDetail("%q", s)
	}
}

type ImportStatus string

const (
	ImportPending    ImportStatus = "pending"
	ImportProcessing ImportStatus = "processing"
	ImportCompleted  ImportStatus = "completed"
	ImportFailed     ImportStatus = "failed"
)

const (
	MaxImportRows   = 50000
	MaxImportErrors = 1000
)

type ImportRowError struct {
	Row     int
	Field   string
	Code    string
	Message string
}

func ImportObjectPrefix(seller kernel.SellerID) string {
	return "catalog/imports/" + seller.String() + "/"
}

type ImportJob struct {
	id            ImportJobID
	sellerID      kernel.SellerID
	requestedBy   kernel.UserID
	format        ImportFormat
	objectKey     string
	status        ImportStatus
	totalRows     int
	succeededRows int
	failedRows    int
	errors        []ImportRowError
	failureReason string
	reportKey     string
	createdAt     time.Time
	startedAt     time.Time
	finishedAt    time.Time
	updatedAt     time.Time
	version       int

	events kernel.EventBuffer
}

func ScheduleImport(id ImportJobID, seller kernel.SellerID, requestedBy kernel.UserID, format ImportFormat, objectKey string, now time.Time) (*ImportJob, error) {
	if id.IsZero() || seller.IsZero() || requestedBy.IsZero() {
		return nil, kernel.ErrInvalidID
	}
	if _, err := ParseImportFormat(string(format)); err != nil {
		return nil, err
	}
	prefix := ImportObjectPrefix(seller)
	if !strings.HasPrefix(objectKey, prefix) || len(objectKey) == len(prefix) || len(objectKey) > 512 || strings.Contains(objectKey, "..") {
		return nil, ErrInvalidObjectKey
	}
	j := &ImportJob{
		id: id, sellerID: seller, requestedBy: requestedBy, format: format, objectKey: objectKey,
		status: ImportPending, createdAt: now, updatedAt: now,
	}
	j.events.Record(ImportScheduled{JobID: id, SellerID: seller, Format: format, At: now})
	return j, nil
}

func (j *ImportJob) Start(now time.Time) error {
	if j.status != ImportPending {
		return ErrImportNotPending
	}
	j.status = ImportProcessing
	j.startedAt = now
	j.updatedAt = now
	return nil
}

func (j *ImportJob) RecordSuccess() error {
	if err := j.countRow(); err != nil {
		return err
	}
	j.succeededRows++
	return nil
}

func (j *ImportJob) RecordFailure(rowErr ImportRowError) error {
	if err := j.countRow(); err != nil {
		return err
	}
	j.failedRows++
	if len(j.errors) < MaxImportErrors {
		j.errors = append(j.errors, rowErr)
	}
	return nil
}

func (j *ImportJob) countRow() error {
	if j.status != ImportProcessing {
		return ErrImportNotRunning
	}
	if j.totalRows >= MaxImportRows {
		return ErrImportTooLarge
	}
	j.totalRows++
	return nil
}

func (j *ImportJob) Touch(now time.Time) {
	j.updatedAt = now
}

func (j *ImportJob) Complete(reportKey string, now time.Time) error {
	if j.status != ImportProcessing {
		return ErrImportNotRunning
	}
	j.status = ImportCompleted
	j.reportKey = reportKey
	j.finishedAt = now
	j.updatedAt = now
	j.events.Record(ImportFinished{
		JobID: j.id, SellerID: j.sellerID, Status: ImportCompleted,
		TotalRows: j.totalRows, SucceededRows: j.succeededRows, FailedRows: j.failedRows, At: now,
	})
	return nil
}

func (j *ImportJob) Fail(reason string, now time.Time) error {
	if j.status != ImportPending && j.status != ImportProcessing {
		return ErrImportNotRunning
	}
	j.status = ImportFailed
	j.failureReason = strings.TrimSpace(reason)
	j.finishedAt = now
	j.updatedAt = now
	j.events.Record(ImportFinished{
		JobID: j.id, SellerID: j.sellerID, Status: ImportFailed, Reason: j.failureReason,
		TotalRows: j.totalRows, SucceededRows: j.succeededRows, FailedRows: j.failedRows, At: now,
	})
	return nil
}

func (j *ImportJob) Requeue(now time.Time) error {
	if j.status != ImportProcessing {
		return ErrImportNotRunning
	}
	j.status = ImportPending
	j.totalRows, j.succeededRows, j.failedRows = 0, 0, 0
	j.errors = nil
	j.startedAt = time.Time{}
	j.updatedAt = now
	return nil
}

func (j *ImportJob) IsStale(now time.Time, timeout time.Duration) bool {
	return j.status == ImportProcessing && now.Sub(j.updatedAt) > timeout
}

func (j *ImportJob) ID() ImportJobID { return j.id }

func (j *ImportJob) SellerID() kernel.SellerID { return j.sellerID }

func (j *ImportJob) RequestedBy() kernel.UserID { return j.requestedBy }

func (j *ImportJob) Format() ImportFormat { return j.format }

func (j *ImportJob) ObjectKey() string { return j.objectKey }

func (j *ImportJob) Status() ImportStatus { return j.status }

func (j *ImportJob) TotalRows() int { return j.totalRows }

func (j *ImportJob) SucceededRows() int { return j.succeededRows }

func (j *ImportJob) FailedRows() int { return j.failedRows }

func (j *ImportJob) Errors() []ImportRowError { return slices.Clone(j.errors) }

func (j *ImportJob) ReportKey() string { return j.reportKey }

func (j *ImportJob) Version() int { return j.version }

func (j *ImportJob) AdvanceVersion() { j.version++ }

func (j *ImportJob) PullEvents() []kernel.DomainEvent { return j.events.Pull() }

type ImportJobSnapshot struct {
	ID            string
	SellerID      string
	RequestedBy   string
	Format        string
	ObjectKey     string
	Status        string
	TotalRows     int
	SucceededRows int
	FailedRows    int
	Errors        []ImportRowError
	FailureReason string
	ReportKey     string
	CreatedAt     time.Time
	StartedAt     time.Time
	FinishedAt    time.Time
	UpdatedAt     time.Time
	Version       int
}

func (j *ImportJob) Snapshot() ImportJobSnapshot {
	return ImportJobSnapshot{
		ID: j.id.String(), SellerID: j.sellerID.String(), RequestedBy: j.requestedBy.String(),
		Format: string(j.format), ObjectKey: j.objectKey, Status: string(j.status),
		TotalRows: j.totalRows, SucceededRows: j.succeededRows, FailedRows: j.failedRows,
		Errors: slices.Clone(j.errors), FailureReason: j.failureReason, ReportKey: j.reportKey,
		CreatedAt: j.createdAt, StartedAt: j.startedAt, FinishedAt: j.finishedAt, UpdatedAt: j.updatedAt, Version: j.version,
	}
}

func RehydrateImportJob(s ImportJobSnapshot) (*ImportJob, error) {
	id, err := ParseImportJobID(s.ID)
	if err != nil {
		return nil, fmt.Errorf("rehydrate import job: %w", err)
	}
	sellerID, err := kernel.ParseSellerID(s.SellerID)
	if err != nil {
		return nil, fmt.Errorf("rehydrate import job %s seller: %w", s.ID, err)
	}
	requestedBy, err := kernel.ParseUserID(s.RequestedBy)
	if err != nil {
		return nil, fmt.Errorf("rehydrate import job %s requester: %w", s.ID, err)
	}
	return &ImportJob{
		id: id, sellerID: sellerID, requestedBy: requestedBy, format: ImportFormat(s.Format), objectKey: s.ObjectKey,
		status: ImportStatus(s.Status), totalRows: s.TotalRows, succeededRows: s.SucceededRows, failedRows: s.FailedRows,
		errors: slices.Clone(s.Errors), failureReason: s.FailureReason, reportKey: s.ReportKey,
		createdAt: s.CreatedAt, startedAt: s.StartedAt, finishedAt: s.FinishedAt, updatedAt: s.UpdatedAt, version: s.Version,
	}, nil
}
