package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type ProcessImport struct {
	JobID string
}

type ProcessImportResult struct {
	Processed bool
	Status    string
	TotalRows int
	Failed    int
}

type ProcessImportHandler struct {
	base    Base
	source  application.ImportSource
	reports application.ImportReportWriter
}

func NewProcessImportHandler(base Base, source application.ImportSource, reports application.ImportReportWriter) *ProcessImportHandler {
	return &ProcessImportHandler{base: base, source: source, reports: reports}
}

func (h *ProcessImportHandler) Handle(ctx context.Context, cmd ProcessImport) (ProcessImportResult, error) {
	jobID, err := domain.ParseImportJobID(cmd.JobID)
	if err != nil {
		return ProcessImportResult{}, domain.ErrImportJobNotFound
	}
	job, started, err := h.start(ctx, jobID)
	if err != nil || !started {
		return ProcessImportResult{}, err
	}

	if err := h.base.requireCanSell(ctx, job.SellerID()); err != nil {
		return h.fail(ctx, job, "seller is not allowed to sell")
	}
	reader, err := h.source.Open(ctx, job.Format(), job.ObjectKey())
	if err != nil {
		return h.fail(ctx, job, "cannot open import file: "+err.Error())
	}
	result, err := h.consume(ctx, job, reader)
	if closeErr := reader.Close(); closeErr != nil && err == nil {
		err = fmt.Errorf("close import reader: %w", closeErr)
	}
	return result, err
}

func (h *ProcessImportHandler) start(ctx context.Context, jobID domain.ImportJobID) (*domain.ImportJob, bool, error) {
	var job *domain.ImportJob
	err := h.base.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		found, err := repos.Imports().FindByID(ctx, jobID)
		if err != nil {
			return err
		}
		if err := found.Start(h.base.clock.Now()); err != nil {
			return err
		}
		job = found
		return repos.Imports().Save(ctx, found)
	})
	switch {
	case errors.Is(err, domain.ErrImportNotPending), errors.Is(err, kernel.ErrConcurrentModification):
		return nil, false, nil
	case err != nil:
		return nil, false, err
	}
	return job, true, nil
}

func (h *ProcessImportHandler) consume(ctx context.Context, job *domain.ImportJob, reader application.RowReader) (ProcessImportResult, error) {
	for {
		row, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return h.fail(ctx, job, "unreadable import file: "+err.Error())
		}
		rowErr, err := h.apply(ctx, job.SellerID(), row)
		if err != nil {
			return ProcessImportResult{}, err
		}
		if rowErr == nil {
			err = job.RecordSuccess()
		} else {
			err = job.RecordFailure(*rowErr)
		}
		if errors.Is(err, domain.ErrImportTooLarge) {
			return h.fail(ctx, job, fmt.Sprintf("import exceeds %d rows", domain.MaxImportRows))
		}
		if err != nil {
			return ProcessImportResult{}, err
		}
		if job.TotalRows()%h.base.policy.ImportProgress == 0 {
			if err := h.save(ctx, job, func(j *domain.ImportJob, now time.Time) error { j.Touch(now); return nil }); err != nil {
				return ProcessImportResult{}, err
			}
		}
	}

	reportKey := ""
	if job.FailedRows() > 0 {
		reportKey = domain.ImportObjectPrefix(job.SellerID()) + "reports/" + job.ID().String() + ".csv"
		if err := h.reports.Write(ctx, reportKey, job.Errors()); err != nil {
			return ProcessImportResult{}, err
		}
	}
	if err := h.save(ctx, job, func(j *domain.ImportJob, now time.Time) error { return j.Complete(reportKey, now) }); err != nil {
		return ProcessImportResult{}, err
	}
	return resultOf(job), nil
}

func (h *ProcessImportHandler) apply(ctx context.Context, seller kernel.SellerID, row application.ImportRow) (*domain.ImportRowError, error) {
	sku, err := domain.NewSellerSKU(strings.TrimSpace(row.SellerSKU))
	if err != nil {
		return rowError(row.Number, "seller_sku", err), nil
	}
	productID, err := domain.ParseProductID(strings.TrimSpace(row.ProductID))
	if err != nil {
		return rowError(row.Number, "product_id", domain.ErrProductNotFound), nil
	}
	terms, field, err := h.parseTerms(row)
	if err != nil {
		return rowError(row.Number, field, err), nil
	}
	status := domain.OfferStatus(strings.TrimSpace(row.Status))
	if status == "" {
		status = domain.OfferActive
	}
	if status != domain.OfferActive && status != domain.OfferPaused {
		return rowError(row.Number, "status", ErrInvalidOfferStatus), nil
	}

	now := h.base.clock.Now()
	err = h.base.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		return upsertOffer(ctx, repos, seller, productID, sku, terms, status, now)
	})
	if err == nil {
		return nil, nil
	}
	if kernel.KindOf(err) == kernel.KindInternal {
		return nil, fmt.Errorf("import row %d: %w", row.Number, err)
	}
	return rowError(row.Number, "", err), nil
}

func (h *ProcessImportHandler) parseTerms(row application.ImportRow) (domain.OfferTerms, string, error) {
	price, err := strconv.ParseInt(strings.TrimSpace(row.Price), 10, 64)
	if err != nil {
		return domain.OfferTerms{}, "price", domain.ErrInvalidPrice
	}
	days, err := strconv.Atoi(strings.TrimSpace(row.ProcessingDays))
	if err != nil {
		return domain.OfferTerms{}, "processing_days", domain.ErrInvalidProcessingTime
	}
	terms, err := domain.NewOfferTerms(price, h.base.currency(strings.TrimSpace(row.Currency)), strings.TrimSpace(row.Condition), days)
	if err != nil {
		return domain.OfferTerms{}, termsField(err), err
	}
	return terms, "", nil
}

func termsField(err error) string {
	switch {
	case errors.Is(err, domain.ErrInvalidPrice):
		return "price"
	case errors.Is(err, domain.ErrInvalidCondition):
		return "condition"
	case errors.Is(err, domain.ErrInvalidProcessingTime):
		return "processing_days"
	default:
		return ""
	}
}

func upsertOffer(ctx context.Context, repos application.Repositories, seller kernel.SellerID, productID domain.ProductID,
	sku domain.SellerSKU, terms domain.OfferTerms, status domain.OfferStatus, now time.Time) error {
	offer, err := repos.Offers().FindBySellerSKU(ctx, seller, sku)
	switch {
	case errors.Is(err, domain.ErrOfferNotFound):
		product, err := repos.Products().FindByID(ctx, productID)
		if err != nil {
			return err
		}
		if offer, err = domain.CreateOffer(domain.NewOfferID(), product, seller, sku, terms, now); err != nil {
			return err
		}
	case err != nil:
		return err
	case offer.ProductID() != productID:
		return ErrImportProductMismatch
	default:
		if err := offer.ChangeTerms(seller, terms, now); err != nil {
			return err
		}
	}
	if status == domain.OfferPaused {
		err = offer.Pause(seller, now)
	} else {
		err = offer.Activate(seller, now)
	}
	if err != nil {
		return err
	}
	return repos.Offers().Save(ctx, offer)
}

var ErrImportProductMismatch = kernel.BusinessRule("CATALOG_IMPORT_PRODUCT_MISMATCH", "seller SKU is already bound to another product")

func rowError(row int, field string, err error) *domain.ImportRowError {
	message := err.Error()
	var withMessage interface{ Message() string }
	if errors.As(err, &withMessage) {
		message = withMessage.Message()
	}
	return &domain.ImportRowError{Row: row, Field: field, Code: kernel.CodeOf(err), Message: message}
}

func (h *ProcessImportHandler) fail(ctx context.Context, job *domain.ImportJob, reason string) (ProcessImportResult, error) {
	if err := h.save(ctx, job, func(j *domain.ImportJob, now time.Time) error { return j.Fail(reason, now) }); err != nil {
		return ProcessImportResult{}, err
	}
	return resultOf(job), nil
}

func (h *ProcessImportHandler) save(ctx context.Context, job *domain.ImportJob, change func(*domain.ImportJob, time.Time) error) error {
	return h.base.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		if err := change(job, h.base.clock.Now()); err != nil {
			return err
		}
		return repos.Imports().Save(ctx, job)
	})
}

func resultOf(job *domain.ImportJob) ProcessImportResult {
	return ProcessImportResult{Processed: true, Status: string(job.Status()), TotalRows: job.TotalRows(), Failed: job.FailedRows()}
}

type staleImportLister interface {
	StaleImportIDs(ctx context.Context, before time.Time, limit int) ([]string, error)
}

type RequeueStaleImports struct {
	Limit int
}

type RequeueStaleImportsHandler struct {
	base   Base
	reader staleImportLister
}

func NewRequeueStaleImportsHandler(base Base, reader staleImportLister) *RequeueStaleImportsHandler {
	return &RequeueStaleImportsHandler{base: base, reader: reader}
}

func (h *RequeueStaleImportsHandler) Handle(ctx context.Context, cmd RequeueStaleImports) (int, error) {
	now := h.base.clock.Now()
	ids, err := h.reader.StaleImportIDs(ctx, now.Add(-h.base.policy.ImportStaleAfter), cmd.Limit)
	if err != nil {
		return 0, err
	}
	requeued := 0
	for _, raw := range ids {
		id, err := domain.ParseImportJobID(raw)
		if err != nil {
			return requeued, err
		}
		err = h.base.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
			job, err := repos.Imports().FindByID(ctx, id)
			if err != nil {
				return err
			}
			if !job.IsStale(now, h.base.policy.ImportStaleAfter) {
				return nil
			}
			if err := job.Requeue(now); err != nil {
				return err
			}
			requeued++
			return repos.Imports().Save(ctx, job)
		})
		if err != nil && !errors.Is(err, kernel.ErrConcurrentModification) {
			return requeued, err
		}
	}
	return requeued, nil
}
