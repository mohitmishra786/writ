package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/mohitmishra786/paypal-ai-hackathon/internal/kernel/audit"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/kernel/fsm"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/paypal"
)

// ApplyWebhook processes a signature-verified PayPal event. Delivery is
// at-least-once and unordered, so events are deduplicated by ID and any
// transition that would move an intent backwards is recorded as stale.
func (s *Service) ApplyWebhook(ctx context.Context, raw []byte) (string, error) {
	var ev paypal.Event
	if err := json.Unmarshal(raw, &ev); err != nil {
		return "", err
	}
	if ev.ID == "" || ev.EventType == "" {
		return "", errors.New("kernel: webhook missing id or event_type")
	}
	tag, err := s.store.Pool.Exec(ctx, `INSERT INTO webhook_events (event_id, event_type, payload) VALUES ($1,$2,$3) ON CONFLICT (event_id) DO NOTHING`,
		ev.ID, ev.EventType, raw)
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() == 0 {
		return "duplicate", nil
	}
	outcome, err := s.dispatch(ctx, ev)
	if err != nil {
		outcome = "error: " + err.Error()
	}
	_, _ = s.store.Pool.Exec(ctx, `UPDATE webhook_events SET applied_at = now(), outcome = $2 WHERE event_id = $1`, ev.ID, outcome)
	s.publish("webhook", map[string]any{"id": ev.ID, "type": ev.EventType, "outcome": outcome})
	return outcome, err
}

func (s *Service) dispatch(ctx context.Context, ev paypal.Event) (string, error) {
	switch ev.EventType {
	case "CHECKOUT.ORDER.APPROVED":
		var r struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(ev.Resource, &r); err != nil {
			return "", err
		}
		it, err := s.OnOrderApproved(ctx, r.ID)
		if errors.Is(err, ErrNotFound) {
			return "ignored: unknown order", nil
		}
		return "intent " + string(it.State), err

	case "PAYMENT.CAPTURE.COMPLETED", "PAYMENT.CAPTURE.PENDING", "PAYMENT.CAPTURE.DENIED", "PAYMENT.CAPTURE.DECLINED":
		var c struct {
			ID                string `json:"id"`
			Status            string `json:"status"`
			SupplementaryData struct {
				RelatedIDs struct {
					OrderID string `json:"order_id"`
				} `json:"related_ids"`
			} `json:"supplementary_data"`
		}
		if err := json.Unmarshal(ev.Resource, &c); err != nil {
			return "", err
		}
		return s.applyCaptureEvent(ctx, ev, c.SupplementaryData.RelatedIDs.OrderID, c.ID)

	case "PAYMENT.PAYOUTS-ITEM.SUCCEEDED", "PAYMENT.PAYOUTS-ITEM.FAILED", "PAYMENT.PAYOUTS-ITEM.BLOCKED",
		"PAYMENT.PAYOUTS-ITEM.RETURNED", "PAYMENT.PAYOUTS-ITEM.UNCLAIMED", "PAYMENT.PAYOUTS-ITEM.REFUNDED",
		"PAYMENT.PAYOUTS-ITEM.CANCELED", "PAYMENT.PAYOUTS-ITEM.HELD":
		var r struct {
			PayoutItemID      string `json:"payout_item_id"`
			PayoutBatchID     string `json:"payout_batch_id"`
			TransactionStatus string `json:"transaction_status"`
			PayoutItem        struct {
				Receiver string `json:"receiver"`
			} `json:"payout_item"`
		}
		if err := json.Unmarshal(ev.Resource, &r); err != nil {
			return "", err
		}
		var batchID string
		err := s.store.Pool.QueryRow(ctx, `SELECT id FROM payout_batches WHERE payout_batch_id = $1`, r.PayoutBatchID).Scan(&batchID)
		if errors.Is(err, pgx.ErrNoRows) {
			return "ignored: unknown payout batch", nil
		}
		if err != nil {
			return "", err
		}
		return "payout item " + r.TransactionStatus, s.applyPayoutItem(ctx, batchID, r.PayoutItem.Receiver, r.PayoutItemID, r.TransactionStatus)

	case "PAYMENT.PAYOUTSBATCH.SUCCESS", "PAYMENT.PAYOUTSBATCH.DENIED", "PAYMENT.PAYOUTSBATCH.PROCESSING":
		var r struct {
			BatchHeader struct {
				PayoutBatchID string `json:"payout_batch_id"`
				BatchStatus   string `json:"batch_status"`
			} `json:"batch_header"`
		}
		if err := json.Unmarshal(ev.Resource, &r); err != nil {
			return "", err
		}
		_, err := s.store.Pool.Exec(ctx, `UPDATE payout_batches SET status = $2, updated_at = now() WHERE payout_batch_id = $1`,
			r.BatchHeader.PayoutBatchID, r.BatchHeader.BatchStatus)
		return "batch " + r.BatchHeader.BatchStatus, err

	case "VAULT.PAYMENT-TOKEN.DELETED":
		var r struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(ev.Resource, &r); err != nil {
			return "", err
		}
		return "mandates paused", s.PauseByVault(ctx, r.ID, "payer deleted the saved PayPal payment method")
	}
	return "ignored", nil
}

func (s *Service) applyCaptureEvent(ctx context.Context, ev paypal.Event, orderID, captureID string) (string, error) {
	var outcome string
	err := s.store.Tx(ctx, func(tx pgx.Tx) error {
		it, err := s.loadIntent(ctx, tx, "order_id", orderID, true)
		if errors.Is(err, ErrNotFound) && captureID != "" {
			it, err = s.loadIntent(ctx, tx, "capture_id", captureID, true)
		}
		if errors.Is(err, ErrNotFound) {
			outcome = "ignored: unknown capture"
			return nil
		}
		if err != nil {
			return err
		}
		var to fsm.State
		switch ev.EventType {
		case "PAYMENT.CAPTURE.COMPLETED":
			to = fsm.Captured
		case "PAYMENT.CAPTURE.PENDING":
			to = fsm.CapturePending
		default:
			to = fsm.Failed
		}
		if it.State == to || fsm.IsStale(it.State, to) || fsm.Check(it.State, to) != nil {
			outcome = fmt.Sprintf("stale: intent %s, event implies %s", it.State, to)
			_, err := s.store.AppendAudit(ctx, tx, audit.Entry{Kind: audit.KindStaleEvent, MandateID: it.MandateID, IntentID: it.ID,
				StateFrom: string(it.State), StateTo: string(to), PayPal: map[string]any{"event_id": ev.ID, "event_type": ev.EventType}})
			return err
		}
		if _, err := s.store.AppendAudit(ctx, tx, audit.Entry{Kind: audit.KindWebhookApplied, MandateID: it.MandateID, IntentID: it.ID,
			PayPal: map[string]any{"event_id": ev.ID, "event_type": ev.EventType, "capture_id": captureID}}); err != nil {
			return err
		}
		switch to {
		case fsm.Captured:
			if _, err := tx.Exec(ctx, `UPDATE captures SET status = 'COMPLETED' WHERE capture_id = $1`, captureID); err != nil {
				return err
			}
			err = s.markCaptured(ctx, tx, &it, "", it.Amount)
		case fsm.Failed:
			if err = releaseReservation(ctx, tx, &it); err == nil {
				err = s.transition(ctx, tx, &it, fsm.Failed, map[string]any{"event": ev.EventType})
			}
		default:
			err = s.transition(ctx, tx, &it, to, map[string]any{"event": ev.EventType})
		}
		outcome = "intent " + string(it.State)
		return err
	})
	return outcome, err
}
