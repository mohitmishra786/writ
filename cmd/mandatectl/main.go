// Command mandatectl is the operator CLI: key generation, migrations,
// webhook registration and offline audit verification.
package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"github.com/mohitmishra786/paypal-ai-hackathon/internal/config"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/kernel/audit"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/paypal"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/store"
)

const usage = `mandatectl <command>

  keygen                      print a new MANDATE_SIGNING_SEED and a supplier key pair
  migrate                     apply database migrations (DATABASE_URL)
  webhooks ensure <url>       register/patch the PayPal webhook for <url>; prints PAYPAL_WEBHOOK_ID
  webhooks list               list PayPal webhooks for this app
  webhooks delete <id>        delete a PayPal webhook
  verify                      verify the whole audit hash chain and checkpoint signatures
  demo-reset [-valid-days N] [-stock N]
                              start a fresh demo: pause the active vaulted mandates, create a
                              new one with the same policy and the owner's saved PayPal account,
                              and set pitcher stock so the agent has something to buy
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		fmt.Print(usage)
		return nil
	}
	ctx := context.Background()
	switch args[0] {
	case "keygen":
		seed := make([]byte, ed25519.SeedSize)
		if _, err := rand.Read(seed); err != nil {
			return err
		}
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return err
		}
		fmt.Printf("MANDATE_SIGNING_SEED=%s\n", hex.EncodeToString(seed))
		fmt.Printf("# supplier key pair (for a supplier agent)\nSUPPLIER_SEED=%s\nSUPPLIER_PUBLIC_KEY=%s\n", hex.EncodeToString(priv.Seed()), hex.EncodeToString(pub))
		return nil

	case "migrate":
		st, err := openStore(ctx)
		if err != nil {
			return err
		}
		defer st.Close()
		if err := st.Migrate(ctx); err != nil {
			return err
		}
		fmt.Println("migrations applied")
		return nil

	case "webhooks":
		if len(args) < 2 {
			return fmt.Errorf("usage: webhooks ensure <url> | list | delete <id>")
		}
		pp, err := paypalClient()
		if err != nil {
			return err
		}
		switch args[1] {
		case "ensure":
			if len(args) < 3 || !strings.HasPrefix(args[2], "https://") {
				return fmt.Errorf("webhooks ensure needs a public https URL")
			}
			w, err := pp.EnsureWebhook(ctx, args[2], paypal.SubscribedEvents)
			if err != nil {
				return err
			}
			fmt.Printf("webhook %s -> %s (%d events)\nPAYPAL_WEBHOOK_ID=%s\n", w.ID, w.URL, len(w.EventTypes), w.ID)
		case "list":
			hooks, err := pp.ListWebhooks(ctx)
			if err != nil {
				return err
			}
			for _, h := range hooks {
				fmt.Printf("%s  %s  (%d events)\n", h.ID, h.URL, len(h.EventTypes))
			}
		case "delete":
			if len(args) < 3 {
				return fmt.Errorf("webhooks delete <id>")
			}
			return pp.DeleteWebhook(ctx, args[2])
		default:
			return fmt.Errorf("unknown webhooks subcommand %q", args[1])
		}
		return nil

	case "demo-reset":
		return demoReset(ctx, args[1:])

	case "verify":
		st, err := openStore(ctx)
		if err != nil {
			return err
		}
		defer st.Close()
		key, _, err := config.SigningKey()
		if err != nil {
			return err
		}
		var rows []audit.Row
		var after int64
		for {
			page, err := st.AuditRows(ctx, after, 5000)
			if err != nil {
				return err
			}
			rows = append(rows, page...)
			if len(page) < 5000 {
				break
			}
			after = page[len(page)-1].Entry.Seq
		}
		rep, err := audit.Verify(rows, key.Public().(ed25519.PublicKey))
		if err != nil {
			return err
		}
		fmt.Printf("audit chain valid: %d entries, %d signed checkpoints, head seq %d, head %x\n", rep.Entries, rep.Checkpoints, rep.HeadSeq, rep.HeadHash)
		return nil
	}
	fmt.Print(usage)
	return fmt.Errorf("unknown command %q", args[0])
}

func openStore(ctx context.Context) (*store.Store, error) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	return store.Open(ctx, url, nil)
}

func paypalClient() (*paypal.Client, error) {
	cfg, err := paypal.ConfigFromEnv()
	if err != nil {
		return nil, err
	}
	return paypal.New(cfg)
}
