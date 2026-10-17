package token

import (
	"crypto/ed25519"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mohitmishra786/paypal-ai-hackathon/internal/money"
)

const seed = "9f1c2b3a4d5e6f708192a3b4c5d6e7f8091a2b3c4d5e6f708192a3b4c5d6e7f8"

var t0 = time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)

func fixture(t *testing.T) (*Minter, *Verifier) {
	t.Helper()
	m, err := NewMinter(seed, "k1")
	if err != nil {
		t.Fatal(err)
	}
	m.now = func() time.Time { return t0 }
	v, err := NewVerifier(map[string]ed25519.PublicKey{"k1": m.PublicKey()})
	if err != nil {
		t.Fatal(err)
	}
	v.now = func() time.Time { return t0.Add(time.Minute) }
	return m, v
}

func capability() Capability {
	return Capability{
		JTI: "01JA0000000000000000000000", Subject: "agent:procure-01",
		MandateID: "mandate_01", IntentID: "intent_01", Op: OpVaultCharge,
		PayeeRef: "supplier:bean-co", Ceiling: money.New(18600, money.USD),
		PolicyHash: "sha256:abc", QuoteHash: "sha256:def",
	}
}

func TestMintVerifyRoundTrip(t *testing.T) {
	m, v := fixture(t)
	raw, minted, err := m.Mint(capability(), 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(raw, "v4.public.") {
		t.Fatalf("not a PASETO v4.public token: %s", raw[:20])
	}
	got, err := v.Verify(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got != minted {
		t.Fatalf("round trip mismatch:\n got %+v\nwant %+v", got, minted)
	}
}

func TestExpiredRejected(t *testing.T) {
	m, v := fixture(t)
	raw, _, _ := m.Mint(capability(), time.Minute)
	v.now = func() time.Time { return t0.Add(2 * time.Minute) }
	if _, err := v.Verify(raw); !errors.Is(err, ErrInvalidCapability) {
		t.Fatalf("got %v", err)
	}
}

func TestTamperedTokenRejected(t *testing.T) {
	m, v := fixture(t)
	raw, _, _ := m.Mint(capability(), time.Minute)
	parts := strings.Split(raw, ".")
	b := []byte(parts[2])
	b[10] ^= 1
	parts[2] = string(b)
	if _, err := v.Verify(strings.Join(parts, ".")); err == nil {
		t.Fatal("tampered token accepted")
	}
}

func TestForeignKeyRejected(t *testing.T) {
	m, _ := fixture(t)
	raw, _, _ := m.Mint(capability(), time.Minute)
	other, _ := NewMinter(strings.Repeat("11", 32), "k1")
	v, _ := NewVerifier(map[string]ed25519.PublicKey{"k1": other.PublicKey()})
	v.now = func() time.Time { return t0 }
	if _, err := v.Verify(raw); !errors.Is(err, ErrInvalidCapability) {
		t.Fatal("token verified under the wrong key")
	}
}

func TestUnknownKeyID(t *testing.T) {
	m, _ := fixture(t)
	raw, _, _ := m.Mint(capability(), time.Minute)
	v, _ := NewVerifier(map[string]ed25519.PublicKey{"k2": m.PublicKey()})
	if _, err := v.Verify(raw); !errors.Is(err, ErrInvalidCapability) {
		t.Fatal("unknown kid accepted")
	}
}

func TestMintValidation(t *testing.T) {
	m, _ := fixture(t)
	if _, _, err := m.Mint(capability(), 11*time.Minute); !errors.Is(err, ErrInvalidCapability) {
		t.Fatal("ttl above max accepted")
	}
	c := capability()
	c.Ceiling = money.New(0, money.USD)
	if _, _, err := m.Mint(c, time.Minute); !errors.Is(err, ErrInvalidCapability) {
		t.Fatal("zero ceiling accepted")
	}
	if _, err := NewMinter("abcd", "k"); err == nil {
		t.Fatal("short seed accepted")
	}
}

func TestNarrow(t *testing.T) {
	parent := capability()
	parent.Expires = t0.Add(10 * time.Minute)
	child := parent
	child.Ceiling = money.New(40, money.USD)
	child.Expires = t0.Add(5 * time.Minute)
	if err := Narrow(parent, child); err != nil {
		t.Fatal(err)
	}
	child.Ceiling = money.New(18601, money.USD)
	if err := Narrow(parent, child); err == nil {
		t.Fatal("child exceeding parent ceiling accepted")
	}
	child.Ceiling = money.New(40, money.USD)
	child.Expires = t0.Add(11 * time.Minute)
	if err := Narrow(parent, child); err == nil {
		t.Fatal("child outliving parent accepted")
	}
}
