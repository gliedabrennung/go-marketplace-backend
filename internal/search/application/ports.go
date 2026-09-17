package application

import (
	"context"
	"strconv"
	"time"

	catalogapi "github.com/gliedabrennung/go-marketplace-backend/internal/catalog/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

var (
	ErrReindexRunning     = kernel.Conflict("SEARCH_REINDEX_RUNNING", "reindex is already in progress")
	ErrReindexJobNotFound = kernel.NotFound("SEARCH_REINDEX_JOB_NOT_FOUND", "reindex job not found")
)

const (
	ReindexPending   = "pending"
	ReindexRunning   = "running"
	ReindexCompleted = "completed"
	ReindexFailed    = "failed"
)

type Clock interface {
	Now() time.Time
}

type Document struct {
	ProductID    string
	SellerID     string
	CategoryID   string
	CategoryPath []string
	Title        string
	Description  string
	Brand        string
	CoverKey     string
	Attributes   map[string][]string
	Numbers      map[string]float64
	PublishedAt  time.Time
}

type OfferState struct {
	OfferID   string
	ProductID string
	SellerID  string
	Price     int64
	Currency  string
	Condition string
	Status    string
	UpdatedAt time.Time
}

type SellerState struct {
	SellerID string
	CanSell  bool
}

type Index interface {
	ActiveTable(ctx context.Context) (string, error)
	InactiveTable(ctx context.Context) (string, error)
	SaveDocuments(ctx context.Context, table string, documents []Document, at time.Time) error
	SetCover(ctx context.Context, table, productID, coverKey string, at time.Time) error
	Truncate(ctx context.Context, table string) error
	Swap(ctx context.Context, table string, at time.Time) error
	SaveOffer(ctx context.Context, offer OfferState) error
	SaveStock(ctx context.Context, sku string, available int, at time.Time) (string, error)
	SaveSeller(ctx context.Context, state SellerState, at time.Time) error
	RefreshProduct(ctx context.Context, productID string, at time.Time) error
	RefreshSeller(ctx context.Context, sellerID string, at time.Time) error
	RefreshLexicon(ctx context.Context, minWeight int) (int, error)
}

type ReindexJob struct {
	ID            string
	Status        string
	Target        string
	RequestedBy   string
	Processed     int
	FailureReason string
	CreatedAt     time.Time
	StartedAt     time.Time
	FinishedAt    time.Time
}

type ReindexJobs interface {
	Create(ctx context.Context, job ReindexJob) error
	Claim(ctx context.Context, at time.Time) (ReindexJob, bool, error)
	Progress(ctx context.Context, jobID string, processed int, at time.Time) error
	Finish(ctx context.Context, jobID, status, reason string, at time.Time) error
	Get(ctx context.Context, jobID string) (ReindexJob, error)
	HasUnfinished(ctx context.Context) (bool, error)
}

type Policy struct {
	FeedBatch        int
	LexiconMinWeight int
	TypoSimilarity   float64
	MaxFacetValues   int
}

func DefaultPolicy() Policy {
	return Policy{FeedBatch: 500, LexiconMinWeight: 2, TypoSimilarity: 0.4, MaxFacetValues: 50}
}

func NewDocument(source catalogapi.ProductDocument) Document {
	doc := Document{
		ProductID: source.ProductID, SellerID: source.SellerID, CategoryID: source.CategoryID,
		CategoryPath: source.CategoryPath, Title: source.Title, Description: source.Description,
		Brand: source.Brand, CoverKey: source.CoverKey, PublishedAt: source.PublishedAt,
		Attributes: map[string][]string{}, Numbers: map[string]float64{},
	}
	for _, attribute := range source.Attributes {
		doc.Attributes[attribute.Code] = append(doc.Attributes[attribute.Code], attribute.Value)
		if attribute.Number != nil {
			doc.Numbers[attribute.Code] = *attribute.Number
			continue
		}
		if number, err := strconv.ParseFloat(attribute.Value, 64); err == nil {
			doc.Numbers[attribute.Code] = number
		}
	}
	return doc
}

func NewDocuments(sources []catalogapi.ProductDocument) []Document {
	out := make([]Document, 0, len(sources))
	for _, source := range sources {
		out = append(out, NewDocument(source))
	}
	return out
}
