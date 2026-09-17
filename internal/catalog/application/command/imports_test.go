package command_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application/command"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
	sellerapi "github.com/gliedabrennung/go-marketplace-backend/internal/seller/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

func (e *env) importFile(t *testing.T, owner auth.Principal, sellerID, format, contentType string, data []byte) string {
	t.Helper()
	upload, err := e.requestImport.Handle(ctx, command.RequestImportUpload{Actor: owner, SellerID: sellerID, Format: format, Size: int64(len(data))})
	require.NoError(t, err)
	require.NoError(t, e.objects.Put(ctx, upload.ObjectKey, contentType, data))
	job, err := e.scheduleImport.Handle(ctx, command.ScheduleImport{Actor: owner, SellerID: sellerID, Format: format, ObjectKey: upload.ObjectKey})
	require.NoError(t, err)
	return job.JobID
}

func (e *env) process(t *testing.T, jobID string) command.ProcessImportResult {
	t.Helper()
	res, err := e.processImport.Handle(ctx, command.ProcessImport{JobID: jobID})
	require.NoError(t, err)
	return res
}

func TestImports_CSVWithRowErrors(t *testing.T) {
	e := newEnv(t)
	tr := e.tree(t)
	owner, seller := e.seller()
	black := e.published(t, owner, seller, tr.phones, "black")
	white := e.published(t, owner, seller, tr.phones, "white")

	_, err := e.requestImport.Handle(ctx, command.RequestImportUpload{Actor: owner, SellerID: seller, Format: "csv", Size: 60 << 20})
	require.ErrorIs(t, err, command.ErrImportFileTooLarge)
	_, err = e.requestImport.Handle(ctx, command.RequestImportUpload{Actor: owner, SellerID: seller, Format: "pdf", Size: 10})
	require.ErrorIs(t, err, domain.ErrInvalidImportFormat)
	_, err = e.requestImport.Handle(ctx, command.RequestImportUpload{Actor: user(), SellerID: seller, Format: "csv", Size: 10})
	require.ErrorIs(t, err, sellerapi.ErrSellerNotFound)

	csv := strings.Join([]string{
		"\ufeffSeller_SKU;product_id;price;condition;processing_days;status",
		"SKU-1;" + black + ";100000;new;2;",
		"SKU-2;" + white + ";120000;used;1;paused",
		";" + black + ";1;new;1;",
		"SKU-3;bad;100;new;1;",
		"",
		"SKU-4;" + black + ";abc;new;1;",
		"SKU-5;" + black + ";100;new;x;",
		"SKU-6;" + black + ";100;broken;1;",
		"SKU-7;" + black + ";100;new;1;archived",
		"SKU-8;" + black + ";100;new;1;",
		"=HYPERLINK(1);" + black + ";100;new;1;",
	}, "\n")
	jobID := e.importFile(t, owner, seller, "csv", "text/csv", []byte(csv))

	pending, err := e.getImport.Handle(ctx, query.GetImportJob{Actor: owner, JobID: jobID})
	require.NoError(t, err)
	assert.Equal(t, "pending", pending.Status)

	res := e.process(t, jobID)
	assert.Equal(t, command.ProcessImportResult{Processed: true, Status: "completed", TotalRows: 10, Failed: 8}, res)
	assert.False(t, e.process(t, jobID).Processed)

	view, err := e.getImport.Handle(ctx, query.GetImportJob{Actor: owner, JobID: jobID})
	require.NoError(t, err)
	assert.Equal(t, 2, view.SucceededRows)
	require.Len(t, view.Errors, 8)
	assert.Equal(t, query.ImportRowErrorView{Row: 4, Field: "seller_sku", Code: "CATALOG_INVALID_SELLER_SKU", Message: domain.ErrInvalidSellerSKU.Message()}, view.Errors[0])
	codes := []string{}
	for _, rowErr := range view.Errors {
		codes = append(codes, rowErr.Code)
	}
	assert.Equal(t, []string{
		"CATALOG_INVALID_SELLER_SKU", "CATALOG_PRODUCT_NOT_FOUND", "CATALOG_INVALID_PRICE", "CATALOG_INVALID_PROCESSING_TIME",
		"CATALOG_INVALID_CONDITION", "CATALOG_INVALID_OFFER_STATUS", "CATALOG_OFFER_EXISTS", "CATALOG_INVALID_SELLER_SKU",
	}, codes)
	assert.Equal(t, 7, view.Errors[2].Row)
	require.NotEmpty(t, view.ReportURL)

	report, ok := e.objects.Get(view.ReportKey)
	require.True(t, ok)
	assert.Contains(t, string(report), "row,field,code,message")
	assert.Contains(t, string(report), "CATALOG_OFFER_EXISTS")

	offers, err := e.sellerOffers.Handle(ctx, query.ListSellerOffers{Actor: owner, SellerID: seller})
	require.NoError(t, err)
	require.Len(t, offers.Items, 2)

	update := "seller_sku,product_id,price,condition,processing_days\nSKU-1," + black + ",95000,new,3\nSKU-2," + black + ",1,new,1\n"
	jobID = e.importFile(t, owner, seller, "csv", "text/csv", []byte(update))
	res = e.process(t, jobID)
	assert.Equal(t, 2, res.TotalRows)
	assert.Equal(t, 1, res.Failed)
	view, err = e.getImport.Handle(ctx, query.GetImportJob{Actor: owner, JobID: jobID})
	require.NoError(t, err)
	assert.Equal(t, "CATALOG_IMPORT_PRODUCT_MISMATCH", view.Errors[0].Code)
	public, err := e.productOffers.Handle(ctx, query.ListProductOffers{ProductID: black})
	require.NoError(t, err)
	require.Len(t, public, 1)
	assert.Equal(t, int64(95000), public[0].Price)
	assert.Equal(t, 3, public[0].ProcessingDays)

	jobs, err := e.listImports.Handle(ctx, query.ListImportJobs{Actor: owner, SellerID: seller, Limit: 1})
	require.NoError(t, err)
	require.Len(t, jobs.Items, 1)
	assert.True(t, jobs.HasMore)
	_, err = e.listImports.Handle(ctx, query.ListImportJobs{Actor: user(), SellerID: seller})
	require.ErrorIs(t, err, sellerapi.ErrSellerNotFound)
	_, err = e.listImports.Handle(ctx, query.ListImportJobs{Actor: owner, SellerID: seller, Cursor: "?"})
	require.Error(t, err)
	_, err = e.getImport.Handle(ctx, query.GetImportJob{Actor: user(), JobID: jobID})
	require.ErrorIs(t, err, domain.ErrImportJobNotFound)
	_, err = e.getImport.Handle(ctx, query.GetImportJob{Actor: owner, JobID: "bad"})
	require.ErrorIs(t, err, domain.ErrImportJobNotFound)
	_, err = e.getImport.Handle(ctx, query.GetImportJob{JobID: jobID})
	require.ErrorIs(t, err, auth.ErrUnauthenticated)
}

func TestImports_InlineXLSXAndJSON(t *testing.T) {
	e := newEnv(t)
	tr := e.tree(t)
	owner, seller := e.seller()
	black := e.published(t, owner, seller, tr.phones, "black")
	white := e.published(t, owner, seller, tr.phones, "white")
	red := e.published(t, owner, seller, tr.phones, "red")

	job, err := e.scheduleImport.Handle(ctx, command.ScheduleImport{Actor: owner, SellerID: seller, Rows: []command.ImportRecord{
		{SellerSKU: "J-1", ProductID: black, Price: "5000", Condition: "new", ProcessingDays: "0"},
	}})
	require.NoError(t, err)
	assert.Equal(t, command.ProcessImportResult{Processed: true, Status: "completed", TotalRows: 1}, e.process(t, job.JobID))
	view, err := e.getImport.Handle(ctx, query.GetImportJob{Actor: owner, JobID: job.JobID})
	require.NoError(t, err)
	assert.Equal(t, "json", view.Format)
	assert.Empty(t, view.ReportURL)

	_, err = e.scheduleImport.Handle(ctx, command.ScheduleImport{Actor: owner, SellerID: seller})
	require.ErrorIs(t, err, command.ErrInvalidInlineRows)
	_, err = e.scheduleImport.Handle(ctx, command.ScheduleImport{Actor: owner, SellerID: seller, Format: "csv", ObjectKey: domain.ImportObjectPrefix(mustParse(t, kernel.ParseSellerID, seller)) + "missing.csv"})
	require.Error(t, err)

	book := excelize.NewFile()
	require.NoError(t, book.SetSheetRow("Sheet1", "A1", &[]any{"seller_sku", "product_id", "price", "condition", "processing_days", "currency"}))
	require.NoError(t, book.SetSheetRow("Sheet1", "A3", &[]any{"X-1", white, 7000, "used", 5, "KZT"}))
	buf, err := book.WriteToBuffer()
	require.NoError(t, err)
	jobID := e.importFile(t, owner, seller, "xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", buf.Bytes())
	assert.Equal(t, command.ProcessImportResult{Processed: true, Status: "completed", TotalRows: 1}, e.process(t, jobID))

	payload := `[{"seller_sku":"N-1","product_id":"` + red + `","price":7000,"condition":"new","processing_days":3,"status":null}]`
	jobID = e.importFile(t, owner, seller, "json", "application/json", []byte(payload))
	assert.Equal(t, command.ProcessImportResult{Processed: true, Status: "completed", TotalRows: 1}, e.process(t, jobID))

	offers, err := e.sellerOffers.Handle(ctx, query.ListSellerOffers{Actor: owner, SellerID: seller})
	require.NoError(t, err)
	assert.Len(t, offers.Items, 3)
}

func TestImports_JobFailures(t *testing.T) {
	e := newEnv(t)
	tr := e.tree(t)
	owner, seller := e.seller()
	black := e.published(t, owner, seller, tr.phones, "black")

	failed := func(format, contentType, body, reason string) {
		t.Helper()
		jobID := e.importFile(t, owner, seller, format, contentType, []byte(body))
		res := e.process(t, jobID)
		assert.Equal(t, "failed", res.Status)
		view, err := e.getImport.Handle(ctx, query.GetImportJob{Actor: owner, JobID: jobID})
		require.NoError(t, err)
		assert.Contains(t, view.FailureReason, reason)
	}
	failed("csv", "text/csv", "sku,price\nA,1\n", "cannot open import file")
	failed("json", "application/json", `{"seller_sku":"A"}`, "cannot open import file")
	failed("json", "application/json", `[{"seller_sku":"A","product_id":"`+black+`","price":"1","condition":"new","processing_days":"1"}, {"price": [1]}]`, "unreadable import file")
	failed("xlsx", "application/octet-stream", "not a zip archive", "cannot open import file")

	jobID := e.importFile(t, owner, seller, "csv", "text/csv", []byte("seller_sku,product_id,price,condition,processing_days\nA,"+black+",1,new,1\n"))
	e.sellers.suspend(seller)
	assert.Equal(t, "failed", e.process(t, jobID).Status)
	_, err := e.scheduleImport.Handle(ctx, command.ScheduleImport{Actor: owner, SellerID: seller, Rows: []command.ImportRecord{{SellerSKU: "A"}}})
	require.ErrorIs(t, err, application.ErrSellerInactive)

	_, err = e.processImport.Handle(ctx, command.ProcessImport{JobID: "bad"})
	require.ErrorIs(t, err, domain.ErrImportJobNotFound)
	_, err = e.processImport.Handle(ctx, command.ProcessImport{JobID: domain.NewImportJobID().String()})
	require.ErrorIs(t, err, domain.ErrImportJobNotFound)
}

func TestImports_RateLimitAndStaleRequeue(t *testing.T) {
	e := newEnv(t)
	tr := e.tree(t)
	owner, seller := e.seller()
	black := e.published(t, owner, seller, tr.phones, "black")
	rows := []command.ImportRecord{{SellerSKU: "R-1", ProductID: black, Price: "100", Condition: "new", ProcessingDays: "1"}}

	e.limiter.remaining = 1
	job, err := e.scheduleImport.Handle(ctx, command.ScheduleImport{Actor: owner, SellerID: seller, Rows: rows})
	require.NoError(t, err)
	_, err = e.scheduleImport.Handle(ctx, command.ScheduleImport{Actor: owner, SellerID: seller, Rows: rows})
	require.ErrorIs(t, err, errImportsLimited)

	id := mustParse(t, domain.ParseImportJobID, job.JobID)
	stuck, err := e.store.Imports().FindByID(ctx, id)
	require.NoError(t, err)
	require.NoError(t, stuck.Start(e.clock.Now()))
	require.NoError(t, e.store.Imports().Save(ctx, stuck))

	count, err := e.requeueImports.Handle(ctx, command.RequeueStaleImports{Limit: 10})
	require.NoError(t, err)
	assert.Zero(t, count)
	assert.False(t, e.process(t, job.JobID).Processed)

	e.clock.Advance(16 * time.Minute)
	count, err = e.requeueImports.Handle(ctx, command.RequeueStaleImports{Limit: 10})
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	assert.Equal(t, "completed", e.process(t, job.JobID).Status)
}
