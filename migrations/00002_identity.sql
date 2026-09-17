-- +goose Up
CREATE SCHEMA IF NOT EXISTS identity;

CREATE TABLE identity.users (
    id              UUID        PRIMARY KEY,
    email           TEXT        NOT NULL,
    email_verified  BOOLEAN     NOT NULL DEFAULT false,
    password_hash   TEXT        NOT NULL,
    roles           TEXT[]      NOT NULL,
    status          TEXT        NOT NULL,
    block_reason    TEXT,
    created_at      TIMESTAMPTZ NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL,
    version         INT         NOT NULL,

    CONSTRAINT chk_users_status CHECK (status IN ('pending_verification', 'active', 'blocked')),
    CONSTRAINT chk_users_buyer_role CHECK ('buyer' = ANY (roles))
);

CREATE UNIQUE INDEX uq_users_verified_email ON identity.users (email) WHERE email_verified;

CREATE TABLE identity.challenges (
    id             UUID        PRIMARY KEY,
    purpose        TEXT        NOT NULL,
    target         TEXT        NOT NULL,
    user_id        UUID        NOT NULL,
    secret_digest  TEXT        NOT NULL,
    status         TEXT        NOT NULL,
    attempts       INT         NOT NULL,
    max_attempts   INT         NOT NULL,
    expires_at     TIMESTAMPTZ NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL,
    updated_at     TIMESTAMPTZ NOT NULL,
    version        INT         NOT NULL,

    CONSTRAINT chk_challenges_purpose CHECK (purpose IN ('email_confirmation')),
    CONSTRAINT chk_challenges_status CHECK (status IN ('pending', 'verified', 'exhausted')),
    CONSTRAINT chk_challenges_attempts CHECK (attempts >= 0 AND attempts <= max_attempts)
);

CREATE INDEX idx_challenges_expires ON identity.challenges (expires_at);

CREATE TABLE identity.sessions (
    id                  UUID        PRIMARY KEY,
    user_id             UUID        NOT NULL,
    refresh_token_hash  TEXT        NOT NULL,
    status              TEXT        NOT NULL,
    device_name         TEXT        NOT NULL DEFAULT '',
    user_agent          TEXT        NOT NULL DEFAULT '',
    ip                  TEXT        NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ NOT NULL,
    last_used_at        TIMESTAMPTZ NOT NULL,
    expires_at          TIMESTAMPTZ NOT NULL,
    revoked_at          TIMESTAMPTZ,
    revoke_reason       TEXT,
    version             INT         NOT NULL,

    CONSTRAINT chk_sessions_status CHECK (status IN ('active', 'revoked'))
);

CREATE INDEX idx_sessions_user_active
    ON identity.sessions (user_id, last_used_at DESC, id DESC)
    WHERE status = 'active';

CREATE TABLE identity.refresh_tokens (
    token_hash  TEXT        PRIMARY KEY,
    session_id  UUID        NOT NULL REFERENCES identity.sessions (id) ON DELETE CASCADE,
    issued_at   TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_refresh_tokens_session ON identity.refresh_tokens (session_id);

-- +goose Down
DROP TABLE IF EXISTS identity.refresh_tokens;
DROP TABLE IF EXISTS identity.sessions;
DROP TABLE IF EXISTS identity.challenges;
DROP TABLE IF EXISTS identity.users;
DROP SCHEMA IF EXISTS identity;
