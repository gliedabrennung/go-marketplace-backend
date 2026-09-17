-- +goose Up
CREATE SCHEMA IF NOT EXISTS seller;

CREATE TABLE seller.sellers (
    id                       UUID        PRIMARY KEY,
    owner_id                 UUID        NOT NULL,
    status                   TEXT        NOT NULL,
    legal_form               TEXT        NOT NULL,
    legal_name               TEXT        NOT NULL,
    tax_id                   TEXT        NOT NULL,
    legal_address            TEXT        NOT NULL,
    bank_iban                TEXT,
    bank_bic                 TEXT,
    bank_name                TEXT,
    bank_beneficiary         TEXT,
    bank_verified            BOOLEAN     NOT NULL DEFAULT false,
    rejection_reason         TEXT,
    suspension_reason        TEXT,
    suspension_note          TEXT,
    rating_score             INT,
    rating_cancellation_bp   INT,
    rating_late_shipment_bp  INT,
    rating_average_review    INT,
    rating_orders            INT,
    rating_provisional       BOOLEAN,
    rating_calculated_at     TIMESTAMPTZ,
    created_at               TIMESTAMPTZ NOT NULL,
    updated_at               TIMESTAMPTZ NOT NULL,
    version                  INT         NOT NULL,

    CONSTRAINT chk_sellers_status CHECK (status IN ('draft', 'pending_review', 'active', 'suspended', 'terminated')),
    CONSTRAINT chk_sellers_legal_form CHECK (legal_form IN ('legal_entity', 'sole_proprietor')),
    CONSTRAINT chk_sellers_bank_complete CHECK ((bank_iban IS NULL) = (bank_bic IS NULL)),
    CONSTRAINT chk_sellers_bank_verified CHECK (bank_iban IS NOT NULL OR NOT bank_verified),
    CONSTRAINT chk_sellers_rating_score CHECK (rating_score IS NULL OR rating_score BETWEEN 0 AND 100)
);

CREATE UNIQUE INDEX uq_sellers_owner_active ON seller.sellers (owner_id) WHERE status <> 'terminated';
CREATE UNIQUE INDEX uq_sellers_tax_id_active ON seller.sellers (tax_id) WHERE status <> 'terminated';
CREATE INDEX idx_sellers_status_queue ON seller.sellers (status, updated_at, id);

CREATE TABLE seller.members (
    seller_id  UUID        NOT NULL REFERENCES seller.sellers (id) ON DELETE CASCADE,
    user_id    UUID        NOT NULL,
    role       TEXT        NOT NULL,
    added_at   TIMESTAMPTZ NOT NULL,
    position   INT         NOT NULL,

    PRIMARY KEY (seller_id, user_id),
    CONSTRAINT chk_members_role CHECK (role IN ('seller_admin', 'seller_operator'))
);

CREATE INDEX idx_members_user ON seller.members (user_id);

CREATE TABLE seller.documents (
    seller_id    UUID        NOT NULL REFERENCES seller.sellers (id) ON DELETE CASCADE,
    object_key   TEXT        NOT NULL,
    kind         TEXT        NOT NULL,
    uploaded_at  TIMESTAMPTZ NOT NULL,
    position     INT         NOT NULL,

    PRIMARY KEY (seller_id, object_key),
    CONSTRAINT chk_documents_kind CHECK (kind IN ('registration_certificate', 'charter', 'bank_confirmation', 'identity_document'))
);

CREATE TABLE seller.commission_overrides (
    seller_id    UUID NOT NULL REFERENCES seller.sellers (id) ON DELETE CASCADE,
    category_id  UUID NOT NULL,
    rate_bp      INT  NOT NULL,

    PRIMARY KEY (seller_id, category_id),
    CONSTRAINT chk_overrides_rate CHECK (rate_bp BETWEEN 0 AND 10000)
);

CREATE TABLE seller.category_commissions (
    category_id  UUID        PRIMARY KEY,
    rate_bp      INT         NOT NULL,
    updated_at   TIMESTAMPTZ NOT NULL,
    version      INT         NOT NULL,

    CONSTRAINT chk_category_commissions_rate CHECK (rate_bp BETWEEN 0 AND 10000)
);

-- +goose Down
DROP TABLE IF EXISTS seller.category_commissions;
DROP TABLE IF EXISTS seller.commission_overrides;
DROP TABLE IF EXISTS seller.documents;
DROP TABLE IF EXISTS seller.members;
DROP TABLE IF EXISTS seller.sellers;
DROP SCHEMA IF EXISTS seller;
