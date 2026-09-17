-- +goose Up
CREATE SCHEMA IF NOT EXISTS inventory;

CREATE TABLE inventory.stock_items (
    sku         TEXT        PRIMARY KEY,
    seller_id   UUID        NOT NULL,
    available   INT         NOT NULL DEFAULT 0,
    reserved    INT         NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL,
    version     INT         NOT NULL,

    CONSTRAINT chk_available_non_negative CHECK (available >= 0),
    CONSTRAINT chk_reserved_non_negative CHECK (reserved >= 0)
);

CREATE INDEX idx_stock_items_seller ON inventory.stock_items (seller_id, sku);

CREATE TABLE inventory.reservations (
    id           UUID        NOT NULL,
    sku          TEXT        NOT NULL REFERENCES inventory.stock_items (sku) ON DELETE CASCADE,
    order_id     UUID,
    quantity     INT         NOT NULL CHECK (quantity > 0),
    status       TEXT        NOT NULL,
    expires_at   TIMESTAMPTZ NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL,
    resolved_at  TIMESTAMPTZ,

    PRIMARY KEY (id, sku),
    CONSTRAINT chk_reservations_status CHECK (status IN ('held', 'committed', 'released', 'expired')),
    CONSTRAINT chk_reservations_resolved CHECK ((status = 'held') = (resolved_at IS NULL))
);

CREATE INDEX idx_reservations_expiring ON inventory.reservations (expires_at) WHERE status = 'held';
CREATE INDEX idx_reservations_order ON inventory.reservations (order_id);

CREATE TABLE inventory.stock_movements (
    id            BIGSERIAL   PRIMARY KEY,
    sku           TEXT        NOT NULL,
    delta         INT         NOT NULL,
    reason        TEXT        NOT NULL,
    reference_id  TEXT        NOT NULL,
    occurred_at   TIMESTAMPTZ NOT NULL,

    CONSTRAINT chk_movement_reason CHECK (reason IN ('reserve', 'release', 'commit', 'restock', 'return', 'correction')),
    CONSTRAINT uq_movement_idempotent UNIQUE (reason, reference_id, sku)
);

CREATE INDEX idx_stock_movements_sku ON inventory.stock_movements (sku, id DESC);

-- +goose Down
DROP TABLE IF EXISTS inventory.stock_movements;
DROP TABLE IF EXISTS inventory.reservations;
DROP TABLE IF EXISTS inventory.stock_items;
DROP SCHEMA IF EXISTS inventory;
