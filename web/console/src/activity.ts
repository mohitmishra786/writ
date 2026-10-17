// Turns kernel and agent events into readable activity entries. Live events
// arrive over SSE (internal/api/events.go: {kind, at, payload}); history is
// seeded from the audit chain.

import { usd, type AuditEvent } from './api';
import { reasonLabel } from './studio/data';

export type Tone = 'neutral' | 'good' | 'critical' | 'attention';

export interface ActivityEntry {
  key: string;
  at: Date;
  tone: Tone;
  title: string;
  detail?: string;
}

interface StreamEvent {
  kind: string;
  at: string;
  payload: Record<string, any>;
}

const STATE_SENTENCES: Record<string, { tone: Tone; text: string }> = {
  MINTED: { tone: 'neutral', text: 'Capability minted' },
  SUBMITTED: { tone: 'neutral', text: 'Charge sent to PayPal' },
  CAPTURED: { tone: 'good', text: 'Payment captured by PayPal' },
  CAPTURE_PENDING: { tone: 'attention', text: 'PayPal is holding the payment for review' },
  DECLINED: { tone: 'critical', text: 'Card declined by PayPal' },
  RECOVERY: { tone: 'attention', text: 'Owner asked to approve another funding source' },
  APPROVED: { tone: 'neutral', text: 'Owner approved in PayPal' },
  SETTLING: { tone: 'neutral', text: 'Supplier payout in progress' },
  SETTLED: { tone: 'good', text: 'Supplier paid through PayPal Payouts' },
  SETTLE_FAILED: { tone: 'critical', text: 'Supplier payout failed' },
  AWAITING_COSIGN: { tone: 'attention', text: 'Above the co-sign threshold; waiting for the owner' },
  EXPIRED: { tone: 'neutral', text: 'Expired; reservation released' },
  FAILED: { tone: 'critical', text: 'Payment failed; reservation released' },
  UNKNOWN: { tone: 'attention', text: 'PayPal outcome unknown; reconciling' },
};

function parse(out: unknown): any {
  if (typeof out !== 'string') return out;
  try {
    return JSON.parse(out);
  } catch {
    return null;
  }
}

function agentStep(p: Record<string, any>): Omit<ActivityEntry, 'key' | 'at'> {
  const out = parse(p.output);
  if (p.error) return { tone: 'critical', title: `Agent tool ${p.tool} failed`, detail: String(p.error) };
  switch (p.tool) {
    case 'get_need':
      return { tone: 'neutral', title: `Agent: restock ${out?.qty_needed ?? '?'} x ${out?.title ?? 'item'}`, detail: `${out?.on_hand ?? '?'} on hand, par ${out?.par_level ?? '?'}` };
    case 'market_benchmark':
      return { tone: 'neutral', title: `Agent checked the market: median ${usd(out?.median_minor ?? 0)}`, detail: `${out?.offers ?? 0} live in-stock offers on Channel3` };
    case 'list_suppliers':
      return { tone: 'neutral', title: `Agent discovered ${Array.isArray(out) ? out.length : 0} supplier agents`, detail: 'A2A agent cards' };
    case 'request_quote':
      return {
        tone: 'neutral',
        title: `Quote from ${out?.supplier_ref ?? 'supplier'}: ${usd(out?.unit_minor ?? 0)} each`,
        detail: `${out?.qty ?? '?'} units, ${usd(out?.total_minor ?? 0)} total incl. shipping, Ed25519-signed`,
      };
    case 'propose_purchase': {
      const denied = out?.outcome === 'DENY';
      return {
        tone: denied ? 'critical' : 'good',
        title: denied ? 'Kernel blocked the purchase' : 'Kernel authorized the purchase',
        detail: denied ? (out?.reasons ?? []).map(reasonLabel).join(', ') : `State ${out?.state ?? ''}`,
      };
    }
    default:
      return { tone: 'neutral', title: `Agent used ${p.tool}` };
  }
}

export function fromStream(e: StreamEvent, seq: number): ActivityEntry | null {
  const at = new Date(e.at);
  const key = `${e.kind}-${seq}`;
  const p = e.payload ?? {};
  switch (e.kind) {
    case 'agent_step':
      return { key, at, ...agentStep(p) };
    case 'agent_run':
      return { key, at, tone: p.status === 'FAILED' ? 'critical' : 'neutral', title: `Agent run ${p.status?.toLowerCase() ?? 'finished'}`, detail: p.final || p.error };
    case 'intent': {
      if (p.outcome === 'DENY') {
        return { key, at, tone: 'critical', title: 'Kernel blocked an intent', detail: (p.reasons ?? []).map(reasonLabel).join(', ') };
      }
      const s = STATE_SENTENCES[p.state];
      if (!s) return null;
      const who = p.payee_ref ? ` to ${p.payee_ref}` : '';
      const amt = p.amount?.minor ? ` ${usd(p.amount.minor)}` : '';
      return { key, at, tone: s.tone, title: s.text, detail: `${amt}${who}`.trim() || undefined };
    }
    case 'settlement':
      return { key, at, tone: 'neutral', title: `Payout batch sent: ${usd(p.total_minor ?? 0)}`, detail: `${p.items} item(s) netting ${p.obligations} obligation(s), PayPal batch ${p.payout_batch_id || 'pending'}` };
    case 'webhook':
      return { key, at, tone: 'neutral', title: `PayPal webhook ${p.type}`, detail: `Signature verified; ${p.outcome}` };
    case 'mandate':
      return { key, at, tone: p.status === 'PAUSED' ? 'attention' : 'neutral', title: `Mandate ${String(p.status).toLowerCase()}`, detail: p.reason };
    case 'refund':
      return { key, at, tone: 'neutral', title: `Refund ${p.status}: ${usd(p.amount?.minor ?? 0)}`, detail: `Capture ${p.capture_id}` };
    case 'inventory':
      return { key, at, tone: 'neutral', title: 'Stock changed', detail: p.sku };
    default:
      return null;
  }
}

export function fromAudit(e: AuditEvent): ActivityEntry | null {
  const at = new Date(Math.floor(e.ts_us / 1000));
  const key = `audit-${e.seq}`;
  const amt = e.amount_minor ? usd(e.amount_minor) : '';
  if (e.kind === 'POLICY_DECISION') {
    const denied = (e.reasons ?? []).length > 0;
    return {
      key, at,
      tone: denied ? 'critical' : 'good',
      title: denied ? `Kernel blocked ${amt} to ${e.payee_ref}` : `Kernel authorized ${amt} to ${e.payee_ref}`,
      detail: denied ? (e.reasons ?? []).map(reasonLabel).join(', ') : undefined,
    };
  }
  if (e.kind === 'TRANSITION' && e.state_to) {
    const s = STATE_SENTENCES[e.state_to];
    if (!s || e.state_to === 'MINTED' || e.state_to === 'SUBMITTED') return null;
    return { key, at, tone: s.tone, title: s.text, detail: [amt, e.payee_ref ? `to ${e.payee_ref}` : ''].filter(Boolean).join(' ') };
  }
  if (e.kind === 'SETTLEMENT') return { key, at, tone: 'neutral', title: `Payout batch created: ${amt}` };
  if (e.kind === 'MANDATE_ACTIVATED') return { key, at, tone: 'good', title: 'Mandate activated', detail: 'PayPal account linked by the owner' };
  return null;
}
