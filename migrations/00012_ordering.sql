-- +goose Up
CREATE SCHEMA IF NOT EXISTS ordering;

CREATE TABLE ordering.orders (
    id               UUID        PRIMARY KEY,
    buyer_id         UUID        NOT NULL,
    status           TEXT        NOT NULL,
    address          JSONB       NOT NULL,
    delivery_method  TEXT        NOT NULL,
    promo_code       TEXT        NOT NULL DEFAULT '',
    currency         TEXT        NOT NULL,
    subtotal         BIGINT      NOT NULL,
    discount         BIGINT      NOT NULL,
    shipping         BIGINT      NOT NULL,
    total            BIGINT      NOT NULL,
    payment_id       TEXT        NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL,
    updated_at       TIMESTAMPTZ NOT NULL,
    version          INT         NOT NULL,

    CONSTRAINT chk_orders_status CHECK (status IN ('created', 'awaiting_payment', 'paid', 'in_fulfilment', 'shipped',
        'delivered', 'completed', 'cancelled', 'failed', 'returning', 'returned')),
    CONSTRAINT chk_orders_total CHECK (total = subtotal - discount + shipping AND discount <= subtotal AND shipping >= 0)
);

CREATE INDEX idx_orders_buyer ON ordering.orders (buyer_id, created_at DESC, id DESC);

CREATE TABLE ordering.order_items (
    order_id    UUID    NOT NULL REFERENCES ordering.orders (id),
    sku         TEXT    NOT NULL,
    product_id  TEXT    NOT NULL,
    seller_id   UUID    NOT NULL,
    title       TEXT    NOT NULL,
    quantity    INT     NOT NULL,
    unit_price  BIGINT  NOT NULL,
    base        BIGINT  NOT NULL,
    final       BIGINT  NOT NULL,
    position    INT     NOT NULL,

    PRIMARY KEY (order_id, sku),
    CONSTRAINT chk_order_items_amounts CHECK (quantity > 0 AND base = unit_price * quantity AND final <= base AND final >= 0)
);

CREATE TABLE ordering.order_parts (
    order_id          UUID        NOT NULL REFERENCES ordering.orders (id),
    seller_id         UUID        NOT NULL,
    subtotal          BIGINT      NOT NULL,
    discount          BIGINT      NOT NULL,
    shipping          BIGINT      NOT NULL,
    total             BIGINT      NOT NULL,
    position          INT         NOT NULL,
    order_created_at  TIMESTAMPTZ NOT NULL,

    PRIMARY KEY (order_id, seller_id)
);

CREATE INDEX idx_order_parts_seller ON ordering.order_parts (seller_id, order_created_at DESC, order_id DESC);

CREATE TABLE ordering.order_status_history (
    order_id     UUID        NOT NULL REFERENCES ordering.orders (id),
    position     INT         NOT NULL,
    from_status  TEXT        NOT NULL,
    to_status    TEXT        NOT NULL,
    actor_kind   TEXT        NOT NULL,
    actor_id     TEXT        NOT NULL,
    reason       TEXT        NOT NULL,
    changed_at   TIMESTAMPTZ NOT NULL,

    PRIMARY KEY (order_id, position)
);

CREATE TABLE ordering.checkout_sagas (
    order_id        UUID        PRIMARY KEY REFERENCES ordering.orders (id),
    buyer_id        UUID        NOT NULL,
    reservation_id  TEXT        NOT NULL,
    payment_id      TEXT        NOT NULL,
    refund_id       TEXT        NOT NULL DEFAULT '',
    promo_code      TEXT        NOT NULL DEFAULT '',
    currency        TEXT        NOT NULL,
    amount          BIGINT      NOT NULL,
    status          TEXT        NOT NULL,
    step            TEXT        NOT NULL,
    captured        BOOLEAN     NOT NULL DEFAULT FALSE,
    compensated     TEXT[]      NOT NULL DEFAULT '{}',
    reason          TEXT        NOT NULL DEFAULT '',
    last_error      TEXT        NOT NULL DEFAULT '',
    attempts        INT         NOT NULL DEFAULT 0,
    deadline        TIMESTAMPTZ NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL,
    version         INT         NOT NULL,

    CONSTRAINT chk_checkout_sagas_status CHECK (status IN ('running', 'compensating', 'completed', 'compensated', 'manual'))
);

CREATE UNIQUE INDEX uq_checkout_sagas_payment ON ordering.checkout_sagas (payment_id);
CREATE INDEX idx_checkout_sagas_deadline ON ordering.checkout_sagas (deadline) WHERE status = 'running';
CREATE INDEX idx_checkout_sagas_active ON ordering.checkout_sagas (updated_at) WHERE status IN ('running', 'compensating');
CREATE INDEX idx_checkout_sagas_status ON ordering.checkout_sagas (status, updated_at DESC, order_id DESC);

-- +goose Down
DROP TABLE IF EXISTS ordering.checkout_sagas;
DROP TABLE IF EXISTS ordering.order_status_history;
DROP TABLE IF EXISTS ordering.order_parts;
DROP TABLE IF EXISTS ordering.order_items;
DROP TABLE IF EXISTS ordering.orders;
DROP SCHEMA IF EXISTS ordering;
