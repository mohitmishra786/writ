# Mandate

**Signed authority for autonomous commerce.**

AI agents can now buy things. Today, the only way to let one pay is to hand it PayPal credentials with no limits: PayPal's own agent toolkit sends one idempotency key on every call ([paypal/agent-toolkit#98](https://github.com/paypal/agent-toolkit/issues/98), still open), and its only access control is a static tool allow-list that cannot see amounts ([PR #91](https://github.com/paypal/agent-toolkit/pull/91), closed unmerged).

Mandate is the financial kernel that sits between any agent and PayPal. A human principal signs a **mandate** (budget envelope, per-order cap, co-sign threshold, velocity limits, allowlisted suppliers and categories). Every agent intent is checked by a deterministic policy engine *before* any credential exists. If authorized, Mandate reserves funds, mints a single-use Ed25519 capability, and executes an idempotent PayPal charge. Every step is written to a hash-chained, signed audit log in the same database transaction.

Mandate does not stop a model from being fooled by prompt injection. It bounds what a fooled model can do.

## What runs today

| Area | Status | Where |
|---|---|---|
| Policy kernel (CEL rules, reason codes) | Done, tested | `internal/kernel/policy` |
| Postgres ledger, invariants enforced by the database | Done, tested | `internal/store/migrations` |
| Capabilities (PASETO v4.public) and hash-chained audit log | Done, tested | `internal/kernel/token`, `internal/kernel/audit` |
| PayPal REST client (Orders v2, Vault v3, Payouts v1, Webhooks v1, refunds) | Done, verified against the live sandbox | `internal/paypal` |
| Decline recovery (`INSTRUMENT_DECLINED` → payer-action link → capture) | Done, tested | `internal/kernel/execute.go` |
| Netted supplier settlement through PayPal Payouts | Done, tested; live payout verified | `internal/kernel/settle.go` |
| Procurement agent on Groq (`openai/gpt-oss-120b`) | Done, live run verified | `internal/procure` |
| Supplier agents over A2A 1.0 (signed quotes) | Done, tested | `internal/mesh` |
| Live market benchmark from Channel3 | Done, live verified | `internal/channel3` |
| MCP server with PayPal Agent Toolkit tool names | Done, live verified | `internal/mcpserver` |
| Operator console (Next.js + AG Studio) | In progress | `web/console` |

Every PayPal behaviour the code depends on is listed with its source (official docs, the v6 sample integration, OpenAPI schemas, or a live sandbox call) in [docs/architecture/BLUEPRINT.md §16](docs/architecture/BLUEPRINT.md).

## Architecture

```
  agent (Groq / any MCP client)
        │  propose            MCP: create_order, pay_order, create_refund, ...
        ▼
  ┌──────────────── mandated (one Go binary) ─────────────────┐
  │ policy ─► reserve ─► mint capability ─► execute ─► audit  │
  │   CEL      Postgres     PASETO v4        PayPal REST       │
  │            CHECKs       single use       idempotent        │
  │ A2A supplier agents · Channel3 benchmark · webhook receiver│
  │ sweeper: expire, resolve UNKNOWN, settle, poll payouts     │
  └──────────────────────────┬────────────────────────────────┘
                             ▼
                  PayPal sandbox: Orders v2, Vault v3, Payouts v1
```

Decisions and their reasons: [Blueprint](docs/architecture/BLUEPRINT.md), [ADR-0001 repository structure](docs/adr/0001-repository-structure.md), [ADR-0002 MCP compatibility](docs/adr/0002-mcp-paypal-tool-compatibility.md).

## Run it locally

Requirements: Go 1.25+, PostgreSQL 14+, a PayPal sandbox REST app (US business account; "Save payment methods" and "Payouts" enabled), a Groq API key, a Channel3 API key.

```bash
cp .env.example .env              # fill in values; .env is git-ignored
go run ./cmd/mandatectl keygen    # paste MANDATE_SIGNING_SEED and the supplier seeds/public keys into .env
createdb mandate_dev
set -a; . ./.env; set +a
go run ./cmd/mandated             # migrates, then serves on :8080
```

### Walk through the golden path with curl

```bash
# 1. Create a mandate ($600/week, $250/order, co-sign above $200, two suppliers).
curl -s localhost:8080/v1/suppliers            # supplier refs and public keys
curl -s -X POST localhost:8080/v1/mandates -H 'content-type: application/json' --data @mandate.json

# 2. Save the principal's PayPal account (vault without purchase).
curl -s -X POST localhost:8080/v1/mandates/$MANDATE/vault/setup     # returns approve_url
#    Open approve_url, log in as the sandbox personal account, approve. Then:
curl -s -X POST localhost:8080/v1/mandates/$MANDATE/vault/complete

# 3. Let the procurement agent restock (Channel3 benchmark, A2A quotes, kernel, PayPal).
curl -s -X POST localhost:8080/v1/procure/runs -H 'content-type: application/json' \
  -d '{"mandate_id":"'$MANDATE'","sku":"pitcher-20oz"}'
curl -s localhost:8080/v1/procure/runs/$RUN

# 4. Watch a compromised agent get blocked.
curl -s -X POST localhost:8080/v1/procure/simulate-compromise -H 'content-type: application/json' \
  -d '{"mandate_id":"'$MANDATE'"}'                                    # PAYEE_NOT_ALLOWED

# 5. Verify the audit chain.
curl -s localhost:8080/v1/audit/verify
```

### Use it from any MCP client

Point an MCP client (Streamable HTTP) at `http://localhost:8080/mcp/<mandate_id>`. Tools: `create_order`, `get_order`, `pay_order`, `create_refund`, `get_refund`, `list_transactions` (same names and inputs as `@paypal/agent-toolkit`), plus `mandate_get`, `mandate_propose_purchase`, `mandate_list_decisions`.

## Tests

```bash
set -a; . ./.env; set +a
go test -race -p 1 ./...                                   # unit + Postgres integration (TEST_DATABASE_URL)
MANDATE_SANDBOX_IT=1 go test ./internal/paypal -run Live   # live PayPal sandbox checks
MANDATE_GROQ_IT=1    go test ./internal/llm -run Live      # live Groq tool call
MANDATE_CHANNEL3_IT=1 go test ./internal/channel3 -run Live
```

Integration tests prove, against real Postgres: concurrent spends never exceed the envelope; illegal state transitions are rejected by a database trigger; ambiguous PayPal outcomes resolve by replaying the same idempotency key with exactly one capture; capabilities are single use; equal partial refunds never share an idempotency key; the audit chain detects tampering, deletion and rewritten history.

## Deploy

`render.yaml` is a Render Blueprint: one Go web service and one Postgres database, with secrets prompted in the Render Dashboard. After the first deploy, register the PayPal webhook:

```bash
go run ./cmd/mandatectl webhooks ensure https://<your-service>.onrender.com/v1/paypal/webhooks
# set the printed PAYPAL_WEBHOOK_ID in the Render Dashboard
```

## Sponsor tools

- **AG Grid / AG Studio**: the operator console (in progress).
- **Channel3**: live retail price benchmark the procurement agent negotiates against.
- **Render**: hosting (`render.yaml`).
- **APIMatic**: not used yet (planned: the APIMatic Context Plugin for PayPal during development).

## License

MIT, see [LICENSE](LICENSE). Tool schemas in `internal/mcpserver` mirror `@paypal/agent-toolkit` (ISC).
