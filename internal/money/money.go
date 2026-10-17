// Package money represents amounts as int64 minor units. Conversion to and
// from decimal strings happens only here, so rounding bugs such as
// paypal/agent-toolkit PR #110 cannot appear elsewhere.
package money

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

type Currency string

const USD Currency = "USD"

// zeroDecimal lists ISO-4217 currencies PayPal treats as having no minor unit.
var zeroDecimal = map[Currency]bool{"JPY": true, "HUF": true, "TWD": true}

func (c Currency) exponent() int {
	if zeroDecimal[c] {
		return 0
	}
	return 2
}

func (c Currency) Valid() bool {
	if len(c) != 3 {
		return false
	}
	for _, r := range c {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return true
}

type Money struct {
	Minor    int64    `json:"minor"`
	Currency Currency `json:"currency"`
}

var (
	ErrCurrencyMismatch = errors.New("money: currency mismatch")
	ErrOverflow         = errors.New("money: overflow")
	ErrInvalid          = errors.New("money: invalid amount")
)

func New(minor int64, c Currency) Money { return Money{Minor: minor, Currency: c} }

func (m Money) IsZero() bool { return m.Minor == 0 }

func (m Money) Add(o Money) (Money, error) {
	if m.Currency != o.Currency {
		return Money{}, ErrCurrencyMismatch
	}
	s := m.Minor + o.Minor
	if (o.Minor > 0 && s < m.Minor) || (o.Minor < 0 && s > m.Minor) {
		return Money{}, ErrOverflow
	}
	return Money{Minor: s, Currency: m.Currency}, nil
}

func (m Money) Sub(o Money) (Money, error) {
	return m.Add(Money{Minor: -o.Minor, Currency: o.Currency})
}

func (m Money) Mul(qty int64) (Money, error) {
	if qty != 0 && (m.Minor*qty)/qty != m.Minor {
		return Money{}, ErrOverflow
	}
	return Money{Minor: m.Minor * qty, Currency: m.Currency}, nil
}

func (m Money) Cmp(o Money) (int, error) {
	if m.Currency != o.Currency {
		return 0, ErrCurrencyMismatch
	}
	switch {
	case m.Minor < o.Minor:
		return -1, nil
	case m.Minor > o.Minor:
		return 1, nil
	}
	return 0, nil
}

// Decimal renders the amount in PayPal's string format, e.g. "186.00".
func (m Money) Decimal() string {
	exp := m.Currency.exponent()
	if exp == 0 {
		return strconv.FormatInt(m.Minor, 10)
	}
	sign := ""
	v := m.Minor
	if v < 0 {
		sign = "-"
		v = -v
	}
	return fmt.Sprintf("%s%d.%02d", sign, v/100, v%100)
}

func (m Money) String() string { return m.Decimal() + " " + string(m.Currency) }

// ParseDecimal parses a PayPal decimal string exactly, rejecting values that
// carry more precision than the currency allows.
func ParseDecimal(s string, c Currency) (Money, error) {
	if !c.Valid() {
		return Money{}, fmt.Errorf("%w: currency %q", ErrInvalid, c)
	}
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	whole, frac, hasFrac := strings.Cut(s, ".")
	exp := c.exponent()
	if whole == "" || !digits(whole) || (hasFrac && (!digits(frac) || len(frac) > exp || frac == "")) {
		return Money{}, fmt.Errorf("%w: %q", ErrInvalid, s)
	}
	frac += strings.Repeat("0", exp-len(frac))
	n, err := strconv.ParseInt(whole+frac, 10, 64)
	if err != nil {
		return Money{}, fmt.Errorf("%w: %q", ErrOverflow, s)
	}
	if neg {
		n = -n
	}
	return Money{Minor: n, Currency: c}, nil
}

// FromFloat converts an untrusted float price (e.g. Channel3 offers) by
// rounding half away from zero to the nearest minor unit.
func FromFloat(f float64, c Currency) (Money, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return Money{}, ErrInvalid
	}
	scaled := math.Round(f * math.Pow10(c.exponent()))
	if math.Abs(scaled) > math.MaxInt64/2 {
		return Money{}, ErrOverflow
	}
	return Money{Minor: int64(scaled), Currency: c}, nil
}

func digits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
