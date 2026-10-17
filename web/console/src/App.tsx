import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import QRCode from 'qrcode';
import { AgStudio } from 'ag-studio-react';
import type { AgDataSourcesDefinition, AgPanelConfig } from 'ag-studio';
import { API_BASE, api, usd, type AuditVerification, type Intent, type Mandate } from './api';
import { buildSources, reasonLabel, type ConsoleData } from './studio/data';
import { initialReport } from './studio/report';
import { mandateTheme } from './studio/theme';
import { aiSetup } from './studio/ai';
import { fromAudit, fromStream, type ActivityEntry } from './activity';

type PageId = 'overview' | 'decisions' | 'settlement' | 'audit' | 'ai';
type Toast = { tone: 'good' | 'critical' | 'neutral'; text: string };

const PAGES: { id: PageId; label: string; description: string }[] = [
  { id: 'overview', label: 'Overview', description: 'What agents spent, what the mandate stopped, and what suppliers were paid.' },
  { id: 'decisions', label: 'Decisions', description: 'Every intent an agent proposed, with the kernel decision and PayPal identifiers.' },
  { id: 'settlement', label: 'Settlement', description: 'Supplier obligations netted into PayPal Payouts batches.' },
  { id: 'audit', label: 'Audit chain', description: 'Every state change, SHA-256 hash-chained and checkpointed with Ed25519 signatures.' },
  { id: 'ai', label: 'AI usage', description: 'Every model call behind the AI assistant: which provider answered, fallbacks, latency and tokens.' },
];

const EMPTY: ConsoleData = { mandate: null, intents: [], audit: [], obligations: [], inventory: [], runs: [], aiCalls: [] };
const STUDIO_STYLE = { height: '100%', width: '100%' };

// In ag-studio 3.0 the AI Assistant panel is an edit-mode panel (view mode only
// offers 'filters'). "Ask AI" opens edit mode with the assistant alone;
// "Customize" opens the full editing layout. Constant objects keep Studio's
// props referentially stable.
type Surface = 'view' | 'ai' | 'customize';
const PANELS_AI: AgPanelConfig = { view: { right: ['filters'] }, edit: { right: ['ai'] } };
const PANELS_CUSTOMIZE: AgPanelConfig = { view: { right: ['filters'] }, edit: { left: ['ai'], right: ['filters', 'edit', 'data'] } };
const SKU = 'pitcher-20oz';

export function App() {
  const studioRef = useRef<AgStudio>(null);
  const [mandates, setMandates] = useState<Mandate[]>([]);
  const [mandateId, setMandateId] = useState<string>(() => new URLSearchParams(location.search).get('mandate') ?? '');
  const [data, setData] = useState<ConsoleData>(EMPTY);
  const [sources, setSources] = useState<AgDataSourcesDefinition | null>(null);
  const [verify, setVerify] = useState<AuditVerification | null>(null);
  const [page, setPage] = useState<PageId>('overview');
  const [surface, setSurface] = useState<Surface>('view');
  const [busy, setBusy] = useState<string | null>(null);
  const [toast, setToast] = useState<Toast | null>(null);
  const [decline, setDecline] = useState(false);
  const [live, setLive] = useState(false);
  const [agentRunning, setAgentRunning] = useState(false);
  const [liveEntries, setLiveEntries] = useState<ActivityEntry[]>([]);
  const [drawer, setDrawer] = useState(false);
  const [unread, setUnread] = useState(0);
  const seq = useRef(0);

  useEffect(() => {
    api.mandates()
      .then((ms) => {
        setMandates(ms);
        setMandateId((cur) => cur || ms.find((m) => m.status === 'ACTIVE' && m.has_vault)?.id || ms[0]?.id || '');
      })
      .catch((e) => setToast({ tone: 'critical', text: `Cannot reach the Writ API at ${API_BASE}. ${String(e)}` }));
  }, []);

  const refresh = useCallback(async () => {
    if (!mandateId) return;
    const [ms, intents, audit, obligations, inventory, runs, v, aiCalls] = await Promise.all([
      api.mandates(), api.intents(mandateId), api.audit(), api.settlements(mandateId), api.inventory(), api.runs(mandateId), api.verify(), api.aiCalls(),
    ]);
    const next: ConsoleData = {
      mandate: ms.find((m) => m.id === mandateId) ?? null,
      intents,
      audit: audit.filter((e) => !e.mandate_id || e.mandate_id === mandateId),
      obligations, inventory, runs, aiCalls,
    };
    setMandates(ms);
    setData(next);
    // A new object reference is how Studio learns that synchronous data changed.
    setSources(buildSources(next));
    setVerify(v);
  }, [mandateId]);

  useEffect(() => {
    refresh().catch((e) => setToast({ tone: 'critical', text: String(e) }));
  }, [refresh]);

  // Live updates: the API streams committed kernel and agent events over SSE.
  // Each event becomes an activity entry; the ledger refetch is debounced so a
  // burst of transitions causes one refresh.
  useEffect(() => {
    const es = new EventSource(`${API_BASE}/v1/stream`);
    let t: ReturnType<typeof setTimeout> | undefined;
    es.onopen = () => setLive(true);
    es.onerror = () => setLive(false);
    es.onmessage = (msg) => {
      try {
        const ev = JSON.parse(msg.data);
        const entry = fromStream(ev, seq.current++);
        if (entry) {
          setLiveEntries((xs) => [entry, ...xs].slice(0, 200));
          setUnread((n) => n + 1);
        }
        if (ev.kind === 'agent_run') setAgentRunning(false);
      } catch {
        // ignore malformed events
      }
      clearTimeout(t);
      t = setTimeout(() => refresh().catch(() => undefined), 500);
    };
    return () => {
      clearTimeout(t);
      es.close();
    };
  }, [refresh]);

  useEffect(() => {
    if (!toast) return;
    const t = setTimeout(() => setToast(null), 7000);
    return () => clearTimeout(t);
  }, [toast]);

  const goTo = useCallback((id: PageId) => {
    setPage(id);
    const studio = studioRef.current?.api;
    if (studio) studio.setState({ ...studio.getState(), selectedPageId: id });
  }, []);

  const act = useCallback(async (label: string, fn: () => Promise<unknown>, done: (r: any) => Toast) => {
    setBusy(label);
    try {
      const r = await fn();
      setToast(done(r));
      await refresh();
    } catch (e) {
      setToast({ tone: 'critical', text: e instanceof Error ? e.message : String(e) });
    } finally {
      setBusy(null);
    }
  }, [refresh]);

  const startAgent = () =>
    act('agent', async () => {
      setAgentRunning(true);
      setDrawer(true);
      setUnread(0);
      try {
        return await api.runAgent(mandateId, SKU, decline);
      } catch (e) {
        setAgentRunning(false);
        throw e;
      }
    }, () => ({ tone: 'neutral', text: 'Procurement agent started. Its steps appear in Activity as they happen.' }));

  const activity = useMemo(() => {
    const history = [...data.audit].reverse().map(fromAudit).filter((x): x is ActivityEntry => !!x);
    return [...liveEntries, ...history].sort((a, b) => b.at.getTime() - a.at.getTime()).slice(0, 150);
  }, [data.audit, liveEntries]);

  const m = data.mandate;
  const pitcher = data.inventory.find((i) => i.sku === SKU);
  const approvals = data.intents.filter((i) => i.state === 'RECOVERY' && i.approval_url);
  const cosigns = data.intents.filter((i) => i.state === 'AWAITING_COSIGN');
  const current = PAGES.find((p) => p.id === page)!;
  const counts: Partial<Record<PageId, number>> = { decisions: data.intents.length, settlement: data.obligations.length, audit: data.audit.length, ai: data.aiCalls.length };

  return (
    <div className={drawer ? 'app with-drawer' : 'app'}>
      <aside className="sidebar">
        <div className="wordmark">
          <span className="name">Writ</span>
          <span className="sub">Signed spending authority for AI agents</span>
        </div>

        <nav className="nav" aria-label="Pages">
          {PAGES.map((p) => (
            <button key={p.id} className={p.id === page ? 'nav-item active' : 'nav-item'} aria-current={p.id === page ? 'page' : undefined} onClick={() => goTo(p.id)}>
              <span>{p.label}</span>
              {counts[p.id] !== undefined && <span className="count">{counts[p.id]}</span>}
            </button>
          ))}
        </nav>

        <section className="block">
          <div className="block-head">
            <span className="eyebrow">Mandate</span>
            {m && <StatusPill status={m.status} />}
          </div>
          <select className="select" value={mandateId} onChange={(e) => setMandateId(e.target.value)} aria-label="Mandate">
            {mandates.map((x) => (
              <option key={x.id} value={x.id}>
                {x.id.replace('mandate_', '').slice(0, 12)} ({x.status.toLowerCase()}{x.has_vault ? '' : ', no PayPal'})
              </option>
            ))}
          </select>
          {m && (
            <>
              <Meter captured={m.captured_minor} reserved={m.reserved_minor} limit={m.limit_minor} />
              <dl className="facts">
                <div><dt>Per order</dt><dd>{usd(m.policy.per_intent_max.minor)}</dd></div>
                <div><dt>Co-sign above</dt><dd>{usd(m.policy.cosign_above.minor)}</dd></div>
                <div><dt>Payees</dt><dd title={m.policy.payees.map((p) => p.ref).join(', ')}>{m.policy.payees.length} allowlisted</dd></div>
                <div><dt>PayPal</dt><dd>{m.has_vault ? 'Account linked' : 'Not linked'}</dd></div>
              </dl>
            </>
          )}
        </section>

        <section className="block">
          <span className="eyebrow">Scenario</span>
          <p className="muted small">{pitcher ? `${pitcher.on_hand} of ${pitcher.par_level} frothing pitchers in stock` : 'Loading stock'}</p>
          <button className="btn primary" disabled={!!busy || agentRunning || !mandateId} onClick={startAgent}>
            {agentRunning ? 'Agent running' : 'Run procurement agent'}
          </button>
          <label className="toggle">
            <input type="checkbox" checked={decline} onChange={(e) => setDecline(e.target.checked)} />
            <span>Card declines at checkout</span>
          </label>
          <div className="btn-row">
            <button className="btn" disabled={!!busy} onClick={() => act('break', () => api.breakItem(SKU), () => ({ tone: 'neutral', text: 'A pitcher broke. Stock is down by one.' }))}>Break a pitcher</button>
            <button className="btn quiet" disabled={!!busy} onClick={() => act('restock', () => api.restock(SKU), () => ({ tone: 'neutral', text: 'Stock reset to par.' }))}>Reset</button>
          </div>
          <button
            className="btn"
            disabled={!!busy || !mandateId}
            onClick={() =>
              act('compromise', () => api.simulateCompromise(mandateId), (d) =>
                d.outcome === 'DENY'
                  ? { tone: 'critical', text: `A compromised agent tried to send $4,800.00 to attacker:refund-desk. Blocked: ${(d.reasons ?? []).map(reasonLabel).join(', ')}.` }
                  : { tone: 'neutral', text: `Decision: ${d.outcome}` })
            }
          >
            Simulate compromised agent
          </button>
          <button className="btn quiet" disabled={!!busy || !mandateId} onClick={() => act('settle', () => api.settle(mandateId), () => ({ tone: 'neutral', text: 'Settlement requested. Payouts appear on the Settlement page.' }))}>
            Settle suppliers now
          </button>
        </section>

        <footer className="sidebar-foot">
          <span className="chip">PayPal sandbox</span>
          <span className={live ? 'dot good' : 'dot'} />
          <span className="muted small">{live ? 'Live' : 'Reconnecting'}</span>
        </footer>
      </aside>

      <div className="main">
        <header className="header">
          <div>
            <h1>{current.label}</h1>
            <p className="muted">{current.description}</p>
          </div>
          <div className="header-actions">
            {verify && (
              <span className={verify.valid ? 'integrity good' : 'integrity critical'} title={verify.head_hash ?? verify.error}>
                {verify.valid ? `Audit chain intact, ${verify.entries} entries` : 'Audit chain broken'}
              </span>
            )}
            <button className="btn" aria-expanded={drawer} onClick={() => { setDrawer((d) => !d); setUnread(0); }}>
              Activity{unread > 0 && <span className="badge">{unread > 99 ? '99+' : unread}</span>}
            </button>
            <button className={surface === 'ai' ? 'btn active' : 'btn'} aria-pressed={surface === 'ai'} onClick={() => setSurface((x) => (x === 'ai' ? 'view' : 'ai'))}>
              Ask AI
            </button>
            <button className="btn quiet" onClick={() => setSurface((x) => (x === 'customize' ? 'view' : 'customize'))}>
              {surface === 'customize' ? 'Done' : 'Customize'}
            </button>
          </div>
        </header>

        {(approvals.length > 0 || cosigns.length > 0) && (
          <div className="banners">
            {approvals.map((i) => <ApprovalBanner key={i.id} intent={i} />)}
            {cosigns.map((i) => (
              <div key={i.id} className="banner attention">
                <div>
                  <strong>{usd(i.amount.minor)} to {i.payee_ref} needs your co-sign.</strong>
                  <p className="muted small">It is above the {m ? usd(m.policy.cosign_above.minor) : ''} threshold in the signed mandate. Limits are checked again when you co-sign.</p>
                </div>
                <button className="btn primary" disabled={!!busy} onClick={() => act('cosign', () => api.cosign(i.id), (d) => ({ tone: d.outcome === 'DENY' ? 'critical' : 'good', text: `Co-signed. Intent is ${String(d.state).toLowerCase()}.` }))}>
                  Co-sign
                </button>
              </div>
            ))}
          </div>
        )}

        <div className="studio">
          {sources ? (
            <AgStudio ref={studioRef} style={STUDIO_STYLE} data={sources} mode={surface === 'view' ? 'view' : 'edit'} panels={surface === 'customize' ? PANELS_CUSTOMIZE : PANELS_AI} initialState={initialReport} theme={mandateTheme} ai={aiSetup} />
          ) : (
            <div className="empty muted">Loading the ledger</div>
          )}
        </div>
      </div>

      {drawer && (
        <aside className="drawer" aria-label="Activity">
          <div className="drawer-head">
            <div>
              <h2>Activity</h2>
              <p className="muted small">Agent steps and kernel decisions as they happen</p>
            </div>
            <button className="btn quiet" onClick={() => setDrawer(false)}>Close</button>
          </div>
          {agentRunning && <div className="running">Procurement agent is working</div>}
          <ol className="timeline">
            {activity.map((e) => (
              <li key={e.key} className={`event ${e.tone}`}>
                <time dateTime={e.at.toISOString()}>{e.at.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit', second: '2-digit' })}</time>
                <div>
                  <p className="event-title">{e.title}</p>
                  {e.detail && <p className="muted small">{e.detail}</p>}
                </div>
              </li>
            ))}
            {activity.length === 0 && <li className="muted small">Nothing yet. Run the procurement agent to watch it work.</li>}
          </ol>
        </aside>
      )}

      {toast && <div className={`toast ${toast.tone}`} role="status">{toast.text}</div>}
    </div>
  );
}

function StatusPill({ status }: { status: string }) {
  const tone = status === 'ACTIVE' ? 'good' : status === 'PAUSED' ? 'attention' : 'neutral';
  return <span className={`pill ${tone}`}>{status.toLowerCase()}</span>;
}

// A ratio against a limit is a meter: one track, captured and reserved as two
// steps of the same hue, with a legend so identity is not color alone.
function Meter({ captured, reserved, limit }: { captured: number; reserved: number; limit: number }) {
  const used = captured + reserved;
  const pct = (x: number) => `${Math.min((x / Math.max(limit, 1)) * 100, 100)}%`;
  return (
    <div className="meter" role="meter" aria-valuemin={0} aria-valuemax={limit} aria-valuenow={used} aria-label="Weekly envelope used">
      <div className="meter-head">
        <span className="meter-value">{usd(used)}</span>
        <span className="muted small">of {usd(limit)} this week</span>
      </div>
      <div className="track">
        {captured > 0 && <div className="fill captured" style={{ width: pct(captured) }} />}
        {reserved > 0 && <div className="fill reserved" style={{ width: pct(reserved) }} />}
      </div>
      <div className="legend small">
        <span><i className="sw captured" />Captured {usd(captured)}</span>
        <span><i className="sw reserved" />Reserved {usd(reserved)}</span>
      </div>
    </div>
  );
}

function ApprovalBanner({ intent }: { intent: Intent }) {
  const [qr, setQr] = useState('');
  useEffect(() => {
    if (intent.approval_url) {
      QRCode.toDataURL(intent.approval_url, { margin: 1, width: 208, color: { dark: '#0b0b0b', light: '#fcfcfb' } }).then(setQr).catch(() => setQr(''));
    }
  }, [intent.approval_url]);
  return (
    <div className="banner attention">
      <div>
        <strong>The card was declined for {usd(intent.amount.minor)} to {intent.payee_ref}.</strong>
        <p className="muted small">Writ did not retry the card. The owner chooses another funding source on PayPal's own page, and the purchase completes when they approve. Scan with a phone or open the link.</p>
        <a className="link" href={intent.approval_url} target="_blank" rel="noreferrer">Open PayPal approval</a>
      </div>
      {qr && <img className="qr" src={qr} alt="QR code for the PayPal approval link" />}
    </div>
  );
}
