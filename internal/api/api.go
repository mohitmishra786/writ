// Package api is Mandate's HTTP surface: the console/agent REST API, the
// live event stream, the PayPal webhook receiver and the buyer return URLs.
package api

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mohitmishra786/paypal-ai-hackathon/internal/kernel"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/kernel/audit"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/kernel/fsm"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/kernel/policy"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/kernel/quote"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/money"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/paypal"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/store"
)

type Config struct {
	PublicBaseURL   string
	ConsoleURL      string   // where the buyer lands after PayPal approval
	AllowedOrigins  []string // CORS for the console
	ClientTokenHost string   // domains[] for v6 client tokens; empty for local dev
	PayPalClientID  string
	WebhookID       string
	KeyID           string
	PublicKey       ed25519.PublicKey
}

type Server struct {
	cfg    Config
	svc    *kernel.Service
	st     *store.Store
	pp     *paypal.Client
	hub    *Hub
	log    *slog.Logger
	limits *limiter
	extra  map[string]http.Handler
}

func New(cfg Config, svc *kernel.Service, st *store.Store, pp *paypal.Client, hub *Hub, log *slog.Logger) *Server {
	return &Server{cfg: cfg, svc: svc, st: st, pp: pp, hub: hub, log: log, limits: newLimiter(30, time.Minute), extra: map[string]http.Handler{}}
}

// Mount adds an extra handler (e.g. the MCP endpoint) under pattern.
func (s *Server) Mount(pattern string, h http.Handler) { s.extra[pattern] = h }

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /.well-known/mandate-keys.json", s.keys)

	mux.HandleFunc("GET /v1/mandates", s.listMandates)
	mux.HandleFunc("POST /v1/mandates", s.limited(s.createMandate))
	mux.HandleFunc("GET /v1/mandates/{id}", s.getMandate)
	mux.HandleFunc("POST /v1/mandates/{id}/vault/setup", s.limited(s.vaultSetup))
	mux.HandleFunc("POST /v1/mandates/{id}/vault/complete", s.limited(s.vaultComplete))
	mux.HandleFunc("POST /v1/mandates/{id}/settle", s.limited(s.settle))

	mux.HandleFunc("GET /v1/intents", s.listIntents)
	mux.HandleFunc("POST /v1/intents", s.limited(s.propose))
	mux.HandleFunc("GET /v1/intents/{id}", s.getIntent)
	mux.HandleFunc("POST /v1/intents/{id}/execute", s.limited(s.execute))
	mux.HandleFunc("POST /v1/intents/{id}/cosign", s.limited(s.cosign))

	mux.HandleFunc("GET /v1/audit", s.auditList)
	mux.HandleFunc("GET /v1/audit/verify", s.auditVerify)
	mux.HandleFunc("GET /v1/stream", s.stream)

	mux.HandleFunc("GET /v1/paypal/config", s.paypalConfig)
	mux.HandleFunc("GET /v1/paypal/client-token", s.limited(s.clientToken))
	mux.HandleFunc("POST /v1/paypal/webhooks", s.webhook)
	mux.HandleFunc("GET /v1/paypal/return", s.paypalReturn)
	mux.HandleFunc("GET /v1/paypal/cancel", s.paypalCancel)
	mux.HandleFunc("GET /a/{code}", s.shortLink)
	mux.HandleFunc("GET /vault/return", s.vaultReturn)
	mux.HandleFunc("GET /vault/cancel", s.plainPage("Saving your PayPal account was cancelled. Nothing was changed."))
	mux.HandleFunc("GET /approved", s.approvedPage)
	for p, h := range s.extra {
		mux.Handle(p, h)
	}
	return s.cors(s.recoverer(mux))
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if err := s.st.Pool.Ping(ctx); err != nil {
		writeErr(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "paypal_env": s.pp.Env()})
}

func (s *Server) keys(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"keys": []map[string]string{{
		"kid": s.cfg.KeyID, "alg": "Ed25519", "use": "PASETO v4.public capabilities and audit checkpoints",
		"public_key_hex": hex.EncodeToString(s.cfg.PublicKey),
	}}})
}

// mandateInput is the JSON shape for creating a mandate.
type mandateInput struct {
	PrincipalID    string   `json:"principal_id"`
	AgentID        string   `json:"agent_id"`
	Currency       string   `json:"currency"`
	EnvelopeMinor  int64    `json:"envelope_minor"`
	PeriodHours    int64    `json:"period_hours"`
	PerIntentMinor int64    `json:"per_intent_minor"`
	CosignMinor    int64    `json:"cosign_above_minor"`
	ValidDays      int64    `json:"valid_days"`
	Categories     []string `json:"categories"`
	Velocity       []struct {
		WindowMinutes  int64 `json:"window_minutes"`
		MaxCount       int   `json:"max_count"`
		MaxAmountMinor int64 `json:"max_amount_minor"`
	} `json:"velocity"`
	Payees []struct {
		Ref           string `json:"ref"`
		ReceiverEmail string `json:"receiver_email"`
		PublicKeyHex  string `json:"public_key_hex"`
	} `json:"payees"`
	Rules []policy.Rule `json:"rules"`
}

func (in mandateInput) toPolicy(now time.Time) (policy.Mandate, error) {
	cur := money.Currency(in.Currency)
	if cur == "" {
		cur = money.USD
	}
	if in.PeriodHours <= 0 || in.ValidDays <= 0 {
		return policy.Mandate{}, errors.New("period_hours and valid_days must be positive")
	}
	m := policy.Mandate{
		PrincipalID: in.PrincipalID, AgentID: in.AgentID,
		NotBefore: now.Add(-time.Minute).Unix(), NotAfter: now.Add(time.Duration(in.ValidDays) * 24 * time.Hour).Unix(),
		Envelope: money.New(in.EnvelopeMinor, cur), Period: time.Duration(in.PeriodHours) * time.Hour,
		PerIntentMax: money.New(in.PerIntentMinor, cur), CosignAbove: money.New(in.CosignMinor, cur),
		Categories: in.Categories, Rules: in.Rules,
	}
	for _, v := range in.Velocity {
		m.Velocity = append(m.Velocity, policy.VelocityRule{Window: time.Duration(v.WindowMinutes) * time.Minute, MaxCount: v.MaxCount, MaxAmount: v.MaxAmountMinor})
	}
	for _, p := range in.Payees {
		pk, err := hex.DecodeString(p.PublicKeyHex)
		if err != nil {
			return policy.Mandate{}, fmt.Errorf("payee %s: bad public key", p.Ref)
		}
		m.Payees = append(m.Payees, policy.Payee{Ref: p.Ref, ReceiverEmail: p.ReceiverEmail, PublicKey: pk})
	}
	return m, nil
}

func (s *Server) createMandate(w http.ResponseWriter, r *http.Request) {
	var in mandateInput
	if !decode(w, r, &in) {
		return
	}
	m, err := in.toPolicy(time.Now())
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	v, err := s.svc.CreateMandate(r.Context(), m)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

func (s *Server) listMandates(w http.ResponseWriter, r *http.Request) {
	v, err := s.svc.ListMandates(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"mandates": v})
}

func (s *Server) getMandate(w http.ResponseWriter, r *http.Request) {
	v, err := s.svc.GetMandate(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) vaultSetup(w http.ResponseWriter, r *http.Request) {
	base := strings.TrimRight(s.cfg.ConsoleURL, "/")
	st, err := s.svc.BeginVaultSetup(r.Context(), r.PathValue("id"), base+"/vault/return", base+"/vault/cancel")
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"setup_token_id": st.ID, "status": st.Status, "approve_url": st.ApproveURL()})
}

func (s *Server) vaultComplete(w http.ResponseWriter, r *http.Request) {
	v, err := s.svc.CompleteVaultSetup(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) settle(w http.ResponseWriter, r *http.Request) {
	res, err := s.svc.SettleMandate(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

type proposeInput struct {
	MandateID   string          `json:"mandate_id"`
	AgentID     string          `json:"agent_id"`
	PayeeRef    string          `json:"payee_ref"`
	AmountMinor int64           `json:"amount_minor"`
	Currency    string          `json:"currency"`
	Category    string          `json:"category"`
	Quote       *quote.Signed   `json:"quote"`
	Trace       json.RawMessage `json:"trace"`
}

func (s *Server) propose(w http.ResponseWriter, r *http.Request) {
	var in proposeInput
	if !decode(w, r, &in) {
		return
	}
	d, err := s.svc.Propose(r.Context(), kernel.Proposal{MandateID: in.MandateID, AgentID: in.AgentID, PayeeRef: in.PayeeRef,
		Amount: money.New(in.AmountMinor, money.Currency(in.Currency)), Category: in.Category, Quote: in.Quote, Trace: in.Trace})
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) listIntents(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	v, err := s.svc.ListIntents(r.Context(), r.URL.Query().Get("mandate_id"), limit)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"intents": v})
}

func (s *Server) getIntent(w http.ResponseWriter, r *http.Request) {
	v, err := s.svc.GetIntent(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) execute(w http.ResponseWriter, r *http.Request) {
	var in struct {
		SimulateDecline bool `json:"simulate_decline"`
	}
	if r.ContentLength > 0 && !decode(w, r, &in) {
		return
	}
	it, err := s.svc.Execute(r.Context(), r.PathValue("id"), kernel.ExecOptions{SimulateDecline: in.SimulateDecline})
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, it)
}

func (s *Server) cosign(w http.ResponseWriter, r *http.Request) {
	d, err := s.svc.Cosign(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) auditList(w http.ResponseWriter, r *http.Request) {
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	rows, err := s.st.AuditRows(r.Context(), after, limit)
	if err != nil {
		s.fail(w, err)
		return
	}
	type out struct {
		audit.Entry
		PrevHash   string `json:"prev_hash"`
		Hash       string `json:"hash"`
		Checkpoint bool   `json:"checkpoint"`
	}
	res := make([]out, len(rows))
	for i, row := range rows {
		res[i] = out{Entry: row.Entry, PrevHash: hex.EncodeToString(row.PrevHash), Hash: hex.EncodeToString(row.Hash), Checkpoint: len(row.Signature) > 0}
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": res})
}

func (s *Server) auditVerify(w http.ResponseWriter, r *http.Request) {
	var all []audit.Row
	var after int64
	for {
		rows, err := s.st.AuditRows(r.Context(), after, 5000)
		if err != nil {
			s.fail(w, err)
			return
		}
		all = append(all, rows...)
		if len(rows) < 5000 {
			break
		}
		after = rows[len(rows)-1].Entry.Seq
	}
	rep, err := audit.Verify(all, s.cfg.PublicKey)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"valid": false, "error": err.Error(), "entries": len(all)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"valid": true, "entries": rep.Entries, "checkpoints": rep.Checkpoints,
		"head_seq": rep.HeadSeq, "head_hash": hex.EncodeToString(rep.HeadHash), "last_checkpoint": rep.LastCheckpoint})
}

func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	ch := s.hub.subscribe()
	defer s.hub.unsubscribe(ch)
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	_, _ = io.WriteString(w, ": connected\n\n")
	fl.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case b := <-ch:
			_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
			fl.Flush()
		case <-ping.C:
			_, _ = io.WriteString(w, ": ping\n\n")
			fl.Flush()
		}
	}
}

func (s *Server) paypalConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"client_id": s.cfg.PayPalClientID, "env": s.pp.Env()})
}

func (s *Server) clientToken(w http.ResponseWriter, r *http.Request) {
	var domains []string
	if s.cfg.ClientTokenHost != "" {
		domains = []string{s.cfg.ClientTokenHost}
	}
	ct, err := s.pp.BrowserClientToken(r.Context(), domains...)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"access_token": ct.Token, "expires_in": ct.ExpiresIn})
}

// webhook verifies with PayPal's verify-webhook-signature endpoint (using the
// raw body) before applying. PayPal retries non-2xx deliveries for 3 days.
func (s *Server) webhook(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "unreadable body")
		return
	}
	if s.cfg.WebhookID == "" {
		s.log.Error("webhook received but PAYPAL_WEBHOOK_ID is not configured")
		writeErr(w, http.StatusServiceUnavailable, "webhook id not configured")
		return
	}
	if err := s.pp.VerifyWebhook(r.Context(), r.Header, s.cfg.WebhookID, raw); err != nil {
		s.log.Warn("webhook verification failed", "err", err)
		writeErr(w, http.StatusBadRequest, "verification failed")
		return
	}
	out, err := s.svc.ApplyWebhook(r.Context(), raw)
	if err != nil {
		s.log.Error("webhook apply failed", "err", err, "outcome", out)
		writeErr(w, http.StatusInternalServerError, "apply failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"outcome": out})
}

// paypalReturn handles the buyer returning from PayPal approval. The intent
// is identified by our own query parameter (PayPal's appended parameters are
// not relied on), and the kernel confirms APPROVED with PayPal before
// capturing. Capture is keyed on the order ID, so racing the
// CHECKOUT.ORDER.APPROVED webhook is harmless.
func (s *Server) paypalReturn(w http.ResponseWriter, r *http.Request) {
	intentID := r.URL.Query().Get("intent")
	orderID := ""
	if intentID != "" {
		if it, err := s.svc.GetIntent(r.Context(), intentID); err == nil {
			orderID = it.OrderID
		}
	}
	if orderID == "" {
		orderID = r.URL.Query().Get("token")
	}
	state := "unknown"
	if orderID != "" {
		it, err := s.svc.OnOrderApproved(r.Context(), orderID)
		if err != nil {
			s.log.Error("return capture failed", "err", err, "order", orderID)
		}
		state = string(it.State)
	}
	http.Redirect(w, r, fmt.Sprintf("%s/approved?intent=%s&state=%s", strings.TrimRight(s.cfg.ConsoleURL, "/"), intentID, state), http.StatusSeeOther)
}

func (s *Server) paypalCancel(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, fmt.Sprintf("%s/approved?intent=%s&state=cancelled", strings.TrimRight(s.cfg.ConsoleURL, "/"), r.URL.Query().Get("intent")), http.StatusSeeOther)
}

func (s *Server) shortLink(w http.ResponseWriter, r *http.Request) {
	url, err := s.svc.ResolveShortLink(r.Context(), r.PathValue("code"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "link expired or unknown")
		return
	}
	http.Redirect(w, r, url, http.StatusFound)
}

func (s *Server) fail(w http.ResponseWriter, err error) {
	var ae *paypal.APIError
	var ite *fsm.IllegalTransitionError
	switch {
	case errors.Is(err, kernel.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not found")
	case errors.Is(err, kernel.ErrState), errors.Is(err, kernel.ErrConflict), errors.As(err, &ite):
		writeErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, policy.ErrInvalidMandate):
		writeErr(w, http.StatusBadRequest, err.Error())
	case errors.As(err, &ae):
		s.log.Warn("paypal error", "err", err, "debug_id", ae.DebugID)
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": ae.Error(), "debug_id": ae.DebugID})
	default:
		s.log.Error("request failed", "err", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
	}
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				s.log.Error("panic", "value", v, "path", r.URL.Path)
				writeErr(w, http.StatusInternalServerError, "internal error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		for _, o := range s.cfg.AllowedOrigins {
			if o == origin {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Mcp-Session-Id")
				break
			}
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// limited applies a per-IP request budget to state-changing endpoints so the
// public sandbox demo cannot be spammed.
func (s *Server) limited(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.limits.allow(clientIP(r)) {
			writeErr(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
		h(w, r)
	}
}

type limiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	hits   map[string][]time.Time
}

func newLimiter(max int, window time.Duration) *limiter {
	return &limiter{max: max, window: window, hits: map[string][]time.Time{}}
}

func (l *limiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	cut := now.Add(-l.window)
	h := l.hits[key]
	i := 0
	for i < len(h) && h[i].Before(cut) {
		i++
	}
	h = h[i:]
	if len(h) >= l.max {
		l.hits[key] = h
		return false
	}
	l.hits[key] = append(h, now)
	return true
}

func clientIP(r *http.Request) string {
	if f := r.Header.Get("X-Forwarded-For"); f != "" {
		return strings.TrimSpace(strings.Split(f, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// vaultReturn is where PayPal sends the payer after approving vault setup.
// PayPal appends approval_token_id (the setup token ID; verified live,
// ledger V32), which identifies the mandate; the vault link is completed
// here so no separate call is needed.
func (s *Server) vaultReturn(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("approval_token_id")
	if token == "" {
		s.plainPage("Missing approval token.")(w, r)
		return
	}
	mandateID, err := s.svc.MandateBySetupToken(r.Context(), token)
	if err != nil {
		s.plainPage("This approval link does not match a mandate.")(w, r)
		return
	}
	v, err := s.svc.CompleteVaultSetup(r.Context(), mandateID)
	if err != nil {
		s.log.Error("vault return", "err", err, "mandate", mandateID)
		s.plainPage("PayPal approval was received but linking failed; it will need to be retried.")(w, r)
		return
	}
	s.plainPage(fmt.Sprintf("Your PayPal account is saved. Mandate %s is %s. You can close this tab.", v.ID, v.Status))(w, r)
}

func (s *Server) approvedPage(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	msg := "Payment approval received (state: " + state + "). You can close this tab."
	if state == "cancelled" {
		msg = "Payment approval was cancelled. Nothing was charged."
	}
	s.plainPage(msg)(w, r)
}

// plainPage renders a minimal HTML page (no scripts) for PayPal redirects.
func (s *Server) plainPage(msg string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprintf(w, `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Mandate</title></head><body style="font-family:system-ui,sans-serif;max-width:32rem;margin:4rem auto;padding:0 1rem;color:#14213d"><h1 style="font-size:1.25rem">Mandate</h1><p>%s</p></body></html>`, html.EscapeString(msg))
	}
}
