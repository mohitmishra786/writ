package paypal

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mohitmishra786/paypal-ai-hackathon/internal/kernel/idem"
)

// Live sandbox checks. Run with:
//
//	set -a; . ./.env; set +a; MANDATE_SANDBOX_IT=1 go test ./internal/paypal -run Live -v
func liveClient(t *testing.T) *Client {
	t.Helper()
	if os.Getenv("MANDATE_SANDBOX_IT") != "1" {
		t.Skip("set MANDATE_SANDBOX_IT=1 to run live sandbox checks")
	}
	cfg, err := ConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Env != Sandbox {
		t.Fatal("live checks must never run against PayPal live")
	}
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func runID() string { return time.Now().UTC().Format("20060102T150405.000000000") }

func TestLiveBrowserClientToken(t *testing.T) {
	c := liveClient(t)
	ct, err := c.BrowserClientToken(context.Background())
	if err != nil || ct.Token == "" || ct.ExpiresIn <= 0 {
		t.Fatalf("got %+v %v", ct, err)
	}
}

func TestLiveSetupTokenAwaitsPayerApproval(t *testing.T) {
	c := liveClient(t)
	key, _ := idem.VaultSetupToken("live-it-"+runID(), 1)
	st, err := c.CreateSetupToken(context.Background(), "https://example.com/return", "https://example.com/cancel", CallOptions{IdempotencyKey: key})
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != "PAYER_ACTION_REQUIRED" || !strings.Contains(st.ApproveURL(), "sandbox.paypal.com") {
		t.Fatalf("got %+v", st)
	}
	// Same key, same result: PayPal honours the idempotency key.
	again, err := c.CreateSetupToken(context.Background(), "https://example.com/return", "https://example.com/cancel", CallOptions{IdempotencyKey: key})
	if err != nil || again.ID != st.ID {
		t.Fatalf("idempotent replay returned %q, want %q (%v)", again.ID, st.ID, err)
	}
}

func TestLiveApprovalOrderRoundTrip(t *testing.T) {
	c := liveClient(t)
	ctx := context.Background()
	s := spec()
	s.InvoiceID = "it-" + runID()
	key, _ := idem.RecoveryOrder(s.InvoiceID, 1)
	o, err := c.CreateApprovalOrder(ctx, s, "https://example.com/return", "https://example.com/cancel", CallOptions{IdempotencyKey: key})
	if err != nil {
		t.Fatal(err)
	}
	if o.Status != OrderPayerActionRequired || o.Link("payer-action") == "" {
		t.Fatalf("got status %s links %+v", o.Status, o.Links)
	}
	got, err := c.GetOrder(ctx, o.ID)
	if err != nil || got.ID != o.ID || got.PurchaseUnits[0].InvoiceID != s.InvoiceID {
		t.Fatalf("get order: %+v %v", got, err)
	}
	amt, err := got.PurchaseUnits[0].Amount.Money()
	if err != nil || amt != s.Total {
		t.Fatalf("amount round-trip %v %v", amt, err)
	}
	// Capturing before buyer approval must fail definitively, not ambiguously.
	ck, _ := idem.Capture(o.ID)
	if _, err := c.CaptureOrder(ctx, o.ID, CallOptions{IdempotencyKey: ck}); !IsIssue(err, IssueOrderNotApproved) {
		t.Fatalf("capture before approval: %v", err)
	}
}

// Spike S5: PayPal negative testing on Orders v2 create.
func TestLiveMockedDeclineOnCreate(t *testing.T) {
	c := liveClient(t)
	s := spec()
	s.InvoiceID = "it-decline-" + runID()
	key, _ := idem.PrimaryOrder(s.InvoiceID)
	_, err := c.CreateApprovalOrder(context.Background(), s, "https://example.com/return", "https://example.com/cancel",
		CallOptions{IdempotencyKey: key, MockApplicationCode: IssueInstrumentDeclined})
	if !IsIssue(err, IssueInstrumentDeclined) {
		t.Fatalf("expected INSTRUMENT_DECLINED, got %v", err)
	}
	t.Logf("mocked decline: %v", err)
}

func TestLiveWebhookEventTypesAvailable(t *testing.T) {
	c := liveClient(t)
	var out struct {
		EventTypes []struct {
			Name string `json:"name"`
		} `json:"event_types"`
	}
	if err := c.do(context.Background(), request{method: "GET", path: "/v1/notifications/webhooks-event-types", retryable: true}, &out); err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, e := range out.EventTypes {
		have[e.Name] = true
	}
	for _, e := range SubscribedEvents {
		if !have[e] {
			t.Errorf("event %s not available to this app", e)
		}
	}
}
