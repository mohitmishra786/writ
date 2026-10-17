package audit

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
	"time"
)

func chain(t *testing.T, key ed25519.PrivateKey, n int, checkpointEvery int) []Row {
	t.Helper()
	prev := Genesis
	var rows []Row
	for i := 1; i <= n; i++ {
		e := Entry{
			Seq: int64(i), TSMicros: Micros(time.Date(2026, 11, 3, 10, 0, i, 0, time.UTC)),
			Kind: KindPolicyDecision, IntentID: "intent", Reasons: []string{"PAYEE_NOT_ALLOWED"},
			AmountMinor: int64(i * 100), Currency: "USD", PayPal: map[string]any{"debug_id": "abc"},
		}
		h, err := Hash(prev, e)
		if err != nil {
			t.Fatal(err)
		}
		row := Row{Entry: e, PrevHash: prev, Hash: h}
		if checkpointEvery > 0 && i%checkpointEvery == 0 {
			row.Signature = SignCheckpoint(key, int64(i), h)
		}
		rows = append(rows, row)
		prev = h
	}
	return rows
}

func keys(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func TestVerifyIntactChain(t *testing.T) {
	pub, priv := keys(t)
	rep, err := Verify(chain(t, priv, 10, 5), pub)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Entries != 10 || rep.Checkpoints != 2 || rep.HeadSeq != 10 || rep.LastCheckpoint != 10 {
		t.Fatalf("unexpected report %+v", rep)
	}
}

func expectAt(t *testing.T, err error, seq int64) {
	t.Helper()
	var ve *VerifyError
	if !errors.As(err, &ve) || ve.Seq != seq {
		t.Fatalf("expected failure at seq %d, got %v", seq, err)
	}
}

func TestTamperedContentDetected(t *testing.T) {
	pub, priv := keys(t)
	rows := chain(t, priv, 10, 5)
	rows[3].Entry.AmountMinor = 999999
	_, err := Verify(rows, pub)
	expectAt(t, err, 4)
}

// Rewriting an entry and recomputing every later hash still fails, because
// the checkpoint signature covers the original head.
func TestRewrittenSuffixBreaksCheckpoint(t *testing.T) {
	pub, priv := keys(t)
	rows := chain(t, priv, 10, 5)
	rows[6].Entry.Reasons = nil
	prev := rows[5].Hash
	for i := 6; i < len(rows); i++ {
		rows[i].PrevHash = prev
		rows[i].Hash, _ = Hash(prev, rows[i].Entry)
		prev = rows[i].Hash
	}
	_, err := Verify(rows, pub)
	expectAt(t, err, 10)
}

func TestDeletedEntryDetected(t *testing.T) {
	pub, priv := keys(t)
	rows := chain(t, priv, 6, 0)
	rows = append(rows[:2], rows[3:]...)
	_, err := Verify(rows, pub)
	expectAt(t, err, 4)
}

func TestForgedCheckpointDetected(t *testing.T) {
	pub, priv := keys(t)
	_, attacker := keys(t)
	rows := chain(t, priv, 5, 5)
	rows[4].Signature = SignCheckpoint(attacker, 5, rows[4].Hash)
	_, err := Verify(rows, pub)
	expectAt(t, err, 5)
}
