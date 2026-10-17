package money

import (
	"errors"
	"testing"
)

func TestParseDecimalRoundTrip(t *testing.T) {
	cases := map[string]int64{"186.00": 18600, "0.05": 5, "0.5": 50, "12": 1200, "-3.10": -310}
	for in, want := range cases {
		m, err := ParseDecimal(in, USD)
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if m.Minor != want {
			t.Fatalf("%s: got %d want %d", in, m.Minor, want)
		}
	}
	m, _ := ParseDecimal("0.5", USD)
	if m.Decimal() != "0.50" {
		t.Fatalf("got %s", m.Decimal())
	}
}

func TestParseDecimalRejectsExcessPrecision(t *testing.T) {
	for _, in := range []string{"0.015", "1.", ".5", "1e3", "abc", "", "1.2.3"} {
		if _, err := ParseDecimal(in, USD); err == nil {
			t.Fatalf("%q: expected error", in)
		}
	}
	if _, err := ParseDecimal("10.5", "JPY"); err == nil {
		t.Fatal("JPY has no minor unit")
	}
}

// PR #110: unit price $0.05, qty 3, 10% tax. Per-unit tax rounds to $0.01, so
// the only consistent totals are item 0.15 + tax 0.03 = 0.18.
func TestTaxBreakdownConsistency(t *testing.T) {
	unit := New(5, USD)
	unitTax := New(1, USD)
	items, _ := unit.Mul(3)
	tax, _ := unitTax.Mul(3)
	total, _ := items.Add(tax)
	if total.Decimal() != "0.18" {
		t.Fatalf("got %s", total.Decimal())
	}
}

func TestFromFloat(t *testing.T) {
	cases := map[float64]int64{19.99: 1999, 0.1 + 0.2: 30, 4499.95: 449995, 26.245: 2625}
	for in, want := range cases {
		m, err := FromFloat(in, USD)
		if err != nil || m.Minor != want {
			t.Fatalf("%v: got %d err %v want %d", in, m.Minor, err, want)
		}
	}
}

func TestCurrencyMismatch(t *testing.T) {
	if _, err := New(1, USD).Add(New(1, "EUR")); !errors.Is(err, ErrCurrencyMismatch) {
		t.Fatal("expected mismatch")
	}
}

func TestOverflow(t *testing.T) {
	big := New(1<<62, USD)
	if _, err := big.Add(big); !errors.Is(err, ErrOverflow) {
		t.Fatal("expected overflow")
	}
	if _, err := big.Mul(4); !errors.Is(err, ErrOverflow) {
		t.Fatal("expected overflow")
	}
}
