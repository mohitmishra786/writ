package mesh

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const seedA = "1111111111111111111111111111111111111111111111111111111111111111"
const seedB = "2222222222222222222222222222222222222222222222222222222222222222"

func serve(t *testing.T) (*httptest.Server, []*Supplier) {
	t.Helper()
	sups, err := DemoSuppliers(seedA, seedB, "a@example.com", "b@example.com", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	for _, s := range sups {
		s.Mount(mux, srv.URL)
	}
	return srv, sups
}

func rfq(t *testing.T, srv *httptest.Server, slug string, r RFQ) (QuoteReply, AgentCard) {
	t.Helper()
	c := Client{}
	ctx := context.Background()
	card, err := c.FetchCard(ctx, srv.URL+"/a2a/"+slug+"/.well-known/agent-card.json")
	if err != nil {
		t.Fatal(err)
	}
	ep, err := card.JSONRPCEndpoint()
	if err != nil {
		t.Fatal(err)
	}
	part, _ := DataPart(r)
	out, err := c.Send(ctx, ep, Message{MessageID: NewMessageID(), Role: "ROLE_USER", Parts: []Part{part}})
	if err != nil {
		t.Fatal(err)
	}
	if out.Role != "ROLE_AGENT" {
		t.Fatalf("role %q", out.Role)
	}
	var reply QuoteReply
	if err := FirstData(out, &reply); err != nil {
		t.Fatal(err)
	}
	return reply, card
}

func TestRFQRoundTripSignedQuote(t *testing.T) {
	srv, _ := serve(t)
	reply, card := rfq(t, srv, "bean-co", RFQ{BuyerRef: "mandate_1", SKU: "pitcher-20oz", Qty: 6, TargetUnitMinor: 1999, Round: 1})
	pkHex := card.Metadata["quote_signing_key_ed25519"].(string)
	pk, _ := hex.DecodeString(pkHex)
	if err := reply.Quote.Verify(ed25519.PublicKey(pk), time.Now()); err != nil {
		t.Fatalf("quote does not verify against the card key: %v", err)
	}
	q := reply.Quote.Quote
	// Round 1 concedes 35% of the gap: 2450 - (2450-1999)*35/100 = 2293.
	if q.Lines[0].UnitPrice.Minor != 2293 || q.Total.Minor != 2293*6+850 || q.BuyerRef != "mandate_1" {
		t.Fatalf("unexpected quote %+v", q)
	}
	if reply.Note == "" || card.SupportedInterfaces[0].ProtocolVersion != ProtocolVersion {
		t.Fatal("missing note or protocol version")
	}
}

func TestPriceNeverBelowFloor(t *testing.T) {
	l := Listing{ListMinor: 2450, FloorMinor: 1875}
	for round := 1; round <= 6; round++ {
		if p := unitPrice(l, 100, round); p < l.FloorMinor {
			t.Fatalf("round %d priced %d below floor", round, p)
		}
	}
	if p := unitPrice(l, 100, 3); p != 1875 {
		t.Fatalf("round 3 should reach floor, got %d", p)
	}
	if p := unitPrice(l, 5000, 1); p != 2450 {
		t.Fatalf("target above list should quote list, got %d", p)
	}
}

func TestFloorIsNeverPublished(t *testing.T) {
	srv, _ := serve(t)
	resp, err := http.Get(srv.URL + "/a2a/bean-co/.well-known/agent-card.json")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	if strings.Contains(buf.String(), "1875") || strings.Contains(strings.ToLower(buf.String()), "floor") {
		t.Fatal("agent card leaks the price floor")
	}
}

func TestInjectionPayloadIsInListing(t *testing.T) {
	srv, _ := serve(t)
	reply, _ := rfq(t, srv, "steam-supply", RFQ{BuyerRef: "m", SKU: "pitcher-20oz", Qty: 2, Round: 1})
	if !strings.Contains(reply.Listing.Description, "attacker:refund-desk") {
		t.Fatal("demo injection payload missing")
	}
}

func TestRPCErrors(t *testing.T) {
	srv, _ := serve(t)
	resp, err := http.Post(srv.URL+"/a2a/bean-co/v1", "application/json", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"message/send","params":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	if !strings.Contains(buf.String(), "-32601") {
		t.Fatalf("expected method-not-found, got %s", buf.String())
	}
	c := Client{}
	part, _ := DataPart(RFQ{BuyerRef: "m", SKU: "pitcher-20oz", Qty: 999})
	if _, err := c.Send(context.Background(), srv.URL+"/a2a/bean-co/v1", Message{MessageID: "x", Role: "ROLE_USER", Parts: []Part{part}}); err == nil {
		t.Fatal("over-stock RFQ accepted")
	}
}
