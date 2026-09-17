package query

import (
	"context"
	"errors"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
	sellerapi "github.com/gliedabrennung/go-marketplace-backend/internal/seller/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/pagination"
)

type GetImportJob struct {
	Actor auth.Principal
	JobID string
}

type GetImportJobHandler struct {
	reader  ReadModel
	sellers application.SellerDirectory
	signer  URLSigner
}

func NewGetImportJobHandler(reader ReadModel, sellers application.SellerDirectory, signer URLSigner) *GetImportJobHandler {
	return &GetImportJobHandler{reader: reader, sellers: sellers, signer: signer}
}

func (h *GetImportJobHandler) Handle(ctx context.Context, q GetImportJob) (ImportJobView, error) {
	if q.Actor.UserID == "" {
		return ImportJobView{}, auth.ErrUnauthenticated
	}
	if _, err := domain.ParseImportJobID(q.JobID); err != nil {
		return ImportJobView{}, domain.ErrImportJobNotFound
	}
	view, err := h.reader.ImportJob(ctx, q.JobID)
	if err != nil {
		return ImportJobView{}, err
	}
	if err := requireMember(ctx, h.sellers, q.Actor, view.SellerID); err != nil {
		if errors.Is(err, sellerapi.ErrSellerNotFound) {
			return ImportJobView{}, domain.ErrImportJobNotFound
		}
		return ImportJobView{}, err
	}
	if view.ReportKey != "" {
		if view.ReportURL, err = h.signer.SignDownload(ctx, view.ReportKey); err != nil {
			return ImportJobView{}, err
		}
	}
	return view, nil
}

type ListImportJobs struct {
	Actor    auth.Principal
	SellerID string
	Limit    int
	Cursor   string
}

type ListImportJobsHandler struct {
	reader  ReadModel
	sellers application.SellerDirectory
}

func NewListImportJobsHandler(reader ReadModel, sellers application.SellerDirectory) *ListImportJobsHandler {
	return &ListImportJobsHandler{reader: reader, sellers: sellers}
}

func (h *ListImportJobsHandler) Handle(ctx context.Context, q ListImportJobs) (pagination.Page[ImportJobView], error) {
	if err := requireMember(ctx, h.sellers, q.Actor, q.SellerID); err != nil {
		return pagination.Page[ImportJobView]{}, err
	}
	after, err := keyset(q.Cursor)
	if err != nil {
		return pagination.Page[ImportJobView]{}, err
	}
	return h.reader.SellerImports(ctx, q.SellerID, pagination.NormalizeLimit(q.Limit), after)
}
