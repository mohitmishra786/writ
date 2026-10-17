package kernel

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/oklog/ulid/v2"

	"github.com/mohitmishra786/paypal-ai-hackathon/internal/kernel/audit"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/kernel/fsm"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/kernel/idem"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/kernel/token"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/money"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/paypal"
)

// ExecOptions are per-execution controls.
type ExecOptions struct {
	// SimulateDecline sends PayPal's negative-testing header so the vaulted
	// charge returns INSTRUMENT_DECLINED (sandbox only; the client refuses
	// it on live). Used to demonstrate the recovery loop deterministically.
	SimulateDecline bool
}

// Execute consumes the intent's capability and performs the vaulted charge.
func (s *Service) Execute(ctx context.Context, intentID string, opt ExecOptions) (Intent, error) {
	var it Intent
	var vaultID, receiver string
	var key string
	err := s.store.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		if it, err = s.loadIntent(ctx, tx, "id", intentID, true); err != nil {
			return err
		}
		if it.State != fsm.Minted {
			return fmt.Errorf("%w: intent is %s", ErrState, it.State)
		}
		var raw string
		if err := tx.QueryRow(ctx, `UPDATE tokens SET consumed_at = now() WHERE jti = $1 AND consumed_at IS NULL RETURNING raw`, it.TokenJTI).Scan(&raw); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("%w: capability already consumed", ErrState)
			}
			return err
		}
		c, err := s.verifier.Verify(raw)
		if err != nil {
			// Expired or invalid: release funds and close the intent.
			if err2 := releaseReservation(ctx, tx, &it); err2 != nil {
				return err2
			}
			return s.transition(ctx, tx, &it, fsm.Expired, map[string]any{"reason": err.Error()})
		}
		// I2: the charge may never exceed what the capability authorizes.
		if c.IntentID != it.ID || c.PayeeRef != it.PayeeRef || c.Op != token.OpVaultCharge ||
			c.Ceiling.Currency != it.Amount.Currency || it.Amount.Minor > c.Ceiling.Minor {
			return fmt.Errorf("kernel: capability does not cover intent %s", it.ID)
		}
		if err := tx.QueryRow(ctx, `SELECT coalesce(vault_id,'') FROM mandates WHERE id = $1`, it.MandateID).Scan(&vaultID); err != nil {
			return err
		}
		if receiver, err = s.receiverFor(ctx, tx, it); err != nil {
			return err
		}
		if key, err = idem.PrimaryOrder(it.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO paypal_requests (idempotency_key, op, intent_id, state) VALUES ($1,'order.primary',$2,'PENDING')
			ON CONFLICT (idempotency_key) DO NOTHING`, key, it.ID); err != nil {
			return err
		}
		if _, err := s.store.AppendAudit(ctx, tx, audit.Entry{Kind: audit.KindPayPalRequest, MandateID: it.MandateID, AgentID: it.AgentID,
			IntentID: it.ID, TokenJTI: c.JTI, AmountMinor: it.Amount.Minor, Currency: string(it.Amount.Currency), PayeeRef: it.PayeeRef,
			PayPal: map[string]any{"op": "POST /v2/checkout/orders", "request_key": key, "vault": true, "simulate_decline": opt.SimulateDecline}}); err != nil {
			return err
		}
		return s.transition(ctx, tx, &it, fsm.Submitted, map[string]any{"request_key": key})
	})
	if err != nil || it.State != fsm.Submitted {
		return it, err
	}
	s.publish("intent", it)

	spec := s.orderSpec(it, it.ID)
	callOpt := paypal.CallOptions{IdempotencyKey: key}
	if opt.SimulateDecline {
		callOpt.MockApplicationCode = paypal.IssueInstrumentDeclined
	}
	order, callErr := s.pp.CreateVaultedOrder(ctx, vaultID, spec, callOpt)
	s.finishRequest(ctx, key, callErr, order)
	it, err = s.applyChargeResult(ctx, it.ID, receiver, order, callErr)
	if err != nil {
		return it, err
	}
	if it.State == fsm.Declined {
		return s.StartRecovery(ctx, it.ID)
	}
	return it, nil
}

func (s *Service) orderSpec(it Intent, invoiceID string) paypal.OrderSpec {
	spec := paypal.OrderSpec{InvoiceID: invoiceID, CustomID: it.MandateID, Total: it.Amount,
		Description: "Mandate purchase from " + it.PayeeRef}
	if it.Quote != nil {
		q := it.Quote.Quote
		for _, l := range q.Lines {
			if l.Qty > 0 {
				spec.Items = append(spec.Items, paypal.Item{Name: l.Title, SKU: l.SKU, Quantity: l.Qty, Unit: l.UnitPrice})
			}
		}
		spec.Shipping = q.Shipping
	}
	return spec
}

// receiverFor resolves the payee's PayPal email from the mandate allowlist.
func (s *Service) receiverFor(ctx context.Context, tx pgx.Tx, it Intent) (string, error) {
	var raw []byte
	if err := tx.QueryRow(ctx, `SELECT policy FROM mandates WHERE id = $1`, it.MandateID).Scan(&raw); err != nil {
		return "", err
	}
	var m struct {
		Payees []struct {
			Ref           string `json:"ref"`
			ReceiverEmail string `json:"receiver_email"`
		} `json:"payees"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return "", err
	}
	for _, p := range m.Payees {
		if p.Ref == it.PayeeRef {
			return p.ReceiverEmail, nil
		}
	}
	return "", fmt.Errorf("kernel: payee %s not in mandate", it.PayeeRef)
}

// applyChargeResult maps a PayPal outcome onto the FSM in one transaction.
func (s *Service) applyChargeResult(ctx context.Context, intentID, receiver string, order paypal.Order, callErr error) (Intent, error) {
	var it Intent
	err := s.store.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		if it, err = s.loadIntent(ctx, tx, "id", intentID, true); err != nil {
			return err
		}
		detail := map[string]any{}
		var ae *paypal.APIError
		if errors.As(callErr, &ae) {
			detail["debug_id"] = ae.DebugID
			detail["error"] = ae.Error()
		}
		if _, err := s.store.AppendAudit(ctx, tx, audit.Entry{Kind: audit.KindPayPalResponse, MandateID: it.MandateID, AgentID: it.AgentID,
			IntentID: it.ID, PayPal: map[string]any{"order_id": order.ID, "status": order.Status}, Detail: detail}); err != nil {
			return err
		}
		switch {
		case callErr == nil:
			return s.recordOrder(ctx, tx, &it, receiver, order)
		case errors.Is(callErr, paypal.ErrAmbiguous):
			_, _ = tx.Exec(ctx, `UPDATE intents SET last_error = $2 WHERE id = $1`, it.ID, callErr.Error())
			return s.transition(ctx, tx, &it, fsm.Unknown, detail)
		case paypal.IsIssue(callErr, paypal.IssueInstrumentDeclined):
			_, _ = tx.Exec(ctx, `UPDATE intents SET last_error = $2 WHERE id = $1`, it.ID, callErr.Error())
			return s.transition(ctx, tx, &it, fsm.Declined, detail)
		default:
			_, _ = tx.Exec(ctx, `UPDATE intents SET last_error = $2 WHERE id = $1`, it.ID, callErr.Error())
			if err := releaseReservation(ctx, tx, &it); err != nil {
				return err
			}
			return s.transition(ctx, tx, &it, fsm.Failed, detail)
		}
	})
	if err == nil {
		s.publish("intent", it)
	}
	return it, err
}

// recordOrder handles a 2xx order response: completed capture, pending
// capture, or an order still needing capture.
func (s *Service) recordOrder(ctx context.Context, tx pgx.Tx, it *Intent, receiver string, order paypal.Order) error {
	if _, err := tx.Exec(ctx, `UPDATE intents SET order_id = $2 WHERE id = $1`, it.ID, order.ID); err != nil {
		return err
	}
	it.OrderID = order.ID
	caps := order.Captures()
	if len(caps) == 0 {
		return fmt.Errorf("kernel: order %s returned %s without a capture", order.ID, order.Status)
	}
	c := caps[0]
	amt, err := c.Amount.Money()
	if err != nil {
		return err
	}
	if amt.Minor > it.Amount.Minor || amt.Currency != it.Amount.Currency {
		return fmt.Errorf("kernel: captured %s exceeds authorized %s", amt, it.Amount)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO captures (capture_id, intent_id, order_id, amount_minor, currency, status) VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (capture_id) DO UPDATE SET status = EXCLUDED.status`, c.ID, it.ID, order.ID, amt.Minor, string(amt.Currency), c.Status); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE intents SET capture_id = $2 WHERE id = $1`, it.ID, c.ID); err != nil {
		return err
	}
	it.CaptureID = c.ID
	switch c.Status {
	case "COMPLETED":
		return s.markCaptured(ctx, tx, it, receiver, amt)
	case "PENDING":
		return s.transition(ctx, tx, it, fsm.CapturePending, map[string]any{"capture_id": c.ID})
	default:
		if err := releaseReservation(ctx, tx, it); err != nil {
			return err
		}
		return s.transition(ctx, tx, it, fsm.Failed, map[string]any{"capture_status": c.Status})
	}
}

// markCaptured converts the reservation and opens a settlement obligation
// to the supplier, to be paid out in a netted batch.
func (s *Service) markCaptured(ctx context.Context, tx pgx.Tx, it *Intent, receiver string, amt money.Money) error {
	if err := convertReservation(ctx, tx, it, amt.Minor); err != nil {
		return err
	}
	if err := s.transition(ctx, tx, it, fsm.Captured, map[string]any{"capture_id": it.CaptureID, "order_id": it.OrderID}); err != nil {
		return err
	}
	if receiver == "" {
		var err error
		if receiver, err = s.receiverFor(ctx, tx, *it); err != nil {
			return err
		}
	}
	_, err := tx.Exec(ctx, `INSERT INTO obligations (id, mandate_id, intent_id, payee_ref, receiver_email, amount_minor, currency, state)
		VALUES ($1,$2,$3,$4,$5,$6,$7,'OPEN')`, "obl_"+ulid.Make().String(), it.MandateID, it.ID, it.PayeeRef, receiver, amt.Minor, string(amt.Currency))
	return err
}

// StartRecovery handles INSTRUMENT_DECLINED the way PayPal recommends: no
// automatic retry of the declined instrument; the payer chooses another
// funding source on PayPal's own approval page (payer-action link), which we
// deliver out of band via push notification and a console QR code.
func (s *Service) StartRecovery(ctx context.Context, intentID string) (Intent, error) {
	it, err := s.GetIntent(ctx, intentID)
	if err != nil {
		return it, err
	}
	if it.State != fsm.Declined {
		return it, fmt.Errorf("%w: intent is %s", ErrState, it.State)
	}
	n := it.RecoveryN + 1
	key, err := idem.RecoveryOrder(it.ID, n)
	if err != nil {
		return it, err
	}
	if _, err := s.store.Pool.Exec(ctx, `INSERT INTO paypal_requests (idempotency_key, op, intent_id, state) VALUES ($1,'order.recovery',$2,'PENDING') ON CONFLICT DO NOTHING`, key, it.ID); err != nil {
		return it, err
	}
	base := strings.TrimRight(s.cfg.PublicBaseURL, "/")
	order, callErr := s.pp.CreateApprovalOrder(ctx, s.orderSpec(it, fmt.Sprintf("%s-r%d", it.ID, n)),
		base+"/v1/paypal/return?intent="+it.ID, base+"/v1/paypal/cancel?intent="+it.ID, paypal.CallOptions{IdempotencyKey: key})
	s.finishRequest(ctx, key, callErr, order)
	if callErr != nil {
		return it, callErr
	}
	link := order.Link("payer-action")
	if link == "" {
		return it, fmt.Errorf("kernel: recovery order %s has no payer-action link", order.ID)
	}
	code := shortCode()
	short := base + "/a/" + code
	err = s.store.Tx(ctx, func(tx pgx.Tx) error {
		if it, err = s.loadIntent(ctx, tx, "id", intentID, true); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO short_links (code, url, intent_id, expires_at) VALUES ($1,$2,$3,$4)`, code, link, it.ID, s.now().Add(s.cfg.ApprovalTTL)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE intents SET order_id = $2, recovery_n = $3, approval_url = $4 WHERE id = $1`, it.ID, order.ID, n, short); err != nil {
			return err
		}
		it.OrderID, it.RecoveryN, it.ApprovalURL = order.ID, n, short
		return s.transition(ctx, tx, &it, fsm.Recovery, map[string]any{"order_id": order.ID, "attempt": n})
	})
	if err != nil {
		return it, err
	}
	if s.notifier != nil {
		msg := fmt.Sprintf("%s to %s was declined. Tap to approve with another funding source in PayPal.", it.Amount, it.PayeeRef)
		_ = s.notifier.Notify(ctx, "Mandate: approval needed", msg, short)
	}
	s.publish("intent", it)
	return it, nil
}

func shortCode() string {
	b := make([]byte, 10)
	_, _ = rand.Read(b)
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b))
}

// ResolveShortLink returns the PayPal URL behind a short code if unexpired.
func (s *Service) ResolveShortLink(ctx context.Context, code string) (string, error) {
	var url string
	err := s.store.Pool.QueryRow(ctx, `SELECT url FROM short_links WHERE code = $1 AND expires_at > now()`, code).Scan(&url)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return url, err
}

// OnOrderApproved is called from CHECKOUT.ORDER.APPROVED or the buyer's
// return URL; whichever arrives first wins, the other is a no-op because the
// capture key is derived from the order ID.
func (s *Service) OnOrderApproved(ctx context.Context, orderID string) (Intent, error) {
	// Never trust the caller (webhook body or return URL): ask PayPal.
	o, err := s.pp.GetOrder(ctx, orderID)
	if err != nil {
		return Intent{}, err
	}
	if o.Status != paypal.OrderApproved && o.Status != paypal.OrderCompleted {
		return s.loadIntent(ctx, s.store.Pool, "order_id", orderID, false)
	}
	var it Intent
	err = s.store.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		if it, err = s.loadIntent(ctx, tx, "order_id", orderID, true); err != nil {
			return err
		}
		if it.State != fsm.Recovery {
			return nil
		}
		return s.transition(ctx, tx, &it, fsm.Approved, map[string]any{"order_id": orderID})
	})
	if err != nil || it.State != fsm.Approved {
		return it, err
	}
	key, err := idem.Capture(orderID)
	if err != nil {
		return it, err
	}
	if _, err := s.store.Pool.Exec(ctx, `INSERT INTO paypal_requests (idempotency_key, op, intent_id, state) VALUES ($1,'order.capture',$2,'PENDING') ON CONFLICT DO NOTHING`, key, it.ID); err != nil {
		return it, err
	}
	order, callErr := s.pp.CaptureOrder(ctx, orderID, paypal.CallOptions{IdempotencyKey: key})
	s.finishRequest(ctx, key, callErr, order)
	if callErr != nil && paypal.IsIssue(callErr, paypal.IssueOrderAlreadyCaptured) {
		order, callErr = s.pp.GetOrder(ctx, orderID)
	}
	return s.applyCaptureResult(ctx, it.ID, order, callErr)
}

func (s *Service) applyCaptureResult(ctx context.Context, intentID string, order paypal.Order, callErr error) (Intent, error) {
	var it Intent
	err := s.store.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		if it, err = s.loadIntent(ctx, tx, "id", intentID, true); err != nil {
			return err
		}
		if callErr != nil {
			if errors.Is(callErr, paypal.ErrAmbiguous) {
				return nil // stays APPROVED; the sweeper re-captures with the same key
			}
			_, _ = tx.Exec(ctx, `UPDATE intents SET last_error = $2 WHERE id = $1`, it.ID, callErr.Error())
			if err := releaseReservation(ctx, tx, &it); err != nil {
				return err
			}
			return s.transition(ctx, tx, &it, fsm.Failed, map[string]any{"error": callErr.Error()})
		}
		return s.recordOrder(ctx, tx, &it, "", order)
	})
	if err == nil {
		s.publish("intent", it)
	}
	return it, err
}

// ResolveUnknown settles an ambiguous charge by replaying the identical
// request with the same idempotency key: PayPal returns the original result
// if the first attempt landed (verified live, ledger V21).
func (s *Service) ResolveUnknown(ctx context.Context, intentID string) (Intent, error) {
	it, err := s.GetIntent(ctx, intentID)
	if err != nil || it.State != fsm.Unknown {
		return it, err
	}
	var vaultID, receiver string
	err = s.store.Tx(ctx, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT coalesce(vault_id,'') FROM mandates WHERE id = $1`, it.MandateID).Scan(&vaultID); err != nil {
			return err
		}
		receiver, err = s.receiverFor(ctx, tx, it)
		return err
	})
	if err != nil {
		return it, err
	}
	key, _ := idem.PrimaryOrder(it.ID)
	order, callErr := s.pp.CreateVaultedOrder(ctx, vaultID, s.orderSpec(it, it.ID), paypal.CallOptions{IdempotencyKey: key})
	s.finishRequest(ctx, key, callErr, order)
	if errors.Is(callErr, paypal.ErrAmbiguous) {
		return it, callErr
	}
	return s.applyChargeResult(ctx, it.ID, receiver, order, callErr)
}

// ExpireStale closes intents whose capability or approval window lapsed,
// releasing their reservations.
func (s *Service) ExpireStale(ctx context.Context) (int, error) {
	rows, err := s.store.Pool.Query(ctx, `SELECT i.id FROM intents i LEFT JOIN tokens t ON t.jti = i.token_jti
		WHERE (i.state = 'MINTED' AND t.expires_at < now() AND t.consumed_at IS NULL)
		   OR (i.state = 'RECOVERY' AND i.updated_at < $1)
		   OR (i.state = 'AWAITING_COSIGN' AND i.updated_at < $1)`, s.now().Add(-s.cfg.ApprovalTTL))
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	n := 0
	for _, id := range ids {
		err := s.store.Tx(ctx, func(tx pgx.Tx) error {
			it, err := s.loadIntent(ctx, tx, "id", id, true)
			if err != nil {
				return err
			}
			if it.State.Terminal() {
				return nil
			}
			if err := releaseReservation(ctx, tx, &it); err != nil {
				return err
			}
			if err := s.transition(ctx, tx, &it, fsm.Expired, map[string]any{"reason": "window elapsed"}); err != nil {
				return err
			}
			n++
			return nil
		})
		if err != nil && !errors.Is(err, ErrConflict) {
			return n, err
		}
	}
	return n, nil
}

// ResolvePendingCapture re-checks a PENDING capture with PayPal. Webhooks
// are hints; this is how CAPTURE_PENDING converges without them.
func (s *Service) ResolvePendingCapture(ctx context.Context, intentID string) (Intent, error) {
	it, err := s.GetIntent(ctx, intentID)
	if err != nil || it.State != fsm.CapturePending || it.CaptureID == "" {
		return it, err
	}
	cp, err := s.pp.GetCapture(ctx, it.CaptureID)
	if err != nil {
		return it, err
	}
	reason := ""
	if cp.StatusDetails != nil {
		reason = cp.StatusDetails.Reason
	}
	if cp.Status == "PENDING" {
		return it, nil
	}
	err = s.store.Tx(ctx, func(tx pgx.Tx) error {
		if it, err = s.loadIntent(ctx, tx, "id", intentID, true); err != nil {
			return err
		}
		if it.State != fsm.CapturePending {
			return nil
		}
		if _, err := tx.Exec(ctx, `UPDATE captures SET status = $2 WHERE capture_id = $1`, cp.ID, cp.Status); err != nil {
			return err
		}
		if _, err := s.store.AppendAudit(ctx, tx, audit.Entry{Kind: audit.KindPayPalResponse, MandateID: it.MandateID, IntentID: it.ID,
			PayPal: map[string]any{"op": "GET /v2/payments/captures/{id}", "capture_id": cp.ID, "status": cp.Status, "reason": reason}}); err != nil {
			return err
		}
		if cp.Status == "COMPLETED" {
			amt, err := cp.Amount.Money()
			if err != nil {
				return err
			}
			return s.markCaptured(ctx, tx, &it, "", amt)
		}
		if err := releaseReservation(ctx, tx, &it); err != nil {
			return err
		}
		return s.transition(ctx, tx, &it, fsm.Failed, map[string]any{"capture_status": cp.Status, "reason": reason})
	})
	if err == nil {
		s.publish("intent", it)
	}
	return it, err
}
