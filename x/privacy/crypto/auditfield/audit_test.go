package auditfield

import (
	"errors"
	"testing"
)

func TestFourKindFixedAuditCipherAndFailureBinding(t *testing.T) {
	for _, tc := range []struct {
		name            string
		kind            Kind
		inputs, outputs uint8
	}{
		{"deposit", KindDeposit, 0, 1}, {"withdraw", KindWithdraw, 1, 0}, {"transfer", KindTransfer2x2, 2, 2}, {"batch", KindBatch16x32, 16, 32},
	} {
		t.Run(tc.name, func(t *testing.T) {
			context := testContext(t, tc.kind, tc.inputs, tc.outputs)
			fields, err := tc.kind.PlaintextFieldCount()
			if err != nil {
				t.Fatal(err)
			}
			values := make([]Field32, fields)
			for i := range values {
				values[i] = Field32FromUint64(uint64(i + 1))
			}
			plain, err := NewAuditPlain(tc.kind, values)
			if err != nil {
				t.Fatal(err)
			}
			var nonce Nonce128
			nonce[15] = byte(tc.kind)
			envelope, root, err := encryptAuditWithNonce(context.AuditKey(), testNonzeroScalar(t, 11), context, nonce, plain)
			if err != nil {
				t.Fatal(err)
			}
			out, err := DecryptAudit(testNonzeroScalar(t, 1), context, envelope, root)
			if err != nil || out == nil {
				t.Fatalf("decrypt: %v", err)
			}
			if got := out.Fields(); len(got) != len(values) || got[0] != values[0] || got[len(got)-1] != values[len(values)-1] {
				t.Fatal("plaintext mismatch")
			}

			wrongRoot := root
			wrongRoot.Left = Field32FromUint64(1)
			if out, err := DecryptAudit(testNonzeroScalar(t, 1), context, envelope, wrongRoot); out != nil || !errors.Is(err, ErrAuditDecrypt) {
				t.Fatal("wrong root disclosed plaintext")
			}
			if out, err := DecryptAudit(testNonzeroScalar(t, 2), context, envelope, root); out != nil || !errors.Is(err, ErrAuditDecrypt) {
				t.Fatal("wrong key disclosed plaintext")
			}
			changedNonce := nonce
			changedNonce[0] = 1
			altered, err := NewEnvelopeFrame(tc.kind, tc.inputs, tc.outputs, changedNonce, envelope.EphemeralFrame(), envelope.Ciphertext(), envelope.Tag())
			if err != nil {
				t.Fatal(err)
			}
			if out, err := DecryptAudit(testNonzeroScalar(t, 1), context, altered, root); out != nil || !errors.Is(err, ErrAuditDecrypt) {
				t.Fatal("nonce/context mismatch disclosed plaintext")
			}
		})
	}
}
