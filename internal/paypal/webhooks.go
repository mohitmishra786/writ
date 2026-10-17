package paypal

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
)

// SubscribedEvents is the explicit list Mandate subscribes to. PayPal advises
// against wildcards; every name was checked against GET
// /v1/notifications/webhooks-event-types on our sandbox app (2026-10-17).
var SubscribedEvents = []string{
	"CHECKOUT.ORDER.APPROVED",
	"PAYMENT.CAPTURE.COMPLETED",
	"PAYMENT.CAPTURE.PENDING",
	"PAYMENT.CAPTURE.DENIED",
	"PAYMENT.CAPTURE.DECLINED",
	"PAYMENT.CAPTURE.REFUNDED",
	"PAYMENT.CAPTURE.REVERSED",
	"PAYMENT.PAYOUTSBATCH.SUCCESS",
	"PAYMENT.PAYOUTSBATCH.DENIED",
	"PAYMENT.PAYOUTSBATCH.PROCESSING",
	"PAYMENT.PAYOUTS-ITEM.SUCCEEDED",
	"PAYMENT.PAYOUTS-ITEM.FAILED",
	"PAYMENT.PAYOUTS-ITEM.BLOCKED",
	"PAYMENT.PAYOUTS-ITEM.RETURNED",
	"PAYMENT.PAYOUTS-ITEM.UNCLAIMED",
	"PAYMENT.PAYOUTS-ITEM.REFUNDED",
	"PAYMENT.PAYOUTS-ITEM.CANCELED",
	"PAYMENT.PAYOUTS-ITEM.HELD",
	"VAULT.PAYMENT-TOKEN.CREATED",
	"VAULT.PAYMENT-TOKEN.DELETED",
}

type eventType struct {
	Name string `json:"name"`
}

type Webhook struct {
	ID         string      `json:"id,omitempty"`
	URL        string      `json:"url"`
	EventTypes []eventType `json:"event_types"`
}

func (w Webhook) Events() []string {
	out := make([]string, len(w.EventTypes))
	for i, e := range w.EventTypes {
		out[i] = e.Name
	}
	return out
}

func toEventTypes(names []string) []eventType {
	out := make([]eventType, len(names))
	for i, n := range names {
		out[i] = eventType{Name: n}
	}
	return out
}

func (c *Client) ListWebhooks(ctx context.Context) ([]Webhook, error) {
	var out struct {
		Webhooks []Webhook `json:"webhooks"`
	}
	err := c.do(ctx, request{method: http.MethodGet, path: "/v1/notifications/webhooks", retryable: true}, &out)
	return out.Webhooks, err
}

// CreateWebhook is not retried: a lost response could otherwise register the
// same URL twice. EnsureWebhook re-lists instead.
func (c *Client) CreateWebhook(ctx context.Context, listenerURL string, events []string) (Webhook, error) {
	var w Webhook
	err := c.do(ctx, request{
		method: http.MethodPost, path: "/v1/notifications/webhooks",
		body: Webhook{URL: listenerURL, EventTypes: toEventTypes(events)},
	}, &w)
	return w, err
}

// ReplaceWebhookEvents uses the JSON Patch body documented for
// PATCH /v1/notifications/webhooks/{id}.
func (c *Client) ReplaceWebhookEvents(ctx context.Context, id string, events []string) (Webhook, error) {
	var w Webhook
	patch := []map[string]any{{"op": "replace", "path": "/event_types", "value": toEventTypes(events)}}
	err := c.do(ctx, request{
		method: http.MethodPatch, path: "/v1/notifications/webhooks/" + url.PathEscape(id),
		body: patch, retryable: true,
	}, &w)
	return w, err
}

func (c *Client) DeleteWebhook(ctx context.Context, id string) error {
	return c.do(ctx, request{method: http.MethodDelete, path: "/v1/notifications/webhooks/" + url.PathEscape(id), retryable: true}, nil)
}

// EnsureWebhook makes exactly one webhook exist for listenerURL with the
// given events, creating or patching as needed. Safe to run repeatedly.
func (c *Client) EnsureWebhook(ctx context.Context, listenerURL string, events []string) (Webhook, error) {
	hooks, err := c.ListWebhooks(ctx)
	if err != nil {
		return Webhook{}, err
	}
	for _, h := range hooks {
		if h.URL == listenerURL {
			if sameSet(h.Events(), events) {
				return h, nil
			}
			return c.ReplaceWebhookEvents(ctx, h.ID, events)
		}
	}
	return c.CreateWebhook(ctx, listenerURL, events)
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]int, len(a))
	for _, x := range a {
		seen[x]++
	}
	for _, x := range b {
		if seen[x] == 0 {
			return false
		}
		seen[x]--
	}
	return true
}

var ErrWebhookUnverified = errors.New("paypal: webhook signature verification failed")

// VerifyWebhook calls POST /v1/notifications/verify-webhook-signature, the
// method PayPal's guidance says to always use. rawBody must be the exact
// bytes received: it is embedded verbatim as webhook_event, since PayPal
// RulesHub lists "body altered by middleware" as the top failure cause.
func (c *Client) VerifyWebhook(ctx context.Context, h http.Header, webhookID string, rawBody []byte) error {
	if !json.Valid(rawBody) {
		return ErrWebhookUnverified
	}
	body := struct {
		AuthAlgo         string          `json:"auth_algo"`
		CertURL          string          `json:"cert_url"`
		TransmissionID   string          `json:"transmission_id"`
		TransmissionSig  string          `json:"transmission_sig"`
		TransmissionTime string          `json:"transmission_time"`
		WebhookID        string          `json:"webhook_id"`
		WebhookEvent     json.RawMessage `json:"webhook_event"`
	}{
		AuthAlgo:         h.Get("Paypal-Auth-Algo"),
		CertURL:          h.Get("Paypal-Cert-Url"),
		TransmissionID:   h.Get("Paypal-Transmission-Id"),
		TransmissionSig:  h.Get("Paypal-Transmission-Sig"),
		TransmissionTime: h.Get("Paypal-Transmission-Time"),
		WebhookID:        webhookID,
		WebhookEvent:     json.RawMessage(rawBody),
	}
	if body.AuthAlgo == "" || body.CertURL == "" || body.TransmissionID == "" || body.TransmissionSig == "" || body.TransmissionTime == "" {
		return ErrWebhookUnverified
	}
	var out struct {
		VerificationStatus string `json:"verification_status"`
	}
	if err := c.do(ctx, request{
		method: http.MethodPost, path: "/v1/notifications/verify-webhook-signature",
		body: body, retryable: true,
	}, &out); err != nil {
		return err
	}
	if out.VerificationStatus != "SUCCESS" {
		return ErrWebhookUnverified
	}
	return nil
}

// Event is the common webhook envelope.
type Event struct {
	ID           string          `json:"id"`
	EventType    string          `json:"event_type"`
	ResourceType string          `json:"resource_type"`
	CreateTime   string          `json:"create_time"`
	Resource     json.RawMessage `json:"resource"`
}
