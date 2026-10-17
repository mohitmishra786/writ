// Package policy evaluates agent intents against a principal's mandate.
//
// Evaluate is a pure function of (mandate, intent, ledger snapshot, now): no
// I/O, no LLM, no clock reads. The caller takes the snapshot inside the same
// database transaction that reserves funds, so the decision and the
// reservation see identical state.
package policy

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"time"

	"cel.dev/cel-go/cel"

	"github.com/mohitmishra786/paypal-ai-hackathon/internal/canon"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/kernel/quote"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/money"
)

type Status string

const (
	Active  Status = "ACTIVE"
	Paused  Status = "PAUSED"
	Revoked Status = "REVOKED"
)

type Payee struct {
	Ref           string            `json:"ref"`
	ReceiverEmail string            `json:"receiver_email"`
	PublicKey     ed25519.PublicKey `json:"public_key"`
}

type VelocityRule struct {
	Window    time.Duration `json:"window_ns"`
	MaxCount  int           `json:"max_count,omitempty"`
	MaxAmount int64         `json:"max_amount_minor,omitempty"`
}

type Rule struct {
	ID   string `json:"id"`
	Expr string `json:"expr"`
}

type Mandate struct {
	ID           string         `json:"id"`
	PrincipalID  string         `json:"principal_id"`
	AgentID      string         `json:"agent_id"`
	Status       Status         `json:"status"`
	NotBefore    int64          `json:"not_before"`
	NotAfter     int64          `json:"not_after"`
	Envelope     money.Money    `json:"envelope"`
	Period       time.Duration  `json:"period_ns"`
	PerIntentMax money.Money    `json:"per_intent_max"`
	CosignAbove  money.Money    `json:"cosign_above"`
	Velocity     []VelocityRule `json:"velocity"`
	Payees       []Payee        `json:"payees"`
	Categories   []string       `json:"categories"`
	Rules        []Rule         `json:"rules"`
}

type Intent struct {
	ID        string        `json:"id"`
	MandateID string        `json:"mandate_id"`
	AgentID   string        `json:"agent_id"`
	PayeeRef  string        `json:"payee_ref"`
	Amount    money.Money   `json:"amount"`
	Category  string        `json:"category"`
	Quote     *quote.Signed `json:"quote,omitempty"`
}

// Spend is a reservation or capture already counted against the envelope.
type Spend struct {
	At     time.Time
	Amount money.Money
}

// Snapshot is the ledger state for one mandate, read under the envelope lock.
type Snapshot struct {
	PeriodCommitted money.Money // reserved + captured in the current period
	Recent          []Spend     // spends inside the longest velocity window
}

type Outcome string

const (
	Deny      Outcome = "DENY"
	Cosign    Outcome = "COSIGN"
	Authorize Outcome = "AUTHORIZE"
)

type Reason string

const (
	MandateInactive       Reason = "MANDATE_INACTIVE"
	SchemaInvalid         Reason = "SCHEMA_INVALID"
	PayeeNotAllowed       Reason = "PAYEE_NOT_ALLOWED"
	QuoteSignatureInvalid Reason = "QUOTE_SIGNATURE_INVALID"
	QuoteMismatch         Reason = "QUOTE_MISMATCH"
	CategoryNotAllowed    Reason = "CATEGORY_NOT_ALLOWED"
	PerIntentExceeded     Reason = "PER_INTENT_EXCEEDED"
	EnvelopeExceeded      Reason = "ENVELOPE_EXCEEDED"
	VelocityExceeded      Reason = "VELOCITY_EXCEEDED"
	CosignRequired        Reason = "COSIGN_REQUIRED"
)

func RuleFailed(id string) Reason { return Reason("CEL_RULE_FAILED:" + id) }

type Decision struct {
	Outcome    Outcome  `json:"outcome"`
	Reasons    []Reason `json:"reasons"`
	PolicyHash string   `json:"policy_hash"`
}

// Policy is a mandate with its CEL rules type-checked and compiled.
type Policy struct {
	Mandate  Mandate
	Hash     string
	payees   map[string]Payee
	programs []compiledRule
}

type compiledRule struct {
	id  string
	prg cel.Program
}

const celCostLimit = 10_000

var ErrInvalidMandate = errors.New("policy: invalid mandate")

func newEnv() (*cel.Env, error) {
	return cel.NewEnv(
		cel.Variable("amount_minor", cel.IntType),
		cel.Variable("currency", cel.StringType),
		cel.Variable("payee", cel.StringType),
		cel.Variable("category", cel.StringType),
		cel.Variable("hour_utc", cel.IntType),
		cel.Variable("weekday_utc", cel.IntType),
		cel.Variable("line_count", cel.IntType),
		cel.Variable("total_qty", cel.IntType),
	)
}

// Compile validates a mandate and type-checks its rules. Rules must be
// boolean expressions; anything else is rejected at signing time, not at
// spend time.
func Compile(m Mandate) (*Policy, error) {
	if m.ID == "" || m.AgentID == "" || m.Period <= 0 || m.NotAfter <= m.NotBefore {
		return nil, fmt.Errorf("%w: missing id, agent, period or validity window", ErrInvalidMandate)
	}
	cur := m.Envelope.Currency
	if !cur.Valid() || m.PerIntentMax.Currency != cur || m.CosignAbove.Currency != cur {
		return nil, fmt.Errorf("%w: all limits must share one valid currency", ErrInvalidMandate)
	}
	if m.Envelope.Minor <= 0 || m.PerIntentMax.Minor <= 0 || m.PerIntentMax.Minor > m.Envelope.Minor {
		return nil, fmt.Errorf("%w: per-intent max must be positive and within the envelope", ErrInvalidMandate)
	}
	p := &Policy{Mandate: m, payees: map[string]Payee{}}
	for _, py := range m.Payees {
		if py.Ref == "" || len(py.PublicKey) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("%w: payee %q needs a ref and an Ed25519 key", ErrInvalidMandate, py.Ref)
		}
		if _, dup := p.payees[py.Ref]; dup {
			return nil, fmt.Errorf("%w: duplicate payee %q", ErrInvalidMandate, py.Ref)
		}
		p.payees[py.Ref] = py
	}
	env, err := newEnv()
	if err != nil {
		return nil, err
	}
	for _, r := range m.Rules {
		ast, iss := env.Compile(r.Expr)
		if iss != nil && iss.Err() != nil {
			return nil, fmt.Errorf("%w: rule %s: %v", ErrInvalidMandate, r.ID, iss.Err())
		}
		if ast.OutputType() != cel.BoolType {
			return nil, fmt.Errorf("%w: rule %s must be boolean", ErrInvalidMandate, r.ID)
		}
		prg, err := env.Program(ast, cel.CostLimit(celCostLimit))
		if err != nil {
			return nil, err
		}
		p.programs = append(p.programs, compiledRule{id: r.ID, prg: prg})
	}
	b, err := canon.Marshal(m)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(b)
	p.Hash = "sha256:" + hex.EncodeToString(sum[:])
	return p, nil
}

func (p *Policy) Payee(ref string) (Payee, bool) {
	py, ok := p.payees[ref]
	return py, ok
}

// Evaluate returns a decision. Identity checks (mandate state, schema, payee,
// quote signature) short-circuit because later checks are meaningless for an
// untrusted counterparty; quantitative checks all run so the audit log shows
// every limit an intent would have breached.
func (p *Policy) Evaluate(in Intent, snap Snapshot, now time.Time) Decision {
	d := Decision{PolicyHash: p.Hash}
	deny := func(r ...Reason) Decision {
		d.Outcome, d.Reasons = Deny, append(d.Reasons, r...)
		return d
	}
	m := p.Mandate
	if m.Status != Active || now.Unix() < m.NotBefore || now.Unix() >= m.NotAfter {
		return deny(MandateInactive)
	}
	if in.ID == "" || in.MandateID != m.ID || in.AgentID != m.AgentID ||
		in.Amount.Currency != m.Envelope.Currency || in.Amount.Minor <= 0 {
		return deny(SchemaInvalid)
	}
	payee, ok := p.payees[in.PayeeRef]
	if !ok {
		return deny(PayeeNotAllowed)
	}
	if in.Quote != nil {
		if err := in.Quote.Verify(payee.PublicKey, now); err != nil {
			if errors.Is(err, quote.ErrBadSignature) {
				return deny(QuoteSignatureInvalid)
			}
			return deny(QuoteMismatch)
		}
		q := in.Quote.Quote
		if q.SupplierRef != in.PayeeRef || q.BuyerRef != m.ID || q.Total != in.Amount {
			return deny(QuoteMismatch)
		}
	}

	if len(m.Categories) > 0 && !slices.Contains(m.Categories, in.Category) {
		d.Reasons = append(d.Reasons, CategoryNotAllowed)
	}
	if in.Amount.Minor > m.PerIntentMax.Minor {
		d.Reasons = append(d.Reasons, PerIntentExceeded)
	}
	if snap.PeriodCommitted.Currency != m.Envelope.Currency ||
		snap.PeriodCommitted.Minor > m.Envelope.Minor-in.Amount.Minor {
		d.Reasons = append(d.Reasons, EnvelopeExceeded)
	}
	if p.velocityExceeded(in, snap, now) {
		d.Reasons = append(d.Reasons, VelocityExceeded)
	}
	d.Reasons = append(d.Reasons, p.ruleFailures(in, now)...)
	if len(d.Reasons) > 0 {
		d.Outcome = Deny
		return d
	}
	if m.CosignAbove.Minor > 0 && in.Amount.Minor > m.CosignAbove.Minor {
		d.Outcome, d.Reasons = Cosign, []Reason{CosignRequired}
		return d
	}
	d.Outcome = Authorize
	return d
}

func (p *Policy) velocityExceeded(in Intent, snap Snapshot, now time.Time) bool {
	for _, v := range p.Mandate.Velocity {
		since := now.Add(-v.Window)
		count, sum := 1, in.Amount.Minor
		for _, s := range snap.Recent {
			if s.At.After(since) {
				count++
				sum += s.Amount.Minor
			}
		}
		if (v.MaxCount > 0 && count > v.MaxCount) || (v.MaxAmount > 0 && sum > v.MaxAmount) {
			return true
		}
	}
	return false
}

// ruleFailures fails closed: an evaluation error counts as a failed rule.
func (p *Policy) ruleFailures(in Intent, now time.Time) []Reason {
	if len(p.programs) == 0 {
		return nil
	}
	vars := map[string]any{
		"amount_minor": in.Amount.Minor,
		"currency":     string(in.Amount.Currency),
		"payee":        in.PayeeRef,
		"category":     in.Category,
		"hour_utc":     int64(now.UTC().Hour()),
		"weekday_utc":  int64(now.UTC().Weekday()),
		"line_count":   int64(0),
		"total_qty":    int64(0),
	}
	if in.Quote != nil {
		var qty int64
		for _, l := range in.Quote.Quote.Lines {
			qty += l.Qty
		}
		vars["line_count"] = int64(len(in.Quote.Quote.Lines))
		vars["total_qty"] = qty
	}
	var out []Reason
	for _, r := range p.programs {
		val, _, err := r.prg.Eval(vars)
		if err != nil || val.Value() != true {
			out = append(out, RuleFailed(r.id))
		}
	}
	return out
}
