package paypal

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/mohitmishra786/paypal-ai-hackathon/internal/money"
)

// Amount is PayPal's money object.
type Amount struct {
	CurrencyCode string `json:"currency_code"`
	Value        string `json:"value"`
}

func amountOf(m money.Money) Amount {
	return Amount{CurrencyCode: string(m.Currency), Value: m.Decimal()}
}

// Money parses a PayPal amount exactly.
func (a Amount) Money() (money.Money, error) {
	return money.ParseDecimal(a.Value, money.Currency(a.CurrencyCode))
}

type Item struct {
	Name     string
	SKU      string
	Quantity int64
	Unit     money.Money
}

// OrderSpec is a purchase unit expressed in integer minor units.
type OrderSpec struct {
	InvoiceID   string // set to the intent ID; max 127 chars per schema
	CustomID    string
	Description string
	Items       []Item
	Shipping    money.Money
	Total       money.Money
}

var ErrBreakdown = errors.New("paypal: amount does not equal item_total + shipping")

type itemBody struct {
	Name       string `json:"name"`
	SKU        string `json:"sku,omitempty"`
	Quantity   string `json:"quantity"`
	UnitAmount Amount `json:"unit_amount"`
}

type breakdownBody struct {
	ItemTotal Amount  `json:"item_total"`
	Shipping  *Amount `json:"shipping,omitempty"`
}

type amountBody struct {
	Amount
	Breakdown *breakdownBody `json:"breakdown,omitempty"`
}

type purchaseUnitBody struct {
	InvoiceID   string     `json:"invoice_id,omitempty"`
	CustomID    string     `json:"custom_id,omitempty"`
	Description string     `json:"description,omitempty"`
	Amount      amountBody `json:"amount"`
	Items       []itemBody `json:"items,omitempty"`
}

// purchaseUnit builds the body and enforces PayPal's breakdown invariant
// (amount.value == item_total + shipping, item_total == sum(unit * qty)) in
// integer arithmetic before anything is sent, which is the class of 422 loop
// behind paypal/agent-toolkit PR #110.
func (s OrderSpec) purchaseUnit() (purchaseUnitBody, error) {
	if len(s.InvoiceID) > 127 || len(s.CustomID) > 127 {
		return purchaseUnitBody{}, fmt.Errorf("paypal: invoice_id/custom_id exceed 127 chars")
	}
	pu := purchaseUnitBody{InvoiceID: s.InvoiceID, CustomID: s.CustomID, Description: s.Description}
	if len(s.Items) == 0 {
		pu.Amount = amountBody{Amount: amountOf(s.Total)}
		return pu, nil
	}
	itemTotal := money.New(0, s.Total.Currency)
	for _, it := range s.Items {
		if it.Quantity < 1 {
			return purchaseUnitBody{}, fmt.Errorf("paypal: item %q quantity must be >= 1", it.Name)
		}
		ext, err := it.Unit.Mul(it.Quantity)
		if err != nil {
			return purchaseUnitBody{}, err
		}
		if itemTotal, err = itemTotal.Add(ext); err != nil {
			return purchaseUnitBody{}, err
		}
		pu.Items = append(pu.Items, itemBody{
			Name: it.Name, SKU: it.SKU,
			Quantity:   strconv.FormatInt(it.Quantity, 10),
			UnitAmount: amountOf(it.Unit),
		})
	}
	shipping := s.Shipping
	if shipping.Currency == "" {
		shipping = money.New(0, s.Total.Currency)
	}
	sum, err := itemTotal.Add(shipping)
	if err != nil {
		return purchaseUnitBody{}, err
	}
	if sum != s.Total {
		return purchaseUnitBody{}, fmt.Errorf("%w: %s != %s + %s", ErrBreakdown, s.Total, itemTotal, shipping)
	}
	bd := &breakdownBody{ItemTotal: amountOf(itemTotal)}
	if shipping.Minor != 0 {
		a := amountOf(shipping)
		bd.Shipping = &a
	}
	pu.Amount = amountBody{Amount: amountOf(s.Total), Breakdown: bd}
	return pu, nil
}

type Link struct {
	Href   string `json:"href"`
	Rel    string `json:"rel"`
	Method string `json:"method"`
}

type Capture struct {
	ID            string `json:"id"`
	Status        string `json:"status"`
	Amount        Amount `json:"amount"`
	InvoiceID     string `json:"invoice_id"`
	CustomID      string `json:"custom_id"`
	StatusDetails *struct {
		Reason string `json:"reason"`
	} `json:"status_details"`
}

type Order struct {
	ID            string `json:"id"`
	Status        string `json:"status"`
	Links         []Link `json:"links"`
	PurchaseUnits []struct {
		ReferenceID string `json:"reference_id"`
		InvoiceID   string `json:"invoice_id"`
		CustomID    string `json:"custom_id"`
		Amount      Amount `json:"amount"`
		Payments    struct {
			Captures []Capture `json:"captures"`
		} `json:"payments"`
	} `json:"purchase_units"`
	PaymentSource *struct {
		PayPal *struct {
			EmailAddress string `json:"email_address"`
			AccountID    string `json:"account_id"`
			Attributes   *struct {
				Vault *struct {
					ID     string `json:"id"`
					Status string `json:"status"`
				} `json:"vault"`
			} `json:"attributes"`
		} `json:"paypal"`
	} `json:"payment_source"`
}

// Order statuses from the Orders v2 schema.
const (
	OrderCreated             = "CREATED"
	OrderApproved            = "APPROVED"
	OrderCompleted           = "COMPLETED"
	OrderPayerActionRequired = "PAYER_ACTION_REQUIRED"
	OrderVoided              = "VOIDED"
)

// Link returns the href for rel (e.g. "payer-action", "approve").
func (o Order) Link(rel string) string {
	for _, l := range o.Links {
		if l.Rel == rel {
			return l.Href
		}
	}
	return ""
}

// Captures flattens captures across purchase units.
func (o Order) Captures() []Capture {
	var out []Capture
	for _, pu := range o.PurchaseUnits {
		out = append(out, pu.Payments.Captures...)
	}
	return out
}

type storedCredential struct {
	PaymentInitiator string `json:"payment_initiator"`
	UsagePattern     string `json:"usage_pattern"`
}

type experienceContext struct {
	ReturnURL          string `json:"return_url"`
	CancelURL          string `json:"cancel_url"`
	UserAction         string `json:"user_action,omitempty"`
	ShippingPreference string `json:"shipping_preference,omitempty"`
}

type paypalSource struct {
	VaultID           string             `json:"vault_id,omitempty"`
	StoredCredential  *storedCredential  `json:"stored_credential,omitempty"`
	ExperienceContext *experienceContext `json:"experience_context,omitempty"`
}

type createOrderBody struct {
	Intent        string             `json:"intent"`
	PurchaseUnits []purchaseUnitBody `json:"purchase_units"`
	PaymentSource map[string]any     `json:"payment_source,omitempty"`
}

// CallOptions carries per-call controls.
type CallOptions struct {
	IdempotencyKey string // required for every state-changing call
	// MockApplicationCode triggers PayPal negative testing, e.g.
	// "INSTRUMENT_DECLINED". Sandbox only; rejected on live.
	MockApplicationCode string
}

func (o CallOptions) mock() string {
	if o.MockApplicationCode == "" {
		return ""
	}
	return `{"mock_application_codes":"` + o.MockApplicationCode + `"}`
}

var ErrMissingIdempotencyKey = errors.New("paypal: state-changing call requires an idempotency key")

// CreateVaultedOrder charges a saved PayPal wallet without the buyer present.
// stored_credential values are from the Orders v2 schema: MERCHANT ("without
// the active engagement of customer") and IMMEDIATE ("on-demand ... variable
// amount, variable frequency"). PayPal's v6 sample notes such an order is
// charged immediately (COMPLETED).
func (c *Client) CreateVaultedOrder(ctx context.Context, vaultID string, spec OrderSpec, opt CallOptions) (Order, error) {
	if vaultID == "" {
		return Order{}, errors.New("paypal: vault_id required")
	}
	src := paypalSource{
		VaultID:          vaultID,
		StoredCredential: &storedCredential{PaymentInitiator: "MERCHANT", UsagePattern: "IMMEDIATE"},
	}
	return c.createOrder(ctx, spec, src, opt)
}

// CreateApprovalOrder creates an order the buyer must approve via the
// payer-action link. Used for the INSTRUMENT_DECLINED recovery loop, where
// PayPal guidance is to let the buyer choose another funding source rather
// than retry the declined instrument.
func (c *Client) CreateApprovalOrder(ctx context.Context, spec OrderSpec, returnURL, cancelURL string, opt CallOptions) (Order, error) {
	src := paypalSource{ExperienceContext: &experienceContext{
		ReturnURL: returnURL, CancelURL: cancelURL,
		UserAction: "PAY_NOW", ShippingPreference: "NO_SHIPPING",
	}}
	return c.createOrder(ctx, spec, src, opt)
}

func (c *Client) createOrder(ctx context.Context, spec OrderSpec, src paypalSource, opt CallOptions) (Order, error) {
	if opt.IdempotencyKey == "" {
		return Order{}, ErrMissingIdempotencyKey
	}
	pu, err := spec.purchaseUnit()
	if err != nil {
		return Order{}, err
	}
	body := createOrderBody{
		Intent:        "CAPTURE",
		PurchaseUnits: []purchaseUnitBody{pu},
		PaymentSource: map[string]any{"paypal": src},
	}
	var o Order
	err = c.do(ctx, request{
		method: http.MethodPost, path: "/v2/checkout/orders", body: body,
		idempotencyKey: opt.IdempotencyKey, mockResponse: opt.mock(),
		prefer: "return=representation", retryable: true,
	}, &o)
	return o, err
}

func (c *Client) GetOrder(ctx context.Context, id string) (Order, error) {
	var o Order
	err := c.do(ctx, request{method: http.MethodGet, path: "/v2/checkout/orders/" + url.PathEscape(id), retryable: true}, &o)
	return o, err
}

func (c *Client) CaptureOrder(ctx context.Context, id string, opt CallOptions) (Order, error) {
	if opt.IdempotencyKey == "" {
		return Order{}, ErrMissingIdempotencyKey
	}
	var o Order
	err := c.do(ctx, request{
		method: http.MethodPost, path: "/v2/checkout/orders/" + url.PathEscape(id) + "/capture",
		body: struct{}{}, idempotencyKey: opt.IdempotencyKey, mockResponse: opt.mock(),
		prefer: "return=representation", retryable: true,
	}, &o)
	return o, err
}
