package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mohitmishra786/paypal-ai-hackathon/internal/config"
)

// demoReset starts a clean demo without touching history. The newest ACTIVE
// mandate with a saved PayPal account is the template: every ACTIVE mandate
// on that vault is paused (audited as MANDATE_PAUSED), a new mandate is
// created from the same policy with a fresh validity window and an empty
// weekly envelope, and it is activated with the same vault ID (audited as
// MANDATE_ACTIVATED). Reusing the vault needs no new approval: approving
// vault setup again for the same payer and merchant returns the same payment
// token (verified 2026-10-17, migration 0004). Nothing is deleted.
func demoReset(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("demo-reset", flag.ContinueOnError)
	validDays := fs.Int("valid-days", 90, "validity of the new mandate in days")
	stock := fs.Int("stock", 2, "frothing pitchers on hand after the reset (par is 8)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rt, err := config.Load(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		return err
	}
	defer rt.Store.Close()

	var oldID, vaultID string
	err = rt.Store.Pool.QueryRow(ctx, `SELECT id, vault_id FROM mandates WHERE status = 'ACTIVE' AND vault_id IS NOT NULL ORDER BY created_at DESC LIMIT 1`).Scan(&oldID, &vaultID)
	if errors.Is(err, pgx.ErrNoRows) {
		return errors.New("no ACTIVE mandate with a saved PayPal account; link one through the vault setup flow first")
	}
	if err != nil {
		return err
	}
	old, err := rt.Kernel.GetMandate(ctx, oldID)
	if err != nil {
		return err
	}

	if err := rt.Kernel.PauseByVault(ctx, vaultID, "superseded by demo reset"); err != nil {
		return fmt.Errorf("pause previous mandates: %w", err)
	}
	m := old.Policy
	now := time.Now()
	m.ID = ""
	m.NotBefore = now.Add(-time.Minute).Unix()
	m.NotAfter = now.Add(time.Duration(*validDays) * 24 * time.Hour).Unix()
	created, err := rt.Kernel.CreateMandate(ctx, m)
	if err != nil {
		return fmt.Errorf("create mandate: %w", err)
	}
	active, err := rt.Kernel.ActivateWithVault(ctx, created.ID, vaultID)
	if err != nil {
		return fmt.Errorf("activate mandate: %w", err)
	}

	if _, err := rt.Store.Pool.Exec(ctx, `UPDATE inventory SET on_hand = par_level, updated_at = now()`); err != nil {
		return err
	}
	if _, err := rt.Store.Pool.Exec(ctx, `UPDATE inventory SET on_hand = least($1, par_level), updated_at = now() WHERE sku = 'pitcher-20oz'`, *stock); err != nil {
		return err
	}

	fmt.Printf("paused   %s (and any other ACTIVE mandate on the same PayPal account)\n", oldID)
	fmt.Printf("active   %s  envelope %d/%d minor, valid until %s\n", active.ID, active.CapturedMinor+active.ReservedMinor, active.LimitMinor,
		time.Unix(m.NotAfter, 0).UTC().Format("2006-01-02"))
	fmt.Printf("stock    pitcher-20oz on hand %d\n", *stock)
	return nil
}
