-- +goose Up
CREATE SCHEMA IF NOT EXISTS cart;

CREATE TABLE cart.carts (
    id          UUID        PRIMARY KEY,
    owner_kind  TEXT        NOT NULL,
    owner_id    TEXT        NOT NULL,
    currency    TEXT        NOT NULL DEFAULT '',
    promo_code  TEXT        NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL,
    expires_at  TIMESTAMPTZ,
    version     INT         NOT NULL,

    CONSTRAINT uq_carts_owner UNIQUE (owner_kind, owner_id),
    CONSTRAINT chk_carts_owner_kind CHECK (owner_kind IN ('user', 'device')),
    CONSTRAINT chk_carts_expiry CHECK ((owner_kind = 'device') = (expires_at IS NOT NULL))
);

CREATE INDEX idx_carts_expires ON cart.carts (expires_at) WHERE expires_at IS NOT NULL;

CREATE TABLE cart.items (
    cart_id     UUID        NOT NULL REFERENCES cart.carts (id) ON DELETE CASCADE,
    sku         TEXT        NOT NULL,
    seller_id   UUID        NOT NULL,
    quantity    INT         NOT NULL,
    price       BIGINT      NOT NULL,
    position    INT         NOT NULL,
    added_at    TIMESTAMPTZ NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL,

    PRIMARY KEY (cart_id, sku),
    CONSTRAINT chk_items_quantity CHECK (quantity > 0),
    CONSTRAINT chk_items_price CHECK (price > 0)
);

-- +goose Down
DROP TABLE IF EXISTS cart.items;
DROP TABLE IF EXISTS cart.carts;
DROP SCHEMA IF EXISTS cart;
