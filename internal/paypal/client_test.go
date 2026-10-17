package paypal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mohitmishra786/paypal-ai-hackathon/internal/money"
)

type fakePayPal struct {
	t          *testing.T
	srv        *httptest.Server
	tokenCalls atomic.Int32
	mu         sync.Mutex
	requests   []*recorded
	handler    func(w http.ResponseWriter, r *http.Request, body []byte)
}

type recorded struct {
	Method, Path, RequestID, Mock, Prefer string
	Body                                  []byte
}

func newFake(t *testing.T, env Env, h func(w http.ResponseWriter, r *http.Request, body []byte)) (*fakePayPal, *Client) {
	t.Helper()
	f := &fakePayPal{t: t, handler: h}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.URL.Path == "/v1/oauth2/token" {
			f.tokenCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "tok", "token_type": "Bearer", "expires_in": 32400})
			return
		}
		f.mu.Lock()
		f.requests = append(f.requests, &recorded{
			Method: r.Method, Path: r.URL.Path, Body: body,
			RequestID: r.Header.Get("PayPal-Request-Id"), Mock: r.Header.Get("PayPal-Mock-Response"), Prefer: r.Header.Get("Prefer"),
		})
		f.mu.Unlock()
		f.handler(w, r, body)
	}))
	t.Cleanup(f.srv.Close)
	// Construct directly: Validate() deliberately rejects non-PayPal hosts.
	c := &Client{
		cfg:  Config{Env: env, BaseURL: f.srv.URL, ClientID: "id", ClientSecret: "secret"},
		http: f.srv.Client(), maxAttempts: 4, baseBackoff: time.Millisecond,
		sleep: func(context.Context, time.Duration) error { return nil },
	}
	return f, c
}

func (f *fakePayPal) reqs() []*recorded {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*recorded(nil), f.requests...)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func usd(minor int64) money.Money { return money.New(minor, money.USD) }

func spec() OrderSpec {
	return OrderSpec{
		InvoiceID: "intent_01", CustomID: "mandate_01",
		Items:    []Item{{Name: "Milk Frothing Pitcher", SKU: "pitcher-20oz", Quantity: 6, Unit: usd(2625)}},
		Shipping: usd(2850), Total: usd(18600),
	}
}

func TestConfigValidate(t *testing.T) {
	ok := Config{Env: Sandbox, BaseURL: "https://api-m.sandbox.paypal.com/", ClientID: "a", ClientSecret: "b"}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]Config{
		"missing env":       {BaseURL: ok.BaseURL, ClientID: "a", ClientSecret: "b"},
		"env/url mismatch":  {Env: Live, BaseURL: ok.BaseURL, ClientID: "a", ClientSecret: "b"},
		"missing secret":    {Env: Sandbox, BaseURL: ok.BaseURL, ClientID: "a"},
		"unknown env value": {Env: "prod", BaseURL: ok.BaseURL, ClientID: "a", ClientSecret: "b"},
	} {
		if err := c.Validate(); !errors.Is(err, ErrConfig) {
			t.Fatalf("%s: expected ErrConfig, got %v", name, err)
		}
	}
}

func TestTokenIsCachedAcrossCalls(t *testing.T) {
	f, c := newFake(t, Sandbox, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		writeJSON(w, 200, map[string]any{"id": "ORDER1", "status": "COMPLETED"})
	})
	for range 3 {
		if _, err := c.GetOrder(context.Background(), "ORDER1"); err != nil {
			t.Fatal(err)
		}
	}
	if n := f.tokenCalls.Load(); n != 1 {
		t.Fatalf("token fetched %d times, want 1", n)
	}
}

func TestVaultedOrderBodyAndHeaders(t *testing.T) {
	f, c := newFake(t, Sandbox, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		writeJSON(w, 201, map[string]any{"id": "5O190127TN364715T", "status": "COMPLETED"})
	})
	o, err := c.CreateVaultedOrder(context.Background(), "vault_123", spec(), CallOptions{IdempotencyKey: "key-1"})
	if err != nil || o.Status != OrderCompleted {
		t.Fatalf("got %+v %v", o, err)
	}
	r := f.reqs()[0]
	if r.RequestID != "key-1" || r.Prefer != "return=representation" || r.Mock != "" {
		t.Fatalf("headers: %+v", r)
	}
	var body map[string]any
	_ = json.Unmarshal(r.Body, &body)
	pp := body["payment_source"].(map[string]any)["paypal"].(map[string]any)
	sc := pp["stored_credential"].(map[string]any)
	if body["intent"] != "CAPTURE" || pp["vault_id"] != "vault_123" ||
		sc["payment_initiator"] != "MERCHANT" || sc["usage_pattern"] != "IMMEDIATE" {
		t.Fatalf("unexpected body %s", r.Body)
	}
	pu := body["purchase_units"].([]any)[0].(map[string]any)
	amt := pu["amount"].(map[string]any)
	bd := amt["breakdown"].(map[string]any)
	if pu["invoice_id"] != "intent_01" || amt["value"] != "186.00" ||
		bd["item_total"].(map[string]any)["value"] != "157.50" || bd["shipping"].(map[string]any)["value"] != "28.50" {
		t.Fatalf("unexpected purchase unit %s", r.Body)
	}
}

func TestBreakdownMismatchRejectedLocally(t *testing.T) {
	f, c := newFake(t, Sandbox, func(w http.ResponseWriter, _ *http.Request, _ []byte) { writeJSON(w, 201, map[string]any{}) })
	s := spec()
	s.Total = usd(18700)
	if _, err := c.CreateVaultedOrder(context.Background(), "v", s, CallOptions{IdempotencyKey: "k"}); !errors.Is(err, ErrBreakdown) {
		t.Fatalf("got %v", err)
	}
	if len(f.reqs()) != 0 {
		t.Fatal("request must not reach PayPal")
	}
}

func TestMissingIdempotencyKey(t *testing.T) {
	_, c := newFake(t, Sandbox, func(w http.ResponseWriter, _ *http.Request, _ []byte) {})
	ctx := context.Background()
	if _, err := c.CreateVaultedOrder(ctx, "v", spec(), CallOptions{}); !errors.Is(err, ErrMissingIdempotencyKey) {
		t.Fatal("vaulted order without key must fail")
	}
	if _, err := c.CaptureOrder(ctx, "ORDER", CallOptions{}); !errors.Is(err, ErrMissingIdempotencyKey) {
		t.Fatal("capture without key must fail")
	}
}

// The #98 fix end to end: retries reuse the same key, distinct operations
// never share one.
func TestRetryReusesIdempotencyKey(t *testing.T) {
	var n atomic.Int32
	f, c := newFake(t, Sandbox, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		if n.Add(1) <= 2 {
			writeJSON(w, 503, map[string]any{"name": "SERVICE_UNAVAILABLE", "debug_id": "dbg"})
			return
		}
		writeJSON(w, 201, map[string]any{"id": "ORDER", "status": "COMPLETED"})
	})
	if _, err := c.CreateVaultedOrder(context.Background(), "v", spec(), CallOptions{IdempotencyKey: "same-key"}); err != nil {
		t.Fatal(err)
	}
	rs := f.reqs()
	if len(rs) != 3 {
		t.Fatalf("want 3 attempts, got %d", len(rs))
	}
	for _, r := range rs {
		if r.RequestID != "same-key" {
			t.Fatalf("retry changed the key: %q", r.RequestID)
		}
	}
}

func TestDeclineIsNotRetried(t *testing.T) {
	f, c := newFake(t, Sandbox, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		w.Header().Set("Paypal-Debug-Id", "hdr-dbg")
		writeJSON(w, 422, map[string]any{
			"name": "UNPROCESSABLE_ENTITY", "message": "The requested action could not be performed",
			"details": []map[string]any{{"issue": "INSTRUMENT_DECLINED", "description": "declined"}},
		})
	})
	_, err := c.CreateVaultedOrder(context.Background(), "v", spec(), CallOptions{IdempotencyKey: "k", MockApplicationCode: IssueInstrumentDeclined})
	if !IsIssue(err, IssueInstrumentDeclined) {
		t.Fatalf("got %v", err)
	}
	var ae *APIError
	if !errors.As(err, &ae) || ae.DebugID != "hdr-dbg" || errors.Is(err, ErrAmbiguous) {
		t.Fatalf("expected definitive APIError with debug id, got %v", err)
	}
	rs := f.reqs()
	if len(rs) != 1 {
		t.Fatalf("decline retried %d times", len(rs)-1)
	}
	if rs[0].Mock != `{"mock_application_codes":"INSTRUMENT_DECLINED"}` {
		t.Fatalf("mock header %q", rs[0].Mock)
	}
}

func TestExhaustedServerErrorsAreAmbiguous(t *testing.T) {
	_, c := newFake(t, Sandbox, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		writeJSON(w, 500, map[string]any{"name": "INTERNAL_SERVER_ERROR"})
	})
	_, err := c.CaptureOrder(context.Background(), "ORDER", CallOptions{IdempotencyKey: "k"})
	if !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("got %v", err)
	}
}

func TestRetryAfterIsHonoured(t *testing.T) {
	var n atomic.Int32
	_, c := newFake(t, Sandbox, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		if n.Add(1) == 1 {
			w.Header().Set("Retry-After", "2")
			writeJSON(w, 429, map[string]any{"name": "RATE_LIMIT_REACHED"})
			return
		}
		writeJSON(w, 200, map[string]any{"id": "ORDER"})
	})
	var slept []time.Duration
	c.sleep = func(_ context.Context, d time.Duration) error { slept = append(slept, d); return nil }
	if _, err := c.GetOrder(context.Background(), "ORDER"); err != nil {
		t.Fatal(err)
	}
	if len(slept) != 1 || slept[0] != 2*time.Second {
		t.Fatalf("slept %v", slept)
	}
}

func TestMockHeaderRefusedOnLive(t *testing.T) {
	_, c := newFake(t, Live, func(w http.ResponseWriter, _ *http.Request, _ []byte) { writeJSON(w, 201, map[string]any{}) })
	_, err := c.CreateVaultedOrder(context.Background(), "v", spec(), CallOptions{IdempotencyKey: "k", MockApplicationCode: IssueInstrumentDeclined})
	if !errors.Is(err, ErrSandboxOnly) {
		t.Fatalf("got %v", err)
	}
}

func TestUnauthorizedRefreshesTokenOnce(t *testing.T) {
	var n atomic.Int32
	f, c := newFake(t, Sandbox, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		if n.Add(1) == 1 {
			writeJSON(w, 401, map[string]any{"error": "invalid_token"})
			return
		}
		writeJSON(w, 201, map[string]any{"id": "WH-1", "url": "https://x.example/wh", "event_types": []any{}})
	})
	// CreateWebhook is non-retryable, yet a 401 must still trigger one refresh.
	if _, err := c.CreateWebhook(context.Background(), "https://x.example/wh", []string{"CHECKOUT.ORDER.APPROVED"}); err != nil {
		t.Fatal(err)
	}
	if f.tokenCalls.Load() != 2 {
		t.Fatalf("token calls %d, want 2", f.tokenCalls.Load())
	}
}

func TestApprovalOrderExposesPayerActionLink(t *testing.T) {
	f, c := newFake(t, Sandbox, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		writeJSON(w, 200, map[string]any{"id": "ORDER", "status": "PAYER_ACTION_REQUIRED", "links": []map[string]any{
			{"rel": "self", "href": "https://api/self"},
			{"rel": "payer-action", "href": "https://www.sandbox.paypal.com/checkoutnow?token=ORDER"},
		}})
	})
	o, err := c.CreateApprovalOrder(context.Background(), spec(), "https://m.example/ok", "https://m.example/cancel", CallOptions{IdempotencyKey: "k"})
	if err != nil || o.Link("payer-action") == "" || o.Status != OrderPayerActionRequired {
		t.Fatalf("got %+v %v", o, err)
	}
	var body map[string]any
	_ = json.Unmarshal(f.reqs()[0].Body, &body)
	ec := body["payment_source"].(map[string]any)["paypal"].(map[string]any)["experience_context"].(map[string]any)
	if ec["user_action"] != "PAY_NOW" || ec["return_url"] != "https://m.example/ok" {
		t.Fatalf("experience_context %v", ec)
	}
}

func TestPayoutValidation(t *testing.T) {
	_, c := newFake(t, Sandbox, func(w http.ResponseWriter, _ *http.Request, _ []byte) { writeJSON(w, 201, map[string]any{}) })
	_, err := c.CreatePayoutBatch(context.Background(), PayoutBatch{
		SenderBatchID: "mb-1",
		Items:         []PayoutItem{{ReceiverEmail: "a@example.com", Amount: usd(100), SenderItemID: strings.Repeat("x", 64)}},
	})
	if err == nil {
		t.Fatal("expected sender_item_id length error")
	}
}

func TestVerifyWebhookSendsRawEventVerbatim(t *testing.T) {
	raw := []byte(`{"id":"WH-EVT-1","event_type":"CHECKOUT.ORDER.APPROVED","resource":{"id":"ORDER","amount":"186.00"}}`)
	f, c := newFake(t, Sandbox, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		writeJSON(w, 200, map[string]any{"verification_status": "SUCCESS"})
	})
	h := http.Header{}
	for k, v := range map[string]string{
		"Paypal-Auth-Algo": "SHA256withRSA", "Paypal-Cert-Url": "https://api.sandbox.paypal.com/cert",
		"Paypal-Transmission-Id": "tid", "Paypal-Transmission-Sig": "sig", "Paypal-Transmission-Time": "2026-10-17T00:00:00Z",
	} {
		h.Set(k, v)
	}
	if err := c.VerifyWebhook(context.Background(), h, "WH-ID", raw); err != nil {
		t.Fatal(err)
	}
	var sent struct {
		WebhookID    string          `json:"webhook_id"`
		WebhookEvent json.RawMessage `json:"webhook_event"`
	}
	_ = json.Unmarshal(f.reqs()[0].Body, &sent)
	if sent.WebhookID != "WH-ID" || !bytes.Equal(sent.WebhookEvent, raw) {
		t.Fatalf("event altered: %s", sent.WebhookEvent)
	}
	h.Del("Paypal-Transmission-Sig")
	if err := c.VerifyWebhook(context.Background(), h, "WH-ID", raw); !errors.Is(err, ErrWebhookUnverified) {
		t.Fatal("missing signature header must fail closed")
	}
}

func TestVerifyWebhookFailureStatus(t *testing.T) {
	_, c := newFake(t, Sandbox, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		writeJSON(w, 200, map[string]any{"verification_status": "FAILURE"})
	})
	h := http.Header{}
	for _, k := range []string{"Paypal-Auth-Algo", "Paypal-Cert-Url", "Paypal-Transmission-Id", "Paypal-Transmission-Sig", "Paypal-Transmission-Time"} {
		h.Set(k, "x")
	}
	if err := c.VerifyWebhook(context.Background(), h, "WH", []byte(`{}`)); !errors.Is(err, ErrWebhookUnverified) {
		t.Fatalf("got %v", err)
	}
}

func TestEnsureWebhookIsIdempotent(t *testing.T) {
	events := []string{"CHECKOUT.ORDER.APPROVED", "PAYMENT.CAPTURE.COMPLETED"}
	f, c := newFake(t, Sandbox, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		writeJSON(w, 200, map[string]any{"webhooks": []map[string]any{{
			"id": "WH-1", "url": "https://m.example/wh",
			"event_types": []map[string]any{{"name": "PAYMENT.CAPTURE.COMPLETED"}, {"name": "CHECKOUT.ORDER.APPROVED"}},
		}}})
	})
	w, err := c.EnsureWebhook(context.Background(), "https://m.example/wh", events)
	if err != nil || w.ID != "WH-1" {
		t.Fatalf("got %+v %v", w, err)
	}
	for _, r := range f.reqs() {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected %s %s: existing webhook must be reused", r.Method, r.Path)
		}
	}
}
