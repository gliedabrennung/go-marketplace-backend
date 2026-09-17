-- +goose Up
CREATE SCHEMA IF NOT EXISTS settlement;

CREATE TABLE settlement.entries (
    id          UUID        PRIMARY KEY,
    order_id    UUID        NOT NULL,
    seller_id   UUID        NOT NULL,
    currency    TEXT        NOT NULL,
    gross       BIGINT      NOT NULL,
    commission  BIGINT      NOT NULL,
    net         BIGINT      NOT NULL,
    accrued_at  TIMESTAMPTZ NOT NULL,

    CONSTRAINT uq_settlement_entries_order_seller UNIQUE (order_id, seller_id),
    CONSTRAINT chk_settlement_entries_amounts CHECK (gross >= 0 AND commission >= 0 AND net = gross - commission AND net >= 0)
);

CREATE INDEX idx_settlement_entries_seller ON settlement.entries (seller_id, accrued_at);

-- +goose Down
DROP TABLE IF EXISTS settlement.entries;
DROP SCHEMA IF EXISTS settlement;
