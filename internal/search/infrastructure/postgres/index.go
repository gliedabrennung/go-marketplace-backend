package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/gliedabrennung/go-marketplace-backend/internal/search/application"
	platform "github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/postgres"
)

const (
	tableA = "documents_a"
	tableB = "documents_b"
)

type Index struct {
	db platform.Querier
}

func NewIndex(db platform.Querier) *Index {
	return &Index{db: db}
}

func qualify(table string) (string, error) {
	if table != tableA && table != tableB {
		return "", fmt.Errorf("unknown index table %q", table)
	}
	return "search." + table, nil
}

func (i *Index) ActiveTable(ctx context.Context) (string, error) {
	var active string
	if err := i.db.QueryRow(ctx, `SELECT active FROM search.index_state WHERE id`).Scan(&active); err != nil {
		return "", fmt.Errorf("read index state: %w", err)
	}
	return active, nil
}

func (i *Index) InactiveTable(ctx context.Context) (string, error) {
	active, err := i.ActiveTable(ctx)
	if err != nil {
		return "", err
	}
	if active == tableA {
		return tableB, nil
	}
	return tableA, nil
}

func (i *Index) SaveDocuments(ctx context.Context, table string, documents []application.Document, at time.Time) error {
	name, err := qualify(table)
	if err != nil {
		return err
	}
	for _, doc := range documents {
		attributes, err := json.Marshal(doc.Attributes)
		if err != nil {
			return fmt.Errorf("encode document attributes: %w", err)
		}
		numbers, err := json.Marshal(doc.Numbers)
		if err != nil {
			return fmt.Errorf("encode document numbers: %w", err)
		}
		_, err = i.db.Exec(ctx, `
			INSERT INTO `+name+` (product_id, seller_id, category_id, category_path, title, description, brand,
				cover_key, attributes, numbers, published_at, indexed_at)
			VALUES ($1, $2, $3, $4::uuid[], $5, $6, $7, $8, $9, $10, $11, $12)
			ON CONFLICT (product_id) DO UPDATE SET
				seller_id = EXCLUDED.seller_id, category_id = EXCLUDED.category_id, category_path = EXCLUDED.category_path,
				title = EXCLUDED.title, description = EXCLUDED.description, brand = EXCLUDED.brand,
				cover_key = EXCLUDED.cover_key, attributes = EXCLUDED.attributes, numbers = EXCLUDED.numbers,
				published_at = EXCLUDED.published_at, indexed_at = EXCLUDED.indexed_at`,
			doc.ProductID, doc.SellerID, doc.CategoryID, doc.CategoryPath, doc.Title, doc.Description, doc.Brand,
			doc.CoverKey, attributes, numbers, doc.PublishedAt.UTC(), at.UTC())
		if err != nil {
			return fmt.Errorf("index document %s: %w", doc.ProductID, err)
		}
	}
	return nil
}

func (i *Index) SetCover(ctx context.Context, table, productID, coverKey string, at time.Time) error {
	name, err := qualify(table)
	if err != nil {
		return err
	}
	if _, err := i.db.Exec(ctx,
		`UPDATE `+name+` SET cover_key = $2, indexed_at = $3 WHERE product_id = $1`,
		productID, coverKey, at.UTC()); err != nil {
		return fmt.Errorf("update cover: %w", err)
	}
	return nil
}

func (i *Index) Truncate(ctx context.Context, table string) error {
	name, err := qualify(table)
	if err != nil {
		return err
	}
	if _, err := i.db.Exec(ctx, `TRUNCATE `+name); err != nil {
		return fmt.Errorf("truncate index: %w", err)
	}
	return nil
}

func (i *Index) Swap(ctx context.Context, table string, at time.Time) error {
	name, err := qualify(table)
	if err != nil {
		return err
	}
	if _, err := i.db.Exec(ctx, `CREATE OR REPLACE VIEW search.documents AS SELECT * FROM `+name); err != nil {
		return fmt.Errorf("swap index view: %w", err)
	}
	if _, err := i.db.Exec(ctx,
		`UPDATE search.index_state SET active = $1, rebuilt_at = $2 WHERE id`, table, at.UTC()); err != nil {
		return fmt.Errorf("update index state: %w", err)
	}
	return nil
}

func (i *Index) SaveOffer(ctx context.Context, offer application.OfferState) error {
	_, err := i.db.Exec(ctx, `
		INSERT INTO search.product_offers (offer_id, product_id, seller_id, price_amount, currency, condition, status, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (offer_id) DO UPDATE SET
			product_id = EXCLUDED.product_id, seller_id = EXCLUDED.seller_id, price_amount = EXCLUDED.price_amount,
			currency = EXCLUDED.currency, condition = EXCLUDED.condition, status = EXCLUDED.status,
			updated_at = EXCLUDED.updated_at`,
		offer.OfferID, offer.ProductID, offer.SellerID, offer.Price, offer.Currency, offer.Condition, offer.Status,
		offer.UpdatedAt.UTC())
	if err != nil {
		return fmt.Errorf("index offer: %w", err)
	}
	return nil
}

func (i *Index) SaveStock(ctx context.Context, sku string, available int, at time.Time) (string, error) {
	var productID string
	err := i.db.QueryRow(ctx, `
		UPDATE search.product_offers SET available = $2, updated_at = $3
		WHERE offer_id = $1 RETURNING product_id::text`, sku, available, at.UTC()).Scan(&productID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("index stock: %w", err)
	}
	return productID, nil
}

func (i *Index) SaveSeller(ctx context.Context, state application.SellerState, at time.Time) error {
	_, err := i.db.Exec(ctx, `
		INSERT INTO search.sellers (seller_id, can_sell, updated_at) VALUES ($1, $2, $3)
		ON CONFLICT (seller_id) DO UPDATE SET can_sell = EXCLUDED.can_sell, updated_at = EXCLUDED.updated_at`,
		state.SellerID, state.CanSell, at.UTC())
	if err != nil {
		return fmt.Errorf("index seller: %w", err)
	}
	return nil
}

const refreshStats = `
	INSERT INTO search.product_stats (product_id, min_price, currency, offers, sellers, conditions, updated_at)
	SELECT t.product_id,
		MIN(o.price_amount),
		COALESCE(MIN(o.currency), 'KZT'),
		COUNT(o.offer_id),
		COUNT(DISTINCT o.seller_id),
		COALESCE(array_agg(DISTINCT o.condition) FILTER (WHERE o.condition IS NOT NULL), '{}'),
		$2
	FROM (%s) t
	LEFT JOIN search.product_offers o
		ON o.product_id = t.product_id AND o.status = 'active' AND o.available > 0
		AND EXISTS (SELECT 1 FROM search.sellers s WHERE s.seller_id = o.seller_id AND s.can_sell)
	GROUP BY t.product_id
	ON CONFLICT (product_id) DO UPDATE SET
		min_price = EXCLUDED.min_price, currency = EXCLUDED.currency, offers = EXCLUDED.offers,
		sellers = EXCLUDED.sellers, conditions = EXCLUDED.conditions, updated_at = EXCLUDED.updated_at`

func (i *Index) RefreshProduct(ctx context.Context, productID string, at time.Time) error {
	sql := fmt.Sprintf(refreshStats, `SELECT $1::uuid AS product_id`)
	if _, err := i.db.Exec(ctx, sql, productID, at.UTC()); err != nil {
		return fmt.Errorf("refresh product stats: %w", err)
	}
	return nil
}

func (i *Index) RefreshSeller(ctx context.Context, sellerID string, at time.Time) error {
	sql := fmt.Sprintf(refreshStats, `SELECT DISTINCT product_id FROM search.product_offers WHERE seller_id = $1::uuid`)
	if _, err := i.db.Exec(ctx, sql, sellerID, at.UTC()); err != nil {
		return fmt.Errorf("refresh seller stats: %w", err)
	}
	return nil
}

func (i *Index) RefreshLexicon(ctx context.Context, minWeight int) (int, error) {
	if _, err := i.db.Exec(ctx, `TRUNCATE search.lexicon`); err != nil {
		return 0, fmt.Errorf("truncate lexicon: %w", err)
	}
	tag, err := i.db.Exec(ctx, `
		INSERT INTO search.lexicon (word, weight)
		SELECT word, ndoc FROM ts_stat('SELECT document FROM search.documents')
		WHERE ndoc >= $1 AND length(word) > 2`, minWeight)
	if err != nil {
		return 0, fmt.Errorf("rebuild lexicon: %w", err)
	}
	return int(tag.RowsAffected()), nil
}
