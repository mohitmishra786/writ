package kernel

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/oklog/ulid/v2"

	"github.com/mohitmishra786/paypal-ai-hackathon/internal/kernel/audit"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/kernel/fsm"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/kernel/idem"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/money"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/paypal"
)

type BatchResult struct {
	BatchID       string `json:"batch_id"`
	SenderBatchID string `json:"sender_batch_id"`
	PayoutBatchID string `json:"payout_batch_id"`
	Status        string `json:"status"`
	Items         int    `json:"items"`
	Obligations   int    `json:"obligations"`
	TotalMinor    int64  `json:"total_minor"`
}

type netItem struct {
	receiver string
	payee    string
	amount   int64
	ids      []string
	intents  []string
}

// SettleMandate nets every OPEN obligation of a mandate into one payout item
// per receiver and sends a single Payouts batch. Netting exists because each
// payout item carries a fee ($0.25 measured in sandbox, ledger V20).
func (s *Service) SettleMandate(ctx context.Context, mandateID string) (BatchResult, error) {
	batchID := "bat_" + ulid.Make().String()
	senderBatchID, err := idem.SenderBatchID(batchID)
	if err != nil {
		return BatchResult{}, err
	}
	var items []netItem
	var currency string
	var total int64
	err = s.store.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id, intent_id, payee_ref, receiver_email, amount_minor, currency FROM obligations
			WHERE mandate_id = $1 AND state = 'OPEN' ORDER BY created_at FOR UPDATE`, mandateID)
		if err != nil {
			return err
		}
		byReceiver := map[string]*netItem{}
		for rows.Next() {
			var id, intentID, payee, receiver, cur string
			var amt int64
			if err := rows.Scan(&id, &intentID, &payee, &receiver, &amt, &cur); err != nil {
				rows.Close()
				return err
			}
			if currency != "" && cur != currency {
				rows.Close()
				return errors.New("kernel: mixed-currency settlement not supported")
			}
			currency = cur
			n := byReceiver[receiver]
			if n == nil {
				n = &netItem{receiver: receiver, payee: payee}
				byReceiver[receiver] = n
			}
			n.amount += amt
			n.ids = append(n.ids, id)
			n.intents = append(n.intents, intentID)
			total += amt
		}
		rows.Close()
		if len(byReceiver) == 0 {
			return nil
		}
		for _, n := range byReceiver {
			items = append(items, *n)
		}
		sort.Slice(items, func(i, j int) bool { return items[i].receiver < items[j].receiver })

		// I8: money conservation across the batch.
		var sum int64
		for _, n := range items {
			sum += n.amount
		}
		if sum != total {
			return fmt.Errorf("kernel: batch not conserved (%d != %d)", sum, total)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO payout_batches (id, mandate_id, sender_batch_id, status, total_minor, currency) VALUES ($1,$2,$3,'CREATED',$4,$5)`,
			batchID, mandateID, senderBatchID, total, currency); err != nil {
			return err
		}
		for _, n := range items {
			if _, err := tx.Exec(ctx, `UPDATE obligations SET state = 'BATCHED', batch_id = $2, updated_at = now() WHERE id = ANY($1)`, n.ids, batchID); err != nil {
				return err
			}
			for _, intentID := range n.intents {
				it, err := s.loadIntent(ctx, tx, "id", intentID, true)
				if err != nil {
					return err
				}
				if it.State == fsm.Captured || it.State == fsm.SettleFailed {
					if err := s.transition(ctx, tx, &it, fsm.Settling, map[string]any{"batch_id": batchID}); err != nil {
						return err
					}
				}
			}
		}
		_, err = s.store.AppendAudit(ctx, tx, audit.Entry{Kind: audit.KindSettlement, MandateID: mandateID,
			AmountMinor: total, Currency: currency, PayPal: map[string]any{"sender_batch_id": senderBatchID},
			Detail: map[string]any{"batch_id": batchID, "items": len(items)}})
		return err
	})
	if err != nil || len(items) == 0 {
		return BatchResult{}, err
	}
	return s.sendBatch(ctx, batchID, senderBatchID, items, currency, total)
}

func (s *Service) sendBatch(ctx context.Context, batchID, senderBatchID string, items []netItem, currency string, total int64) (BatchResult, error) {
	req := paypal.PayoutBatch{SenderBatchID: senderBatchID, EmailSubject: "Payment from Mandate"}
	for i, n := range items {
		itemID, err := idem.SenderItemID(fmt.Sprintf("%s-%d", batchID[4:], i))
		if err != nil {
			return BatchResult{}, err
		}
		req.Items = append(req.Items, paypal.PayoutItem{ReceiverEmail: n.receiver, Amount: money.New(n.amount, money.Currency(currency)),
			SenderItemID: itemID, Note: fmt.Sprintf("Mandate settlement for %d purchase(s) from %s", len(n.ids), n.payee)})
	}
	res, callErr := s.pp.CreatePayoutBatch(ctx, req)
	status := res.BatchHeader.BatchStatus
	if callErr != nil {
		status = "ERROR"
		if errors.Is(callErr, paypal.ErrAmbiguous) {
			status = "UNKNOWN"
		}
	}
	if _, err := s.store.Pool.Exec(ctx, `UPDATE payout_batches SET payout_batch_id = nullif($2,''), status = $3, updated_at = now() WHERE id = $1`,
		batchID, res.BatchHeader.PayoutBatchID, status); err != nil {
		return BatchResult{}, err
	}
	out := BatchResult{BatchID: batchID, SenderBatchID: senderBatchID, PayoutBatchID: res.BatchHeader.PayoutBatchID,
		Status: status, Items: len(items), TotalMinor: total}
	for _, n := range items {
		out.Obligations += len(n.ids)
	}
	s.publish("settlement", out)
	return out, callErr
}

// SyncBatch polls a payout batch and applies item outcomes; it is the
// fallback when payout webhooks are delayed or missing.
func (s *Service) SyncBatch(ctx context.Context, batchID string) (BatchResult, error) {
	var payoutID string
	if err := s.store.Pool.QueryRow(ctx, `SELECT coalesce(payout_batch_id,'') FROM payout_batches WHERE id = $1`, batchID).Scan(&payoutID); err != nil {
		return BatchResult{}, err
	}
	if payoutID == "" {
		return BatchResult{}, fmt.Errorf("%w: batch %s has no PayPal id", ErrState, batchID)
	}
	res, err := s.pp.GetPayoutBatch(ctx, payoutID)
	if err != nil {
		return BatchResult{}, err
	}
	for _, item := range res.Items {
		if err := s.applyPayoutItem(ctx, batchID, item.PayoutItem.Receiver, item.PayoutItemID, item.TransactionStatus); err != nil {
			return BatchResult{}, err
		}
	}
	if _, err := s.store.Pool.Exec(ctx, `UPDATE payout_batches SET status = $2, updated_at = now() WHERE id = $1`, batchID, res.BatchHeader.BatchStatus); err != nil {
		return BatchResult{}, err
	}
	return BatchResult{BatchID: batchID, PayoutBatchID: payoutID, Status: res.BatchHeader.BatchStatus, Items: len(res.Items)}, nil
}

// applyPayoutItem maps a payout item status (webhook or poll) onto the
// obligations and intents it settles.
func (s *Service) applyPayoutItem(ctx context.Context, batchID, receiver, payoutItemID, status string) error {
	var target fsm.State
	var oblState string
	switch status {
	case "SUCCESS", "SUCCEEDED":
		target, oblState = fsm.Settled, "SETTLED"
	case "FAILED", "BLOCKED", "RETURNED", "UNCLAIMED", "REFUNDED", "CANCELED", "DENIED":
		target, oblState = fsm.SettleFailed, "FAILED"
	default:
		return nil // PENDING, HELD, ONHOLD: not terminal
	}
	return s.store.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `UPDATE obligations SET state = $3, payout_item_id = $4, updated_at = now()
			WHERE batch_id = $1 AND receiver_email = $2 AND state = 'BATCHED' RETURNING intent_id`, batchID, receiver, oblState, payoutItemID)
		if err != nil {
			return err
		}
		var intents []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			intents = append(intents, id)
		}
		rows.Close()
		for _, id := range intents {
			it, err := s.loadIntent(ctx, tx, "id", id, true)
			if err != nil {
				return err
			}
			if it.State != fsm.Settling {
				continue
			}
			if err := s.transition(ctx, tx, &it, target, map[string]any{"payout_item_id": payoutItemID, "status": status}); err != nil {
				return err
			}
			if target == fsm.SettleFailed {
				if _, err := tx.Exec(ctx, `UPDATE obligations SET state = 'OPEN', batch_id = NULL WHERE intent_id = $1 AND state = 'FAILED'`, it.ID); err != nil {
					return err
				}
			}
			s.publish("intent", it)
		}
		return nil
	})
}
