package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mohitmishra786/paypal-ai-hackathon/internal/kernel/audit"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/kernel/idem"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/kernel/policy"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/paypal"
)

type MandateView struct {
	ID            string         `json:"id"`
	PrincipalID   string         `json:"principal_id"`
	AgentID       string         `json:"agent_id"`
	Status        string         `json:"status"`
	PolicyHash    string         `json:"policy_hash"`
	Policy        policy.Mandate `json:"policy"`
	HasVault      bool           `json:"has_vault"`
	PausedReason  string         `json:"paused_reason,omitempty"`
	ReservedMinor int64          `json:"reserved_minor"`
	CapturedMinor int64          `json:"captured_minor"`
	LimitMinor    int64          `json:"limit_minor"`
	PeriodStart   time.Time      `json:"period_start"`
}

// CreateMandate stores a compiled policy as a DRAFT. It becomes ACTIVE only
// after the payer approves vaulting in PayPal (CompleteVaultSetup).
func (s *Service) CreateMandate(ctx context.Context, m policy.Mandate) (MandateView, error) {
	if m.ID == "" {
		m.ID = newID("mandate")
	}
	m.Status = policy.Active // the policy evaluates as active; the row status gates use
	p, err := policy.Compile(m)
	if err != nil {
		return MandateView{}, err
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return MandateView{}, err
	}
	err = s.store.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO mandates (id, principal_id, agent_id, status, policy, policy_hash) VALUES ($1,$2,$3,'DRAFT',$4,$5)`,
			m.ID, m.PrincipalID, m.AgentID, raw, p.Hash); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO envelopes (mandate_id, currency, limit_minor, period_seconds, period_start) VALUES ($1,$2,$3,$4,$5)`,
			m.ID, string(m.Envelope.Currency), m.Envelope.Minor, int64(m.Period/time.Second), s.now().UTC()); err != nil {
			return err
		}
		_, err := s.store.AppendAudit(ctx, tx, audit.Entry{Kind: audit.KindMandateCreated, MandateID: m.ID, AgentID: m.AgentID,
			PolicyHash: p.Hash, AmountMinor: m.Envelope.Minor, Currency: string(m.Envelope.Currency)})
		return err
	})
	if err != nil {
		return MandateView{}, err
	}
	s.policies.Store(p.Hash, p)
	return s.GetMandate(ctx, m.ID)
}

func (s *Service) GetMandate(ctx context.Context, id string) (MandateView, error) {
	var v MandateView
	var raw []byte
	var vault *string
	var paused *string
	err := s.store.Pool.QueryRow(ctx, `SELECT m.id, m.principal_id, m.agent_id, m.status, m.policy_hash, m.policy, m.vault_id, m.paused_reason,
		e.reserved_minor, e.captured_minor, e.limit_minor, e.period_start
		FROM mandates m JOIN envelopes e ON e.mandate_id = m.id WHERE m.id = $1`, id).
		Scan(&v.ID, &v.PrincipalID, &v.AgentID, &v.Status, &v.PolicyHash, &raw, &vault, &paused,
			&v.ReservedMinor, &v.CapturedMinor, &v.LimitMinor, &v.PeriodStart)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, err
	}
	v.HasVault = vault != nil && *vault != ""
	if paused != nil {
		v.PausedReason = *paused
	}
	return v, json.Unmarshal(raw, &v.Policy)
}

func (s *Service) ListMandates(ctx context.Context) ([]MandateView, error) {
	rows, err := s.store.Pool.Query(ctx, `SELECT id FROM mandates ORDER BY created_at DESC LIMIT 100`)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	out := make([]MandateView, 0, len(ids))
	for _, id := range ids {
		v, err := s.GetMandate(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// BeginVaultSetup creates a PayPal setup token (save PayPal without a
// purchase) and returns the setup token ID the v6 SDK session needs.
func (s *Service) BeginVaultSetup(ctx context.Context, mandateID, returnURL, cancelURL string) (paypal.SetupToken, error) {
	var attempt int
	if err := s.store.Pool.QueryRow(ctx, `SELECT count(*) FROM paypal_requests WHERE op = 'vault.setup_token' AND intent_id = $1`, mandateID).Scan(&attempt); err != nil {
		return paypal.SetupToken{}, err
	}
	key, err := idem.VaultSetupToken(mandateID, attempt+1)
	if err != nil {
		return paypal.SetupToken{}, err
	}
	if _, err := s.store.Pool.Exec(ctx, `INSERT INTO paypal_requests (idempotency_key, op, intent_id, state) VALUES ($1,'vault.setup_token',$2,'PENDING')`, key, mandateID); err != nil {
		return paypal.SetupToken{}, err
	}
	st, err := s.pp.CreateSetupToken(ctx, returnURL, cancelURL, paypal.CallOptions{IdempotencyKey: key})
	if err != nil {
		s.finishRequest(ctx, key, err, nil)
		return paypal.SetupToken{}, err
	}
	s.finishRequest(ctx, key, nil, st)
	if _, err := s.store.Pool.Exec(ctx, `UPDATE mandates SET setup_token_id = $2, updated_at = now() WHERE id = $1`, mandateID, st.ID); err != nil {
		return paypal.SetupToken{}, err
	}
	return st, nil
}

// CompleteVaultSetup exchanges the approved setup token for a vault ID,
// stores it server-side only (as PayPal's sample advises) and activates.
func (s *Service) CompleteVaultSetup(ctx context.Context, mandateID string) (MandateView, error) {
	var setup *string
	if err := s.store.Pool.QueryRow(ctx, `SELECT setup_token_id FROM mandates WHERE id = $1`, mandateID).Scan(&setup); err != nil {
		return MandateView{}, err
	}
	if setup == nil {
		return MandateView{}, fmt.Errorf("%w: no setup token", ErrState)
	}
	key, err := idem.VaultPaymentToken(*setup)
	if err != nil {
		return MandateView{}, err
	}
	pt, err := s.pp.CreatePaymentToken(ctx, *setup, paypal.CallOptions{IdempotencyKey: key})
	if err != nil {
		return MandateView{}, err
	}
	return s.ActivateWithVault(ctx, mandateID, pt.ID)
}

// ActivateWithVault records a vault ID and activates the mandate.
func (s *Service) ActivateWithVault(ctx context.Context, mandateID, vaultID string) (MandateView, error) {
	err := s.store.Tx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE mandates SET vault_id = $2, status = 'ACTIVE', paused_reason = NULL, updated_at = now() WHERE id = $1 AND status IN ('DRAFT','PAUSED')`, mandateID, vaultID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("%w: mandate not in DRAFT/PAUSED", ErrState)
		}
		_, err = s.store.AppendAudit(ctx, tx, audit.Entry{Kind: audit.KindMandateActivated, MandateID: mandateID})
		return err
	})
	if err != nil {
		return MandateView{}, err
	}
	s.publish("mandate", map[string]any{"id": mandateID, "status": "ACTIVE"})
	return s.GetMandate(ctx, mandateID)
}

// PauseByVault pauses every mandate using a vault ID that PayPal reports as
// deleted (VAULT.PAYMENT-TOKEN.DELETED): the payer revoked consent in PayPal.
func (s *Service) PauseByVault(ctx context.Context, vaultID, reason string) error {
	return s.store.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `UPDATE mandates SET status = 'PAUSED', paused_reason = $2, updated_at = now() WHERE vault_id = $1 AND status = 'ACTIVE' RETURNING id`, vaultID, reason)
		if err != nil {
			return err
		}
		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		rows.Close()
		for _, id := range ids {
			if _, err := s.store.AppendAudit(ctx, tx, audit.Entry{Kind: audit.KindMandatePaused, MandateID: id, Detail: map[string]any{"reason": reason}}); err != nil {
				return err
			}
			s.publish("mandate", map[string]any{"id": id, "status": "PAUSED", "reason": reason})
		}
		return nil
	})
}

func (s *Service) finishRequest(ctx context.Context, key string, callErr error, resp any) {
	state, debug := "DONE", ""
	if callErr != nil {
		state = "FAILED"
		if errors.Is(callErr, paypal.ErrAmbiguous) {
			state = "UNKNOWN"
		}
		var ae *paypal.APIError
		if errors.As(callErr, &ae) {
			debug = ae.DebugID
		}
		resp = map[string]string{"error": callErr.Error()}
	}
	raw, _ := json.Marshal(resp)
	_, _ = s.store.Pool.Exec(ctx, `UPDATE paypal_requests SET state = $2, debug_id = nullif($3,''), response = $4, updated_at = now() WHERE idempotency_key = $1`, key, state, debug, raw)
}
