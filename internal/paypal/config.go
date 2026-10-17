// Package paypal is Mandate's hand-written client for the PayPal REST APIs it
// uses: OAuth2, Orders v2, Vault v3, Payouts v1 and Webhooks v1. It is the only
// package that ever sees the client secret.
//
// Every request shape here is taken from PayPal's published OpenAPI schemas
// or the official v6 sample integration; see docs/architecture/BLUEPRINT.md §16.
package paypal

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

type Env string

const (
	Sandbox Env = "sandbox"
	Live    Env = "live"
)

var baseURLs = map[Env]string{
	Sandbox: "https://api-m.sandbox.paypal.com",
	Live:    "https://api-m.paypal.com",
}

type Config struct {
	Env          Env
	BaseURL      string
	ClientID     string
	ClientSecret string
}

var ErrConfig = errors.New("paypal: invalid configuration")

// Validate refuses to guess. paypal/paypal-js#945 silently fell back to the
// sandbox when the environment was omitted; here the environment is
// mandatory and must agree with the base URL.
func (c Config) Validate() error {
	want, ok := baseURLs[c.Env]
	if !ok {
		return fmt.Errorf("%w: PAYPAL_ENV must be %q or %q, got %q", ErrConfig, Sandbox, Live, c.Env)
	}
	if strings.TrimRight(c.BaseURL, "/") != want {
		return fmt.Errorf("%w: PAYPAL_BASE_URL %q does not match PAYPAL_ENV %q (expected %q)", ErrConfig, c.BaseURL, c.Env, want)
	}
	if c.ClientID == "" || c.ClientSecret == "" {
		return fmt.Errorf("%w: PAYPAL_CLIENT_ID and PAYPAL_CLIENT_SECRET are required", ErrConfig)
	}
	return nil
}

func ConfigFromEnv() (Config, error) {
	c := Config{
		Env:          Env(os.Getenv("PAYPAL_ENV")),
		BaseURL:      os.Getenv("PAYPAL_BASE_URL"),
		ClientID:     os.Getenv("PAYPAL_CLIENT_ID"),
		ClientSecret: os.Getenv("PAYPAL_CLIENT_SECRET"),
	}
	return c, c.Validate()
}
