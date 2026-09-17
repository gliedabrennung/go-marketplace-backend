package command

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

var (
	ErrImportFileTooLarge = kernel.Validation("CATALOG_IMPORT_FILE_TOO_LARGE", "import file is too large")
	ErrInvalidInlineRows  = kernel.Validation("CATALOG_INVALID_INLINE_IMPORT", "inline import must contain between 1 and 1000 rows")
)

var importContentTypes = map[domain.ImportFormat]string{
	domain.ImportCSV:  "text/csv",
	domain.ImportXLSX: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	domain.ImportJSON: "application/json",
}

type ImportRecord struct {
	SellerSKU      string `json:"seller_sku"`
	ProductID      string `json:"product_id"`
	Price          string `json:"price"`
	Currency       string `json:"currency,omitempty"`
	Condition      string `json:"condition"`
	ProcessingDays string `json:"processing_days"`
	Status         string `json:"status,omitempty"`
}

type RequestImportUpload struct {
	Actor    auth.Principal
	SellerID string
	Format   string
	Size     int64
}

type RequestImportUploadResult struct {
	ObjectKey string
	Upload    application.UploadTarget
}

type RequestImportUploadHandler struct {
	base    Base
	storage application.ObjectStorage
}

func NewRequestImportUploadHandler(base Base, storage application.ObjectStorage) *RequestImportUploadHandler {
	return &RequestImportUploadHandler{base: base, storage: storage}
}

func (h *RequestImportUploadHandler) Handle(ctx context.Context, cmd RequestImportUpload) (RequestImportUploadResult, error) {
	seller, err := h.base.requireSellingMember(ctx, cmd.Actor, cmd.SellerID)
	if err != nil {
		return RequestImportUploadResult{}, err
	}
	format, err := domain.ParseImportFormat(cmd.Format)
	if err != nil {
		return RequestImportUploadResult{}, err
	}
	if cmd.Size <= 0 || cmd.Size > h.base.policy.MaxImportBytes {
		return RequestImportUploadResult{}, ErrImportFileTooLarge
	}
	key := domain.ImportObjectPrefix(seller) + kernel.NewID[struct{}]().String() + "." + string(format)
	target, err := h.storage.PresignUpload(ctx, key, importContentTypes[format], cmd.Size)
	if err != nil {
		return RequestImportUploadResult{}, err
	}
	return RequestImportUploadResult{ObjectKey: key, Upload: target}, nil
}

type ScheduleImport struct {
	Actor     auth.Principal
	SellerID  string
	Format    string
	ObjectKey string
	Rows      []ImportRecord
}

type ScheduleImportResult struct {
	JobID string
}

type ScheduleImportHandler struct {
	base    Base
	storage application.ObjectStorage
	limiter application.ImportLimiter
}

func NewScheduleImportHandler(base Base, storage application.ObjectStorage, limiter application.ImportLimiter) *ScheduleImportHandler {
	return &ScheduleImportHandler{base: base, storage: storage, limiter: limiter}
}

func (h *ScheduleImportHandler) Handle(ctx context.Context, cmd ScheduleImport) (ScheduleImportResult, error) {
	seller, err := h.base.requireSellingMember(ctx, cmd.Actor, cmd.SellerID)
	if err != nil {
		return ScheduleImportResult{}, err
	}
	requester, err := actorID(cmd.Actor)
	if err != nil {
		return ScheduleImportResult{}, err
	}
	format, key, err := h.source(ctx, seller, cmd)
	if err != nil {
		return ScheduleImportResult{}, err
	}
	if err := h.limiter.Allow(ctx, seller.String()); err != nil {
		return ScheduleImportResult{}, err
	}
	if format == domain.ImportJSON && len(cmd.Rows) > 0 {
		if err := h.storeInline(ctx, key, cmd.Rows); err != nil {
			return ScheduleImportResult{}, err
		}
	}

	job, err := domain.ScheduleImport(domain.NewImportJobID(), seller, requester, format, key, h.base.clock.Now())
	if err != nil {
		return ScheduleImportResult{}, err
	}
	err = h.base.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		return repos.Imports().Save(ctx, job)
	})
	if err != nil {
		return ScheduleImportResult{}, err
	}
	return ScheduleImportResult{JobID: job.ID().String()}, nil
}

func (h *ScheduleImportHandler) source(ctx context.Context, seller kernel.SellerID, cmd ScheduleImport) (domain.ImportFormat, string, error) {
	if len(cmd.Rows) > 0 || cmd.ObjectKey == "" {
		if len(cmd.Rows) == 0 || len(cmd.Rows) > h.base.policy.MaxInlineRows {
			return "", "", ErrInvalidInlineRows
		}
		return domain.ImportJSON, domain.ImportObjectPrefix(seller) + kernel.NewID[struct{}]().String() + ".json", nil
	}
	format, err := domain.ParseImportFormat(cmd.Format)
	if err != nil {
		return "", "", err
	}
	info, err := h.storage.Head(ctx, cmd.ObjectKey)
	if err != nil {
		return "", "", err
	}
	if info.Size > h.base.policy.MaxImportBytes {
		return "", "", ErrImportFileTooLarge
	}
	return format, cmd.ObjectKey, nil
}

func (h *ScheduleImportHandler) storeInline(ctx context.Context, key string, rows []ImportRecord) error {
	data, err := json.Marshal(rows)
	if err != nil {
		return fmt.Errorf("encode inline import: %w", err)
	}
	return h.storage.Put(ctx, key, importContentTypes[domain.ImportJSON], data)
}
