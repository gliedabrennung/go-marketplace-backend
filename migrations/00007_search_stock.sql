-- +goose Up
ALTER TABLE search.product_offers ADD COLUMN available INT NOT NULL DEFAULT 0;

CREATE INDEX idx_search_offers_in_stock ON search.product_offers (product_id) WHERE status = 'active' AND available > 0;

-- +goose Down
DROP INDEX IF EXISTS search.idx_search_offers_in_stock;
ALTER TABLE search.product_offers DROP COLUMN IF EXISTS available;
