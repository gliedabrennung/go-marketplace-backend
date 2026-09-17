-- +goose Up
CREATE SCHEMA IF NOT EXISTS search;
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE search.documents_a (
    product_id     UUID        PRIMARY KEY,
    seller_id      UUID        NOT NULL,
    category_id    UUID        NOT NULL,
    category_path  UUID[]      NOT NULL,
    title          TEXT        NOT NULL,
    description    TEXT        NOT NULL DEFAULT '',
    brand          TEXT        NOT NULL DEFAULT '',
    cover_key      TEXT        NOT NULL DEFAULT '',
    attributes     JSONB       NOT NULL DEFAULT '{}'::jsonb,
    numbers        JSONB       NOT NULL DEFAULT '{}'::jsonb,
    published_at   TIMESTAMPTZ NOT NULL,
    indexed_at     TIMESTAMPTZ NOT NULL,
    document       TSVECTOR    GENERATED ALWAYS AS (
        setweight(to_tsvector('russian', title), 'A') ||
        setweight(to_tsvector('russian', brand), 'B') ||
        setweight(to_tsvector('russian', description), 'C')
    ) STORED
);

CREATE INDEX idx_documents_a_document ON search.documents_a USING GIN (document);
CREATE INDEX idx_documents_a_path ON search.documents_a USING GIN (category_path);
CREATE INDEX idx_documents_a_attributes ON search.documents_a USING GIN (attributes jsonb_path_ops);
CREATE INDEX idx_documents_a_published ON search.documents_a (published_at DESC, product_id DESC);

CREATE TABLE search.documents_b (
    product_id     UUID        PRIMARY KEY,
    seller_id      UUID        NOT NULL,
    category_id    UUID        NOT NULL,
    category_path  UUID[]      NOT NULL,
    title          TEXT        NOT NULL,
    description    TEXT        NOT NULL DEFAULT '',
    brand          TEXT        NOT NULL DEFAULT '',
    cover_key      TEXT        NOT NULL DEFAULT '',
    attributes     JSONB       NOT NULL DEFAULT '{}'::jsonb,
    numbers        JSONB       NOT NULL DEFAULT '{}'::jsonb,
    published_at   TIMESTAMPTZ NOT NULL,
    indexed_at     TIMESTAMPTZ NOT NULL,
    document       TSVECTOR    GENERATED ALWAYS AS (
        setweight(to_tsvector('russian', title), 'A') ||
        setweight(to_tsvector('russian', brand), 'B') ||
        setweight(to_tsvector('russian', description), 'C')
    ) STORED
);

CREATE INDEX idx_documents_b_document ON search.documents_b USING GIN (document);
CREATE INDEX idx_documents_b_path ON search.documents_b USING GIN (category_path);
CREATE INDEX idx_documents_b_attributes ON search.documents_b USING GIN (attributes jsonb_path_ops);
CREATE INDEX idx_documents_b_published ON search.documents_b (published_at DESC, product_id DESC);

CREATE VIEW search.documents AS SELECT * FROM search.documents_a;

CREATE TABLE search.index_state (
    id          BOOLEAN     PRIMARY KEY DEFAULT true,
    active      TEXT        NOT NULL,
    rebuilt_at  TIMESTAMPTZ,

    CONSTRAINT chk_index_state_single CHECK (id),
    CONSTRAINT chk_index_state_active CHECK (active IN ('documents_a', 'documents_b'))
);

INSERT INTO search.index_state (id, active) VALUES (true, 'documents_a');

CREATE TABLE search.sellers (
    seller_id   UUID        PRIMARY KEY,
    can_sell    BOOLEAN     NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL
);

CREATE TABLE search.product_offers (
    offer_id      UUID        PRIMARY KEY,
    product_id    UUID        NOT NULL,
    seller_id     UUID        NOT NULL,
    price_amount  BIGINT      NOT NULL,
    currency      TEXT        NOT NULL,
    condition     TEXT        NOT NULL,
    status        TEXT        NOT NULL,
    updated_at    TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_search_offers_product ON search.product_offers (product_id);
CREATE INDEX idx_search_offers_seller ON search.product_offers (seller_id);

CREATE TABLE search.product_stats (
    product_id   UUID        PRIMARY KEY,
    min_price    BIGINT,
    currency     TEXT        NOT NULL DEFAULT 'KZT',
    offers       INT         NOT NULL DEFAULT 0,
    sellers      INT         NOT NULL DEFAULT 0,
    conditions   TEXT[]      NOT NULL DEFAULT '{}',
    updated_at   TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_search_stats_price ON search.product_stats (min_price, product_id);

CREATE TABLE search.lexicon (
    word    TEXT PRIMARY KEY,
    weight  INT  NOT NULL
);

CREATE INDEX idx_search_lexicon_trgm ON search.lexicon USING GIN (word gin_trgm_ops);

CREATE TABLE search.reindex_jobs (
    id              UUID        PRIMARY KEY,
    status          TEXT        NOT NULL,
    requested_by    UUID        NOT NULL,
    target          TEXT        NOT NULL,
    processed       INT         NOT NULL DEFAULT 0,
    failure_reason  TEXT,
    created_at      TIMESTAMPTZ NOT NULL,
    started_at      TIMESTAMPTZ,
    finished_at     TIMESTAMPTZ,
    updated_at      TIMESTAMPTZ NOT NULL,

    CONSTRAINT chk_reindex_jobs_status CHECK (status IN ('pending', 'running', 'completed', 'failed')),
    CONSTRAINT chk_reindex_jobs_target CHECK (target IN ('documents_a', 'documents_b'))
);

CREATE INDEX idx_search_reindex_pending ON search.reindex_jobs (created_at) WHERE status = 'pending';

-- +goose Down
DROP TABLE IF EXISTS search.reindex_jobs;
DROP TABLE IF EXISTS search.lexicon;
DROP TABLE IF EXISTS search.product_stats;
DROP TABLE IF EXISTS search.product_offers;
DROP TABLE IF EXISTS search.sellers;
DROP TABLE IF EXISTS search.index_state;
DROP VIEW IF EXISTS search.documents;
DROP TABLE IF EXISTS search.documents_b;
DROP TABLE IF EXISTS search.documents_a;
DROP SCHEMA IF EXISTS search;
