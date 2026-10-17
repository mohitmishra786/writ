// Package store owns the Postgres connection, schema migrations and the
// audit-chain append. Ledger invariants live in the schema itself
// (migrations/0001_init.sql).
package store

import (
	"context"
	"crypto/ed25519"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mohitmishra786/paypal-ai-hackathon/internal/kernel/audit"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/kernel/fsm"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Store struct {
	Pool            *pgxpool.Pool
	signer          ed25519.PrivateKey
	checkpointEvery int64
}

// Open connects and waits for the database to accept connections, retrying
// with backoff for up to about 90 seconds. Seen live on Render: a newly
// provisioned database refused connections briefly right after the
// pre-deploy migration had succeeded.
func Open(ctx context.Context, url string, signer ed25519.PrivateKey) (*Store, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	delay := time.Second
	deadline := time.Now().Add(90 * time.Second)
	for {
		pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err = pool.Ping(pingCtx)
		cancel()
		if err == nil {
			return &Store{Pool: pool, signer: signer, checkpointEvery: 50}, nil
		}
		if time.Now().Add(delay).After(deadline) || ctx.Err() != nil {
			pool.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			pool.Close()
			return nil, ctx.Err()
		case <-time.After(delay):
		}
		delay = min(delay*2, 15*time.Second)
	}
}

func (s *Store) Close() { s.Pool.Close() }

// Migrate applies embedded migrations in order, once each, then syncs the
// FSM edge table from Go so SQL and Go can never disagree.
func (s *Store) Migrate(ctx context.Context) error {
	if _, err := s.Pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		name text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	for _, name := range names {
		var exists bool
		if err := s.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE name = $1)`, name).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		sql, err := migrations.ReadFile(name)
		if err != nil {
			return err
		}
		if err := s.Tx(ctx, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, string(sql)); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
			_, err := tx.Exec(ctx, `INSERT INTO schema_migrations (name) VALUES ($1)`, name)
			return err
		}); err != nil {
			return err
		}
	}
	return s.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM intent_transitions`); err != nil {
			return err
		}
		for _, e := range fsm.Edges() {
			if _, err := tx.Exec(ctx, `INSERT INTO intent_transitions (from_state, to_state) VALUES ($1, $2)`, string(e[0]), string(e[1])); err != nil {
				return err
			}
		}
		return nil
	})
}

// Tx runs fn in a transaction, committing on nil error.
func (s *Store) Tx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// AppendAudit adds one entry to the hash chain inside tx. The chain head row
// is locked, so appends serialise and seq stays gapless (I6); because it runs
// in the caller's transaction, the entry commits atomically with the state
// change it describes (I7).
func (s *Store) AppendAudit(ctx context.Context, tx pgx.Tx, e audit.Entry) (audit.Row, error) {
	var seq int64
	var prev []byte
	if err := tx.QueryRow(ctx, `SELECT seq, hash FROM audit_head WHERE id = 1 FOR UPDATE`).Scan(&seq, &prev); err != nil {
		return audit.Row{}, err
	}
	now := time.Now().UTC()
	e.Seq = seq + 1
	if e.TSMicros == 0 {
		e.TSMicros = audit.Micros(now)
	}
	h, err := audit.Hash(prev, e)
	if err != nil {
		return audit.Row{}, err
	}
	row := audit.Row{Entry: e, PrevHash: prev, Hash: h}
	if s.signer != nil && (e.Seq%s.checkpointEvery == 0 || isLifecycle(e.Kind)) {
		row.Signature = audit.SignCheckpoint(s.signer, e.Seq, h)
	}
	entry, err := json.Marshal(e)
	if err != nil {
		return audit.Row{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_events (seq, ts, kind, mandate_id, agent_id, intent_id, entry, prev_hash, hash, signature)
		VALUES ($1, $2, $3, nullif($4, ''), nullif($5, ''), nullif($6, ''), $7, $8, $9, $10)`,
		e.Seq, time.UnixMicro(e.TSMicros).UTC(), e.Kind, e.MandateID, e.AgentID, e.IntentID, entry, prev, h, row.Signature); err != nil {
		return audit.Row{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE audit_head SET seq = $1, hash = $2 WHERE id = 1`, e.Seq, h); err != nil {
		return audit.Row{}, err
	}
	return row, nil
}

func isLifecycle(kind string) bool {
	switch kind {
	case audit.KindMandateCreated, audit.KindMandateActivated, audit.KindMandatePaused:
		return true
	}
	return false
}

// AuditRows loads the chain in order for offline verification.
func (s *Store) AuditRows(ctx context.Context, afterSeq int64, limit int) ([]audit.Row, error) {
	rows, err := s.Pool.Query(ctx, `SELECT entry, prev_hash, hash, signature FROM audit_events WHERE seq > $1 ORDER BY seq LIMIT $2`, afterSeq, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []audit.Row
	for rows.Next() {
		var raw []byte
		var r audit.Row
		if err := rows.Scan(&raw, &r.PrevHash, &r.Hash, &r.Signature); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &r.Entry); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// IsCheckViolation reports Postgres check/trigger rejections (SQLSTATE 23514),
// which is how the database surfaces broken ledger invariants.
func IsCheckViolation(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23514"
}

func IsUniqueViolation(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505"
}

// ResetForTests drops every Mandate table. Refuses unless the database name
// contains "test".
func ResetForTests(ctx context.Context, s *Store) error {
	var db string
	if err := s.Pool.QueryRow(ctx, `SELECT current_database()`).Scan(&db); err != nil {
		return err
	}
	if !strings.Contains(db, "test") {
		return fmt.Errorf("refusing to reset non-test database %q", db)
	}
	_, err := s.Pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`)
	return err
}
