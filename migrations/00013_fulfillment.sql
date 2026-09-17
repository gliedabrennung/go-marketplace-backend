-- +goose Up
ALTER TABLE ordering.orders ADD COLUMN delivered_at TIMESTAMPTZ;
ALTER TABLE ordering.order_items ADD COLUMN category_id UUID;

CREATE INDEX idx_orders_delivered ON ordering.orders (delivered_at) WHERE status = 'delivered';

-- +goose Down
DROP INDEX IF EXISTS ordering.idx_orders_delivered;
ALTER TABLE ordering.order_items DROP COLUMN IF EXISTS category_id;
ALTER TABLE ordering.orders DROP COLUMN IF EXISTS delivered_at;
