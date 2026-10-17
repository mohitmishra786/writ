// Package quote defines supplier quotes signed with Ed25519 over their JCS
// encoding. Verification always uses the key bound into the mandate, never a
// key carried in the message, so a forged or injected quote cannot verify.
package quote

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/mohitmishra786/paypal-ai-hackathon/internal/canon"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/money"
)

type Line struct {
	SKU       string      `json:"sku"`
	Title     string      `json:"title"`
	Qty       int64       `json:"qty"`
	UnitPrice money.Money `json:"unit_price"`
}

type Quote struct {
	ID          string      `json:"id"`
	SupplierRef string      `json:"supplier_ref"`
	BuyerRef    string      `json:"buyer_ref"`
	Lines       []Line      `json:"lines"`
	Shipping    money.Money `json:"shipping"`
	Total       money.Money `json:"total"`
	IssuedAt    int64       `json:"issued_at"`
	ExpiresAt   int64       `json:"expires_at"`
}

type Signed struct {
	Quote     Quote  `json:"quote"`
	Signature []byte `json:"signature"`
}

var (
	ErrBadSignature = errors.New("quote: signature does not verify")
	ErrTotal        = errors.New("quote: total does not equal lines plus shipping")
	ErrExpired      = errors.New("quote: expired")
)

// ComputeTotal sums lines and shipping in integer minor units.
func (q Quote) ComputeTotal() (money.Money, error) {
	total := money.New(0, q.Total.Currency)
	for _, l := range q.Lines {
		ext, err := l.UnitPrice.Mul(l.Qty)
		if err != nil {
			return money.Money{}, err
		}
		if total, err = total.Add(ext); err != nil {
			return money.Money{}, err
		}
	}
	return total.Add(q.Shipping)
}

func (q Quote) Bytes() ([]byte, error) { return canon.Marshal(q) }

func (q Quote) Hash() (string, error) {
	b, err := q.Bytes()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func Sign(q Quote, key ed25519.PrivateKey) (Signed, error) {
	b, err := q.Bytes()
	if err != nil {
		return Signed{}, err
	}
	return Signed{Quote: q, Signature: ed25519.Sign(key, b)}, nil
}

// Verify checks the signature against trusted, internal arithmetic and expiry.
func (s Signed) Verify(trusted ed25519.PublicKey, now time.Time) error {
	b, err := s.Quote.Bytes()
	if err != nil {
		return err
	}
	if len(trusted) != ed25519.PublicKeySize || !ed25519.Verify(trusted, b, s.Signature) {
		return ErrBadSignature
	}
	total, err := s.Quote.ComputeTotal()
	if err != nil {
		return err
	}
	if total != s.Quote.Total {
		return ErrTotal
	}
	if now.Unix() >= s.Quote.ExpiresAt {
		return ErrExpired
	}
	return nil
}
