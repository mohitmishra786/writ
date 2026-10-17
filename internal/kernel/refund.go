package kernel

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/oklog/ulid/v2"

	"github.com/mohitmishra786/paypal-ai-hackathon/internal/kernel/audit"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/kernel/idem"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/money"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/paypal"
)

// Refunder is the PayPal capability refunds need (kept separate so the
// kernel's core PayPal interface stays small).
type Refunder interface {
	RefundCapture(ctx context.Context, captureID string, amount *money.Money, invoiceID, note string, opt paypal.CallOptions) (paypal.Refund, error)
}

type RefundResult struct {
	RefundID       string      `json:"refund_id"`
	PayPalRefundID string      `json:"paypal_refund_id,omitempty"`
	CaptureID      string      `json:"capture_id"`
	IntentID       string      `json:"intent_id"`
	Amount         money.Money `json:"amount"`
	Status         string      `json:"status"`
	Remaining      money.Money `json:"remaining"`
}

var ErrRefundDenied = errors.New("kernel: refund denied")

// Refund returns money to the principal for a capture Mandate made. It is
// refused for unknown captures (an agent cannot refund arbitrary payments,
// the PR #91 blast-radius problem) and capped at captured minus refunded.
func (s *Service) Refund(ctx context.Context, rf Refunder, captureID string, amount *money.Money, note string) (RefundResult, error) {
	var res RefundResult
	var key string
	err := s.store.Tx(ctx, func(tx pgx.Tx) error {
		var intentID, cur, status string
		var captured int64
		err := tx.QueryRow(ctx, `SELECT intent_id, amount_minor, currency, status FROM captures WHERE capture_id = $1 FOR UPDATE`, captureID).
			Scan(&intentID, &captured, &cur, &status)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: capture %s was not made by Writ", ErrRefundDenied, captureID)
		}
		if err != nil {
			return err
		}
		if status != "COMPLETED" {
			return fmt.Errorf("%w: capture is %s", ErrRefundDenied, status)
		}
		var refunded int64
		var seq int
		if err := tx.QueryRow(ctx, `SELECT coalesce(sum(amount_minor) FILTER (WHERE status <> 'FAILED' AND status <> 'CANCELLED'), 0), count(*) FROM refunds WHERE capture_id = $1`, captureID).
			Scan(&refunded, &seq); err != nil {
			return err
		}
		remaining := captured - refunded
		amt := remaining
		if amount != nil {
			if amount.Currency != money.Currency(cur) {
				return fmt.Errorf("%w: currency mismatch", ErrRefundDenied)
			}
			amt = amount.Minor
		}
		if amt <= 0 || amt > remaining {
			return fmt.Errorf("%w: requested %s but only %s remains refundable", ErrRefundDenied,
				money.New(amt, money.Currency(cur)), money.New(remaining, money.Currency(cur)))
		}
		seq++
		if key, err = idem.Refund(captureID, int64(seq)); err != nil {
			return err
		}
		res = RefundResult{RefundID: "rf_" + ulid.Make().String(), CaptureID: captureID, IntentID: intentID,
			Amount: money.New(amt, money.Currency(cur)), Status: "PENDING", Remaining: money.New(remaining-amt, money.Currency(cur))}
		if _, err := tx.Exec(ctx, `INSERT INTO refunds (id, capture_id, intent_id, seq, amount_minor, currency, status) VALUES ($1,$2,$3,$4,$5,$6,'PENDING')`,
			res.RefundID, captureID, intentID, seq, amt, cur); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO paypal_requests (idempotency_key, op, intent_id, state) VALUES ($1,'capture.refund',$2,'PENDING')`, key, intentID); err != nil {
			return err
		}
		_, err = s.store.AppendAudit(ctx, tx, audit.Entry{Kind: audit.KindPayPalRequest, IntentID: intentID, AmountMinor: amt, Currency: cur,
			PayPal: map[string]any{"op": "POST /v2/payments/captures/{id}/refund", "capture_id": captureID, "request_key": key, "seq": seq}})
		return err
	})
	if err != nil {
		return RefundResult{}, err
	}
	amt := res.Amount
	r, callErr := rf.RefundCapture(ctx, captureID, &amt, "", note, paypal.CallOptions{IdempotencyKey: key})
	s.finishRequest(ctx, key, callErr, r)
	status := r.Status
	if callErr != nil {
		status = "FAILED"
		if errors.Is(callErr, paypal.ErrAmbiguous) {
			status = "UNKNOWN"
		}
	}
	res.Status, res.PayPalRefundID = status, r.ID
	err = s.store.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE refunds SET status = $2, paypal_refund_id = nullif($3,''), updated_at = now() WHERE id = $1`, res.RefundID, status, r.ID); err != nil {
			return err
		}
		if status == "COMPLETED" || status == "PENDING" {
			// Refunded money no longer counts against the period envelope.
			if _, err := tx.Exec(ctx, `UPDATE envelopes e SET captured_minor = greatest(e.captured_minor - $2, 0)
				FROM intents i WHERE i.id = $1 AND e.mandate_id = i.mandate_id`, res.IntentID, amt.Minor); err != nil {
				return err
			}
		}
		_, err := s.store.AppendAudit(ctx, tx, audit.Entry{Kind: audit.KindPayPalResponse, IntentID: res.IntentID, AmountMinor: amt.Minor,
			Currency: string(amt.Currency), PayPal: map[string]any{"refund_id": r.ID, "status": status}})
		return err
	})
	s.publish("refund", res)
	if callErr != nil {
		return res, callErr
	}
	return res, err
}
