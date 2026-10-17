package kernel

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/mohitmishra786/paypal-ai-hackathon/internal/kernel/audit"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/kernel/fsm"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/kernel/policy"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/kernel/quote"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/kernel/token"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/money"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/paypal"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/store"
)

type fakePP struct {
	mu          sync.Mutex
	keys        []string
	orders      map[string]paypal.Order // by idempotency key
	declineOn   map[string]bool         // idempotency keys that decline
	ambiguous   int                     // remaining ambiguous responses
	payouts     []paypal.PayoutBatch
	seq         int
	notApproved bool
}

func newFakePP() *fakePP {
	return &fakePP{orders: map[string]paypal.Order{}, declineOn: map[string]bool{}}
}

func (f *fakePP) Env() paypal.Env { return paypal.Sandbox }

func (f *fakePP) CreateSetupToken(_ context.Context, _, _ string, opt paypal.CallOptions) (paypal.SetupToken, error) {
	return paypal.SetupToken{ID: "SETUP-" + opt.IdempotencyKey[:8], Status: "PAYER_ACTION_REQUIRED"}, nil
}

func (f *fakePP) CreatePaymentToken(context.Context, string, paypal.CallOptions) (paypal.PaymentToken, error) {
	return paypal.PaymentToken{ID: "VAULT-1"}, nil
}

func (f *fakePP) completed(spec paypal.OrderSpec) paypal.Order {
	f.seq++
	o := paypal.Order{ID: fmt.Sprintf("ORDER%013d", f.seq), Status: paypal.OrderCompleted}
	o.PurchaseUnits = make([]struct {
		ReferenceID string        `json:"reference_id"`
		InvoiceID   string        `json:"invoice_id"`
		CustomID    string        `json:"custom_id"`
		Amount      paypal.Amount `json:"amount"`
		Payments    struct {
			Captures []paypal.Capture `json:"captures"`
		} `json:"payments"`
	}, 1)
	o.PurchaseUnits[0].Payments.Captures = []paypal.Capture{{ID: fmt.Sprintf("CAP%013d", f.seq), Status: "COMPLETED",
		Amount: paypal.Amount{CurrencyCode: string(spec.Total.Currency), Value: spec.Total.Decimal()}}}
	return o
}

func (f *fakePP) CreateVaultedOrder(_ context.Context, _ string, spec paypal.OrderSpec, opt paypal.CallOptions) (paypal.Order, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.keys = append(f.keys, opt.IdempotencyKey)
	if opt.MockApplicationCode == paypal.IssueInstrumentDeclined || f.declineOn[opt.IdempotencyKey] {
		return paypal.Order{}, &paypal.APIError{Status: 422, Name: "UNPROCESSABLE_ENTITY", DebugID: "dbg",
			Details: []paypal.ErrorDetail{{Issue: paypal.IssueInstrumentDeclined}}}
	}
	if o, ok := f.orders[opt.IdempotencyKey]; ok {
		return o, nil // PayPal idempotency: same key, same result
	}
	o := f.completed(spec)
	f.orders[opt.IdempotencyKey] = o
	if f.ambiguous > 0 {
		f.ambiguous--
		return paypal.Order{}, fmt.Errorf("%w: timeout", paypal.ErrAmbiguous)
	}
	return o, nil
}

func (f *fakePP) CreateApprovalOrder(_ context.Context, spec paypal.OrderSpec, _, _ string, opt paypal.CallOptions) (paypal.Order, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.keys = append(f.keys, opt.IdempotencyKey)
	f.seq++
	return paypal.Order{ID: fmt.Sprintf("APPR%014d", f.seq), Status: paypal.OrderPayerActionRequired,
		Links: []paypal.Link{{Rel: "payer-action", Href: "https://www.sandbox.paypal.com/checkoutnow?token=x"}}}, nil
}

func (f *fakePP) GetOrder(_ context.Context, id string) (paypal.Order, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.notApproved {
		return paypal.Order{ID: id, Status: paypal.OrderPayerActionRequired}, nil
	}
	return paypal.Order{ID: id, Status: paypal.OrderApproved}, nil
}

func (f *fakePP) CaptureOrder(_ context.Context, id string, opt paypal.CallOptions) (paypal.Order, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.keys = append(f.keys, opt.IdempotencyKey)
	o := f.completed(paypal.OrderSpec{Total: money.New(18600, money.USD)})
	o.ID = id
	return o, nil
}

func (f *fakePP) CreatePayoutBatch(_ context.Context, b paypal.PayoutBatch) (paypal.PayoutBatchResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.payouts = append(f.payouts, b)
	var r paypal.PayoutBatchResult
	r.BatchHeader.PayoutBatchID = "PB" + b.SenderBatchID[3:15]
	r.BatchHeader.BatchStatus = paypal.PayoutPending
	return r, nil
}

func (f *fakePP) GetPayoutBatch(context.Context, string) (paypal.PayoutBatchResult, error) {
	return paypal.PayoutBatchResult{}, nil
}

type recorder struct {
	mu    sync.Mutex
	notes []string
}

func (r *recorder) Notify(_ context.Context, title, _, click string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.notes = append(r.notes, title+" "+click)
	return nil
}

type harness struct {
	svc      *Service
	st       *store.Store
	pp       *fakePP
	notes    *recorder
	pub      ed25519.PublicKey
	supplier ed25519.PrivateKey
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	seed := make([]byte, 32)
	_, _ = rand.Read(seed)
	minter, err := token.NewMinter(hex.EncodeToString(seed), "k1")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	st, err := store.Open(ctx, url, ed25519.NewKeyFromSeed(seed))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	if err := store.ResetForTests(ctx, st); err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	verifier, _ := token.NewVerifier(map[string]ed25519.PublicKey{"k1": minter.PublicKey()})
	pp := newFakePP()
	notes := &recorder{}
	_, supplierKey, _ := ed25519.GenerateKey(rand.Reader)
	return &harness{
		svc: New(st, pp, minter, verifier, notes, nil, Config{PublicBaseURL: "https://mandate.test"}),
		st:  st, pp: pp, notes: notes, pub: minter.PublicKey(), supplier: supplierKey,
	}
}

func usd(m int64) money.Money { return money.New(m, money.USD) }

func (h *harness) mandate(t *testing.T, mutate func(*policy.Mandate)) MandateView {
	t.Helper()
	now := time.Now()
	m := policy.Mandate{
		PrincipalID: "owner", AgentID: "agent:procure-01",
		NotBefore: now.Add(-time.Hour).Unix(), NotAfter: now.Add(30 * 24 * time.Hour).Unix(),
		Envelope: usd(60000), Period: 7 * 24 * time.Hour, PerIntentMax: usd(25000), CosignAbove: usd(20000),
		Payees: []policy.Payee{{Ref: "supplier:bean-co", ReceiverEmail: "bean-co@example.com",
			PublicKey: h.supplier.Public().(ed25519.PublicKey)}},
	}
	if mutate != nil {
		mutate(&m)
	}
	v, err := h.svc.CreateMandate(context.Background(), m)
	if err != nil {
		t.Fatal(err)
	}
	if v, err = h.svc.ActivateWithVault(context.Background(), v.ID, "VAULT-1"); err != nil {
		t.Fatal(err)
	}
	return v
}

func (h *harness) signedQuote(t *testing.T, mandateID string, total int64) *quote.Signed {
	t.Helper()
	q := quote.Quote{ID: "q", SupplierRef: "supplier:bean-co", BuyerRef: mandateID,
		Lines:    []quote.Line{{SKU: "pitcher-20oz", Title: "Milk Frothing Pitcher", Qty: 1, UnitPrice: usd(total)}},
		Shipping: usd(0), Total: usd(total), IssuedAt: time.Now().Unix(), ExpiresAt: time.Now().Add(10 * time.Minute).Unix()}
	s, err := quote.Sign(q, h.supplier)
	if err != nil {
		t.Fatal(err)
	}
	return &s
}

func (h *harness) propose(t *testing.T, m MandateView, payee string, amount int64) Decision {
	t.Helper()
	p := Proposal{MandateID: m.ID, AgentID: "agent:procure-01", PayeeRef: payee, Amount: usd(amount), Category: "frothing-pitchers"}
	if payee == "supplier:bean-co" {
		p.Quote = h.signedQuote(t, m.ID, amount)
	}
	d, err := h.svc.Propose(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func (h *harness) verifyAudit(t *testing.T) audit.Report {
	t.Helper()
	rows, err := h.st.AuditRows(context.Background(), 0, 100000)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := audit.Verify(rows, h.pub)
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func TestGoldenPath(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	m := h.mandate(t, nil)

	d := h.propose(t, m, "supplier:bean-co", 15750)
	if d.Outcome != policy.Authorize || d.State != fsm.Minted || d.TokenJTI == "" {
		t.Fatalf("decision %+v", d)
	}
	mv, _ := h.svc.GetMandate(ctx, m.ID)
	if mv.ReservedMinor != 15750 {
		t.Fatalf("reserved %d", mv.ReservedMinor)
	}

	it, err := h.svc.Execute(ctx, d.IntentID, ExecOptions{})
	if err != nil || it.State != fsm.Captured {
		t.Fatalf("execute: %+v %v", it, err)
	}
	mv, _ = h.svc.GetMandate(ctx, m.ID)
	if mv.ReservedMinor != 0 || mv.CapturedMinor != 15750 {
		t.Fatalf("envelope after capture %+v", mv)
	}

	res, err := h.svc.SettleMandate(ctx, m.ID)
	if err != nil || res.Items != 1 || res.TotalMinor != 15750 {
		t.Fatalf("settle %+v %v", res, err)
	}
	if err := h.svc.applyPayoutItem(ctx, res.BatchID, "bean-co@example.com", "ITEM1", "SUCCESS"); err != nil {
		t.Fatal(err)
	}
	it, _ = h.svc.GetIntent(ctx, d.IntentID)
	if it.State != fsm.Settled {
		t.Fatalf("final state %s", it.State)
	}
	rep := h.verifyAudit(t)
	if rep.Entries < 8 || rep.Checkpoints < 2 {
		t.Fatalf("audit report %+v", rep)
	}
}

// The demo's injection beat: a poisoned listing makes the agent pay an
// account outside the mandate. Nothing is reserved and no token exists.
func TestInjectedPayeeDenied(t *testing.T) {
	h := newHarness(t)
	m := h.mandate(t, nil)
	d := h.propose(t, m, "attacker:refund-desk", 480000)
	if d.Outcome != policy.Deny || d.State != fsm.Denied || d.Reasons[0] != string(policy.PayeeNotAllowed) || d.TokenJTI != "" {
		t.Fatalf("decision %+v", d)
	}
	mv, _ := h.svc.GetMandate(context.Background(), m.ID)
	if mv.ReservedMinor != 0 {
		t.Fatal("denied intent reserved funds")
	}
	h.verifyAudit(t)
}

func TestDeclineRecoveryApproveCapture(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	m := h.mandate(t, nil)
	d := h.propose(t, m, "supplier:bean-co", 18600)
	it, err := h.svc.Execute(ctx, d.IntentID, ExecOptions{SimulateDecline: true})
	if err != nil || it.State != fsm.Recovery || it.ApprovalURL == "" || it.OrderID == "" {
		t.Fatalf("after decline: %+v %v", it, err)
	}
	if len(h.notes.notes) != 1 {
		t.Fatalf("expected one push notification, got %v", h.notes.notes)
	}
	mv, _ := h.svc.GetMandate(ctx, m.ID)
	if mv.ReservedMinor != 18600 {
		t.Fatal("recovery must keep the original reservation")
	}
	link, err := h.svc.ResolveShortLink(ctx, it.ApprovalURL[len("https://mandate.test/a/"):])
	if err != nil || link == "" {
		t.Fatalf("short link %q %v", link, err)
	}
	it, err = h.svc.OnOrderApproved(ctx, it.OrderID)
	if err != nil || it.State != fsm.Captured {
		t.Fatalf("after approval: %+v %v", it, err)
	}
	// A late duplicate approval (return URL racing the webhook) is a no-op.
	again, err := h.svc.OnOrderApproved(ctx, it.OrderID)
	if err != nil || again.State != fsm.Captured {
		t.Fatalf("duplicate approval: %+v %v", again, err)
	}
	h.verifyAudit(t)
}

func TestAmbiguousOutcomeResolvedBySameKey(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	m := h.mandate(t, nil)
	h.pp.ambiguous = 1
	d := h.propose(t, m, "supplier:bean-co", 15000)
	it, err := h.svc.Execute(ctx, d.IntentID, ExecOptions{})
	if err != nil || it.State != fsm.Unknown {
		t.Fatalf("expected UNKNOWN, got %+v %v", it, err)
	}
	it, err = h.svc.ResolveUnknown(ctx, d.IntentID)
	if err != nil || it.State != fsm.Captured {
		t.Fatalf("resolve: %+v %v", it, err)
	}
	if len(h.pp.keys) != 2 || h.pp.keys[0] != h.pp.keys[1] {
		t.Fatalf("replay must reuse the idempotency key: %v", h.pp.keys)
	}
	var n int
	_ = h.st.Pool.QueryRow(ctx, `SELECT count(*) FROM captures WHERE intent_id = $1`, d.IntentID).Scan(&n)
	if n != 1 {
		t.Fatalf("expected exactly one capture, got %d", n)
	}
}

func TestCapabilityIsSingleUse(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	m := h.mandate(t, nil)
	d := h.propose(t, m, "supplier:bean-co", 15000)
	if _, err := h.svc.Execute(ctx, d.IntentID, ExecOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.Execute(ctx, d.IntentID, ExecOptions{}); !errors.Is(err, ErrState) {
		t.Fatalf("second execution must be refused, got %v", err)
	}
	if len(h.pp.keys) != 1 {
		t.Fatalf("PayPal called %d times", len(h.pp.keys))
	}
}

// Concurrent proposals can never reserve past the envelope (I1 under load).
func TestConcurrentProposalsNeverOverspend(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	m := h.mandate(t, func(m *policy.Mandate) {
		m.CosignAbove = usd(0)
		m.Velocity = nil
	})
	var wg sync.WaitGroup
	results := make(chan Decision, 12)
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := h.svc.Propose(ctx, Proposal{MandateID: m.ID, AgentID: "agent:procure-01", PayeeRef: "supplier:bean-co",
				Amount: usd(15000), Category: "frothing-pitchers"})
			if err != nil {
				t.Error(err)
				return
			}
			results <- d
		}()
	}
	wg.Wait()
	close(results)
	authorized := 0
	for d := range results {
		if d.Outcome == policy.Authorize {
			authorized++
		}
	}
	mv, _ := h.svc.GetMandate(ctx, m.ID)
	if authorized != 4 || mv.ReservedMinor != 60000 {
		t.Fatalf("authorized %d, reserved %d (limit 60000)", authorized, mv.ReservedMinor)
	}
	h.verifyAudit(t)
}

func TestCosignFlow(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	m := h.mandate(t, nil)
	d := h.propose(t, m, "supplier:bean-co", 21000)
	if d.State != fsm.AwaitingCosign {
		t.Fatalf("state %s", d.State)
	}
	mv, _ := h.svc.GetMandate(ctx, m.ID)
	if mv.ReservedMinor != 0 {
		t.Fatal("awaiting co-sign must not reserve")
	}
	d, err := h.svc.Cosign(ctx, d.IntentID)
	if err != nil || d.State != fsm.Minted {
		t.Fatalf("cosign %+v %v", d, err)
	}
}

func TestWebhookDedupAndVaultRevocation(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	m := h.mandate(t, nil)
	ev, _ := json.Marshal(map[string]any{"id": "WH-1", "event_type": "VAULT.PAYMENT-TOKEN.DELETED", "resource": map[string]any{"id": "VAULT-1"}})
	if out, err := h.svc.ApplyWebhook(ctx, ev); err != nil || out != "mandates paused" {
		t.Fatalf("%q %v", out, err)
	}
	if out, _ := h.svc.ApplyWebhook(ctx, ev); out != "duplicate" {
		t.Fatalf("duplicate not detected: %q", out)
	}
	d := h.propose(t, m, "supplier:bean-co", 1000)
	if d.Outcome != policy.Deny || d.Reasons[0] != string(policy.MandateInactive) {
		t.Fatalf("paused mandate still spends: %+v", d)
	}
}

func TestStaleCaptureEventIgnored(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	m := h.mandate(t, nil)
	d := h.propose(t, m, "supplier:bean-co", 15000)
	it, _ := h.svc.Execute(ctx, d.IntentID, ExecOptions{})
	ev, _ := json.Marshal(map[string]any{"id": "WH-2", "event_type": "PAYMENT.CAPTURE.PENDING",
		"resource": map[string]any{"id": it.CaptureID, "supplementary_data": map[string]any{"related_ids": map[string]any{"order_id": it.OrderID}}}})
	out, err := h.svc.ApplyWebhook(ctx, ev)
	if err != nil || out[:5] != "stale" {
		t.Fatalf("%q %v", out, err)
	}
	it, _ = h.svc.GetIntent(ctx, d.IntentID)
	if it.State != fsm.Captured {
		t.Fatalf("stale event regressed state to %s", it.State)
	}
}

func TestNettedSettlementAcrossIntents(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	m := h.mandate(t, func(m *policy.Mandate) { m.Velocity = nil })
	for _, amt := range []int64{3500, 4000, 2500} {
		d := h.propose(t, m, "supplier:bean-co", amt)
		if _, err := h.svc.Execute(ctx, d.IntentID, ExecOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	res, err := h.svc.SettleMandate(ctx, m.ID)
	if err != nil || res.Items != 1 || res.Obligations != 3 || res.TotalMinor != 10000 {
		t.Fatalf("netting %+v %v", res, err)
	}
	if got := h.pp.payouts[0].Items[0].Amount.Minor; got != 10000 {
		t.Fatalf("payout item %d", got)
	}
	if again, _ := h.svc.SettleMandate(ctx, m.ID); again.Items != 0 {
		t.Fatal("obligations settled twice")
	}
}

// A return-URL hit (or forged webhook) before the buyer actually approved
// must not move the intent or capture anything.
func TestPrematureApprovalIgnored(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	m := h.mandate(t, nil)
	d := h.propose(t, m, "supplier:bean-co", 18600)
	it, _ := h.svc.Execute(ctx, d.IntentID, ExecOptions{SimulateDecline: true})
	h.pp.notApproved = true
	got, err := h.svc.OnOrderApproved(ctx, it.OrderID)
	if err != nil || got.State != fsm.Recovery {
		t.Fatalf("premature approval moved intent: %+v %v", got, err)
	}
}

type fakeRefunder struct {
	mu   sync.Mutex
	keys []string
}

func (f *fakeRefunder) RefundCapture(_ context.Context, _ string, amount *money.Money, _, _ string, opt paypal.CallOptions) (paypal.Refund, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.keys = append(f.keys, opt.IdempotencyKey)
	return paypal.Refund{ID: fmt.Sprintf("RF%016d", len(f.keys)), Status: "COMPLETED",
		Amount: paypal.Amount{CurrencyCode: string(amount.Currency), Value: amount.Decimal()}}, nil
}

func TestRefundPolicy(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	m := h.mandate(t, nil)
	d := h.propose(t, m, "supplier:bean-co", 10000)
	it, err := h.svc.Execute(ctx, d.IntentID, ExecOptions{})
	if err != nil || it.State != fsm.Captured {
		t.Fatal(err)
	}
	rf := &fakeRefunder{}
	if _, err := h.svc.Refund(ctx, rf, "CAP-NOT-OURS", nil, ""); !errors.Is(err, ErrRefundDenied) {
		t.Fatalf("refund of a foreign capture must be denied, got %v", err)
	}
	half := usd(5000)
	r1, err := h.svc.Refund(ctx, rf, it.CaptureID, &half, "partial")
	if err != nil || r1.Status != "COMPLETED" || r1.Remaining.Minor != 5000 {
		t.Fatalf("first partial: %+v %v", r1, err)
	}
	r2, err := h.svc.Refund(ctx, rf, it.CaptureID, &half, "partial")
	if err != nil || r2.Remaining.Minor != 0 {
		t.Fatalf("second partial: %+v %v", r2, err)
	}
	if rf.keys[0] == rf.keys[1] {
		t.Fatal("two equal partial refunds shared an idempotency key (PR #108 flaw)")
	}
	one := usd(1)
	if _, err := h.svc.Refund(ctx, rf, it.CaptureID, &one, ""); !errors.Is(err, ErrRefundDenied) {
		t.Fatalf("over-refund must be denied, got %v", err)
	}
	mv, _ := h.svc.GetMandate(ctx, m.ID)
	if mv.CapturedMinor != 0 {
		t.Fatalf("refunds must credit the envelope, captured=%d", mv.CapturedMinor)
	}
	h.verifyAudit(t)
}
