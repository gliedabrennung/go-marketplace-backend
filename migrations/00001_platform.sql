-- +goose Up
CREATE SCHEMA IF NOT EXISTS platform;

CREATE TABLE platform.outbox (
    id              BIGSERIAL   PRIMARY KEY,
    aggregate_id    TEXT        NOT NULL,
    event_name      TEXT        NOT NULL,
    payload         JSONB       NOT NULL,
    metadata        JSONB       NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at    TIMESTAMPTZ,
    attempts        INT         NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_error      TEXT
);

CREATE INDEX idx_outbox_unpublished
    ON platform.outbox (next_attempt_at, id)
    WHERE published_at IS NULL;

CREATE INDEX idx_outbox_published
    ON platform.outbox (published_at)
    WHERE published_at IS NOT NULL;

CREATE TABLE platform.idempotency_keys (
    principal     TEXT        NOT NULL,
    scope         TEXT        NOT NULL,
    key           TEXT        NOT NULL,
    request_hash  TEXT        NOT NULL,
    response      BYTEA,
    status_code   INT,
    content_type  TEXT,
    state         TEXT        NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at    TIMESTAMPTZ NOT NULL,

    PRIMARY KEY (principal, scope, key),
    CONSTRAINT chk_idempotency_state CHECK (state IN ('in_progress', 'completed'))
);

CREATE INDEX idx_idempotency_expiry ON platform.idempotency_keys (expires_at);

CREATE TABLE platform.processed_messages (
    consumer_group TEXT        NOT NULL,
    message_id     TEXT        NOT NULL,
    processed_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (consumer_group, message_id)
);

CREATE INDEX idx_processed_messages_at ON platform.processed_messages (processed_at);

CREATE TABLE platform.audit_log (
    id           BIGSERIAL   PRIMARY KEY,
    actor_id     TEXT        NOT NULL,
    actor_roles  TEXT[]      NOT NULL DEFAULT '{}',
    action       TEXT        NOT NULL,
    object_type  TEXT        NOT NULL,
    object_id    TEXT        NOT NULL,
    details      JSONB       NOT NULL DEFAULT '{}',
    occurred_at  TIMESTAMPTZ NOT NULL,
    recorded_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_audit_object ON platform.audit_log (object_type, object_id, occurred_at DESC);
CREATE INDEX idx_audit_actor ON platform.audit_log (actor_id, occurred_at DESC);

-- +goose StatementBegin
CREATE FUNCTION platform.reject_audit_mutation() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'platform.audit_log is append-only';
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER trg_audit_log_immutable
    BEFORE UPDATE OR DELETE ON platform.audit_log
    FOR EACH ROW EXECUTE FUNCTION platform.reject_audit_mutation();

-- +goose Down
DROP TABLE IF EXISTS platform.audit_log;
DROP FUNCTION IF EXISTS platform.reject_audit_mutation();
DROP TABLE IF EXISTS platform.processed_messages;
DROP TABLE IF EXISTS platform.idempotency_keys;
DROP TABLE IF EXISTS platform.outbox;
DROP SCHEMA IF EXISTS platform;
