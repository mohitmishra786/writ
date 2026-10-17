// Package fsm defines the intent lifecycle. Only edges listed in transitions
// are legal; the same table is mirrored into Postgres so the database rejects
// illegal updates even if Go code is wrong.
package fsm

import (
	"cmp"
	"fmt"
	"slices"
)

type State string

const (
	Received       State = "RECEIVED"
	Denied         State = "DENIED"
	AwaitingCosign State = "AWAITING_COSIGN"
	Authorized     State = "AUTHORIZED"
	Minted         State = "MINTED"
	Submitted      State = "SUBMITTED"
	Unknown        State = "UNKNOWN"
	CapturePending State = "CAPTURE_PENDING"
	Declined       State = "DECLINED"
	Recovery       State = "RECOVERY"
	Approved       State = "APPROVED"
	Captured       State = "CAPTURED"
	Settling       State = "SETTLING"
	Settled        State = "SETTLED"
	SettleFailed   State = "SETTLE_FAILED"
	Reconciled     State = "RECONCILED"
	Expired        State = "EXPIRED"
	Revoked        State = "REVOKED"
	Failed         State = "FAILED"
)

var transitions = map[State][]State{
	Received:       {Denied, AwaitingCosign, Authorized},
	AwaitingCosign: {Authorized, Expired, Revoked},
	Authorized:     {Minted, Revoked},
	Minted:         {Submitted, Expired, Revoked},
	Submitted:      {Captured, CapturePending, Unknown, Declined, Failed},
	Unknown:        {Captured, CapturePending, Declined, Failed},
	CapturePending: {Captured, Failed},
	Declined:       {Recovery, Failed},
	Recovery:       {Approved, Expired},
	Approved:       {Captured, CapturePending, Failed},
	Captured:       {Settling},
	Settling:       {Settled, SettleFailed},
	SettleFailed:   {Settling},
	Settled:        {Reconciled},
}

// rank orders states along the happy path so stale, out-of-order webhooks
// can be recognised as regressions and ignored.
var rank = map[State]int{
	Received: 0, AwaitingCosign: 1, Authorized: 2, Minted: 3, Submitted: 4,
	Unknown: 5, Declined: 5, Recovery: 6, Approved: 7, CapturePending: 8,
	Captured: 9, Settling: 10, SettleFailed: 10, Settled: 11, Reconciled: 12,
	Denied: 99, Expired: 99, Revoked: 99, Failed: 99,
}

// ReservationHeld reports whether funds stay reserved against the envelope.
// Once captured, the reservation is converted rather than released.
func (s State) ReservationHeld() bool {
	switch s {
	case Minted, Submitted, Unknown, CapturePending, Declined, Recovery, Approved:
		return true
	}
	return false
}

func (s State) Terminal() bool {
	_, hasNext := transitions[s]
	return !hasNext
}

func (s State) Valid() bool {
	_, ok := rank[s]
	return ok
}

type IllegalTransitionError struct{ From, To State }

func (e *IllegalTransitionError) Error() string {
	return fmt.Sprintf("fsm: illegal transition %s -> %s", e.From, e.To)
}

func Check(from, to State) error {
	if !slices.Contains(transitions[from], to) {
		return &IllegalTransitionError{From: from, To: to}
	}
	return nil
}

// IsStale reports whether moving from current to incoming would regress the
// lifecycle, e.g. a late CHECKOUT.ORDER.APPROVED after the capture landed.
func IsStale(current, incoming State) bool {
	return Check(current, incoming) != nil && rank[incoming] <= rank[current]
}

// Edges returns every legal transition, used to seed the database table.
func Edges() [][2]State {
	var out [][2]State
	for from, tos := range transitions {
		for _, to := range tos {
			out = append(out, [2]State{from, to})
		}
	}
	slices.SortFunc(out, func(a, b [2]State) int {
		return cmp.Or(cmp.Compare(a[0], b[0]), cmp.Compare(a[1], b[1]))
	})
	return out
}
