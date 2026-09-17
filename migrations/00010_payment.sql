-- +goose Up
CREATE SCHEMA IF NOT EXISTS payment;

CREATE TABLE payment.payments (
    id                   UUID        PRIMARY KEY,
    order_id             UUID        NOT NULL,
    buyer_id             UUID        NOT NULL,
    provider             TEXT        NOT NULL,
    provider_payment_id  TEXT,
    redirect_url         TEXT        NOT NULL DEFAULT '',
    method_id            UUID,
    save_method          BOOLEAN     NOT NULL DEFAULT FALSE,
    status               TEXT        NOT NULL,
    currency             TEXT        NOT NULL,
    amount               BIGINT      NOT NULL,
    authorized           BIGINT      NOT NULL DEFAULT 0,
    captured             BIGINT      NOT NULL DEFAULT 0,
    refunded             BIGINT      NOT NULL DEFAULT 0,
    failure_reason       TEXT        NOT NULL DEFAULT '',
    created_at           TIMESTAMPTZ NOT NULL,
    updated_at           TIMESTAMPTZ NOT NULL,
    version              INT         NOT NULL,

    CONSTRAINT chk_payments_status CHECK (status IN ('created', 'pending', 'authorized', 'captured', 'failed', 'cancelled', 'refunded')),
    CONSTRAINT chk_payments_amounts CHECK (amount > 0 AND authorized <= amount AND captured <= authorized AND refunded <= captured)
);

CREATE UNIQUE INDEX uq_payments_provider_payment ON payment.payments (provider, provider_payment_id)
    WHERE provider_payment_id IS NOT NULL;
CREATE INDEX idx_payments_order ON payment.payments (order_id, created_at);
CREATE INDEX idx_payments_ledger ON payment.payments (provider, created_at);

CREATE TABLE payment.refunds (
    id                  UUID        PRIMARY KEY,
    payment_id          UUID        NOT NULL REFERENCES payment.payments (id),
    amount              BIGINT      NOT NULL,
    reason              TEXT        NOT NULL DEFAULT '',
    status              TEXT        NOT NULL,
    provider_refund_id  TEXT        NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ NOT NULL,
    completed_at        TIMESTAMPTZ,

    CONSTRAINT chk_refunds_amount CHECK (amount > 0),
    CONSTRAINT chk_refunds_status CHECK (status IN ('pending', 'succeeded', 'failed'))
);

CREATE INDEX idx_refunds_payment ON payment.refunds (payment_id, created_at);

CREATE TABLE payment.saved_methods (
    id          UUID        PRIMARY KEY,
    buyer_id    UUID        NOT NULL,
    provider    TEXT        NOT NULL,
    token       TEXT        NOT NULL,
    label       TEXT        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL,
    removed_at  TIMESTAMPTZ,
    version     INT         NOT NULL,

    CONSTRAINT uq_saved_methods_token UNIQUE (provider, token)
);

CREATE INDEX idx_saved_methods_buyer ON payment.saved_methods (buyer_id, created_at DESC) WHERE removed_at IS NULL;

CREATE TABLE payment.webhook_events (
    provider     TEXT        NOT NULL,
    event_id     TEXT        NOT NULL,
    event_type   TEXT        NOT NULL,
    received_at  TIMESTAMPTZ NOT NULL,

    PRIMARY KEY (provider, event_id)
);

CREATE INDEX idx_webhook_events_received ON payment.webhook_events (received_at);

CREATE TABLE payment.reconciliation_reports (
    provider    TEXT        NOT NULL,
    day         DATE        NOT NULL,
    checked     INT         NOT NULL,
    mismatches  JSONB       NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL,

    PRIMARY KEY (provider, day)
);

-- +goose Down
DROP TABLE IF EXISTS payment.reconciliation_reports;
DROP TABLE IF EXISTS payment.webhook_events;
DROP TABLE IF EXISTS payment.saved_methods;
DROP TABLE IF EXISTS payment.refunds;
DROP TABLE IF EXISTS payment.payments;
DROP SCHEMA IF EXISTS payment;
