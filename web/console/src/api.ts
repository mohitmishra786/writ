// Typed client for the Writ API (mandated). Field names match the Go
// handlers in internal/api.

export const API_BASE: string = (import.meta.env.VITE_API_BASE as string | undefined)?.replace(/\/$/, '') ?? 'http://localhost:8080';

export interface Money {
  minor: number;
  currency: string;
}

export interface Intent {
  id: string;
  mandate_id: string;
  agent_id: string;
  payee_ref: string;
  amount: Money;
  category: string;
  state: string;
  reasons: string[] | null;
  policy_hash: string;
  reserved_minor: number;
  token_jti?: string;
  order_id?: string;
  capture_id?: string;
  recovery_n: number;
  approval_url?: string;
  last_error?: string;
  created_at: string;
  updated_at: string;
}

export interface Mandate {
  id: string;
  principal_id: string;
  agent_id: string;
  status: string;
  policy_hash: string;
  has_vault: boolean;
  paused_reason?: string;
  reserved_minor: number;
  captured_minor: number;
  limit_minor: number;
  period_start: string;
  policy: {
    per_intent_max: Money;
    cosign_above: Money;
    payees: { ref: string; receiver_email: string }[];
    categories: string[] | null;
  };
}

export interface AuditEvent {
  seq: number;
  ts_us: number;
  kind: string;
  mandate_id?: string;
  agent_id?: string;
  intent_id?: string;
  state_from?: string;
  state_to?: string;
  reasons?: string[];
  amount_minor?: number;
  currency?: string;
  payee_ref?: string;
  paypal?: Record<string, unknown>;
  policy_hash?: string;
  token_jti?: string;
  hash: string;
  prev_hash: string;
  checkpoint: boolean;
}

export interface Obligation {
  id: string;
  intent_id: string;
  payee_ref: string;
  receiver_email: string;
  amount_minor: number;
  currency: string;
  state: string;
  payout_item_id: string;
  batch_id: string;
  payout_batch_id: string;
  batch_status: string;
  created_at: string;
}

export interface InventoryItem {
  sku: string;
  title: string;
  category: string;
  on_hand: number;
  par_level: number;
  qty_needed: number;
}

export interface AgentStep {
  reasoning?: string;
  content?: string;
  tool_name?: string;
  tool_args?: unknown;
  tool_output?: string;
  tool_error?: string;
  tokens: number;
}

export interface AgentRun {
  id: string;
  mandate_id: string;
  sku: string;
  status: string;
  final: string;
  steps: AgentStep[] | null;
  intent_id: string;
  error: string;
  created_at: string;
}

export interface AuditVerification {
  valid: boolean;
  entries: number;
  checkpoints?: number;
  head_seq?: number;
  head_hash?: string;
  error?: string;
}

export interface AiCall {
  id: number;
  at: string;
  turn_id: string;
  attempt: number;
  provider: string;
  model: string;
  outcome: string;
  http_status: number;
  latency_ms: number;
  input_tokens: number;
  output_tokens: number;
  tool_calls: string[];
  error: string;
}

export interface Decision {
  intent_id: string;
  outcome: string;
  reasons: string[] | null;
  state: string;
}

async function call<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(API_BASE + path, {
    ...init,
    headers: { 'Content-Type': 'application/json', ...(init?.headers ?? {}) },
  });
  const body = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new Error((body as { error?: string }).error ?? `${res.status} ${res.statusText}`);
  }
  return body as T;
}

const post = <T,>(path: string, body?: unknown) => call<T>(path, { method: 'POST', body: body === undefined ? undefined : JSON.stringify(body) });

export const api = {
  mandates: () => call<{ mandates: Mandate[] }>('/v1/mandates').then((r) => r.mandates ?? []),
  intents: (mandateId: string) => call<{ intents: Intent[] | null }>(`/v1/intents?mandate_id=${encodeURIComponent(mandateId)}&limit=500`).then((r) => r.intents ?? []),
  audit: () => call<{ events: AuditEvent[] | null }>('/v1/audit?limit=1000').then((r) => r.events ?? []),
  verify: () => call<AuditVerification>('/v1/audit/verify'),
  settlements: (mandateId: string) => call<{ obligations: Obligation[] }>(`/v1/settlements?mandate_id=${encodeURIComponent(mandateId)}`).then((r) => r.obligations ?? []),
  inventory: () => call<{ items: InventoryItem[] | null }>('/v1/inventory').then((r) => r.items ?? []),
  runs: (mandateId: string) => call<{ runs: AgentRun[] }>(`/v1/procure/runs?mandate_id=${encodeURIComponent(mandateId)}`).then((r) => r.runs ?? []),

  aiCalls: () => call<{ calls: AiCall[] }>('/v1/ai/calls').then((r) => r.calls ?? []),

  breakItem: (sku: string) => post<unknown>(`/v1/inventory/${sku}/break`),
  restock: (sku: string) => post<unknown>(`/v1/inventory/${sku}/restock`),
  runAgent: (mandateId: string, sku: string, simulateDecline: boolean) =>
    post<{ run_id: string }>('/v1/procure/runs', { mandate_id: mandateId, sku, simulate_decline: simulateDecline }),
  simulateCompromise: (mandateId: string) => post<Decision>('/v1/procure/simulate-compromise', { mandate_id: mandateId }),
  cosign: (intentId: string) => post<Decision>(`/v1/intents/${intentId}/cosign`),
  settle: (mandateId: string) => post<unknown>(`/v1/mandates/${mandateId}/settle`),
};

export function usd(minor: number): string {
  return (minor / 100).toLocaleString('en-US', { style: 'currency', currency: 'USD' });
}
