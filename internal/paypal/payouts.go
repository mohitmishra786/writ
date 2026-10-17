package paypal

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"github.com/mohitmishra786/paypal-ai-hackathon/internal/money"
)

type PayoutItem struct {
	ReceiverEmail string
	Amount        money.Money
	SenderItemID  string // max 63 chars (Payouts v1 schema)
	Note          string
}

type PayoutBatch struct {
	SenderBatchID string // PayPal rejects reuse within 30 days
	EmailSubject  string
	Items         []PayoutItem
}

type PayoutBatchResult struct {
	BatchHeader struct {
		PayoutBatchID string `json:"payout_batch_id"`
		BatchStatus   string `json:"batch_status"`
		SenderBatch   struct {
			SenderBatchID string `json:"sender_batch_id"`
		} `json:"sender_batch_header"`
		Fees *Amount `json:"fees"`
	} `json:"batch_header"`
	Items []struct {
		PayoutItemID      string  `json:"payout_item_id"`
		TransactionStatus string  `json:"transaction_status"`
		PayoutItemFee     *Amount `json:"payout_item_fee"`
		PayoutItem        struct {
			Receiver     string `json:"receiver"`
			Amount       Amount `json:"amount"`
			SenderItemID string `json:"sender_item_id"`
		} `json:"payout_item"`
	} `json:"items"`
}

// Batch statuses from the Payouts v1 schema / live sandbox responses.
const (
	PayoutPending    = "PENDING"
	PayoutProcessing = "PROCESSING"
	PayoutSuccess    = "SUCCESS"
	PayoutDenied     = "DENIED"
)

// CreatePayoutBatch is retryable because PayPal itself dedupes on
// sender_batch_id (documented; verified live: reuse returns
// USER_BUSINESS_ERROR). The Payouts schema documents no PayPal-Request-Id.
func (c *Client) CreatePayoutBatch(ctx context.Context, b PayoutBatch) (PayoutBatchResult, error) {
	if b.SenderBatchID == "" || len(b.Items) == 0 {
		return PayoutBatchResult{}, errors.New("paypal: payout batch needs sender_batch_id and items")
	}
	items := make([]map[string]any, 0, len(b.Items))
	for _, it := range b.Items {
		if it.SenderItemID == "" || len(it.SenderItemID) > 63 {
			return PayoutBatchResult{}, errors.New("paypal: sender_item_id must be 1..63 chars")
		}
		item := map[string]any{
			"recipient_type": "EMAIL",
			"receiver":       it.ReceiverEmail,
			"amount":         amountOf(it.Amount),
			"sender_item_id": it.SenderItemID,
		}
		if it.Note != "" {
			item["note"] = it.Note
		}
		items = append(items, item)
	}
	hdr := map[string]any{"sender_batch_id": b.SenderBatchID}
	if b.EmailSubject != "" {
		hdr["email_subject"] = b.EmailSubject
	}
	var out PayoutBatchResult
	err := c.do(ctx, request{
		method: http.MethodPost, path: "/v1/payments/payouts",
		body:      map[string]any{"sender_batch_header": hdr, "items": items},
		retryable: true,
	}, &out)
	return out, err
}

func (c *Client) GetPayoutBatch(ctx context.Context, payoutBatchID string) (PayoutBatchResult, error) {
	var out PayoutBatchResult
	err := c.do(ctx, request{method: http.MethodGet, path: "/v1/payments/payouts/" + url.PathEscape(payoutBatchID), retryable: true}, &out)
	return out, err
}
