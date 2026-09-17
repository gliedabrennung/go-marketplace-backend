package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

func scheduled(t *testing.T) *domain.ImportJob {
	t.Helper()
	seller := kernel.NewSellerID()
	job, err := domain.ScheduleImport(domain.NewImportJobID(), seller, kernel.NewUserID(), domain.ImportCSV, domain.ImportObjectPrefix(seller)+"offers.csv", now)
	require.NoError(t, err)
	return job
}

func TestScheduleImport(t *testing.T) {
	seller := kernel.NewSellerID()
	prefix := domain.ImportObjectPrefix(seller)
	job, err := domain.ScheduleImport(domain.NewImportJobID(), seller, kernel.NewUserID(), domain.ImportXLSX, prefix+"a.xlsx", now)
	require.NoError(t, err)
	assert.Equal(t, domain.ImportPending, job.Status())
	assert.Equal(t, domain.ImportXLSX, job.Format())
	assert.Equal(t, prefix+"a.xlsx", job.ObjectKey())
	assert.Equal(t, seller, job.SellerID())
	assert.False(t, job.RequestedBy().IsZero())
	assert.Equal(t, []string{"catalog.offer_import_scheduled.v1"}, names(job.PullEvents()))

	for _, key := range []string{"", prefix, "catalog/imports/other/a.csv", prefix + "../x.csv"} {
		_, err := domain.ScheduleImport(domain.NewImportJobID(), seller, kernel.NewUserID(), domain.ImportCSV, key, now)
		require.ErrorIs(t, err, domain.ErrInvalidObjectKey, key)
	}
	_, err = domain.ScheduleImport(domain.NewImportJobID(), seller, kernel.NewUserID(), "pdf", prefix+"a.pdf", now)
	require.ErrorIs(t, err, domain.ErrInvalidImportFormat)
	_, err = domain.ScheduleImport(domain.ImportJobID{}, seller, kernel.NewUserID(), domain.ImportCSV, prefix+"a.csv", now)
	require.ErrorIs(t, err, kernel.ErrInvalidID)

	for _, f := range []string{"csv", "xlsx", "json"} {
		parsed, err := domain.ParseImportFormat(f)
		require.NoError(t, err)
		assert.Equal(t, f, string(parsed))
	}
}

func TestImportJob_Processing(t *testing.T) {
	job := scheduled(t)
	job.PullEvents()
	require.ErrorIs(t, job.RecordSuccess(), domain.ErrImportNotRunning)
	require.ErrorIs(t, job.Complete("", now), domain.ErrImportNotRunning)

	require.NoError(t, job.Start(now))
	require.ErrorIs(t, job.Start(now), domain.ErrImportNotPending)
	assert.Equal(t, domain.ImportProcessing, job.Status())

	require.NoError(t, job.RecordSuccess())
	require.NoError(t, job.RecordFailure(domain.ImportRowError{Row: 2, Field: "price", Code: "INVALID", Message: "bad"}))
	job.Touch(now.Add(time.Minute))
	assert.False(t, job.IsStale(now.Add(2*time.Minute), 5*time.Minute))
	assert.True(t, job.IsStale(now.Add(10*time.Minute), 5*time.Minute))

	require.NoError(t, job.Complete("catalog/imports/report.csv", now.Add(time.Minute)))
	assert.Equal(t, domain.ImportCompleted, job.Status())
	assert.Equal(t, 2, job.TotalRows())
	assert.Equal(t, 1, job.SucceededRows())
	assert.Equal(t, 1, job.FailedRows())
	assert.Equal(t, "catalog/imports/report.csv", job.ReportKey())
	require.Len(t, job.Errors(), 1)
	assert.Equal(t, 2, job.Errors()[0].Row)

	events := job.PullEvents()
	require.Len(t, events, 1)
	finished := events[0].(domain.ImportFinished)
	assert.Equal(t, domain.ImportCompleted, finished.Status)
	assert.Equal(t, 2, finished.TotalRows)
	require.ErrorIs(t, job.Fail("late", now), domain.ErrImportNotRunning)
}

func TestImportJob_LimitsAndFailures(t *testing.T) {
	job := scheduled(t)
	require.NoError(t, job.Start(now))
	for i := range domain.MaxImportRows {
		var err error
		if i%2 == 0 {
			err = job.RecordFailure(domain.ImportRowError{Row: i + 1})
		} else {
			err = job.RecordSuccess()
		}
		require.NoError(t, err)
	}
	assert.Len(t, job.Errors(), domain.MaxImportErrors)
	require.ErrorIs(t, job.RecordSuccess(), domain.ErrImportTooLarge)
	require.ErrorIs(t, job.RecordFailure(domain.ImportRowError{}), domain.ErrImportTooLarge)

	require.NoError(t, job.Requeue(now))
	assert.Equal(t, domain.ImportPending, job.Status())
	assert.Zero(t, job.TotalRows())
	assert.Empty(t, job.Errors())
	require.ErrorIs(t, job.Requeue(now), domain.ErrImportNotRunning)

	require.NoError(t, job.Fail(" file is not a valid CSV ", now))
	assert.Equal(t, domain.ImportFailed, job.Status())
	job.PullEvents()

	job.AdvanceVersion()
	restored, err := domain.RehydrateImportJob(job.Snapshot())
	require.NoError(t, err)
	assert.Equal(t, job.Snapshot(), restored.Snapshot())
	for _, mutate := range []func(*domain.ImportJobSnapshot){
		func(s *domain.ImportJobSnapshot) { s.ID = "bad" },
		func(s *domain.ImportJobSnapshot) { s.SellerID = "bad" },
		func(s *domain.ImportJobSnapshot) { s.RequestedBy = "bad" },
	} {
		snap := job.Snapshot()
		mutate(&snap)
		_, err := domain.RehydrateImportJob(snap)
		require.Error(t, err)
	}
}
