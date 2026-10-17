-- Refunds are capped per capture: sum(refunds) <= captured amount, enforced
-- in the kernel under a row lock; seq gives each refund its own idempotency
-- key so two equal partial refunds never collide (paypal/agent-toolkit PR #108).
CREATE TABLE refunds (
    id               text PRIMARY KEY,
    capture_id       text        NOT NULL REFERENCES captures (capture_id),
    intent_id        text        NOT NULL REFERENCES intents (id),
    seq              integer     NOT NULL CHECK (seq > 0),
    amount_minor     bigint      NOT NULL CHECK (amount_minor > 0),
    currency         char(3)     NOT NULL,
    status           text        NOT NULL,
    paypal_refund_id text UNIQUE,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    UNIQUE (capture_id, seq)
);
