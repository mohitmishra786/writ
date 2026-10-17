package fsm

import (
	"errors"
	"testing"
)

func walk(t *testing.T, path ...State) {
	t.Helper()
	for i := 1; i < len(path); i++ {
		if err := Check(path[i-1], path[i]); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGoldenPath(t *testing.T) {
	walk(t, Received, Authorized, Minted, Submitted, Captured, Settling, Settled, Reconciled)
}

func TestDeclineRecoveryPath(t *testing.T) {
	walk(t, Received, AwaitingCosign, Authorized, Minted, Submitted, Declined, Recovery, Approved, Captured, Settling, Settled)
}

func TestAmbiguousOutcomePath(t *testing.T) {
	walk(t, Minted, Submitted, Unknown, Captured)
}

func TestIllegalTransitions(t *testing.T) {
	cases := [][2]State{
		{Received, Minted},   // must pass policy first
		{Denied, Authorized}, // terminal
		{Captured, Submitted},
		{Settled, Captured},
		{Minted, Captured}, // no capture without submission
	}
	for _, c := range cases {
		err := Check(c[0], c[1])
		var ite *IllegalTransitionError
		if !errors.As(err, &ite) {
			t.Fatalf("%s -> %s should be illegal", c[0], c[1])
		}
	}
}

func TestTerminalStates(t *testing.T) {
	for _, s := range []State{Denied, Expired, Revoked, Failed, Reconciled} {
		if !s.Terminal() {
			t.Fatalf("%s should be terminal", s)
		}
	}
	if Captured.Terminal() {
		t.Fatal("captured is not terminal")
	}
}

func TestStaleWebhookDetection(t *testing.T) {
	if !IsStale(Captured, Approved) {
		t.Fatal("late APPROVED after capture is stale")
	}
	if IsStale(Recovery, Approved) {
		t.Fatal("APPROVED during recovery is the expected next step")
	}
}

func TestReservationHeld(t *testing.T) {
	if !Declined.ReservationHeld() || !Recovery.ReservationHeld() {
		t.Fatal("recovery keeps the original reservation")
	}
	if Captured.ReservationHeld() || Denied.ReservationHeld() {
		t.Fatal("captured converts and denied never reserved")
	}
}

func TestEveryEdgeUsesKnownStates(t *testing.T) {
	for _, e := range Edges() {
		if !e[0].Valid() || !e[1].Valid() {
			t.Fatalf("unknown state in edge %v", e)
		}
	}
}
