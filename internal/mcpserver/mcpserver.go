// Package mcpserver exposes Mandate over the Model Context Protocol with the
// same tool names and input fields as @paypal/agent-toolkit v1.11.0
// (typescript/src/shared/parameters.ts, ISC license), so an agent built for
// PayPal's toolkit can be pointed at Mandate unchanged (ADR-0002). Every
// money-moving call goes through the kernel's policy, capability and audit
// path instead of straight to PayPal.
package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mohitmishra786/paypal-ai-hackathon/internal/kernel"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/kernel/policy"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/money"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/store"
)

// ID patterns copied from @paypal/agent-toolkit typescript/src/shared/regex.ts.
var (
	orderIDRe   = regexp.MustCompile(`^[A-Z0-9]{17,32}$`)
	captureIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{15,32}$`)
	refundIDRe  = regexp.MustCompile(`^[A-Za-z0-9_-]{15,32}$`)
)

type Deps struct {
	Kernel   *kernel.Service
	Store    *store.Store
	Refunder kernel.Refunder
	AgentID  string
}

// Handler serves MCP at /mcp/{mandate_id} (stateless streamable HTTP).
func Handler(d Deps) http.Handler {
	h := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		mandateID := strings.Trim(strings.TrimPrefix(r.URL.Path, "/mcp/"), "/")
		if mandateID == "" {
			return nil
		}
		return newServer(d, mandateID)
	}, &mcp.StreamableHTTPOptions{Stateless: true})
	return h
}

func textResult(v any) *mcp.CallToolResult {
	b, _ := json.Marshal(v)
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}
}

// orderID maps between PayPal-shaped order IDs and Mandate intent IDs. ULIDs
// are uppercase Crockford base32, so they satisfy PayPal's ORDER_ID_REGEX.
func orderID(intentID string) string { return strings.TrimPrefix(intentID, "intent_") }
func intentID(orderID string) string { return "intent_" + orderID }

type lineItem struct {
	Name        string  `json:"name" jsonschema:"The name of the item."`
	Quantity    float64 `json:"quantity" jsonschema:"The item quantity. Must be a whole number."`
	Description string  `json:"description,omitempty" jsonschema:"The detailed item description."`
	ItemCost    float64 `json:"itemCost" jsonschema:"The cost of each item - upto 2 decimal points."`
	TaxPercent  float64 `json:"taxPercent,omitempty" jsonschema:"The tax percent for the specific item."`
	ItemTotal   float64 `json:"itemTotal" jsonschema:"The total cost of this line item."`
}

type createOrderIn struct {
	CurrencyCode    string         `json:"currencyCode" jsonschema:"Currency code of the amount. Only USD is supported."`
	Items           []lineItem     `json:"items"`
	Discount        float64        `json:"discount,omitempty" jsonschema:"The discount amount for the order."`
	ShippingCost    float64        `json:"shippingCost,omitempty" jsonschema:"The cost of shipping for the order."`
	ShippingAddress map[string]any `json:"shippingAddress,omitempty" jsonschema:"The shipping address for the order."`
	Notes           string         `json:"notes,omitempty"`
	ReturnURL       string         `json:"returnUrl,omitempty"`
	CancelURL       string         `json:"cancelUrl,omitempty"`
	// Mandate extensions (optional for toolkit compatibility).
	PayeeRef string `json:"payee_ref,omitempty" jsonschema:"Mandate extension: the allowlisted supplier reference to pay."`
	Category string `json:"category,omitempty" jsonschema:"Mandate extension: product category slug."`
}

type idIn struct {
	ID string `json:"id" jsonschema:"The order id generated during create call"`
}

type createRefundIn struct {
	CaptureID string `json:"capture_id" jsonschema:"The ID of the capture to refund."`
	Amount    *struct {
		CurrencyCode string `json:"currency_code"`
		Value        string `json:"value"`
	} `json:"amount,omitempty" jsonschema:"The amount to refund. If not specified, the full captured amount is refunded."`
	InvoiceID   string `json:"invoice_id,omitempty" jsonschema:"The invoice ID that is used to track this payment."`
	NoteToPayer string `json:"note_to_payer,omitempty" jsonschema:"A note to the payer."`
}

type getRefundIn struct {
	RefundID string `json:"refund_id" jsonschema:"The ID of the refund to get details for."`
}

type empty struct{}

type proposeIn struct {
	PayeeRef    string `json:"payee_ref" jsonschema:"Allowlisted supplier reference."`
	AmountMinor int64  `json:"amount_minor" jsonschema:"Amount in integer US cents."`
	Category    string `json:"category,omitempty"`
}

// cents converts toolkit float money exactly once, rounding half away from
// zero; totals are then recomputed in integers (the PR #110 lesson).
func cents(v float64) (int64, error) {
	m, err := money.FromFloat(v, money.USD)
	return m.Minor, err
}

func newServer(d Deps, mandateID string) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "mandate", Version: "1.0.0"}, &mcp.ServerOptions{
		Instructions: "Mandate: a spending firewall for AI agents. Tools mirror PayPal Agent Toolkit names; every payment is checked against the principal's signed mandate before PayPal is called.",
	})

	mcp.AddTool(s, &mcp.Tool{Name: "create_order", Description: "Create an order in PayPal. This tool is used to create a new order in PayPal. Through Mandate, the order is a purchase checked against the spending mandate; pay it with pay_order."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in createOrderIn) (*mcp.CallToolResult, any, error) {
			if in.CurrencyCode != "USD" {
				return nil, nil, errors.New("currencyCode must be USD")
			}
			if len(in.Items) == 0 || len(in.Items) > 50 {
				return nil, nil, errors.New("items must contain 1-50 entries")
			}
			var total int64
			for _, it := range in.Items {
				if it.Quantity < 1 || it.Quantity != math.Trunc(it.Quantity) {
					return nil, nil, fmt.Errorf("item %q quantity must be a whole number >= 1", it.Name)
				}
				unit, err := cents(it.ItemCost)
				if err != nil {
					return nil, nil, err
				}
				tax := int64(math.Round(float64(unit) * it.TaxPercent / 100))
				total += (unit + tax) * int64(it.Quantity)
			}
			ship, err := cents(in.ShippingCost)
			if err != nil {
				return nil, nil, err
			}
			disc, err := cents(in.Discount)
			if err != nil {
				return nil, nil, err
			}
			total += ship - disc
			dec, err := d.Kernel.Propose(ctx, kernel.Proposal{MandateID: mandateID, AgentID: d.AgentID, PayeeRef: in.PayeeRef,
				Amount: money.New(total, money.USD), Category: in.Category, Trace: mustJSON(map[string]any{"via": "mcp:create_order", "notes": in.Notes})})
			if err != nil {
				return nil, nil, err
			}
			// PayPal order statuses: CREATED only when the mandate authorized the
			// order; a co-sign wait is PAYER_ACTION_REQUIRED; a denial is VOIDED.
			status := "VOIDED"
			switch dec.Outcome {
			case policy.Authorize:
				status = "CREATED"
			case policy.Cosign:
				status = "PAYER_ACTION_REQUIRED"
			}
			return textResult(map[string]any{"id": orderID(dec.IntentID), "status": status,
				"mandate": map[string]any{"outcome": dec.Outcome, "state": dec.State, "reasons": dec.Reasons, "amount": money.New(total, money.USD).Decimal()}}), nil, nil
		})

	mcp.AddTool(s, &mcp.Tool{Name: "get_order", Description: "Retrieves the order details."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in idIn) (*mcp.CallToolResult, any, error) {
			if !orderIDRe.MatchString(in.ID) {
				return nil, nil, errors.New("Invalid PayPal Order ID") //nolint:staticcheck // exact @paypal/agent-toolkit error text
			}
			it, err := d.Kernel.GetIntent(ctx, intentID(in.ID))
			if err != nil || it.MandateID != mandateID {
				return nil, nil, errors.New("order not found")
			}
			return textResult(map[string]any{"id": in.ID, "status": it.State, "amount": it.Amount.Decimal(), "currency": it.Amount.Currency,
				"payee": it.PayeeRef, "paypal_order_id": it.OrderID, "capture_id": it.CaptureID, "reasons": it.Reasons}), nil, nil
		})

	mcp.AddTool(s, &mcp.Tool{Name: "pay_order", Description: "Capture a payment for an order. Through Mandate, this consumes the single-use capability minted when the mandate authorized the order."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in idIn) (*mcp.CallToolResult, any, error) {
			if !orderIDRe.MatchString(in.ID) {
				return nil, nil, errors.New("Invalid PayPal Order ID") //nolint:staticcheck // exact @paypal/agent-toolkit error text
			}
			it, err := d.Kernel.GetIntent(ctx, intentID(in.ID))
			if err != nil || it.MandateID != mandateID {
				return nil, nil, errors.New("order not found")
			}
			it, err = d.Kernel.Execute(ctx, it.ID, kernel.ExecOptions{})
			if err != nil {
				return nil, nil, err
			}
			out := map[string]any{"id": in.ID, "status": it.State, "capture_id": it.CaptureID}
			if it.ApprovalURL != "" {
				out["payer_action"] = it.ApprovalURL
			}
			return textResult(out), nil, nil
		})

	mcp.AddTool(s, &mcp.Tool{Name: "create_refund", Description: "Initiate a refund for a PayPal payment capture. Through Mandate, only captures made under this mandate can be refunded, and never beyond the captured amount."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in createRefundIn) (*mcp.CallToolResult, any, error) {
			if !captureIDRe.MatchString(in.CaptureID) {
				return nil, nil, errors.New("Invalid Transaction ID") //nolint:staticcheck // exact @paypal/agent-toolkit error text
			}
			var amt *money.Money
			if in.Amount != nil {
				m, err := money.ParseDecimal(in.Amount.Value, money.Currency(in.Amount.CurrencyCode))
				if err != nil {
					return nil, nil, err
				}
				amt = &m
			}
			var owner string
			if err := d.Store.Pool.QueryRow(ctx, `SELECT i.mandate_id FROM captures c JOIN intents i ON i.id = c.intent_id WHERE c.capture_id = $1`, in.CaptureID).Scan(&owner); err != nil || owner != mandateID {
				return nil, nil, errors.New("refund denied: capture was not made under this mandate")
			}
			r, err := d.Kernel.Refund(ctx, d.Refunder, in.CaptureID, amt, in.NoteToPayer)
			if err != nil {
				return nil, nil, err
			}
			return textResult(map[string]any{"id": r.PayPalRefundID, "status": r.Status, "amount": r.Amount.Decimal(), "remaining_refundable": r.Remaining.Decimal()}), nil, nil
		})

	mcp.AddTool(s, &mcp.Tool{Name: "get_refund", Description: "Get the details for a specific refund."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in getRefundIn) (*mcp.CallToolResult, any, error) {
			if !refundIDRe.MatchString(in.RefundID) {
				return nil, nil, errors.New("Invalid refund ID") //nolint:staticcheck // exact @paypal/agent-toolkit error text
			}
			var status, cur, captureID string
			var minor int64
			err := d.Store.Pool.QueryRow(ctx, `SELECT r.status, r.amount_minor, r.currency, r.capture_id FROM refunds r JOIN intents i ON i.id = r.intent_id
				WHERE r.paypal_refund_id = $1 AND i.mandate_id = $2`, in.RefundID, mandateID).Scan(&status, &minor, &cur, &captureID)
			if err != nil {
				return nil, nil, errors.New("refund not found")
			}
			return textResult(map[string]any{"id": in.RefundID, "status": status, "amount": money.New(minor, money.Currency(cur)).Decimal(), "capture_id": captureID}), nil, nil
		})

	mcp.AddTool(s, &mcp.Tool{Name: "list_transactions", Description: "List transactions made under this mandate (from the Mandate ledger)."},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ empty) (*mcp.CallToolResult, any, error) {
			its, err := d.Kernel.ListIntents(ctx, mandateID, 100)
			if err != nil {
				return nil, nil, err
			}
			var out []map[string]any
			for _, it := range its {
				out = append(out, map[string]any{"id": orderID(it.ID), "status": it.State, "amount": it.Amount.Decimal(), "payee": it.PayeeRef,
					"capture_id": it.CaptureID, "created_at": it.CreatedAt})
			}
			return textResult(map[string]any{"transactions": out}), nil, nil
		})

	mcp.AddTool(s, &mcp.Tool{Name: "mandate_get", Description: "Mandate-native: the spending mandate's limits, allowlisted payees and current envelope usage."},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ empty) (*mcp.CallToolResult, any, error) {
			v, err := d.Kernel.GetMandate(ctx, mandateID)
			if err != nil {
				return nil, nil, err
			}
			payees := make([]string, len(v.Policy.Payees))
			for i, p := range v.Policy.Payees {
				payees[i] = p.Ref
			}
			return textResult(map[string]any{"id": v.ID, "status": v.Status, "envelope_limit": money.New(v.LimitMinor, v.Policy.Envelope.Currency).Decimal(),
				"reserved": money.New(v.ReservedMinor, v.Policy.Envelope.Currency).Decimal(), "captured": money.New(v.CapturedMinor, v.Policy.Envelope.Currency).Decimal(),
				"per_order_max": v.Policy.PerIntentMax.Decimal(), "cosign_above": v.Policy.CosignAbove.Decimal(), "payees": payees, "categories": v.Policy.Categories}), nil, nil
		})

	mcp.AddTool(s, &mcp.Tool{Name: "mandate_propose_purchase", Description: "Mandate-native: propose a purchase to an allowlisted payee; returns the policy decision with reason codes."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in proposeIn) (*mcp.CallToolResult, any, error) {
			dec, err := d.Kernel.Propose(ctx, kernel.Proposal{MandateID: mandateID, AgentID: d.AgentID, PayeeRef: in.PayeeRef,
				Amount: money.New(in.AmountMinor, money.USD), Category: in.Category, Trace: mustJSON(map[string]any{"via": "mcp:mandate_propose_purchase"})})
			if err != nil {
				return nil, nil, err
			}
			return textResult(map[string]any{"order_id": orderID(dec.IntentID), "outcome": dec.Outcome, "state": dec.State, "reasons": dec.Reasons}), nil, nil
		})

	mcp.AddTool(s, &mcp.Tool{Name: "mandate_list_decisions", Description: "Mandate-native: recent policy decisions, including denials and their reason codes."},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ empty) (*mcp.CallToolResult, any, error) {
			its, err := d.Kernel.ListIntents(ctx, mandateID, 50)
			if err != nil {
				return nil, nil, err
			}
			var out []map[string]any
			for _, it := range its {
				out = append(out, map[string]any{"order_id": orderID(it.ID), "state": it.State, "reasons": it.Reasons, "payee": it.PayeeRef, "amount": it.Amount.Decimal()})
			}
			return textResult(map[string]any{"decisions": out}), nil, nil
		})
	return s
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
