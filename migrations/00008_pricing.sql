-- +goose Up
CREATE SCHEMA IF NOT EXISTS pricing;

CREATE TABLE pricing.offer_prices (
    sku         TEXT        PRIMARY KEY,
    product_id  UUID        NOT NULL,
    seller_id   UUID        NOT NULL,
    amount      BIGINT      NOT NULL,
    compare_at  BIGINT,
    currency    TEXT        NOT NULL,
    active      BOOLEAN     NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL,
    version     INT         NOT NULL,

    CONSTRAINT chk_offer_prices_amount CHECK (amount > 0),
    CONSTRAINT chk_offer_prices_compare_at CHECK (compare_at IS NULL OR compare_at > amount)
);

CREATE INDEX idx_offer_prices_product ON pricing.offer_prices (product_id);

CREATE TABLE pricing.product_categories (
    product_id     UUID        PRIMARY KEY,
    category_path  UUID[]      NOT NULL,
    updated_at     TIMESTAMPTZ NOT NULL
);

CREATE TABLE pricing.promotions (
    id                 UUID        PRIMARY KEY,
    name               TEXT        NOT NULL,
    kind               TEXT        NOT NULL,
    basis_points       INT         NOT NULL DEFAULT 0,
    amount             BIGINT      NOT NULL DEFAULT 0,
    currency           TEXT        NOT NULL DEFAULT '',
    buy_quantity       INT         NOT NULL DEFAULT 0,
    free_units         INT         NOT NULL DEFAULT 0,
    target_skus        TEXT[]      NOT NULL DEFAULT '{}',
    target_sellers     UUID[]      NOT NULL DEFAULT '{}',
    target_categories  UUID[]      NOT NULL DEFAULT '{}',
    priority           INT         NOT NULL,
    exclusive          BOOLEAN     NOT NULL,
    status             TEXT        NOT NULL,
    starts_at          TIMESTAMPTZ,
    ends_at            TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL,
    updated_at         TIMESTAMPTZ NOT NULL,
    version            INT         NOT NULL,

    CONSTRAINT chk_promotions_kind CHECK (kind IN ('percentage', 'fixed', 'buy_n_get_m')),
    CONSTRAINT chk_promotions_status CHECK (status IN ('draft', 'active', 'paused', 'ended')),
    CONSTRAINT chk_promotions_period CHECK (ends_at IS NULL OR starts_at IS NULL OR ends_at > starts_at)
);

CREATE INDEX idx_promotions_running ON pricing.promotions (priority DESC) WHERE status = 'active';
CREATE INDEX idx_promotions_created ON pricing.promotions (created_at DESC, id DESC);

CREATE TABLE pricing.promo_codes (
    code                TEXT        PRIMARY KEY,
    kind                TEXT        NOT NULL,
    basis_points        INT         NOT NULL DEFAULT 0,
    amount              BIGINT      NOT NULL DEFAULT 0,
    currency            TEXT        NOT NULL,
    min_cart_amount     BIGINT      NOT NULL DEFAULT 0,
    total_limit         INT         NOT NULL DEFAULT 0,
    per_customer_limit  INT         NOT NULL DEFAULT 0,
    used                INT         NOT NULL DEFAULT 0,
    status              TEXT        NOT NULL,
    starts_at           TIMESTAMPTZ,
    ends_at             TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL,
    updated_at          TIMESTAMPTZ NOT NULL,
    version             INT         NOT NULL,

    CONSTRAINT chk_promo_codes_kind CHECK (kind IN ('percentage', 'fixed')),
    CONSTRAINT chk_promo_codes_status CHECK (status IN ('active', 'disabled')),
    CONSTRAINT chk_promo_codes_used CHECK (used >= 0 AND (total_limit = 0 OR used <= total_limit))
);

CREATE TABLE pricing.promo_redemptions (
    code         TEXT        NOT NULL REFERENCES pricing.promo_codes (code),
    order_id     UUID        NOT NULL,
    customer_id  UUID        NOT NULL,
    amount       BIGINT      NOT NULL,
    currency     TEXT        NOT NULL,
    redeemed_at  TIMESTAMPTZ NOT NULL,

    PRIMARY KEY (code, order_id)
);

CREATE INDEX idx_promo_redemptions_customer ON pricing.promo_redemptions (code, customer_id);

-- +goose Down
DROP TABLE IF EXISTS pricing.promo_redemptions;
DROP TABLE IF EXISTS pricing.promo_codes;
DROP TABLE IF EXISTS pricing.promotions;
DROP TABLE IF EXISTS pricing.product_categories;
DROP TABLE IF EXISTS pricing.offer_prices;
DROP SCHEMA IF EXISTS pricing;
