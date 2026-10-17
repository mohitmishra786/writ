-- One row per provider attempt made by the Studio AI proxy
-- (internal/api/ai.go). A turn that falls back writes several rows sharing a
-- turn_id; the console's AI usage page reads this table.
CREATE TABLE ai_calls (
    id            bigserial PRIMARY KEY,
    at            timestamptz NOT NULL DEFAULT now(),
    turn_id       text        NOT NULL,
    attempt       int         NOT NULL,
    provider      text        NOT NULL,
    model         text        NOT NULL,
    outcome       text        NOT NULL CHECK (outcome IN ('ok', 'rate_limited', 'timeout', 'provider_error', 'rejected')),
    http_status   int         NOT NULL DEFAULT 0,
    latency_ms    int         NOT NULL,
    input_tokens  int         NOT NULL DEFAULT 0,
    output_tokens int         NOT NULL DEFAULT 0,
    tool_calls    text[]      NOT NULL DEFAULT '{}',
    error         text        NOT NULL DEFAULT ''
);
CREATE INDEX ai_calls_at ON ai_calls (at DESC);
