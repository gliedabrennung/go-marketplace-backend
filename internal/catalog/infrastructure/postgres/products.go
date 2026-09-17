package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/outbox"
	platform "github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/postgres"
)

const productColumns = `id::text, category_id::text, seller_id::text, title, description, brand, status,
	rejection_reason, created_at, updated_at, published_at, version`

type productRepository struct {
	q      platform.Querier
	events *outbox.Writer
}

func (r productRepository) FindByID(ctx context.Context, id domain.ProductID) (*domain.Product, error) {
	snap, err := loadProduct(ctx, r.q, id.String())
	if err != nil {
		return nil, err
	}
	return domain.RehydrateProduct(snap)
}

func (r productRepository) Save(ctx context.Context, product *domain.Product) error {
	snap := product.Snapshot()
	var err error
	if snap.Version == 0 {
		err = exec(ctx, r.q, "insert product", `
			INSERT INTO catalog.products (id, category_id, seller_id, title, description, brand, status, rejection_reason,
				created_at, updated_at, published_at, version)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, 1)`,
			snap.ID, snap.CategoryID, snap.SellerID, snap.Title, snap.Description, snap.Brand, snap.Status,
			nullable(snap.RejectionReason), utc(snap.CreatedAt), utc(snap.UpdatedAt), optionalTime(snap.PublishedAt))
	} else {
		err = update(ctx, r.q, "update product", `
			UPDATE catalog.products
			SET title = $2, description = $3, brand = $4, status = $5, rejection_reason = $6,
				updated_at = $7, published_at = $8, version = version + 1
			WHERE id = $1 AND version = $9`,
			snap.ID, snap.Title, snap.Description, snap.Brand, snap.Status, nullable(snap.RejectionReason),
			utc(snap.UpdatedAt), optionalTime(snap.PublishedAt), snap.Version)
	}
	if err != nil {
		return err
	}
	if err := replaceProductAttributes(ctx, r.q, snap); err != nil {
		return err
	}
	if err := replaceProductImages(ctx, r.q, snap); err != nil {
		return err
	}
	product.AdvanceVersion()
	return r.events.Write(ctx, r.q, product.PullEvents())
}

func replaceProductAttributes(ctx context.Context, q platform.Querier, snap domain.ProductSnapshot) error {
	if err := exec(ctx, q, "delete product attributes",
		`DELETE FROM catalog.product_attributes WHERE product_id = $1`, snap.ID); err != nil {
		return err
	}
	for code, value := range snap.Attributes {
		err := exec(ctx, q, "insert product attribute", `
			INSERT INTO catalog.product_attributes (product_id, code, type, value) VALUES ($1, $2, $3, $4)`,
			snap.ID, code, value.Type, value.Value)
		if err != nil {
			return err
		}
	}
	return nil
}

func replaceProductImages(ctx context.Context, q platform.Querier, snap domain.ProductSnapshot) error {
	if err := exec(ctx, q, "delete product images",
		`DELETE FROM catalog.product_images WHERE product_id = $1`, snap.ID); err != nil {
		return err
	}
	for i, img := range snap.Images {
		err := exec(ctx, q, "insert product image", `
			INSERT INTO catalog.product_images (product_id, id, content_type, size, status, width, height, created_at, position)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			snap.ID, img.ID, img.ContentType, img.Size, img.Status, img.Width, img.Height, utc(img.CreatedAt), i)
		if err != nil {
			return err
		}
	}
	return nil
}

func loadProduct(ctx context.Context, q platform.Querier, id string) (domain.ProductSnapshot, error) {
	snap, err := scanProduct(q.QueryRow(ctx, `SELECT `+productColumns+` FROM catalog.products WHERE id = $1`, id))
	if err != nil {
		return domain.ProductSnapshot{}, notFound(err, domain.ErrProductNotFound)
	}
	if snap.Attributes, err = loadProductAttributes(ctx, q, id); err != nil {
		return domain.ProductSnapshot{}, err
	}
	if snap.Images, err = loadProductImages(ctx, q, id); err != nil {
		return domain.ProductSnapshot{}, err
	}
	return snap, nil
}

func loadProductAttributes(ctx context.Context, q platform.Querier, id string) (map[string]domain.AttributeValueSnapshot, error) {
	rows, err := q.Query(ctx, `SELECT code, type, value FROM catalog.product_attributes WHERE product_id = $1`, id)
	if err != nil {
		return nil, fmt.Errorf("select product attributes: %w", err)
	}
	defer rows.Close()
	out := map[string]domain.AttributeValueSnapshot{}
	for rows.Next() {
		var (
			code  string
			value domain.AttributeValueSnapshot
		)
		if err := rows.Scan(&code, &value.Type, &value.Value); err != nil {
			return nil, fmt.Errorf("scan product attribute: %w", err)
		}
		out[code] = value
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read product attributes: %w", err)
	}
	return out, nil
}

func loadProductImages(ctx context.Context, q platform.Querier, id string) ([]domain.ImageSnapshot, error) {
	rows, err := q.Query(ctx, `
		SELECT id::text, content_type, size, status, width, height, created_at
		FROM catalog.product_images WHERE product_id = $1 ORDER BY position`, id)
	if err != nil {
		return nil, fmt.Errorf("select product images: %w", err)
	}
	images, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.ImageSnapshot, error) {
		var img domain.ImageSnapshot
		err := row.Scan(&img.ID, &img.ContentType, &img.Size, &img.Status, &img.Width, &img.Height, &img.CreatedAt)
		img.CreatedAt = utc(img.CreatedAt)
		return img, err
	})
	if err != nil {
		return nil, fmt.Errorf("scan product images: %w", err)
	}
	return images, nil
}

func scanProduct(row pgx.Row) (domain.ProductSnapshot, error) {
	var (
		snap        domain.ProductSnapshot
		rejection   *string
		publishedAt *time.Time
	)
	err := row.Scan(&snap.ID, &snap.CategoryID, &snap.SellerID, &snap.Title, &snap.Description, &snap.Brand,
		&snap.Status, &rejection, &snap.CreatedAt, &snap.UpdatedAt, &publishedAt, &snap.Version)
	if err != nil {
		return domain.ProductSnapshot{}, err
	}
	snap.RejectionReason = deref(rejection)
	snap.CreatedAt, snap.UpdatedAt = utc(snap.CreatedAt), utc(snap.UpdatedAt)
	snap.PublishedAt = moment(publishedAt)
	return snap, nil
}
