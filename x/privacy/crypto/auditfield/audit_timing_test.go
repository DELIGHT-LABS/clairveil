package auditfield

import (
	"crypto/subtle"
	"encoding/hex"
	"testing"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/ctbn254/scalarct"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/ctbn254/secretprofile"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/timingtest"
)

// This opt-in diagnostic measures different low/high-Hamming scalar inputs in
// the secret SAFE/Poseidon2 relation. It is not a constant-time certificate.
func TestT19AuditSAFETiming(t *testing.T) {
	if err := secretprofile.Check(); err != nil {
		t.Skip(err)
	}
	context := testContext(t, KindDeposit, 0, 1)
	values := []Field32{Field32FromUint64(1), Field32FromUint64(2), Field32FromUint64(3), Field32FromUint64(4), Field32FromUint64(5), Field32FromUint64(6)}
	plain, err := NewAuditPlain(KindDeposit, values)
	if err != nil {
		t.Fatal(err)
	}
	highFieldRaw, _ := hex.DecodeString("30644e72e131a029b85045b68181585d2833e84879b9709143e1f593f0000000")
	var highField Field32
	copy(highField[:], highFieldRaw)
	highValues := make([]Field32, len(values))
	for i := range highValues {
		highValues[i] = highField
	}
	highPlain, err := NewAuditPlain(KindDeposit, highValues)
	if err != nil {
		t.Fatal(err)
	}
	r1 := testNonzeroScalar(t, 1)
	highRaw, err := hex.DecodeString("060c89ce5c263405370a08b6d0302b0bab3eedb83920ee0a677297dc392126f0") // q-1
	if err != nil {
		t.Fatal(err)
	}
	r2, err := scalarct.ParseNonzeroScalarBE32(highRaw)
	if err != nil {
		t.Fatal(err)
	}
	var nonce Nonce128
	nonce[15] = 1
	if _, _, err := encryptAuditWithNonce(context.AuditKey(), r1, context, nonce, plain); err != nil {
		t.Fatal(err)
	}
	if _, _, err := encryptAuditWithNonce(context.AuditKey(), r2, context, nonce, plain); err != nil {
		t.Fatal(err)
	}
	timingtest.Run(t, "audit-safe-poseidon2-encrypt", func(class uint64) {
		r := r1
		p := plain
		if class == 1 {
			r = r2
			p = highPlain
		}
		subtle.WithDataIndependentTiming(func() {
			_, _, err = encryptAuditWithNonce(context.AuditKey(), r, context, nonce, p)
		})
		if err != nil {
			t.Fatalf("timing operation failed: %v", err)
		}
	})
}
