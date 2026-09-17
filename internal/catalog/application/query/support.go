package query

import (
	"context"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
	sellerapi "github.com/gliedabrennung/go-marketplace-backend/internal/seller/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/pagination"
)

var ErrUnknownStatus = kernel.Validation("CATALOG_UNKNOWN_STATUS", "status filter is unknown")

type ReadModel interface {
	CategoryTree(ctx context.Context) ([]CategoryNode, error)
	Category(ctx context.Context, categoryID string) (CategoryView, error)
	Product(ctx context.Context, productID string) (ProductView, error)
	ProductVariants(ctx context.Context, productID string, publishedOnly bool) (*VariantView, error)
	ProductOffers(ctx context.Context, productID, status string) ([]OfferView, error)
	SellerProducts(ctx context.Context, sellerID, status string, limit int, after *pagination.Keyset) (pagination.Page[ProductSummary], error)
	ModerationQueue(ctx context.Context, limit int, after *pagination.Keyset) (pagination.Page[ProductSummary], error)
	SellerOffers(ctx context.Context, sellerID, status string, limit int, after *pagination.Keyset) (pagination.Page[OfferView], error)
	ImportJob(ctx context.Context, jobID string) (ImportJobView, error)
	SellerImports(ctx context.Context, sellerID string, limit int, after *pagination.Keyset) (pagination.Page[ImportJobView], error)
	StaleImportIDs(ctx context.Context, before time.Time, limit int) ([]string, error)
}

type URLSigner interface {
	SignDownload(ctx context.Context, key string) (string, error)
}

func isMember(ctx context.Context, sellers application.SellerDirectory, p auth.Principal, sellerID string) (bool, error) {
	if p.UserID == "" {
		return false, nil
	}
	_, member, err := sellers.MemberRole(ctx, sellerID, p.UserID)
	return member, err
}

func requireMember(ctx context.Context, sellers application.SellerDirectory, p auth.Principal, sellerID string) error {
	if p.UserID == "" {
		return auth.ErrUnauthenticated
	}
	if _, err := kernel.ParseSellerID(sellerID); err != nil {
		return sellerapi.ErrSellerNotFound
	}
	member, err := isMember(ctx, sellers, p, sellerID)
	if err != nil {
		return err
	}
	if !member {
		return sellerapi.ErrSellerNotFound
	}
	return nil
}

func keyset(cursor string) (*pagination.Keyset, error) {
	k, ok, err := pagination.DecodeKeyset(cursor)
	if err != nil || !ok {
		return nil, err
	}
	return &k, nil
}

func validStatus[S ~string](raw string, known ...S) error {
	if raw == "" {
		return nil
	}
	for _, s := range known {
		if string(s) == raw {
			return nil
		}
	}
	return ErrUnknownStatus.WithDetail("%q", raw)
}

func imageKey(productID, imageID, variant string) (string, bool) {
	pid, err := domain.ParseProductID(productID)
	if err != nil {
		return "", false
	}
	iid, err := domain.ParseImageID(imageID)
	if err != nil {
		return "", false
	}
	return domain.ImageObjectKey(pid, iid, variant), true
}
