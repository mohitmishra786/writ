package quote

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/mohitmishra786/paypal-ai-hackathon/internal/money"
)

var now = time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)

func sample() Quote {
	return Quote{
		ID: "q_1", SupplierRef: "supplier:bean-co", BuyerRef: "mandate:01",
		Lines: []Line{
			{SKU: "pitcher-20oz", Title: "Milk Frothing Pitcher", Qty: 6, UnitPrice: money.New(2625, money.USD)},
			{SKU: "grinder-pro", Title: "Smart Grinder Pro", Qty: 0, UnitPrice: money.New(15995, money.USD)},
		},
		Shipping:  money.New(2850, money.USD),
		Total:     money.New(18600, money.USD),
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(10 * time.Minute).Unix(),
	}
}

func keys(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func TestSignVerify(t *testing.T) {
	pub, priv := keys(t)
	s, err := Sign(sample(), priv)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Verify(pub, now); err != nil {
		t.Fatal(err)
	}
}

func TestTamperedTotalFails(t *testing.T) {
	pub, priv := keys(t)
	s, _ := Sign(sample(), priv)
	s.Quote.Total = money.New(480000, money.USD)
	if err := s.Verify(pub, now); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("got %v", err)
	}
}

func TestAttackerKeyFails(t *testing.T) {
	trusted, _ := keys(t)
	_, attacker := keys(t)
	s, _ := Sign(sample(), attacker)
	if err := s.Verify(trusted, now); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("got %v", err)
	}
}

func TestInconsistentArithmeticFails(t *testing.T) {
	pub, priv := keys(t)
	q := sample()
	q.Total = money.New(18601, money.USD)
	s, _ := Sign(q, priv)
	if err := s.Verify(pub, now); !errors.Is(err, ErrTotal) {
		t.Fatalf("got %v", err)
	}
}

func TestExpiry(t *testing.T) {
	pub, priv := keys(t)
	s, _ := Sign(sample(), priv)
	if err := s.Verify(pub, now.Add(11*time.Minute)); !errors.Is(err, ErrExpired) {
		t.Fatalf("got %v", err)
	}
}

func TestHashStable(t *testing.T) {
	a, _ := sample().Hash()
	b, _ := sample().Hash()
	if a != b {
		t.Fatal("hash not stable")
	}
}
