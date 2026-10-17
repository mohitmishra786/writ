# ADR-0002: Mandate's MCP Server Mirrors PayPal's Agent Toolkit Tool Names

- **Status:** Accepted
- **Date:** 2026-10-17
- **Deciders:** Mandate core team
- **Context docs:** [Blueprint](../architecture/BLUEPRINT.md) §2, §16 (V13, V14)

## Context

The hackathon's resources point builders to PayPal's Agent Toolkit and MCP server. Agents built on them call tools such as `create_order` and `pay_order` directly against PayPal, with:
- no argument-level admission control (PR #91 closed unmerged),
- no per-call idempotency (#98 open, fix PR #108 unmerged).

All three were verified on GitHub on 2026-10-17.

PayPal's own MCP guidance says to prefer MCP tools in agent contexts, but *"for full control over request structure, fall back to the REST API directly."*

## Decision

Mandate's MCP server exposes the **same tool names and input schemas** as `@paypal/agent-toolkit` v1.11.0 (ISC license; schemas mirrored from `typescript/src/shared/parameters.ts` with attribution). An agent configured for PayPal's toolkit can be repointed to Mandate's MCP endpoint with no prompt or code change. Every call then passes through the kernel: policy → reservation → capability token → idempotent REST call → audit.

| Tool (PayPal name) | Mandate behaviour |
|---|---|
| `create_order` | Becomes an **Intent**. Prices arrive as JSON floats (toolkit schema: `itemCost: z.number()`), so they are converted once to integer minor units with exact rounding, and totals are recomputed locally (blocks the PR #110 class). If authorized and a mandate vault exists, it executes as a vaulted merchant-initiated order (Blueprint §7.2); otherwise it returns the `payer-action` link for approval. |
| `get_order` | Read-through to `GET /v2/checkout/orders/{id}`, limited to orders the calling agent's mandate created. |
| `pay_order` | Capture keyed `PayPal-Request-Id = UUIDv8(order_id)`. Retries are safe by construction. |
| `create_refund` | Policy-checked (refunds have their own per-mandate ceiling). Key = `UUIDv8(capture_id, refund_seq)`, never amount-derived. |
| `get_refund` | Read-through. |
| `list_transactions` | Read-only; feeds the AG Studio Auditor agent. |

Input validation copies the toolkit's ID patterns exactly:
- `ORDER_ID_REGEX = ^[A-Z0-9]{17,32}$`
- `CAPTURE_ID_REGEX` / `REFUND_ID_REGEX = ^[A-Za-z0-9_-]{15,32}$`
- `INVOICE_ID_REGEX = ^[A-Za-z0-9_-]{1,127}$`

**Additional Mandate-native tools** (prefixed so they never collide with PayPal names): `mandate_get`, `mandate_propose_purchase` (with a signed supplier quote), `mandate_list_decisions`.

**Deliberately not exposed:** dispute acceptance, subscription cancellation, and invoice cancellation, the irreversible tools PR #91 tried to gate. An agent asking for them gets a structured `TOOL_NOT_DELEGATED` error.

## Consequences

- **Demo moment:** the same agent prompt runs against PayPal's toolkit (no guardrails) and against Mandate. The injected purchase is blocked only in the second case.
- Groq supports remote MCP tool calling server-side, so a hosted Mandate MCP endpoint can be handed straight to a Groq agent. Spike before relying on it.
- If PayPal changes tool schemas, we re-sync from the pinned toolkit version; a contract test asserts our schemas against the vendored copy.
