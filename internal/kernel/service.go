// Package kernel is Mandate's financial core. It turns agent intents into
// policy decisions, reservations, single-use capabilities and idempotent
// PayPal calls, and records every step in the audit chain inside the same
// database transaction as the state change.
package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/oklog/ulid/v2"

	"github.com/mohitmishra786/paypal-ai-hackathon/internal/kernel/audit"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/kernel/fsm"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/kernel/policy"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/kernel/quote"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/kernel/token"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/money"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/paypal"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/store"
)

// PayPal is the subset of the PayPal client the kernel uses; tests fake it.
type PayPal interface {
	Env() paypal.Env
	CreateSetupToken(ctx context.Context, returnURL, cancelURL string, opt paypal.CallOptions) (paypal.SetupToken, error)
	CreatePaymentToken(ctx context.Context, setupTokenID string, opt paypal.CallOptions) (paypal.PaymentToken, error)
	CreateVaultedOrder(ctx context.Context, vaultID string, spec paypal.OrderSpec, opt paypal.CallOptions) (paypal.Order, error)
	CreateApprovalOrder(ctx context.Context, spec paypal.OrderSpec, returnURL, cancelURL string, opt paypal.CallOptions) (paypal.Order, error)
	GetOrder(ctx context.Context, id string) (paypal.Order, error)
	CaptureOrder(ctx context.Context, id string, opt paypal.CallOptions) (paypal.Order, error)
	GetCapture(ctx context.Context, captureID string) (paypal.Capture, error)
	CreatePayoutBatch(ctx context.Context, b paypal.PayoutBatch) (paypal.PayoutBatchResult, error)
	GetPayoutBatch(ctx context.Context, id string) (paypal.PayoutBatchResult, error)
}

// Notifier delivers out-of-band step-up prompts (ntfy push).
type Notifier interface {
	Notify(ctx context.Context, title, message, clickURL string) error
}

// Events receives committed changes for live streaming (SSE to the console).
type Events interface {
	Publish(kind string, payload any)
}

type Config struct {
	PublicBaseURL string        // e.g. https://mandate.onrender.com
	TokenTTL      time.Duration // capability lifetime
	ApprovalTTL   time.Duration // payer-action short link lifetime
}

type Service struct {
	store    *store.Store
	pp       PayPal
	minter   *token.Minter
	verifier *token.Verifier
	notifier Notifier
	events   Events
	cfg      Config
	now      func() time.Time

	policies sync.Map // policy hash -> *policy.Policy
}

func New(st *store.Store, pp PayPal, minter *token.Minter, verifier *token.Verifier, n Notifier, ev Events, cfg Config) *Service {
	if cfg.TokenTTL == 0 {
		cfg.TokenTTL = 5 * time.Minute
	}
	if cfg.ApprovalTTL == 0 {
		cfg.ApprovalTTL = 30 * time.Minute
	}
	return &Service{store: st, pp: pp, minter: minter, verifier: verifier, notifier: n, events: ev, cfg: cfg, now: time.Now}
}

var (
	ErrNotFound = errors.New("kernel: not found")
	ErrConflict = errors.New("kernel: concurrent state change")
	ErrState    = errors.New("kernel: operation not valid in current state")
)

func newID(prefix string) string { return prefix + "_" + ulid.Make().String() }

func (s *Service) publish(kind string, payload any) {
	if s.events != nil {
		s.events.Publish(kind, payload)
	}
}

// Intent is the ledger view of one intent.
type Intent struct {
	ID            string        `json:"id"`
	MandateID     string        `json:"mandate_id"`
	AgentID       string        `json:"agent_id"`
	PayeeRef      string        `json:"payee_ref"`
	Amount        money.Money   `json:"amount"`
	Category      string        `json:"category"`
	State         fsm.State     `json:"state"`
	Reasons       []string      `json:"reasons"`
	PolicyHash    string        `json:"policy_hash"`
	ReservedMinor int64         `json:"reserved_minor"`
	TokenJTI      string        `json:"token_jti,omitempty"`
	OrderID       string        `json:"order_id,omitempty"`
	CaptureID     string        `json:"capture_id,omitempty"`
	RecoveryN     int           `json:"recovery_n"`
	ApprovalURL   string        `json:"approval_url,omitempty"`
	LastError     string        `json:"last_error,omitempty"`
	Quote         *quote.Signed `json:"quote,omitempty"`
	QuoteHash     string        `json:"quote_hash,omitempty"`
	CreatedAt     time.Time     `json:"created_at"`
	UpdatedAt     time.Time     `json:"updated_at"`
}

const intentCols = `id, mandate_id, agent_id, payee_ref, amount_minor, currency, category, state, reasons, policy_hash,
	reserved_minor, coalesce(token_jti,''), coalesce(order_id,''), coalesce(capture_id,''), recovery_n,
	coalesce(approval_url,''), coalesce(last_error,''), quote, coalesce(quote_hash,''), created_at, updated_at`

func scanIntent(row pgx.Row) (Intent, error) {
	var it Intent
	var minor int64
	var cur, state string
	var q []byte
	err := row.Scan(&it.ID, &it.MandateID, &it.AgentID, &it.PayeeRef, &minor, &cur, &it.Category, &state, &it.Reasons,
		&it.PolicyHash, &it.ReservedMinor, &it.TokenJTI, &it.OrderID, &it.CaptureID, &it.RecoveryN, &it.ApprovalURL,
		&it.LastError, &q, &it.QuoteHash, &it.CreatedAt, &it.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return it, ErrNotFound
	}
	if err != nil {
		return it, err
	}
	it.Amount = money.New(minor, money.Currency(cur))
	it.State = fsm.State(state)
	if len(q) > 0 {
		it.Quote = &quote.Signed{}
		if err := json.Unmarshal(q, it.Quote); err != nil {
			return it, err
		}
	}
	return it, nil
}

func (s *Service) loadIntent(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, where string, arg any, forUpdate bool) (Intent, error) {
	sql := `SELECT ` + intentCols + ` FROM intents WHERE ` + where + ` = $1`
	if forUpdate {
		sql += ` FOR UPDATE`
	}
	return scanIntent(q.QueryRow(ctx, sql, arg))
}

func (s *Service) GetIntent(ctx context.Context, id string) (Intent, error) {
	return s.loadIntent(ctx, s.store.Pool, "id", id, false)
}

func (s *Service) ListIntents(ctx context.Context, mandateID string, limit int) ([]Intent, error) {
	rows, err := s.store.Pool.Query(ctx, `SELECT `+intentCols+` FROM intents WHERE ($1 = '' OR mandate_id = $1) ORDER BY created_at DESC LIMIT $2`, mandateID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Intent
	for rows.Next() {
		it, err := scanIntent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// transition moves an intent along a legal edge (checked in Go and again by
// the database trigger), optimistically guarded on the current state, and
// appends the audit entry in the same transaction.
func (s *Service) transition(ctx context.Context, tx pgx.Tx, it *Intent, to fsm.State, detail map[string]any) error {
	if err := fsm.Check(it.State, to); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE intents SET state = $2 WHERE id = $1 AND state = $3`, it.ID, string(to), string(it.State))
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrConflict
	}
	if _, err := s.store.AppendAudit(ctx, tx, audit.Entry{
		Kind: audit.KindTransition, MandateID: it.MandateID, AgentID: it.AgentID, IntentID: it.ID,
		StateFrom: string(it.State), StateTo: string(to), AmountMinor: it.Amount.Minor,
		Currency: string(it.Amount.Currency), PayeeRef: it.PayeeRef, Detail: detail,
	}); err != nil {
		return err
	}
	it.State = to
	return nil
}

// releaseReservation returns held funds to the envelope (deny paths after
// minting, expiry, definitive failure).
func releaseReservation(ctx context.Context, tx pgx.Tx, it *Intent) error {
	if it.ReservedMinor == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `UPDATE envelopes SET reserved_minor = reserved_minor - $2 WHERE mandate_id = $1`, it.MandateID, it.ReservedMinor); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE intents SET reserved_minor = 0 WHERE id = $1`, it.ID); err != nil {
		return err
	}
	it.ReservedMinor = 0
	return nil
}

// convertReservation moves a reservation into captured spend.
func convertReservation(ctx context.Context, tx pgx.Tx, it *Intent, captured int64) error {
	if _, err := tx.Exec(ctx, `UPDATE envelopes SET reserved_minor = reserved_minor - $2, captured_minor = captured_minor + $3 WHERE mandate_id = $1`,
		it.MandateID, it.ReservedMinor, captured); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE intents SET reserved_minor = 0 WHERE id = $1`, it.ID); err != nil {
		return err
	}
	it.ReservedMinor = 0
	return nil
}

func (s *Service) policyFor(m policy.Mandate, hash string) (*policy.Policy, error) {
	if p, ok := s.policies.Load(hash); ok {
		return p.(*policy.Policy), nil
	}
	p, err := policy.Compile(m)
	if err != nil {
		return nil, err
	}
	if p.Hash != hash {
		return nil, fmt.Errorf("kernel: stored policy hash mismatch for mandate %s", m.ID)
	}
	s.policies.Store(hash, p)
	return p, nil
}
