package postgres

import (
	"context"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
	platform "github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/postgres"
)

type Feed struct {
	db platform.Querier
}

func NewFeed(db platform.Querier) *Feed {
	return &Feed{db: db}
}

func (f *Feed) ScanPublished(ctx context.Context, cursor api.FeedCursor, limit int) ([]api.ProductDocument, error) {
	rows, err := f.db.Query(ctx, `
		SELECT p.id::text, p.seller_id::text, p.category_id::text, c.ancestors::text[] || c.id::text,
			p.title, p.description, p.brand, p.published_at, p.updated_at,
			(SELECT i.id::text FROM catalog.product_images i
				WHERE i.product_id = p.id AND i.status = 'processed' ORDER BY i.position LIMIT 1)
		FROM catalog.products p
		JOIN catalog.categories c ON c.id = p.category_id
		WHERE p.status = 'published' AND p.updated_at >= $1 AND ($2 = '' OR p.id::text > $2)
		ORDER BY p.id
		LIMIT $3`, cursor.Since.UTC(), cursor.AfterID, limit)
	if err != nil {
		return nil, fmt.Errorf("scan published products: %w", err)
	}
	documents, err := pgx.CollectRows(rows, scanDocument)
	if err != nil {
		return nil, fmt.Errorf("read published products: %w", err)
	}
	if len(documents) == 0 {
		return documents, nil
	}
	return f.withAttributes(ctx, documents)
}

func scanDocument(row pgx.CollectableRow) (api.ProductDocument, error) {
	var (
		doc     api.ProductDocument
		coverID *string
	)
	err := row.Scan(&doc.ProductID, &doc.SellerID, &doc.CategoryID, &doc.CategoryPath,
		&doc.Title, &doc.Description, &doc.Brand, &doc.PublishedAt, &doc.UpdatedAt, &coverID)
	if err != nil {
		return api.ProductDocument{}, err
	}
	doc.PublishedAt, doc.UpdatedAt = utc(doc.PublishedAt), utc(doc.UpdatedAt)
	if coverID != nil {
		productID, err := domain.ParseProductID(doc.ProductID)
		if err != nil {
			return api.ProductDocument{}, err
		}
		imageID, err := domain.ParseImageID(*coverID)
		if err != nil {
			return api.ProductDocument{}, err
		}
		doc.CoverKey = domain.ImageObjectKey(productID, imageID, domain.ImageVariantSmall)
	}
	return doc, nil
}

func (f *Feed) withAttributes(ctx context.Context, documents []api.ProductDocument) ([]api.ProductDocument, error) {
	ids := make([]string, len(documents))
	for i, doc := range documents {
		ids[i] = doc.ProductID
	}
	rows, err := f.db.Query(ctx, `
		SELECT pa.product_id::text, pa.code, pa.type, pa.value,
			COALESCE(ca.name, pa.code), COALESCE(ca.unit, ''), COALESCE(ca.filterable, false)
		FROM catalog.product_attributes pa
		JOIN catalog.products p ON p.id = pa.product_id
		JOIN catalog.categories c ON c.id = p.category_id
		LEFT JOIN catalog.category_attributes ca
			ON ca.code = pa.code AND (ca.category_id = c.id OR ca.category_id = ANY (c.ancestors))
		WHERE pa.product_id::text = ANY ($1)
		ORDER BY pa.product_id, pa.code`, ids)
	if err != nil {
		return nil, fmt.Errorf("select feed attributes: %w", err)
	}
	defer rows.Close()

	byProduct := map[string][]api.ProductAttributeV1{}
	for rows.Next() {
		var (
			productID string
			attribute api.ProductAttributeV1
		)
		err := rows.Scan(&productID, &attribute.Code, &attribute.Type, &attribute.Value,
			&attribute.Name, &attribute.Unit, &attribute.Filterable)
		if err != nil {
			return nil, fmt.Errorf("scan feed attribute: %w", err)
		}
		if number, err := strconv.ParseFloat(attribute.Value, 64); err == nil &&
			(attribute.Type == "number" || attribute.Type == "unit") {
			attribute.Number = &number
		}
		byProduct[productID] = append(byProduct[productID], attribute)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read feed attributes: %w", err)
	}
	for i := range documents {
		documents[i].Attributes = byProduct[documents[i].ProductID]
	}
	return documents, nil
}
