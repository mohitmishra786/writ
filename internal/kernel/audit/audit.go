// Package audit defines Mandate's tamper-evident log: each entry's hash is
// SHA-256(prev_hash || JCS(entry)), and periodic checkpoints are Ed25519
// signatures over the chain head. Anyone holding the public key can verify
// the whole history offline (mandatectl verify).
package audit

import (
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/mohitmishra786/paypal-ai-hackathon/internal/canon"
)

// Entry is the hashed content of one audit row. Times are Unix microseconds
// so the canonical encoding stays integer-only.
type Entry struct {
	Seq         int64          `json:"seq"`
	TSMicros    int64          `json:"ts_us"`
	Kind        string         `json:"kind"`
	MandateID   string         `json:"mandate_id,omitempty"`
	AgentID     string         `json:"agent_id,omitempty"`
	IntentID    string         `json:"intent_id,omitempty"`
	StateFrom   string         `json:"state_from,omitempty"`
	StateTo     string         `json:"state_to,omitempty"`
	Reasons     []string       `json:"reasons,omitempty"`
	AmountMinor int64          `json:"amount_minor,omitempty"`
	Currency    string         `json:"currency,omitempty"`
	PayeeRef    string         `json:"payee_ref,omitempty"`
	PayPal      map[string]any `json:"paypal,omitempty"`
	PolicyHash  string         `json:"policy_hash,omitempty"`
	TokenJTI    string         `json:"token_jti,omitempty"`
	QuoteHash   string         `json:"quote_hash,omitempty"`
	TraceDigest string         `json:"trace_digest,omitempty"`
	Detail      map[string]any `json:"detail,omitempty"`
}

// Kinds recorded by the kernel.
const (
	KindMandateCreated   = "MANDATE_CREATED"
	KindMandateActivated = "MANDATE_ACTIVATED"
	KindMandatePaused    = "MANDATE_PAUSED"
	KindIntentReceived   = "INTENT_RECEIVED"
	KindPolicyDecision   = "POLICY_DECISION"
	KindTokenMinted      = "TOKEN_MINTED"
	KindTransition       = "TRANSITION"
	KindPayPalRequest    = "PAYPAL_REQUEST"
	KindPayPalResponse   = "PAYPAL_RESPONSE"
	KindWebhookApplied   = "WEBHOOK_APPLIED"
	KindStaleEvent       = "STALE_EVENT"
	KindSettlement       = "SETTLEMENT"
	KindCheckpoint       = "CHECKPOINT"
)

// Genesis is the prev_hash of the first entry.
var Genesis = make([]byte, sha256.Size)

func Hash(prev []byte, e Entry) ([]byte, error) {
	if len(prev) != sha256.Size {
		return nil, errors.New("audit: prev hash must be 32 bytes")
	}
	b, err := canon.Marshal(e)
	if err != nil {
		return nil, err
	}
	h := sha256.New()
	h.Write(prev)
	h.Write(b)
	return h.Sum(nil), nil
}

// checkpointMessage domain-separates checkpoint signatures from any other
// use of the kernel key.
func checkpointMessage(seq int64, head []byte) []byte {
	return fmt.Appendf(nil, "mandate-audit-checkpoint-v1:%d:%x", seq, head)
}

func SignCheckpoint(key ed25519.PrivateKey, seq int64, head []byte) []byte {
	return ed25519.Sign(key, checkpointMessage(seq, head))
}

// Row is an entry as stored, with its chain links and optional checkpoint.
type Row struct {
	Entry     Entry
	PrevHash  []byte
	Hash      []byte
	Signature []byte
}

type VerifyError struct {
	Seq    int64
	Reason string
}

func (e *VerifyError) Error() string { return fmt.Sprintf("audit: seq %d: %s", e.Seq, e.Reason) }

// Report summarises a successful verification.
type Report struct {
	Entries        int
	Checkpoints    int
	HeadSeq        int64
	HeadHash       []byte
	LastCheckpoint int64
}

// Verify recomputes the chain from genesis (rows must start at seq 1 and be
// gapless) and checks every checkpoint signature.
func Verify(rows []Row, pub ed25519.PublicKey) (Report, error) {
	prev := Genesis
	var r Report
	for i, row := range rows {
		want := int64(i + 1)
		if row.Entry.Seq != want {
			return r, &VerifyError{Seq: row.Entry.Seq, Reason: fmt.Sprintf("gap: expected seq %d", want)}
		}
		if string(row.PrevHash) != string(prev) {
			return r, &VerifyError{Seq: want, Reason: "prev_hash does not link to previous entry"}
		}
		h, err := Hash(prev, row.Entry)
		if err != nil {
			return r, &VerifyError{Seq: want, Reason: err.Error()}
		}
		if string(h) != string(row.Hash) {
			return r, &VerifyError{Seq: want, Reason: "content does not match stored hash (tampered)"}
		}
		if len(row.Signature) > 0 {
			if !ed25519.Verify(pub, checkpointMessage(want, h), row.Signature) {
				return r, &VerifyError{Seq: want, Reason: "checkpoint signature invalid"}
			}
			r.Checkpoints++
			r.LastCheckpoint = want
		}
		prev = h
		r.Entries++
		r.HeadSeq, r.HeadHash = want, h
	}
	return r, nil
}

func Micros(t time.Time) int64 { return t.UTC().UnixMicro() }
