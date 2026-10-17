// AG Studio data sources for the Writ console. Shapes follow ag-studio
// 3.0.0: AgSimpleDataSourceDefinition {id, name, description, data, fields}
// with field formats from AgFormats; datetime values are Date objects.

import type { AgDataSourcesDefinition, AgFieldDefinition } from 'ag-studio';
import type { AgentRun, AiCall, AuditEvent, Intent, InventoryItem, Mandate, Obligation } from '../api';

export interface ConsoleData {
  mandate: Mandate | null;
  intents: Intent[];
  audit: AuditEvent[];
  obligations: Obligation[];
  inventory: InventoryItem[];
  runs: AgentRun[];
  aiCalls: AiCall[];
}

type FieldSpec = [id: string, name: string, format: AgFieldDefinition['format']];

// Display formats via AgBaseFieldDefinition.formatOptions ({ format: Intl.* }).
const CURRENCY = { format: new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD' }) };
const DATETIME = { format: new Intl.DateTimeFormat('en-US', { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' }) };
const PERCENT = { format: new Intl.NumberFormat('en-US', { style: 'percent', maximumFractionDigits: 0 }) };

const fields = (specs: FieldSpec[]): AgFieldDefinition[] =>
  specs.map(([id, name, format]) => ({
    id,
    name,
    format,
    ...(format === 'currencyFormat' ? { formatOptions: CURRENCY } : {}),
    ...(format === 'dateTimeFormat' ? { formatOptions: DATETIME } : {}),
    ...(format === 'percentageFormat' ? { formatOptions: PERCENT } : {}),
  }));

const dollars = (minor: number | undefined) => (minor ?? 0) / 100;
const CAPTURED_STATES = new Set(['CAPTURED', 'SETTLING', 'SETTLED', 'SETTLE_FAILED', 'RECONCILED']);

const REASON_LABELS: Record<string, string> = {
  PAYEE_NOT_ALLOWED: 'Payee not on allowlist',
  QUOTE_SIGNATURE_INVALID: 'Quote signature invalid',
  QUOTE_MISMATCH: 'Amount differs from signed quote',
  CATEGORY_NOT_ALLOWED: 'Category not allowed',
  PER_INTENT_EXCEEDED: 'Over per-order cap',
  ENVELOPE_EXCEEDED: 'Over weekly envelope',
  VELOCITY_EXCEEDED: 'Too many, too fast',
  MANDATE_INACTIVE: 'Mandate not active',
  SCHEMA_INVALID: 'Malformed request',
  VAULT_NOT_LINKED: 'PayPal not linked',
};

export const reasonLabel = (code: string) => REASON_LABELS[code] ?? code.replace(/_/g, ' ').toLowerCase();

function intentRows(intents: Intent[]) {
  return intents.map((it) => {
    const captured = CAPTURED_STATES.has(it.state);
    const denied = it.state === 'DENIED';
    return {
      intentId: it.id,
      createdAt: new Date(it.created_at),
      state: it.state,
      decision: denied ? 'Denied' : it.state === 'AWAITING_COSIGN' ? 'Awaiting co-sign' : 'Authorized',
      payee: it.payee_ref,
      category: it.category || '(none)',
      amount: dollars(it.amount.minor),
      capturedAmount: captured ? dollars(it.amount.minor) : 0,
      blockedAmount: denied ? dollars(it.amount.minor) : 0,
      blockedCount: denied ? 1 : 0,
      reasons: (it.reasons ?? []).map(reasonLabel).join(', '),
      paypalOrder: it.order_id ?? '',
      paypalCapture: it.capture_id ?? '',
      recoveries: it.recovery_n,
    };
  });
}

function auditRows(events: AuditEvent[]) {
  return events.map((e) => ({
    seq: e.seq,
    at: new Date(Math.floor(e.ts_us / 1000)),
    kind: e.kind,
    intentId: e.intent_id ?? '',
    transition: e.state_from && e.state_to ? `${e.state_from} -> ${e.state_to}` : '',
    reasons: (e.reasons ?? []).join(', '),
    amount: dollars(e.amount_minor),
    payee: e.payee_ref ?? '',
    paypal: e.paypal ? Object.entries(e.paypal).map(([k, v]) => `${k}=${String(v)}`).join(' ') : '',
    checkpoint: e.checkpoint ? 'Signed' : '',
    hash: e.hash.slice(0, 16),
  }));
}

function obligationRows(obligations: Obligation[]) {
  return obligations.map((o) => ({
    obligationId: o.id,
    createdAt: new Date(o.created_at),
    payee: o.payee_ref,
    receiver: o.receiver_email,
    amount: dollars(o.amount_minor),
    settledAmount: o.state === 'SETTLED' ? dollars(o.amount_minor) : 0,
    state: o.state,
    payoutBatch: o.payout_batch_id,
    batchStatus: o.batch_status || 'NOT_BATCHED',
    payoutItem: o.payout_item_id,
  }));
}

interface QuoteOut {
  quote_id: string;
  supplier_ref: string;
  unit_minor: number;
  qty: number;
  total_minor: number;
}

function quoteRows(runs: AgentRun[]) {
  const rows: Record<string, string | number | Date>[] = [];
  for (const run of runs) {
    let round = 0;
    for (const step of run.steps ?? []) {
      if (step.tool_name !== 'request_quote' || !step.tool_output) continue;
      try {
        const q = JSON.parse(step.tool_output) as QuoteOut;
        round += 1;
        rows.push({
          runId: run.id,
          at: new Date(run.created_at),
          supplier: q.supplier_ref,
          round: `Quote ${round}`,
          unitPrice: dollars(q.unit_minor),
          total: dollars(q.total_minor),
          qty: q.qty,
        });
      } catch {
        // tool output that is not a quote (an error) is skipped
      }
    }
  }
  return rows;
}

interface BenchmarkOut {
  offers: number;
  median_minor: number;
  p25_minor: number;
}

function benchmarkRows(runs: AgentRun[]) {
  for (const run of runs) {
    for (const step of run.steps ?? []) {
      if (step.tool_name !== 'market_benchmark' || !step.tool_output) continue;
      try {
        const b = JSON.parse(step.tool_output) as BenchmarkOut;
        return [{ median: dollars(b.median_minor), p25: dollars(b.p25_minor), offers: b.offers, at: new Date(run.created_at) }];
      } catch {
        // not a benchmark
      }
    }
  }
  return [];
}

export function buildSources(d: ConsoleData): AgDataSourcesDefinition {
  const m = d.mandate;
  const limit = dollars(m?.limit_minor);
  const used = dollars((m?.reserved_minor ?? 0) + (m?.captured_minor ?? 0));
  return {
    sources: [
      {
        id: 'intents',
        name: 'Agent intents',
        description: 'Every spend an agent proposed, the kernel decision and the PayPal outcome.',
        data: intentRows(d.intents),
        fields: fields([
          ['intentId', 'Intent', 'textFormat'],
          ['createdAt', 'Proposed at', 'dateTimeFormat'],
          ['state', 'State', 'textFormat'],
          ['decision', 'Decision', 'textFormat'],
          ['payee', 'Payee', 'textFormat'],
          ['category', 'Category', 'textFormat'],
          ['amount', 'Amount', 'currencyFormat'],
          ['capturedAmount', 'Captured', 'currencyFormat'],
          ['blockedAmount', 'Blocked amount', 'currencyFormat'],
          ['blockedCount', 'Blocked', 'integerFormat'],
          ['reasons', 'All reasons', 'textFormat'],
          ['paypalOrder', 'PayPal order', 'textFormat'],
          ['paypalCapture', 'PayPal capture', 'textFormat'],
          ['recoveries', 'Recovery attempts', 'integerFormat'],
        ]),
      },
      {
        id: 'denials',
        name: 'Blocked intents',
        description: 'Only the intents the kernel denied, one row per reason code.',
        data: d.intents
          .filter((it) => it.state === 'DENIED')
          .flatMap((it) =>
            (it.reasons?.length ? it.reasons : ['UNSPECIFIED']).map((code) => ({
              intentId: it.id,
              createdAt: new Date(it.created_at),
              reason: reasonLabel(code),
              payee: it.payee_ref,
              amount: dollars(it.amount.minor),
            })),
          ),
        fields: fields([
          ['intentId', 'Intent', 'textFormat'],
          ['createdAt', 'Blocked at', 'dateTimeFormat'],
          ['reason', 'Reason', 'textFormat'],
          ['payee', 'Payee', 'textFormat'],
          ['amount', 'Amount', 'currencyFormat'],
        ]),
      },
      {
        id: 'benchmark',
        name: 'Market benchmark',
        description: 'Live in-stock retail prices from the Channel3 catalog for the item being restocked.',
        data: benchmarkRows(d.runs),
        fields: fields([
          ['median', 'Market median', 'currencyFormat'],
          ['p25', 'Market 25th percentile', 'currencyFormat'],
          ['offers', 'Offers sampled', 'integerFormat'],
          ['at', 'Sampled at', 'dateTimeFormat'],
        ]),
      },
      {
        id: 'aiCalls',
        name: 'AI calls',
        description: 'Every model call the AI assistant made: provider, model, outcome, latency, tokens and tools called. A turn that fell back to another provider has several rows with the same turn.',
        data: d.aiCalls.map((c) => ({
          at: new Date(c.at),
          turn: c.turn_id.slice(-8),
          attempt: c.attempt + 1,
          fallback: c.attempt > 0 ? 'Fallback' : 'First choice',
          provider: c.provider,
          model: c.model,
          outcome: c.outcome,
          ok: c.outcome === 'ok' ? 1 : 0,
          failed: c.outcome === 'ok' ? 0 : 1,
          latency: c.latency_ms / 1000,
          inputTokens: c.input_tokens,
          outputTokens: c.output_tokens,
          tools: c.tool_calls.join(', '),
          error: c.error,
        })),
        fields: fields([
          ['at', 'At', 'dateTimeFormat'],
          ['turn', 'Turn', 'textFormat'],
          ['attempt', 'Attempt', 'integerFormat'],
          ['fallback', 'Route', 'textFormat'],
          ['provider', 'Provider', 'textFormat'],
          ['model', 'Model', 'textFormat'],
          ['outcome', 'Outcome', 'textFormat'],
          ['ok', 'Succeeded', 'integerFormat'],
          ['failed', 'Failed', 'integerFormat'],
          ['latency', 'Latency (s)', 'decimalFormat'],
          ['inputTokens', 'Input tokens', 'integerFormat'],
          ['outputTokens', 'Output tokens', 'integerFormat'],
          ['tools', 'Tools called', 'textFormat'],
          ['error', 'Error', 'textFormat'],
        ]),
      },
      {
        id: 'audit',
        name: 'Audit chain',
        description: 'Hash-chained, Ed25519-checkpointed log of every kernel state change.',
        data: auditRows(d.audit),
        fields: fields([
          ['seq', 'Seq', 'integerFormat'],
          ['at', 'At', 'dateTimeFormat'],
          ['kind', 'Event', 'textFormat'],
          ['intentId', 'Intent', 'textFormat'],
          ['transition', 'Transition', 'textFormat'],
          ['reasons', 'Reasons', 'textFormat'],
          ['amount', 'Amount', 'currencyFormat'],
          ['payee', 'Payee', 'textFormat'],
          ['paypal', 'PayPal', 'textFormat'],
          ['checkpoint', 'Checkpoint', 'textFormat'],
          ['hash', 'Hash', 'textFormat'],
        ]),
      },
      {
        id: 'settlements',
        name: 'Supplier settlements',
        description: 'Obligations owed to suppliers, netted into PayPal Payouts batches.',
        data: obligationRows(d.obligations),
        fields: fields([
          ['obligationId', 'Obligation', 'textFormat'],
          ['createdAt', 'Opened at', 'dateTimeFormat'],
          ['payee', 'Payee', 'textFormat'],
          ['receiver', 'PayPal receiver', 'textFormat'],
          ['amount', 'Amount', 'currencyFormat'],
          ['settledAmount', 'Settled', 'currencyFormat'],
          ['state', 'State', 'textFormat'],
          ['payoutBatch', 'Payout batch', 'textFormat'],
          ['batchStatus', 'Batch status', 'textFormat'],
          ['payoutItem', 'Payout item', 'textFormat'],
        ]),
      },
      {
        id: 'quotes',
        name: 'Supplier quotes',
        description: 'Signed A2A quotes the procurement agent received while negotiating.',
        data: quoteRows(d.runs),
        fields: fields([
          ['runId', 'Agent run', 'textFormat'],
          ['at', 'At', 'dateTimeFormat'],
          ['supplier', 'Supplier', 'textFormat'],
          ['round', 'Quote', 'textFormat'],
          ['unitPrice', 'Unit price', 'currencyFormat'],
          ['total', 'Total', 'currencyFormat'],
          ['qty', 'Qty', 'integerFormat'],
        ]),
      },
      {
        id: 'inventory',
        name: 'Cafe inventory',
        description: 'Stock levels that trigger procurement.',
        data: d.inventory.map((i) => ({ sku: i.sku, item: i.title, onHand: i.on_hand, par: i.par_level, needed: i.qty_needed })),
        fields: fields([
          ['sku', 'SKU', 'textFormat'],
          ['item', 'Item', 'textFormat'],
          ['onHand', 'On hand', 'integerFormat'],
          ['par', 'Par level', 'integerFormat'],
          ['needed', 'Needed', 'integerFormat'],
        ]),
      },
      {
        id: 'envelope',
        name: 'Spending envelope',
        description: 'The mandate budget for the current period.',
        data: [{ limit, reserved: dollars(m?.reserved_minor), captured: dollars(m?.captured_minor), used, available: Math.max(limit - used, 0), utilization: limit > 0 ? used / limit : 0 }],
        fields: fields([
          ['limit', 'Weekly limit', 'currencyFormat'],
          ['reserved', 'Reserved', 'currencyFormat'],
          ['captured', 'Captured', 'currencyFormat'],
          ['used', 'Used', 'currencyFormat'],
          ['available', 'Available', 'currencyFormat'],
          ['utilization', 'Utilization', 'percentageFormat'],
        ]),
      },
    ],
  };
}
