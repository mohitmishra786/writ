package paypal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Client struct {
	cfg  Config
	http *http.Client

	maxAttempts int
	baseBackoff time.Duration
	sleep       func(context.Context, time.Duration) error

	mu     sync.Mutex
	token  string
	expiry time.Time
}

type Option func(*Client)

func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// WithRetry overrides attempt count and base backoff (tests use tiny values).
func WithRetry(attempts int, base time.Duration) Option {
	return func(c *Client) { c.maxAttempts, c.baseBackoff = attempts, base }
}

func New(cfg Config, opts ...Option) (*Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	c := &Client{
		cfg:         cfg,
		http:        &http.Client{Timeout: 30 * time.Second},
		maxAttempts: 4,
		baseBackoff: 300 * time.Millisecond,
		sleep:       sleepCtx,
	}
	for _, o := range opts {
		o(c)
	}
	return c, nil
}

func (c *Client) Env() Env { return c.cfg.Env }

// request describes one PayPal call. Retryable must only be true when a
// replay cannot cause a second effect: GETs, calls carrying an idempotency
// key, or calls whose body carries PayPal's own dedupe key (sender_batch_id).
type request struct {
	method         string
	path           string
	body           any
	idempotencyKey string
	mockResponse   string
	prefer         string
	retryable      bool
}

// tokenRefreshMargin refreshes early so an in-flight call never presents an
// expired token. PayPal guidance: read expires_in rather than assuming a TTL.
const tokenRefreshMargin = 5 * time.Minute

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
	AppID       string `json:"app_id"`
	Scope       string `json:"scope"`
}

func (c *Client) accessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Now().Before(c.expiry) {
		return c.token, nil
	}
	tr, err := c.oauth(ctx, url.Values{"grant_type": {"client_credentials"}})
	if err != nil {
		return "", err
	}
	ttl := time.Duration(tr.ExpiresIn) * time.Second
	if ttl > 2*tokenRefreshMargin {
		ttl -= tokenRefreshMargin
	} else {
		ttl /= 2
	}
	c.token, c.expiry = tr.AccessToken, time.Now().Add(ttl)
	return c.token, nil
}

func (c *Client) oauth(ctx context.Context, form url.Values) (tokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/v1/oauth2/token", strings.NewReader(form.Encode()))
	if err != nil {
		return tokenResponse{}, err
	}
	req.SetBasicAuth(c.cfg.ClientID, c.cfg.ClientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return tokenResponse{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return tokenResponse{}, err
	}
	if resp.StatusCode/100 != 2 {
		return tokenResponse{}, decodeError(resp, raw)
	}
	var tr tokenResponse
	if err := json.Unmarshal(raw, &tr); err != nil {
		return tokenResponse{}, err
	}
	return tr, nil
}

// do executes r and decodes a 2xx JSON body into out (if non-nil).
func (c *Client) do(ctx context.Context, r request, out any) error {
	if r.mockResponse != "" && c.cfg.Env != Sandbox {
		return ErrSandboxOnly
	}
	var payload []byte
	if r.body != nil {
		var err error
		if payload, err = json.Marshal(r.body); err != nil {
			return err
		}
	}
	attempts := 1
	if r.retryable {
		attempts = c.maxAttempts
	}
	var lastErr error
	authRetried := false
	for attempt := 0; ; attempt++ {
		retry, err := c.once(ctx, r, payload, out)
		if err == nil {
			return nil
		}
		lastErr = err
		// A 401 is rejected before any effect, so one token refresh is
		// always safe, even for non-retryable requests.
		var ae *APIError
		if errors.As(err, &ae) && ae.Status == http.StatusUnauthorized && !authRetried {
			authRetried = true
			continue
		}
		if !retry {
			return err
		}
		if attempt+1 >= attempts {
			break
		}
		if err := c.sleep(ctx, c.backoff(attempt+1, lastErr)); err != nil {
			return err
		}
	}
	if r.method == http.MethodGet {
		return lastErr
	}
	return fmt.Errorf("%w: %w", ErrAmbiguous, lastErr)
}

// once performs a single attempt and reports whether a retry is sensible.
func (c *Client) once(ctx context.Context, r request, payload []byte, out any) (bool, error) {
	tok, err := c.accessToken(ctx)
	if err != nil {
		return isTransient(err), err
	}
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, r.method, c.cfg.BaseURL+r.path, body)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if r.idempotencyKey != "" {
		req.Header.Set("PayPal-Request-Id", r.idempotencyKey)
	}
	if r.mockResponse != "" {
		req.Header.Set("PayPal-Mock-Response", r.mockResponse)
	}
	if r.prefer != "" {
		req.Header.Set("Prefer", r.prefer)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return isTransient(err), err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return true, err
	}
	switch {
	case resp.StatusCode/100 == 2:
		if out != nil && len(raw) > 0 {
			return false, json.Unmarshal(raw, out)
		}
		return false, nil
	case resp.StatusCode == http.StatusUnauthorized:
		c.mu.Lock()
		c.token = ""
		c.mu.Unlock()
		return false, decodeError(resp, raw)
	default:
		apiErr := decodeError(resp, raw)
		return resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500, apiErr
	}
}

type retryAfterError struct {
	*APIError
	after time.Duration
}

func (e *retryAfterError) Unwrap() error { return e.APIError }

func decodeError(resp *http.Response, raw []byte) error {
	ae := &APIError{Status: resp.StatusCode}
	_ = json.Unmarshal(raw, ae)
	if ae.DebugID == "" {
		ae.DebugID = resp.Header.Get("Paypal-Debug-Id")
	}
	if ae.Name == "" && ae.OAuthError == "" {
		ae.Name = http.StatusText(resp.StatusCode)
	}
	if s := resp.Header.Get("Retry-After"); s != "" {
		if secs, err := strconv.Atoi(s); err == nil && secs >= 0 {
			return &retryAfterError{APIError: ae, after: time.Duration(secs) * time.Second}
		}
	}
	return ae
}

const maxBackoff = 10 * time.Second

func (c *Client) backoff(attempt int, lastErr error) time.Duration {
	var ra *retryAfterError
	if errors.As(lastErr, &ra) && ra.after > 0 {
		return min(ra.after, maxBackoff)
	}
	d := c.baseBackoff << (attempt - 1)
	jitter := time.Duration(rand.Int64N(int64(d)/2 + 1))
	return min(d+jitter, maxBackoff)
}

func isTransient(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var ae *APIError
	if errors.As(err, &ae) {
		return ae.Status == http.StatusTooManyRequests || ae.Status >= 500
	}
	return true
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
