// Package config wires Mandate's runtime from environment variables. Values
// are read once at startup; missing required values fail fast.
package config

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/mohitmishra786/paypal-ai-hackathon/internal/api"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/channel3"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/kernel"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/kernel/token"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/llm"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/mesh"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/notify"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/paypal"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/procure"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/store"
)

type Runtime struct {
	Agents  api.Agents
	Store   *store.Store
	PayPal  *paypal.Client
	Kernel  *kernel.Service
	Hub     *api.Hub
	API     api.Config
	Minter  *token.Minter
	Signing ed25519.PrivateKey
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// SigningKey derives the kernel Ed25519 key from MANDATE_SIGNING_SEED.
func SigningKey() (ed25519.PrivateKey, string, error) {
	seedHex := os.Getenv("MANDATE_SIGNING_SEED")
	seed, err := hex.DecodeString(seedHex)
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, "", errors.New("config: MANDATE_SIGNING_SEED must be 64 hex chars (run: mandatectl keygen)")
	}
	return ed25519.NewKeyFromSeed(seed), env("MANDATE_KEY_ID", "mandate-k1"), nil
}

func Load(ctx context.Context, log *slog.Logger) (*Runtime, error) {
	key, kid, err := SigningKey()
	if err != nil {
		return nil, err
	}
	minter, err := token.NewMinter(hex.EncodeToString(key.Seed()), kid)
	if err != nil {
		return nil, err
	}
	verifier, err := token.NewVerifier(map[string]ed25519.PublicKey{kid: minter.PublicKey()})
	if err != nil {
		return nil, err
	}
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return nil, errors.New("config: DATABASE_URL is required")
	}
	st, err := store.Open(ctx, dbURL, key)
	if err != nil {
		return nil, fmt.Errorf("config: database: %w", err)
	}
	ppCfg, err := paypal.ConfigFromEnv()
	if err != nil {
		st.Close()
		return nil, err
	}
	pp, err := paypal.New(ppCfg)
	if err != nil {
		st.Close()
		return nil, err
	}
	var notifier kernel.Notifier = notify.Discard{}
	if topic := os.Getenv("NTFY_TOPIC"); topic != "" {
		n, err := notify.NewNtfy(os.Getenv("NTFY_SERVER"), topic)
		if err != nil {
			st.Close()
			return nil, err
		}
		notifier = n
	} else {
		log.Warn("NTFY_TOPIC not set: step-up approvals are shown in the console only")
	}
	// Render injects RENDER_EXTERNAL_URL for web services.
	public := strings.TrimRight(env("PUBLIC_BASE_URL", env("RENDER_EXTERNAL_URL", "http://localhost:8080")), "/")
	if !strings.HasPrefix(public, "https://") {
		// Verified live (ledger V40): PayPal checkout fails for approval orders
		// whose return_url is http://localhost, so payer-action recovery needs
		// an https public URL (Render, or a tunnel in local development).
		log.Warn("PUBLIC_BASE_URL is not https: PayPal buyer approval (decline recovery) will fail until it is", "public_base_url", public)
	}
	console := strings.TrimRight(env("CONSOLE_URL", "http://localhost:3000"), "/")
	hub := api.NewHub()
	svc := kernel.New(st, pp, minter, verifier, notifier, hub, kernel.Config{
		PublicBaseURL: public,
		TokenTTL:      5 * time.Minute,
		ApprovalTTL:   30 * time.Minute,
	})
	origins := []string{console}
	if extra := os.Getenv("ALLOWED_ORIGINS"); extra != "" {
		origins = append(origins, strings.Split(extra, ",")...)
	}
	agents, err := loadAgents(svc, st, hub, public, log)
	if err != nil {
		st.Close()
		return nil, err
	}
	return &Runtime{
		Agents: agents,
		Store:  st, PayPal: pp, Kernel: svc, Hub: hub, Minter: minter, Signing: key,
		API: api.Config{
			PublicBaseURL: public, ConsoleURL: console, AllowedOrigins: origins,
			ClientTokenHost: os.Getenv("CLIENT_TOKEN_DOMAIN"), PayPalClientID: ppCfg.ClientID,
			WebhookID: os.Getenv("PAYPAL_WEBHOOK_ID"), KeyID: kid, PublicKey: minter.PublicKey(),
		},
	}, nil
}

// loadAgents builds the supplier agents and the procurement agent. Each is
// optional: missing keys disable that part with a warning instead of failing.
func loadAgents(svc *kernel.Service, st *store.Store, hub *api.Hub, public string, log *slog.Logger) (api.Agents, error) {
	var out api.Agents
	var client *llm.Client
	if cfg, err := llm.ConfigFromEnv(); err == nil {
		client = llm.New(cfg)
	} else {
		log.Warn("GROQ not configured: agents disabled", "err", err)
	}
	if os.Getenv("SUPPLIER_A_SEED") != "" && os.Getenv("SUPPLIER_B_SEED") != "" {
		sups, err := mesh.DemoSuppliers(os.Getenv("SUPPLIER_A_SEED"), os.Getenv("SUPPLIER_B_SEED"),
			os.Getenv("SANDBOX_SUPPLIER_A_EMAIL"), os.Getenv("SANDBOX_SUPPLIER_B_EMAIL"), client, env("GROQ_MODEL_SUPPLIER", "openai/gpt-oss-20b"))
		if err != nil {
			return out, err
		}
		out.Suppliers = sups
	} else {
		log.Warn("SUPPLIER_A_SEED/SUPPLIER_B_SEED not set: supplier agents disabled")
	}
	c3, err := channel3.FromEnv()
	if err != nil || client == nil || len(out.Suppliers) == 0 {
		log.Warn("procurement agent disabled (needs GROQ, CHANNEL3_API_KEY and supplier agents)")
		return out, nil
	}
	var cards []string
	for _, sp := range out.Suppliers {
		cards = append(cards, public+"/a2a/"+sp.Slug+"/.well-known/agent-card.json")
	}
	out.Procure = &procure.Agent{
		Store: st, Kernel: svc, LLM: client, Model: env("GROQ_MODEL_ORCHESTRATOR", "openai/gpt-oss-120b"), Channel3: c3,
		CardURLs: cards, Events: hub, Log: log, AgentID: env("PROCURE_AGENT_ID", "agent:procure-01"),
	}
	return out, nil
}
