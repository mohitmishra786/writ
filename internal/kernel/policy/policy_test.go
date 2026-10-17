package policy

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/mohitmishra786/paypal-ai-hackathon/internal/kernel/quote"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/money"
)

var now = time.Date(2026, 11, 3, 15, 0, 0, 0, time.UTC)

func usd(minor int64) money.Money { return money.New(minor, money.USD) }

type fixture struct {
	policy      *Policy
	supplierKey ed25519.PrivateKey
}

func newFixture(t *testing.T, mutate func(*Mandate)) fixture {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	m := Mandate{
		ID: "mandate_01", PrincipalID: "owner_01", AgentID: "agent:procure-01",
		Status:    Active,
		NotBefore: now.Add(-time.Hour).Unix(), NotAfter: now.Add(30 * 24 * time.Hour).Unix(),
		Envelope: usd(60000), Period: 7 * 24 * time.Hour,
		PerIntentMax: usd(25000), CosignAbove: usd(20000),
		Velocity: []VelocityRule{
			{Window: 10 * time.Minute, MaxCount: 5},
			{Window: time.Hour, MaxAmount: 40000},
		},
		Payees:     []Payee{{Ref: "supplier:bean-co", ReceiverEmail: "bean-co@example.com", PublicKey: pub}},
		Categories: []string{"frothing-pitchers", "coffee-grinders"},
	}
	if mutate != nil {
		mutate(&m)
	}
	p, err := Compile(m)
	if err != nil {
		t.Fatal(err)
	}
	return fixture{policy: p, supplierKey: priv}
}

func (f fixture) signedQuote(t *testing.T, total int64) *quote.Signed {
	t.Helper()
	q := quote.Quote{
		ID: "q_1", SupplierRef: "supplier:bean-co", BuyerRef: "mandate_01",
		Lines:     []quote.Line{{SKU: "pitcher-20oz", Title: "Milk Frothing Pitcher", Qty: 1, UnitPrice: usd(total)}},
		Shipping:  usd(0),
		Total:     usd(total),
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(10 * time.Minute).Unix(),
	}
	s, err := quote.Sign(q, f.supplierKey)
	if err != nil {
		t.Fatal(err)
	}
	return &s
}

func intent(amount int64, q *quote.Signed) Intent {
	return Intent{
		ID: "intent_01", MandateID: "mandate_01", AgentID: "agent:procure-01",
		PayeeRef: "supplier:bean-co", Amount: usd(amount), Category: "frothing-pitchers", Quote: q,
	}
}

func empty() Snapshot { return Snapshot{PeriodCommitted: usd(0)} }

func expect(t *testing.T, d Decision, o Outcome, reasons ...Reason) {
	t.Helper()
	if d.Outcome != o || !slices.Equal(d.Reasons, reasons) {
		t.Fatalf("got %s %v, want %s %v", d.Outcome, d.Reasons, o, reasons)
	}
}

func TestAuthorizeWithinLimits(t *testing.T) {
	f := newFixture(t, nil)
	d := f.policy.Evaluate(intent(15750, f.signedQuote(t, 15750)), empty(), now)
	expect(t, d, Authorize)
	if d.PolicyHash == "" || d.PolicyHash != f.policy.Hash {
		t.Fatal("decision must carry the policy hash")
	}
}

func TestCosignAboveThreshold(t *testing.T) {
	f := newFixture(t, nil)
	expect(t, f.policy.Evaluate(intent(21000, f.signedQuote(t, 21000)), empty(), now), Cosign, CosignRequired)
}

// The demo's injection beat: a poisoned listing convinces the agent to pay an
// account outside the mandate. Identity checks run first, so the decision is
// PAYEE_NOT_ALLOWED regardless of amount.
func TestInjectedPayeeDeniedBeforeAmountChecks(t *testing.T) {
	f := newFixture(t, nil)
	in := intent(480000, nil)
	in.PayeeRef = "attacker:refund-desk"
	expect(t, f.policy.Evaluate(in, empty(), now), Deny, PayeeNotAllowed)
}

func TestQuoteSignedByAttackerKeyDenied(t *testing.T) {
	f := newFixture(t, nil)
	_, attacker, _ := ed25519.GenerateKey(rand.Reader)
	forged := newFixture(t, nil)
	forged.supplierKey = attacker
	expect(t, f.policy.Evaluate(intent(15750, forged.signedQuote(t, 15750)), empty(), now), Deny, QuoteSignatureInvalid)
}

func TestIntentAmountMustEqualQuote(t *testing.T) {
	f := newFixture(t, nil)
	expect(t, f.policy.Evaluate(intent(19999, f.signedQuote(t, 15750)), empty(), now), Deny, QuoteMismatch)
}

func TestExpiredQuoteDenied(t *testing.T) {
	f := newFixture(t, nil)
	q := f.signedQuote(t, 15750)
	expect(t, f.policy.Evaluate(intent(15750, q), empty(), now.Add(11*time.Minute)), Deny, QuoteMismatch)
}

func TestQuantitativeChecksAllReported(t *testing.T) {
	f := newFixture(t, nil)
	in := intent(26000, nil)
	in.Category = "espresso-machines"
	snap := Snapshot{PeriodCommitted: usd(50000)}
	expect(t, f.policy.Evaluate(in, snap, now), Deny, CategoryNotAllowed, PerIntentExceeded, EnvelopeExceeded)
}

func TestEnvelopeBoundaryIsInclusive(t *testing.T) {
	f := newFixture(t, nil)
	expect(t, f.policy.Evaluate(intent(10000, nil), Snapshot{PeriodCommitted: usd(50000)}, now), Authorize)
	expect(t, f.policy.Evaluate(intent(10001, nil), Snapshot{PeriodCommitted: usd(50000)}, now), Deny, EnvelopeExceeded)
}

func TestVelocityCount(t *testing.T) {
	f := newFixture(t, nil)
	snap := empty()
	for i := range 5 {
		snap.Recent = append(snap.Recent, Spend{At: now.Add(-time.Duration(i+1) * time.Minute), Amount: usd(100)})
	}
	expect(t, f.policy.Evaluate(intent(100, nil), snap, now), Deny, VelocityExceeded)
	snap.Recent[0].At = now.Add(-11 * time.Minute)
	expect(t, f.policy.Evaluate(intent(100, nil), snap, now), Authorize)
}

func TestVelocityAmount(t *testing.T) {
	f := newFixture(t, nil)
	snap := Snapshot{PeriodCommitted: usd(30000), Recent: []Spend{{At: now.Add(-20 * time.Minute), Amount: usd(30000)}}}
	expect(t, f.policy.Evaluate(intent(15000, nil), snap, now), Deny, VelocityExceeded)
}

func TestInactiveMandate(t *testing.T) {
	f := newFixture(t, func(m *Mandate) { m.Status = Paused })
	expect(t, f.policy.Evaluate(intent(100, nil), empty(), now), Deny, MandateInactive)
	g := newFixture(t, nil)
	expect(t, g.policy.Evaluate(intent(100, nil), empty(), now.Add(31*24*time.Hour)), Deny, MandateInactive)
}

func TestSchemaChecks(t *testing.T) {
	f := newFixture(t, nil)
	for name, mut := range map[string]func(*Intent){
		"wrong agent":    func(i *Intent) { i.AgentID = "agent:other" },
		"wrong mandate":  func(i *Intent) { i.MandateID = "mandate_02" },
		"wrong currency": func(i *Intent) { i.Amount = money.New(100, "EUR") },
		"non-positive":   func(i *Intent) { i.Amount = usd(0) },
	} {
		in := intent(100, nil)
		mut(&in)
		if d := f.policy.Evaluate(in, empty(), now); d.Outcome != Deny || d.Reasons[0] != SchemaInvalid {
			t.Fatalf("%s: got %v", name, d)
		}
	}
}

func TestCELRules(t *testing.T) {
	f := newFixture(t, func(m *Mandate) {
		m.Rules = []Rule{
			{ID: "business-hours", Expr: "hour_utc >= 13 && hour_utc < 23"},
			{ID: "max-qty", Expr: "total_qty <= 12"},
		}
	})
	expect(t, f.policy.Evaluate(intent(15750, f.signedQuote(t, 15750)), empty(), now), Authorize)
	night := now.Add(10 * time.Hour)
	q := f.signedQuote(t, 15750)
	q.Quote.IssuedAt, q.Quote.ExpiresAt = night.Unix(), night.Add(10*time.Minute).Unix()
	resigned, _ := quote.Sign(q.Quote, f.supplierKey)
	expect(t, f.policy.Evaluate(intent(15750, &resigned), empty(), night), Deny, RuleFailed("business-hours"))
}

func TestCompileRejectsBadMandates(t *testing.T) {
	cases := map[string]func(*Mandate){
		"non-boolean rule":    func(m *Mandate) { m.Rules = []Rule{{ID: "r", Expr: "amount_minor + 1"}} },
		"unknown variable":    func(m *Mandate) { m.Rules = []Rule{{ID: "r", Expr: "balance > 0"}} },
		"per-intent > env":    func(m *Mandate) { m.PerIntentMax = usd(70000) },
		"mixed currency":      func(m *Mandate) { m.CosignAbove = money.New(1, "EUR") },
		"payee without key":   func(m *Mandate) { m.Payees = []Payee{{Ref: "x"}} },
		"empty window":        func(m *Mandate) { m.NotAfter = m.NotBefore },
		"duplicate payee ref": func(m *Mandate) { m.Payees = append(m.Payees, m.Payees[0]) },
	}
	for name, mut := range cases {
		t.Run(name, func(t *testing.T) {
			pub, _, _ := ed25519.GenerateKey(rand.Reader)
			m := Mandate{
				ID: "m", AgentID: "a", Status: Active, NotBefore: 0, NotAfter: 10,
				Envelope: usd(60000), Period: time.Hour, PerIntentMax: usd(25000), CosignAbove: usd(0),
				Payees: []Payee{{Ref: "s", PublicKey: pub}},
			}
			mut(&m)
			if _, err := Compile(m); !errors.Is(err, ErrInvalidMandate) {
				t.Fatalf("expected ErrInvalidMandate, got %v", err)
			}
		})
	}
}

func TestPolicyHashChangesWithPolicy(t *testing.T) {
	a := newFixture(t, nil)
	b := newFixture(t, func(m *Mandate) { m.PerIntentMax = usd(24000) })
	if a.policy.Hash == b.policy.Hash {
		t.Fatal("hash must bind the policy contents")
	}
}

func TestDeterministic(t *testing.T) {
	f := newFixture(t, nil)
	in := intent(15750, f.signedQuote(t, 15750))
	first := f.policy.Evaluate(in, empty(), now)
	for range 100 {
		if d := f.policy.Evaluate(in, empty(), now); !slices.Equal(d.Reasons, first.Reasons) || d.Outcome != first.Outcome {
			t.Fatal("evaluation is not deterministic")
		}
	}
}
