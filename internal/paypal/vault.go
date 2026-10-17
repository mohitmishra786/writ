package paypal

import (
	"context"
	"net/http"
	"net/url"
)

// SetupToken is the first step of saving a PayPal wallet without a purchase.
type SetupToken struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Links  []Link `json:"links"`
}

func (s SetupToken) ApproveURL() string {
	for _, l := range s.Links {
		if l.Rel == "approve" {
			return l.Href
		}
	}
	return ""
}

// PaymentToken is the long-lived vault ID. PayPal's sample stores it
// server-side and never returns it to the browser; so do we.
type PaymentToken struct {
	ID       string `json:"id"`
	Customer struct {
		ID string `json:"id"`
	} `json:"customer"`
}

// CreateSetupToken mirrors PayPal's v6 sample (vaultRouteHandler.ts):
// usage_type MERCHANT, vault_instruction ON_PAYER_APPROVAL.
func (c *Client) CreateSetupToken(ctx context.Context, returnURL, cancelURL string, opt CallOptions) (SetupToken, error) {
	if opt.IdempotencyKey == "" {
		return SetupToken{}, ErrMissingIdempotencyKey
	}
	body := map[string]any{"payment_source": map[string]any{"paypal": map[string]any{
		"usage_type": "MERCHANT",
		"experience_context": map[string]any{
			"return_url":        returnURL,
			"cancel_url":        cancelURL,
			"vault_instruction": "ON_PAYER_APPROVAL",
		},
	}}}
	var st SetupToken
	err := c.do(ctx, request{
		method: http.MethodPost, path: "/v3/vault/setup-tokens", body: body,
		idempotencyKey: opt.IdempotencyKey, retryable: true,
	}, &st)
	return st, err
}

// CreatePaymentToken exchanges an approved setup token for a vault ID.
func (c *Client) CreatePaymentToken(ctx context.Context, setupTokenID string, opt CallOptions) (PaymentToken, error) {
	if opt.IdempotencyKey == "" {
		return PaymentToken{}, ErrMissingIdempotencyKey
	}
	body := map[string]any{"payment_source": map[string]any{"token": map[string]any{
		"id": setupTokenID, "type": "SETUP_TOKEN",
	}}}
	var pt PaymentToken
	err := c.do(ctx, request{
		method: http.MethodPost, path: "/v3/vault/payment-tokens", body: body,
		idempotencyKey: opt.IdempotencyKey, retryable: true,
	}, &pt)
	return pt, err
}

func (c *Client) GetSetupToken(ctx context.Context, id string) (SetupToken, error) {
	var st SetupToken
	err := c.do(ctx, request{method: http.MethodGet, path: "/v3/vault/setup-tokens/" + url.PathEscape(id), retryable: true}, &st)
	return st, err
}

// ClientToken is the short-lived browser token the v6 SDK needs for vaulting.
type ClientToken struct {
	Token     string
	ExpiresIn int64
}

// BrowserClientToken requests response_type=client_token. Verified live:
// omitting domains[] works; "localhost" is rejected as invalid_domain, so
// domains are passed only for real hostnames.
func (c *Client) BrowserClientToken(ctx context.Context, domains ...string) (ClientToken, error) {
	form := url.Values{"grant_type": {"client_credentials"}, "response_type": {"client_token"}}
	for _, d := range domains {
		form.Add("domains[]", d)
	}
	tr, err := c.oauth(ctx, form)
	if err != nil {
		return ClientToken{}, err
	}
	return ClientToken{Token: tr.AccessToken, ExpiresIn: tr.ExpiresIn}, nil
}
