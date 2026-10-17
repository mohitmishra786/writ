package kernel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/oklog/ulid/v2"

	"github.com/mohitmishra786/paypal-ai-hackathon/internal/kernel/audit"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/kernel/fsm"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/kernel/policy"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/kernel/quote"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/kernel/token"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/money"
)

// Proposal is an agent's request to spend.
type Proposal struct {
	MandateID string
	AgentID   string
	PayeeRef  string
	Amount    money.Money
	Category  string
	Quote     *quote.Signed
	Trace     json.RawMessage // agent reasoning steps, stored with the intent
}

// Decision is what the kernel tells the agent.
type Decision struct {
	IntentID   string         `json:"intent_id"`
	Outcome    policy.Outcome `json:"outcome"`
	Reasons    []string       `json:"reasons"`
	State      fsm.State      `json:"state"`
	PolicyHash string         `json:"policy_hash"`
	TokenJTI   string         `json:"token_jti,omitempty"`
}

type mandateRow struct {
	m         policy.Mandate
	hash      string
	status    string
	vaultID   string
	limit     int64
	reserved  int64
	captured  int64
	periodSec int64
	periodAt  time.Time
}

// lockMandate locks the envelope row (serialising every spend decision for
// this mandate) and rolls the period over if it has elapsed.
func (s *Service) lockMandate(ctx context.Context, tx pgx.Tx, mandateID string, now time.Time) (mandateRow, error) {
	var r mandateRow
	var raw []byte
	var vault *string
	err := tx.QueryRow(ctx, `SELECT m.policy, m.policy_hash, m.status, m.vault_id, e.limit_minor, e.reserved_minor, e.captured_minor, e.period_seconds, e.period_start
		FROM envelopes e JOIN mandates m ON m.id = e.mandate_id WHERE e.mandate_id = $1 FOR UPDATE OF e, m`, mandateID).
		Scan(&raw, &r.hash, &r.status, &vault, &r.limit, &r.reserved, &r.captured, &r.periodSec, &r.periodAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	if vault != nil {
		r.vaultID = *vault
	}
	if err := json.Unmarshal(raw, &r.m); err != nil {
		return r, err
	}
	period := time.Duration(r.periodSec) * time.Second
	if now.Sub(r.periodAt) >= period {
		elapsed := now.Sub(r.periodAt) / period
		r.periodAt = r.periodAt.Add(elapsed * period)
		r.captured = 0
		if _, err := tx.Exec(ctx, `UPDATE envelopes SET period_start = $2, captured_minor = 0 WHERE mandate_id = $1`, mandateID, r.periodAt); err != nil {
			return r, err
		}
	}
	return r, nil
}

// spendingStates count toward velocity windows.
var spendingStates = []string{
	string(fsm.Authorized), string(fsm.Minted), string(fsm.Submitted), string(fsm.Unknown),
	string(fsm.CapturePending), string(fsm.Declined), string(fsm.Recovery), string(fsm.Approved),
	string(fsm.Captured), string(fsm.Settling), string(fsm.Settled), string(fsm.SettleFailed), string(fsm.Reconciled),
}

func (s *Service) snapshot(ctx context.Context, tx pgx.Tx, r mandateRow, now time.Time) (policy.Snapshot, error) {
	snap := policy.Snapshot{PeriodCommitted: money.New(r.reserved+r.captured, r.m.Envelope.Currency)}
	var window time.Duration
	for _, v := range r.m.Velocity {
		window = max(window, v.Window)
	}
	if window == 0 {
		return snap, nil
	}
	rows, err := tx.Query(ctx, `SELECT created_at, amount_minor FROM intents WHERE mandate_id = $1 AND created_at > $2 AND state = ANY($3)`,
		r.m.ID, now.Add(-window), spendingStates)
	if err != nil {
		return snap, err
	}
	defer rows.Close()
	for rows.Next() {
		var at time.Time
		var minor int64
		if err := rows.Scan(&at, &minor); err != nil {
			return snap, err
		}
		snap.Recent = append(snap.Recent, policy.Spend{At: at, Amount: money.New(minor, r.m.Envelope.Currency)})
	}
	return snap, rows.Err()
}

// Propose evaluates an intent and, if authorized, reserves funds and mints a
// single-use capability, all in one transaction with its audit entries.
func (s *Service) Propose(ctx context.Context, p Proposal) (Decision, error) {
	now := s.now().UTC()
	intentID := newID("intent")
	var d Decision
	err := s.store.Tx(ctx, func(tx pgx.Tx) error {
		r, err := s.lockMandate(ctx, tx, p.MandateID, now)
		if err != nil {
			return err
		}
		pol, err := s.policyFor(r.m, r.hash)
		if err != nil {
			return err
		}
		in := policy.Intent{ID: intentID, MandateID: p.MandateID, AgentID: p.AgentID, PayeeRef: p.PayeeRef,
			Amount: p.Amount, Category: p.Category, Quote: p.Quote}
		snap, err := s.snapshot(ctx, tx, r, now)
		if err != nil {
			return err
		}
		dec := pol.Evaluate(in, snap, now)
		if r.status != "ACTIVE" {
			dec = policy.Decision{Outcome: policy.Deny, Reasons: []policy.Reason{policy.MandateInactive}, PolicyHash: pol.Hash}
		}
		if r.vaultID == "" && dec.Outcome != policy.Deny {
			dec = policy.Decision{Outcome: policy.Deny, Reasons: []policy.Reason{"VAULT_NOT_LINKED"}, PolicyHash: pol.Hash}
		}

		var quoteJSON []byte
		var quoteHash string
		if p.Quote != nil {
			if quoteJSON, err = json.Marshal(p.Quote); err != nil {
				return err
			}
			quoteHash, _ = p.Quote.Quote.Hash()
		}
		amount := p.Amount
		if amount.Minor <= 0 || !amount.Currency.Valid() {
			// Keep malformed proposals in the ledger for the audit trail.
			amount = money.New(1, r.m.Envelope.Currency)
		}
		reasons := make([]string, len(dec.Reasons))
		for i, rs := range dec.Reasons {
			reasons[i] = string(rs)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO intents (id, mandate_id, agent_id, payee_ref, amount_minor, currency, category, quote, quote_hash, state, reasons, policy_hash, trace)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,nullif($9,''),'RECEIVED',$10,$11,$12)`,
			intentID, p.MandateID, p.AgentID, p.PayeeRef, amount.Minor, string(amount.Currency), p.Category,
			nullJSON(quoteJSON), quoteHash, reasons, pol.Hash, nullJSON(p.Trace)); err != nil {
			return err
		}
		it := Intent{ID: intentID, MandateID: p.MandateID, AgentID: p.AgentID, PayeeRef: p.PayeeRef, Amount: amount,
			Category: p.Category, State: fsm.Received, PolicyHash: pol.Hash, QuoteHash: quoteHash}
		if _, err := s.store.AppendAudit(ctx, tx, audit.Entry{
			Kind: audit.KindPolicyDecision, MandateID: it.MandateID, AgentID: it.AgentID, IntentID: it.ID,
			Reasons: reasons, AmountMinor: p.Amount.Minor, Currency: string(p.Amount.Currency), PayeeRef: p.PayeeRef,
			PolicyHash: pol.Hash, QuoteHash: quoteHash, TraceDigest: digest(p.Trace),
			Detail: map[string]any{"outcome": string(dec.Outcome), "category": p.Category},
		}); err != nil {
			return err
		}

		switch dec.Outcome {
		case policy.Deny:
			err = s.transition(ctx, tx, &it, fsm.Denied, nil)
		case policy.Cosign:
			err = s.transition(ctx, tx, &it, fsm.AwaitingCosign, nil)
		default:
			if err = s.transition(ctx, tx, &it, fsm.Authorized, nil); err == nil {
				err = s.reserveAndMint(ctx, tx, &it, r)
			}
		}
		if err != nil {
			return err
		}
		d = Decision{IntentID: it.ID, Outcome: dec.Outcome, Reasons: reasons, State: it.State, PolicyHash: pol.Hash, TokenJTI: it.TokenJTI}
		return nil
	})
	if err != nil {
		return Decision{}, err
	}
	s.publish("intent", d)
	return d, nil
}

// reserveAndMint holds funds (the envelope CHECK enforces I1) and issues the
// capability the executor will consume.
func (s *Service) reserveAndMint(ctx context.Context, tx pgx.Tx, it *Intent, r mandateRow) error {
	if _, err := tx.Exec(ctx, `UPDATE envelopes SET reserved_minor = reserved_minor + $2 WHERE mandate_id = $1`, it.MandateID, it.Amount.Minor); err != nil {
		return fmt.Errorf("reserve: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE intents SET reserved_minor = $2 WHERE id = $1`, it.ID, it.Amount.Minor); err != nil {
		return err
	}
	it.ReservedMinor = it.Amount.Minor
	raw, c, err := s.minter.Mint(token.Capability{
		JTI: "cap_" + ulid.Make().String(), Subject: it.AgentID, MandateID: it.MandateID, IntentID: it.ID,
		Op: token.OpVaultCharge, PayeeRef: it.PayeeRef, Ceiling: it.Amount, PolicyHash: it.PolicyHash, QuoteHash: it.QuoteHash,
	}, s.cfg.TokenTTL)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO tokens (jti, intent_id, raw, expires_at) VALUES ($1,$2,$3,$4)`, c.JTI, it.ID, raw, c.Expires); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE intents SET token_jti = $2 WHERE id = $1`, it.ID, c.JTI); err != nil {
		return err
	}
	it.TokenJTI = c.JTI
	if _, err := s.store.AppendAudit(ctx, tx, audit.Entry{Kind: audit.KindTokenMinted, MandateID: it.MandateID, AgentID: it.AgentID,
		IntentID: it.ID, TokenJTI: c.JTI, AmountMinor: c.Ceiling.Minor, Currency: string(c.Ceiling.Currency), PayeeRef: c.PayeeRef,
		PolicyHash: c.PolicyHash, Detail: map[string]any{"expires_at": c.Expires.Format(time.RFC3339), "kid": c.KeyID}}); err != nil {
		return err
	}
	return s.transition(ctx, tx, it, fsm.Minted, map[string]any{"token_jti": c.JTI})
}

// Cosign records the principal's approval for an intent above the co-sign
// threshold. Policy is re-evaluated under a fresh lock (limits may have moved
// since the agent asked) with the co-sign requirement satisfied.
func (s *Service) Cosign(ctx context.Context, intentID string) (Decision, error) {
	now := s.now().UTC()
	var d Decision
	err := s.store.Tx(ctx, func(tx pgx.Tx) error {
		it, err := s.loadIntent(ctx, tx, "id", intentID, true)
		if err != nil {
			return err
		}
		if it.State != fsm.AwaitingCosign {
			return fmt.Errorf("%w: intent is %s", ErrState, it.State)
		}
		r, err := s.lockMandate(ctx, tx, it.MandateID, now)
		if err != nil {
			return err
		}
		m := r.m
		m.CosignAbove = money.New(0, m.Envelope.Currency)
		pol, err := policy.Compile(m)
		if err != nil {
			return err
		}
		snap, err := s.snapshot(ctx, tx, r, now)
		if err != nil {
			return err
		}
		dec := pol.Evaluate(policy.Intent{ID: it.ID, MandateID: it.MandateID, AgentID: it.AgentID, PayeeRef: it.PayeeRef,
			Amount: it.Amount, Category: it.Category, Quote: it.Quote}, snap, now)
		reasons := make([]string, len(dec.Reasons))
		for i, rs := range dec.Reasons {
			reasons[i] = string(rs)
		}
		if dec.Outcome != policy.Authorize {
			if _, err := tx.Exec(ctx, `UPDATE intents SET reasons = $2 WHERE id = $1`, it.ID, reasons); err != nil {
				return err
			}
			if err := s.transition(ctx, tx, &it, fsm.Revoked, map[string]any{"cosign_reevaluation": reasons}); err != nil {
				return err
			}
			d = Decision{IntentID: it.ID, Outcome: policy.Deny, Reasons: reasons, State: it.State, PolicyHash: it.PolicyHash}
			return nil
		}
		if err := s.transition(ctx, tx, &it, fsm.Authorized, map[string]any{"cosigned_by": "principal"}); err != nil {
			return err
		}
		if err := s.reserveAndMint(ctx, tx, &it, r); err != nil {
			return err
		}
		d = Decision{IntentID: it.ID, Outcome: policy.Authorize, State: it.State, PolicyHash: it.PolicyHash, TokenJTI: it.TokenJTI}
		return nil
	})
	if err != nil {
		return Decision{}, err
	}
	s.publish("intent", d)
	return d, nil
}

// digest binds the stored agent trace into the hashed audit entry.
func digest(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func nullJSON(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}
