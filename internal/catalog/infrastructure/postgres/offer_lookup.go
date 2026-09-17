package postgres

import (
	"context"
	"fmt"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
	platform "github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/postgres"
)

type OfferLookup struct {
	db platform.Querier
}

func NewOfferLookup(db platform.Querier) *OfferLookup {
	return &OfferLookup{db: db}
}

func (l *OfferLookup) Offers(ctx context.Context, offerIDs []string) (map[string]api.OfferSummary, error) {
	rows, err := l.db.Query(ctx, `
		SELECT o.id::text, o.product_id::text, o.seller_id::text, o.seller_sku, p.title, o.status,
			o.price_amount, o.currency, p.status = 'published', p.category_id::text,
			(SELECT i.id::text FROM catalog.product_images i
				WHERE i.product_id = p.id AND i.status = 'processed' ORDER BY i.position LIMIT 1)
		FROM catalog.offers o
		JOIN catalog.products p ON p.id = o.product_id
		WHERE o.id::text = ANY ($1)`, offerIDs)
	if err != nil {
		return nil, fmt.Errorf("select offer summaries: %w", err)
	}
	defer rows.Close()

	out := make(map[string]api.OfferSummary, len(offerIDs))
	for rows.Next() {
		var (
			summary api.OfferSummary
			coverID *string
		)
		if err := rows.Scan(&summary.OfferID, &summary.ProductID, &summary.SellerID, &summary.SellerSKU, &summary.Title,
			&summary.Status, &summary.PriceAmount, &summary.Currency, &summary.ProductPublished, &summary.CategoryID, &coverID); err != nil {
			return nil, fmt.Errorf("scan offer summary: %w", err)
		}
		if coverID != nil {
			productID, err := domain.ParseProductID(summary.ProductID)
			if err != nil {
				return nil, err
			}
			imageID, err := domain.ParseImageID(*coverID)
			if err != nil {
				return nil, err
			}
			summary.CoverKey = domain.ImageObjectKey(productID, imageID, domain.ImageVariantSmall)
		}
		out[summary.OfferID] = summary
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read offer summaries: %w", err)
	}
	return out, nil
}
