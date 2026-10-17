package paypal

import (
	"errors"
	"fmt"
)

// Issue values documented in the Orders v2 error reference.
const (
	IssueInstrumentDeclined     = "INSTRUMENT_DECLINED"
	IssueDuplicateInvoiceID     = "DUPLICATE_INVOICE_ID"
	IssueNotEnabledToVault      = "NOT_ENABLED_TO_VAULT_PAYMENT_SOURCE"
	IssueTransactionRefused     = "TRANSACTION_REFUSED"
	IssueOrderAlreadyCaptured   = "ORDER_ALREADY_CAPTURED"
	IssuePayerActionRequired    = "PAYER_ACTION_REQUIRED"
	IssueInvalidVaultID         = "INVALID_VAULT_ID"
	IssueMismatchedVaultPayment = "MISMATCHED_VAULT_ID_TO_PAYMENT_SOURCE"
	IssueOrderNotApproved       = "ORDER_NOT_APPROVED"
	IssueMaxPaymentAttempts     = "MAX_NUMBER_OF_PAYMENT_ATTEMPTS_EXCEEDED"
)

type ErrorDetail struct {
	Field       string `json:"field,omitempty"`
	Value       string `json:"value,omitempty"`
	Location    string `json:"location,omitempty"`
	Issue       string `json:"issue"`
	Description string `json:"description,omitempty"`
}

// APIError is a non-2xx PayPal response. DebugID must be logged on every
// failure (PayPal RulesHub error-handling guidance).
type APIError struct {
	Status  int           `json:"-"`
	Name    string        `json:"name"`
	Message string        `json:"message"`
	DebugID string        `json:"debug_id"`
	Details []ErrorDetail `json:"details"`
	// OAuth endpoints use a different error envelope.
	OAuthError       string `json:"error"`
	OAuthDescription string `json:"error_description"`
}

func (e *APIError) Error() string {
	if e.OAuthError != "" {
		return fmt.Sprintf("paypal: %d %s: %s", e.Status, e.OAuthError, e.OAuthDescription)
	}
	issue := ""
	if len(e.Details) > 0 {
		issue = " [" + e.Details[0].Issue + "]"
	}
	return fmt.Sprintf("paypal: %d %s%s: %s (debug_id=%s)", e.Status, e.Name, issue, e.Message, e.DebugID)
}

// HasIssue reports whether any detail carries the given issue code.
func (e *APIError) HasIssue(issue string) bool {
	for _, d := range e.Details {
		if d.Issue == issue {
			return true
		}
	}
	return false
}

// IsIssue unwraps err and checks for a documented issue code.
func IsIssue(err error, issue string) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.HasIssue(issue)
}

// ErrAmbiguous means the request may or may not have taken effect (timeout,
// connection reset, or 5xx after all retries). Callers move the intent to
// UNKNOWN and resolve it by replaying the same idempotency key or by lookup.
var ErrAmbiguous = errors.New("paypal: outcome unknown")

// ErrSandboxOnly guards negative-testing features from ever reaching live.
var ErrSandboxOnly = errors.New("paypal: PayPal-Mock-Response is sandbox-only")
