package application

import (
	"context"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
	sellerapi "github.com/gliedabrennung/go-marketplace-backend/internal/seller/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

var (
	ErrUploadMissing  = kernel.BusinessRule("CATALOG_UPLOAD_MISSING", "uploaded file was not found in storage")
	ErrSellerInactive = kernel.BusinessRule("CATALOG_SELLER_INACTIVE", "seller is not allowed to sell")
	ErrInvalidImage   = kernel.Validation("CATALOG_INVALID_IMAGE", "file is not a valid image")
)

type Clock interface {
	Now() time.Time
}

type AuditEntry struct {
	ActorID    string
	ActorRoles []string
	Action     string
	ObjectType string
	ObjectID   string
	Details    map[string]string
	OccurredAt time.Time
}

type AuditTrail interface {
	Record(ctx context.Context, e AuditEntry) error
}

type Repositories interface {
	Categories() domain.CategoryRepository
	Products() domain.ProductRepository
	VariantGroups() domain.VariantGroupRepository
	Offers() domain.OfferRepository
	Imports() domain.ImportJobRepository
	Audit() AuditTrail
}

type UnitOfWork interface {
	Do(ctx context.Context, fn func(ctx context.Context, repos Repositories) error) error
}

type UploadTarget struct {
	Method    string
	URL       string
	Headers   map[string]string
	ExpiresAt time.Time
}

type ObjectInfo struct {
	Size        int64
	ContentType string
}

type ObjectStorage interface {
	PresignUpload(ctx context.Context, key, contentType string, size int64) (UploadTarget, error)
	SignDownload(ctx context.Context, key string) (string, error)
	Head(ctx context.Context, key string) (ObjectInfo, error)
	ReadPrefix(ctx context.Context, key string, limit int64) ([]byte, error)
	Put(ctx context.Context, key, contentType string, data []byte) error
}

type ImageInfo struct {
	ContentType string
	Width       int
	Height      int
}

type ImageProber interface {
	Probe(header []byte) (ImageInfo, error)
}

type Thumbnail struct {
	Key     string
	MaxSide int
}

type ThumbnailRenderer interface {
	Render(ctx context.Context, sourceKey string, targets []Thumbnail) error
}

type SellerDirectory interface {
	Seller(ctx context.Context, sellerID string) (sellerapi.SellerInfo, error)
	MemberRole(ctx context.Context, sellerID, userID string) (string, bool, error)
}

type ImportLimiter interface {
	Allow(ctx context.Context, sellerID string) error
}

type ImportRow struct {
	Number         int
	SellerSKU      string
	ProductID      string
	Price          string
	Currency       string
	Condition      string
	ProcessingDays string
	Status         string
}

type RowReader interface {
	Next() (ImportRow, error)
	Close() error
}

type ImportSource interface {
	Open(ctx context.Context, format domain.ImportFormat, key string) (RowReader, error)
}

type ImportReportWriter interface {
	Write(ctx context.Context, key string, rows []domain.ImportRowError) error
}

type Policy struct {
	DefaultCurrency  kernel.Currency
	ImportStaleAfter time.Duration
	ImportProgress   int
	MaxImportBytes   int64
	MaxInlineRows    int
}

func DefaultPolicy() Policy {
	return Policy{
		DefaultCurrency:  kernel.KZT,
		ImportStaleAfter: 15 * time.Minute,
		ImportProgress:   500,
		MaxImportBytes:   50 << 20,
		MaxInlineRows:    1000,
	}
}
