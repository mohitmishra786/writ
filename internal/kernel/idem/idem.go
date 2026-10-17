// Package idem derives PayPal-Request-Id values from durable identifiers.
//
// The same logical operation always yields the same key and distinct
// operations never share one, which closes paypal/agent-toolkit#98 (a single
// client-level key reused across calls). Keys never depend on the amount, so
// two legitimate equal partial refunds cannot collide (the PR #108 flaw).
//
// PayPal's guidance is "a unique UUID on every POST request — reuse the same
// value on retries", so keys are RFC 9562 UUIDv8 values built from SHA-256:
// UUID-shaped, yet deterministic.
package idem

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
)

type Op string

const (
	OpVaultSetupToken   Op = "vault.setup_token"
	OpVaultPaymentToken Op = "vault.payment_token"
	OpOrderPrimary      Op = "order.primary"
	OpOrderRecovery     Op = "order.recovery"
	OpOrderBatch        Op = "order.batch"
	OpCapture           Op = "order.capture"
	OpRefund            Op = "capture.refund"
)

var ErrEmptyScope = errors.New("idem: empty scope component")

// maxSenderItemID is the Payouts v1 schema limit for sender_item_id.
const maxSenderItemID = 63

func derive(op Op, parts ...string) (string, error) {
	for _, p := range parts {
		if p == "" {
			return "", ErrEmptyScope
		}
	}
	h := sha256.New()
	h.Write([]byte(op))
	for _, p := range parts {
		h.Write([]byte{0})
		h.Write([]byte(strconv.Itoa(len(p))))
		h.Write([]byte{':'})
		h.Write([]byte(p))
	}
	var u [16]byte
	copy(u[:], h.Sum(nil))
	u[6] = u[6]&0x0f | 0x80 // version 8
	u[8] = u[8]&0x3f | 0x80 // RFC 9562 variant
	x := hex.EncodeToString(u[:])
	return x[0:8] + "-" + x[8:12] + "-" + x[12:16] + "-" + x[16:20] + "-" + x[20:32], nil
}

func VaultSetupToken(principalID string, attempt int) (string, error) {
	return derive(OpVaultSetupToken, principalID, strconv.Itoa(attempt))
}

func VaultPaymentToken(setupTokenID string) (string, error) {
	return derive(OpVaultPaymentToken, setupTokenID)
}

func PrimaryOrder(intentID string) (string, error) {
	return derive(OpOrderPrimary, intentID)
}

func RecoveryOrder(intentID string, n int) (string, error) {
	if n < 1 {
		return "", errors.New("idem: recovery attempt must be >= 1")
	}
	return derive(OpOrderRecovery, intentID, strconv.Itoa(n))
}

func BatchOrder(batchID string) (string, error) {
	return derive(OpOrderBatch, batchID)
}

func Capture(orderID string) (string, error) {
	return derive(OpCapture, orderID)
}

// Refund keys use a ledger-allocated sequence number, never the amount.
func Refund(captureID string, refundSeq int64) (string, error) {
	if refundSeq < 1 {
		return "", errors.New("idem: refund sequence must be >= 1")
	}
	return derive(OpRefund, captureID, strconv.FormatInt(refundSeq, 10))
}

// SenderBatchID is the Payouts dedupe key; PayPal rejects reuse of a
// sender_batch_id, so it must be stable for the lifetime of the batch.
func SenderBatchID(batchID string) (string, error) {
	if batchID == "" {
		return "", ErrEmptyScope
	}
	return "mb-" + batchID, nil
}

func SenderItemID(obligationID string) (string, error) {
	if obligationID == "" {
		return "", ErrEmptyScope
	}
	if len(obligationID) > maxSenderItemID {
		return "", fmt.Errorf("idem: sender_item_id exceeds %d chars", maxSenderItemID)
	}
	return obligationID, nil
}
