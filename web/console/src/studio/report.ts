// Initial AG Studio report (AgStudioProperties.initialState, read once).
// Forms follow the dataviz method: headline numbers are stat tiles, a ratio
// against a limit is a meter (drawn in the sidebar; here as tiles), magnitude by category is a single-series bar,
// and anything with one category is a tile rather than a one-bar chart.
// Widget type ids and dataMapping keys follow ag-studio 3.0.0 types.

import type { AgReportState } from 'ag-studio';

const title = (text: string, subtitle?: string) => ({
  title: { enabled: true, text },
  ...(subtitle ? { subtitle: { enabled: true, text: subtitle } } : {}),
});

const sum = (id: string) => ({ id, aggregation: 'sum' as const });
const count = (id: string) => ({ id, aggregation: 'count' as const });
const min = (id: string) => ({ id, aggregation: 'min' as const });
const avg = (id: string) => ({ id, aggregation: 'avg' as const });
const col = (id: string) => ({ id });

const tile = (text: string, subtitle: string, field: { id: string; aggregation: 'sum' | 'count' | 'min' | 'max' | 'avg' }) => ({
  type: 'value' as const,
  format: title(text, subtitle),
  dataMapping: { value: [field] },
});

export const PAGE_IDS = ['overview', 'decisions', 'settlement', 'audit', 'ai'] as const;

export const initialReport: AgReportState = {
  pages: [
    {
      id: 'overview',
      widgets: {
        captured: tile('Captured spend', 'Charged through PayPal', sum('intents.capturedAmount')),
        settled: tile('Paid to suppliers', 'PayPal Payouts, netted', sum('settlements.settledAmount')),
        prevented: tile('Spend prevented', 'Value of blocked intents', sum('intents.blockedAmount')),
        blocked: tile('Blocked intents', 'Stopped by the mandate', sum('intents.blockedCount')),
        envelope: tile('Envelope used', 'Reserved plus captured, of the signed weekly limit', sum('envelope.utilization')),
        available: tile('Envelope available', 'Left to spend this week', sum('envelope.available')),
        reasons: {
          type: 'bar-chart-grouped',
          // Counts are small integers, and the value axis has no tick interval
          // option, so label each bar directly and hide the value axis labels
          // instead of showing fractional ticks.
          format: {
            ...title('Why intents were blocked', 'Kernel reason codes, denials only'),
            style: {
              theme: { bar: { series: { width: 22, label: { enabled: true, color: '#ffffff' } } } },
              custom: { valueAxis: { label: { color: 'rgba(0,0,0,0)' }, gridLine: { enabled: false }, tick: { enabled: false } } },
            },
          },
          dataMapping: { categoryKey: [col('denials.reason')], valueKey: [count('denials.intentId')] },
        },
        median: tile('Market median', 'Channel3, live in-stock offers', sum('benchmark.median')),
        bestQuote: tile('Best negotiated unit price', 'Signed A2A supplier quotes', min('quotes.unitPrice')),
        recent: {
          type: 'grid',
          format: title('Recent decisions'),
          dataMapping: {
            cols: [col('intents.createdAt'), col('intents.payee'), col('intents.amount'), col('intents.state')],
          },
          sort: [{ field: { id: 'intents.createdAt' }, direction: 'desc' }],
        },
      },
      widgetLayout: {
        captured: { xTrack: 0, yTrack: 0, xSpan: 6, ySpan: 6 },
        settled: { xTrack: 6, yTrack: 0, xSpan: 6, ySpan: 6 },
        prevented: { xTrack: 12, yTrack: 0, xSpan: 6, ySpan: 6 },
        blocked: { xTrack: 18, yTrack: 0, xSpan: 6, ySpan: 6 },
        envelope: { xTrack: 0, yTrack: 6, xSpan: 6, ySpan: 6 },
        available: { xTrack: 6, yTrack: 6, xSpan: 6, ySpan: 6 },
        median: { xTrack: 12, yTrack: 6, xSpan: 6, ySpan: 6 },
        bestQuote: { xTrack: 18, yTrack: 6, xSpan: 6, ySpan: 6 },
        reasons: { xTrack: 0, yTrack: 12, xSpan: 10, ySpan: 22 },
        recent: { xTrack: 10, yTrack: 12, xSpan: 14, ySpan: 22 },
      },
    },
    {
      id: 'decisions',
      widgets: {
        grid: {
          type: 'grid',
          format: title('Every agent intent', 'Kernel decision, reason codes and PayPal identifiers'),
          dataMapping: {
            cols: [
              col('intents.createdAt'),
              col('intents.decision'),
              col('intents.state'),
              col('intents.payee'),
              col('intents.amount'),
              col('intents.reasons'),
              col('intents.paypalOrder'),
              col('intents.paypalCapture'),
              col('intents.recoveries'),
            ],
          },
          sort: [{ field: { id: 'intents.createdAt' }, direction: 'desc' }],
        },
      },
      widgetLayout: { grid: { xTrack: 0, yTrack: 0, xSpan: 24, ySpan: 32 } },
    },
    {
      id: 'settlement',
      widgets: {
        settled: tile('Paid to suppliers', 'Payout items PayPal reported SUCCESS', sum('settlements.settledAmount')),
        owed: tile('Owed to suppliers', 'All obligations opened by captures', sum('settlements.amount')),
        grid: {
          type: 'grid',
          format: title('Netted payouts', 'One PayPal Payouts item per supplier per batch'),
          dataMapping: {
            cols: [
              col('settlements.createdAt'),
              col('settlements.payee'),
              col('settlements.amount'),
              col('settlements.state'),
              col('settlements.batchStatus'),
              col('settlements.payoutBatch'),
              col('settlements.payoutItem'),
            ],
          },
          sort: [{ field: { id: 'settlements.createdAt' }, direction: 'desc' }],
        },
      },
      widgetLayout: {
        settled: { xTrack: 0, yTrack: 0, xSpan: 6, ySpan: 6 },
        owed: { xTrack: 6, yTrack: 0, xSpan: 6, ySpan: 6 },
        grid: { xTrack: 0, yTrack: 6, xSpan: 24, ySpan: 24 },
      },
    },
    {
      id: 'audit',
      widgets: {
        grid: {
          type: 'grid',
          format: title('Tamper-evident audit log', 'SHA-256 hash chain with Ed25519-signed checkpoints'),
          dataMapping: {
            cols: [
              col('audit.seq'),
              col('audit.at'),
              col('audit.kind'),
              col('audit.transition'),
              col('audit.reasons'),
              col('audit.amount'),
              col('audit.paypal'),
              col('audit.checkpoint'),
              col('audit.hash'),
            ],
          },
          sort: [{ field: { id: 'audit.seq' }, direction: 'desc' }],
        },
      },
      widgetLayout: { grid: { xTrack: 0, yTrack: 0, xSpan: 24, ySpan: 32 } },
    },
    {
      id: 'ai',
      widgets: {
        ok: tile('Successful calls', 'Model calls that returned an answer', sum('aiCalls.ok')),
        failed: tile('Failed attempts', 'Rate limited, timed out or rejected; the turn fell through', sum('aiCalls.failed')),
        latency: tile('Average latency', 'Seconds per model call', avg('aiCalls.latency')),
        tokens: tile('Input tokens', 'Prompt tokens sent to providers', sum('aiCalls.inputTokens')),
        byModel: {
          type: 'bar-chart-grouped',
          format: title('Calls by model', 'Succeeded and failed attempts'),
          dataMapping: { categoryKey: [col('aiCalls.model')], valueKey: [sum('aiCalls.ok'), sum('aiCalls.failed')] },
        },
        speed: {
          type: 'bar-chart-grouped',
          format: title('Average latency by model', 'Seconds per call'),
          dataMapping: { categoryKey: [col('aiCalls.model')], valueKey: [avg('aiCalls.latency')] },
        },
        calls: {
          type: 'grid',
          format: title('Every AI call', 'Turns that fell back share a turn id'),
          dataMapping: {
            cols: [
              col('aiCalls.at'),
              col('aiCalls.turn'),
              col('aiCalls.attempt'),
              col('aiCalls.provider'),
              col('aiCalls.model'),
              col('aiCalls.outcome'),
              col('aiCalls.latency'),
              col('aiCalls.inputTokens'),
              col('aiCalls.outputTokens'),
              col('aiCalls.tools'),
              col('aiCalls.error'),
            ],
          },
          sort: [{ field: { id: 'aiCalls.at' }, direction: 'desc' }],
        },
      },
      widgetLayout: {
        ok: { xTrack: 0, yTrack: 0, xSpan: 6, ySpan: 6 },
        failed: { xTrack: 6, yTrack: 0, xSpan: 6, ySpan: 6 },
        latency: { xTrack: 12, yTrack: 0, xSpan: 6, ySpan: 6 },
        tokens: { xTrack: 18, yTrack: 0, xSpan: 6, ySpan: 6 },
        byModel: { xTrack: 0, yTrack: 6, xSpan: 12, ySpan: 16 },
        speed: { xTrack: 12, yTrack: 6, xSpan: 12, ySpan: 16 },
        calls: { xTrack: 0, yTrack: 22, xSpan: 24, ySpan: 24 },
      },
    },
  ],
  selectedPageId: 'overview',
  panels: {
    filters: { collapsed: true },
    ai: { collapsed: false },
  },
};
