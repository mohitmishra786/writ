package idem

import (
	"regexp"
	"strings"
	"testing"
)

var uuidV8 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-8[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func mustFor(t *testing.T) func(string, error) string {
	return func(k string, err error) string {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return k
	}
}

func TestDeterministic(t *testing.T) {
	must := mustFor(t)
	a := must(PrimaryOrder("intent_01"))
	b := must(PrimaryOrder("intent_01"))
	if a != b {
		t.Fatalf("same operation produced different keys: %s %s", a, b)
	}
}

func TestDistinctOperationsNeverCollide(t *testing.T) {
	must := mustFor(t)
	keys := map[string]string{}
	add := func(name, k string) {
		if prev, ok := keys[k]; ok {
			t.Fatalf("collision between %s and %s", prev, name)
		}
		keys[k] = name
	}
	add("primary i1", must(PrimaryOrder("i1")))
	add("primary i2", must(PrimaryOrder("i2")))
	add("recovery i1 #1", must(RecoveryOrder("i1", 1)))
	add("recovery i1 #2", must(RecoveryOrder("i1", 2)))
	add("capture i1", must(Capture("i1")))
	add("batch i1", must(BatchOrder("i1")))
	// Length-prefixing prevents ("ab","c") colliding with ("a","bc").
	add("setup ab/1", must(VaultSetupToken("ab", 1)))
	add("setup a/b1", must(VaultSetupToken("a", 11)))
}

// Two legitimate $10 partial refunds of the same capture must get distinct
// keys; the PR #108 amount-based derivation would collide here.
func TestEqualPartialRefundsDoNotCollide(t *testing.T) {
	must := mustFor(t)
	r1 := must(Refund("CAP-1", 1))
	r2 := must(Refund("CAP-1", 2))
	if r1 == r2 {
		t.Fatal("equal partial refunds collided")
	}
}

func TestKeyShape(t *testing.T) {
	must := mustFor(t)
	k := must(Capture("5O190127TN364715T"))
	if !uuidV8.MatchString(k) {
		t.Fatalf("not an RFC 9562 UUIDv8: %q", k)
	}
}

func TestRejectsEmptyScope(t *testing.T) {
	if _, err := PrimaryOrder(""); err == nil {
		t.Fatal("expected error")
	}
	if _, err := Refund("CAP-1", 0); err == nil {
		t.Fatal("expected error")
	}
	if _, err := RecoveryOrder("i1", 0); err == nil {
		t.Fatal("expected error")
	}
}

func TestSenderItemIDLength(t *testing.T) {
	if _, err := SenderItemID(strings.Repeat("x", 64)); err == nil {
		t.Fatal("expected length error")
	}
	if id, err := SenderItemID("01JA0000000000000000000000"); err != nil || id == "" {
		t.Fatalf("unexpected %q %v", id, err)
	}
}
