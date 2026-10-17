-- Mandate ledger. Invariants from docs/architecture/BLUEPRINT.md §5.1 are
-- enforced here so that a bug in Go code cannot violate them.

CREATE TABLE mandates (
    id              text PRIMARY KEY,
    principal_id    text        NOT NULL,
    agent_id        text        NOT NULL,
    status          text        NOT NULL CHECK (status IN ('DRAFT', 'ACTIVE', 'PAUSED', 'REVOKED')),
    policy          jsonb       NOT NULL,
    policy_hash     text        NOT NULL,
    setup_token_id  text,
    vault_id        text,
    paused_reason   text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX mandates_vault_id ON mandates (vault_id) WHERE vault_id IS NOT NULL;

-- I1: reserved + captured never exceeds the envelope limit.
CREATE TABLE envelopes (
    mandate_id      text        PRIMARY KEY REFERENCES mandates (id),
    currency        char(3)     NOT NULL,
    limit_minor     bigint      NOT NULL CHECK (limit_minor > 0),
    period_seconds  bigint      NOT NULL CHECK (period_seconds > 0),
    period_start    timestamptz NOT NULL,
    reserved_minor  bigint      NOT NULL DEFAULT 0 CHECK (reserved_minor >= 0),
    captured_minor  bigint      NOT NULL DEFAULT 0 CHECK (captured_minor >= 0),
    CONSTRAINT envelope_never_overspent CHECK (reserved_minor + captured_minor <= limit_minor)
);

CREATE TABLE intent_transitions (
    from_state text NOT NULL,
    to_state   text NOT NULL,
    PRIMARY KEY (from_state, to_state)
);

CREATE TABLE intents (
    id              text PRIMARY KEY,
    mandate_id      text        NOT NULL REFERENCES mandates (id),
    agent_id        text        NOT NULL,
    payee_ref       text        NOT NULL,
    amount_minor    bigint      NOT NULL CHECK (amount_minor > 0),
    currency        char(3)     NOT NULL,
    category        text        NOT NULL DEFAULT '',
    quote           jsonb,
    quote_hash      text,
    state           text        NOT NULL,
    version         integer     NOT NULL DEFAULT 1,
    reasons         text[]      NOT NULL DEFAULT '{}',
    policy_hash     text        NOT NULL,
    reserved_minor  bigint      NOT NULL DEFAULT 0 CHECK (reserved_minor >= 0),
    token_jti       text,
    order_id        text,
    capture_id      text,
    recovery_n      integer     NOT NULL DEFAULT 0,
    approval_url    text,
    last_error      text,
    trace           jsonb,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX intents_mandate_created ON intents (mandate_id, created_at);
CREATE INDEX intents_open ON intents (state, updated_at)
    WHERE state NOT IN ('DENIED', 'EXPIRED', 'REVOKED', 'FAILED', 'RECONCILED', 'SETTLED');
CREATE UNIQUE INDEX intents_order_id ON intents (order_id) WHERE order_id IS NOT NULL;

-- I5: only legal FSM edges. The edge table is seeded from Go (fsm.Edges).
CREATE FUNCTION enforce_intent_transition() RETURNS trigger AS $$
BEGIN
    IF NEW.state IS DISTINCT FROM OLD.state THEN
        IF NOT EXISTS (SELECT 1 FROM intent_transitions
                       WHERE from_state = OLD.state AND to_state = NEW.state) THEN
            RAISE EXCEPTION 'illegal intent transition % -> %', OLD.state, NEW.state
                USING ERRCODE = 'check_violation';
        END IF;
        NEW.version := OLD.version + 1;
    END IF;
    NEW.updated_at := now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER intents_transition BEFORE UPDATE ON intents
    FOR EACH ROW EXECUTE FUNCTION enforce_intent_transition();

-- I4: capabilities are single use.
CREATE TABLE tokens (
    jti          text PRIMARY KEY,
    intent_id    text        NOT NULL UNIQUE REFERENCES intents (id),
    raw          text        NOT NULL,
    expires_at   timestamptz NOT NULL,
    consumed_at  timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now()
);

-- I3: at most one completed capture per intent.
CREATE TABLE captures (
    capture_id   text PRIMARY KEY,
    intent_id    text        NOT NULL REFERENCES intents (id),
    order_id     text        NOT NULL,
    amount_minor bigint      NOT NULL,
    currency     char(3)     NOT NULL,
    status       text        NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX captures_one_completed_per_intent ON captures (intent_id) WHERE status = 'COMPLETED';

-- Write-ahead record of every state-changing PayPal call (§6.1 rule 2).
CREATE TABLE paypal_requests (
    idempotency_key text PRIMARY KEY,
    op              text        NOT NULL,
    intent_id       text,
    state           text        NOT NULL CHECK (state IN ('PENDING', 'DONE', 'FAILED', 'UNKNOWN')),
    debug_id        text,
    response        jsonb,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE webhook_events (
    event_id     text PRIMARY KEY,
    event_type   text        NOT NULL,
    resource_id  text,
    payload      jsonb       NOT NULL,
    received_at  timestamptz NOT NULL DEFAULT now(),
    applied_at   timestamptz,
    outcome      text
);

CREATE TABLE payout_batches (
    id               text PRIMARY KEY,
    mandate_id       text        NOT NULL REFERENCES mandates (id),
    sender_batch_id  text        NOT NULL UNIQUE,
    payout_batch_id  text UNIQUE,
    status           text        NOT NULL,
    total_minor      bigint      NOT NULL CHECK (total_minor > 0),
    currency         char(3)     NOT NULL,
    charge_intent_id text REFERENCES intents (id),
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE obligations (
    id              text PRIMARY KEY,
    mandate_id      text        NOT NULL REFERENCES mandates (id),
    intent_id       text        NOT NULL REFERENCES intents (id),
    payee_ref       text        NOT NULL,
    receiver_email  text        NOT NULL,
    amount_minor    bigint      NOT NULL CHECK (amount_minor > 0),
    currency        char(3)     NOT NULL,
    state           text        NOT NULL CHECK (state IN ('OPEN', 'BATCHED', 'SETTLED', 'FAILED')),
    batch_id        text REFERENCES payout_batches (id),
    payout_item_id  text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX obligations_open ON obligations (mandate_id, state);

CREATE TABLE short_links (
    code        text PRIMARY KEY,
    url         text        NOT NULL,
    intent_id   text REFERENCES intents (id),
    expires_at  timestamptz NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);

-- I6/I7: append-only hash chain; the head row serialises appends.
CREATE TABLE audit_events (
    seq          bigint PRIMARY KEY,
    ts           timestamptz NOT NULL,
    kind         text        NOT NULL,
    mandate_id   text,
    agent_id     text,
    intent_id    text,
    entry        jsonb       NOT NULL,
    prev_hash    bytea       NOT NULL,
    hash         bytea       NOT NULL UNIQUE,
    signature    bytea
);
CREATE INDEX audit_events_mandate ON audit_events (mandate_id, seq);
CREATE INDEX audit_events_intent ON audit_events (intent_id, seq);

CREATE TABLE audit_head (
    id    smallint PRIMARY KEY CHECK (id = 1),
    seq   bigint NOT NULL,
    hash  bytea  NOT NULL
);
INSERT INTO audit_head (id, seq, hash) VALUES (1, 0, decode(repeat('00', 32), 'hex'));

CREATE FUNCTION forbid_audit_mutation() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'audit_events is append-only' USING ERRCODE = 'insufficient_privilege';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER audit_events_append_only BEFORE UPDATE OR DELETE ON audit_events
    FOR EACH ROW EXECUTE FUNCTION forbid_audit_mutation();
