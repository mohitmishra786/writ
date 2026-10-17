package store

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/mohitmishra786/paypal-ai-hackathon/internal/kernel/audit"
)

// OpenTest returns a migrated, empty store on TEST_DATABASE_URL, or skips.
func openTest(t *testing.T) (*Store, ed25519.PublicKey) {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	ctx := context.Background()
	s, err := Open(ctx, url, priv)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := ResetForTests(ctx, s); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return s, pub
}

func TestMigrateIsIdempotent(t *testing.T) {
	s, _ := openTest(t)
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func seedMandate(t *testing.T, s *Store, limit int64) {
	t.Helper()
	ctx := context.Background()
	if _, err := s.Pool.Exec(ctx, `INSERT INTO mandates (id, principal_id, agent_id, status, policy, policy_hash) VALUES ('m1','p','a','ACTIVE','{}','h')`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pool.Exec(ctx, `INSERT INTO envelopes (mandate_id, currency, limit_minor, period_seconds, period_start) VALUES ('m1','USD',$1,604800,now())`, limit); err != nil {
		t.Fatal(err)
	}
}

// I1 is a database CHECK: no code path can overspend the envelope.
func TestEnvelopeCannotBeOverspent(t *testing.T) {
	s, _ := openTest(t)
	seedMandate(t, s, 60000)
	ctx := context.Background()
	if _, err := s.Pool.Exec(ctx, `UPDATE envelopes SET reserved_minor = 40000, captured_minor = 20000 WHERE mandate_id = 'm1'`); err != nil {
		t.Fatalf("exactly at the limit must succeed: %v", err)
	}
	_, err := s.Pool.Exec(ctx, `UPDATE envelopes SET reserved_minor = reserved_minor + 1 WHERE mandate_id = 'm1'`)
	if !IsCheckViolation(err) {
		t.Fatalf("expected check violation, got %v", err)
	}
}

// I5 is a trigger: illegal FSM edges are rejected by Postgres itself.
func TestIllegalTransitionRejectedByDatabase(t *testing.T) {
	s, _ := openTest(t)
	seedMandate(t, s, 60000)
	ctx := context.Background()
	if _, err := s.Pool.Exec(ctx, `INSERT INTO intents (id, mandate_id, agent_id, payee_ref, amount_minor, currency, state, policy_hash)
		VALUES ('i1','m1','a','supplier:x',100,'USD','RECEIVED','h')`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pool.Exec(ctx, `UPDATE intents SET state = 'CAPTURED' WHERE id = 'i1'`); !IsCheckViolation(err) {
		t.Fatalf("RECEIVED -> CAPTURED must be rejected, got %v", err)
	}
	if _, err := s.Pool.Exec(ctx, `UPDATE intents SET state = 'AUTHORIZED' WHERE id = 'i1'`); err != nil {
		t.Fatalf("legal edge rejected: %v", err)
	}
	var v int
	_ = s.Pool.QueryRow(ctx, `SELECT version FROM intents WHERE id = 'i1'`).Scan(&v)
	if v != 2 {
		t.Fatalf("version not bumped: %d", v)
	}
}

func TestAuditChainAppendAndVerify(t *testing.T) {
	s, pub := openTest(t)
	s.checkpointEvery = 3
	ctx := context.Background()
	for i := range 7 {
		err := s.Tx(ctx, func(tx pgx.Tx) error {
			_, err := s.AppendAudit(ctx, tx, audit.Entry{Kind: audit.KindPolicyDecision, IntentID: "i1", AmountMinor: int64(i + 1), Currency: "USD"})
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	rows, err := s.AuditRows(ctx, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := audit.Verify(rows, pub)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Entries != 7 || rep.Checkpoints != 2 {
		t.Fatalf("report %+v", rep)
	}
}

func TestAuditIsAppendOnly(t *testing.T) {
	s, _ := openTest(t)
	ctx := context.Background()
	_ = s.Tx(ctx, func(tx pgx.Tx) error {
		_, err := s.AppendAudit(ctx, tx, audit.Entry{Kind: audit.KindPolicyDecision})
		return err
	})
	if _, err := s.Pool.Exec(ctx, `UPDATE audit_events SET kind = 'X'`); err == nil {
		t.Fatal("audit row was mutated")
	}
	if _, err := s.Pool.Exec(ctx, `DELETE FROM audit_events`); err == nil {
		t.Fatal("audit row was deleted")
	}
}

// A rolled-back business transaction must not leave an audit entry behind.
func TestAuditRollsBackWithBusinessChange(t *testing.T) {
	s, _ := openTest(t)
	ctx := context.Background()
	_ = s.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := s.AppendAudit(ctx, tx, audit.Entry{Kind: audit.KindPolicyDecision}); err != nil {
			return err
		}
		return context.Canceled
	})
	var n int
	_ = s.Pool.QueryRow(ctx, `SELECT count(*) FROM audit_events`).Scan(&n)
	if n != 0 {
		t.Fatalf("orphan audit rows: %d", n)
	}
}
