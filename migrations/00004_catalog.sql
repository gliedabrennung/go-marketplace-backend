-- +goose Up
CREATE SCHEMA IF NOT EXISTS catalog;

CREATE TABLE catalog.categories (
    id          UUID        PRIMARY KEY,
    parent_id   UUID        REFERENCES catalog.categories (id),
    name        TEXT        NOT NULL,
    slug        TEXT        NOT NULL,
    ancestors   UUID[]      NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL,
    version     INT         NOT NULL,

    CONSTRAINT chk_categories_depth CHECK (cardinality(ancestors) <= 5)
);

CREATE UNIQUE INDEX uq_categories_sibling_slug
    ON catalog.categories (COALESCE(parent_id, '00000000-0000-0000-0000-000000000000'::uuid), slug);
CREATE INDEX idx_categories_ancestors ON catalog.categories USING GIN (ancestors);

CREATE TABLE catalog.category_attributes (
    category_id  UUID    NOT NULL REFERENCES catalog.categories (id) ON DELETE CASCADE,
    code         TEXT    NOT NULL,
    name         TEXT    NOT NULL,
    type         TEXT    NOT NULL,
    required     BOOLEAN NOT NULL DEFAULT false,
    filterable   BOOLEAN NOT NULL DEFAULT false,
    options      TEXT[]  NOT NULL DEFAULT '{}',
    unit         TEXT    NOT NULL DEFAULT '',
    position     INT     NOT NULL,

    PRIMARY KEY (category_id, code),
    CONSTRAINT chk_category_attributes_type CHECK (type IN ('string', 'number', 'boolean', 'enum', 'unit'))
);

CREATE TABLE catalog.products (
    id                UUID        PRIMARY KEY,
    category_id       UUID        NOT NULL REFERENCES catalog.categories (id),
    seller_id         UUID        NOT NULL,
    title             TEXT        NOT NULL,
    description       TEXT        NOT NULL DEFAULT '',
    brand             TEXT        NOT NULL DEFAULT '',
    status            TEXT        NOT NULL,
    rejection_reason  TEXT,
    created_at        TIMESTAMPTZ NOT NULL,
    updated_at        TIMESTAMPTZ NOT NULL,
    published_at      TIMESTAMPTZ,
    version           INT         NOT NULL,

    CONSTRAINT chk_products_status CHECK (status IN ('draft', 'on_moderation', 'published', 'rejected')),
    CONSTRAINT chk_products_published CHECK (status <> 'published' OR published_at IS NOT NULL)
);

CREATE INDEX idx_products_seller ON catalog.products (seller_id, created_at DESC, id DESC);
CREATE INDEX idx_products_moderation ON catalog.products (updated_at, id) WHERE status = 'on_moderation';
CREATE INDEX idx_products_category ON catalog.products (category_id) WHERE status = 'published';

CREATE TABLE catalog.product_attributes (
    product_id  UUID NOT NULL REFERENCES catalog.products (id) ON DELETE CASCADE,
    code        TEXT NOT NULL,
    type        TEXT NOT NULL,
    value       TEXT NOT NULL,

    PRIMARY KEY (product_id, code)
);

CREATE TABLE catalog.product_images (
    product_id    UUID        NOT NULL REFERENCES catalog.products (id) ON DELETE CASCADE,
    id            UUID        NOT NULL,
    content_type  TEXT        NOT NULL,
    size          BIGINT      NOT NULL,
    status        TEXT        NOT NULL,
    width         INT         NOT NULL DEFAULT 0,
    height        INT         NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL,
    position      INT         NOT NULL,

    PRIMARY KEY (product_id, id),
    CONSTRAINT chk_product_images_status CHECK (status IN ('awaiting_upload', 'uploaded', 'processed'))
);

CREATE TABLE catalog.variant_groups (
    id           UUID        PRIMARY KEY,
    category_id  UUID        NOT NULL REFERENCES catalog.categories (id),
    seller_id    UUID        NOT NULL,
    axes         TEXT[]      NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL,
    updated_at   TIMESTAMPTZ NOT NULL,
    version      INT         NOT NULL,

    CONSTRAINT chk_variant_groups_axes CHECK (cardinality(axes) BETWEEN 1 AND 3)
);

CREATE INDEX idx_variant_groups_seller ON catalog.variant_groups (seller_id);

CREATE TABLE catalog.variant_members (
    group_id     UUID  NOT NULL REFERENCES catalog.variant_groups (id) ON DELETE CASCADE,
    product_id   UUID  NOT NULL REFERENCES catalog.products (id) ON DELETE CASCADE,
    axis_values  JSONB NOT NULL,
    position     INT   NOT NULL,

    PRIMARY KEY (group_id, product_id)
);

CREATE UNIQUE INDEX uq_variant_members_product ON catalog.variant_members (product_id);

CREATE TABLE catalog.offers (
    id               UUID        PRIMARY KEY,
    product_id       UUID        NOT NULL REFERENCES catalog.products (id),
    seller_id        UUID        NOT NULL,
    seller_sku       TEXT        NOT NULL,
    price_amount     BIGINT      NOT NULL,
    currency         TEXT        NOT NULL,
    condition        TEXT        NOT NULL,
    processing_days  INT         NOT NULL,
    status           TEXT        NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL,
    updated_at       TIMESTAMPTZ NOT NULL,
    version          INT         NOT NULL,

    CONSTRAINT chk_offers_status CHECK (status IN ('active', 'paused', 'archived')),
    CONSTRAINT chk_offers_condition CHECK (condition IN ('new', 'used', 'refurbished')),
    CONSTRAINT chk_offers_price CHECK (price_amount > 0),
    CONSTRAINT chk_offers_processing_days CHECK (processing_days BETWEEN 0 AND 30)
);

CREATE UNIQUE INDEX uq_offers_seller_sku ON catalog.offers (seller_id, seller_sku) WHERE status <> 'archived';
CREATE UNIQUE INDEX uq_offers_seller_product ON catalog.offers (seller_id, product_id) WHERE status <> 'archived';
CREATE INDEX idx_offers_product_active ON catalog.offers (product_id, price_amount, id) WHERE status = 'active';
CREATE INDEX idx_offers_seller ON catalog.offers (seller_id, created_at DESC, id DESC);

CREATE TABLE catalog.import_jobs (
    id              UUID        PRIMARY KEY,
    seller_id       UUID        NOT NULL,
    requested_by    UUID        NOT NULL,
    format          TEXT        NOT NULL,
    object_key      TEXT        NOT NULL,
    status          TEXT        NOT NULL,
    total_rows      INT         NOT NULL DEFAULT 0,
    succeeded_rows  INT         NOT NULL DEFAULT 0,
    failed_rows     INT         NOT NULL DEFAULT 0,
    errors          JSONB       NOT NULL DEFAULT '[]'::jsonb,
    failure_reason  TEXT,
    report_key      TEXT,
    created_at      TIMESTAMPTZ NOT NULL,
    started_at      TIMESTAMPTZ,
    finished_at     TIMESTAMPTZ,
    updated_at      TIMESTAMPTZ NOT NULL,
    version         INT         NOT NULL,

    CONSTRAINT chk_import_jobs_status CHECK (status IN ('pending', 'processing', 'completed', 'failed')),
    CONSTRAINT chk_import_jobs_format CHECK (format IN ('csv', 'xlsx', 'json'))
);

CREATE INDEX idx_import_jobs_seller ON catalog.import_jobs (seller_id, created_at DESC, id DESC);
CREATE INDEX idx_import_jobs_stale ON catalog.import_jobs (updated_at) WHERE status = 'processing';

-- +goose Down
DROP TABLE IF EXISTS catalog.import_jobs;
DROP TABLE IF EXISTS catalog.offers;
DROP TABLE IF EXISTS catalog.variant_members;
DROP TABLE IF EXISTS catalog.variant_groups;
DROP TABLE IF EXISTS catalog.product_images;
DROP TABLE IF EXISTS catalog.product_attributes;
DROP TABLE IF EXISTS catalog.products;
DROP TABLE IF EXISTS catalog.category_attributes;
DROP TABLE IF EXISTS catalog.categories;
DROP SCHEMA IF EXISTS catalog;
