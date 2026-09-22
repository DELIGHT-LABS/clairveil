package auditfield

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestT01ContextRangeAndZeroValue(t *testing.T) {
	base := testContext(t, KindDeposit, 0, 1).PublicInputs()
	if _, err := NewAuditContext(KindDeposit, base); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		index, byteIndex int
		value            byte
	}{{0, 15, 1}, {1, 15, 1}, {2, 15, 1}, {3, 15, 1}, {17, 15, 1}, {18, 15, 1}, {19, 15, 1}, {20, 15, 1}, {4, 23, 1}, {7, 23, 1}, {15, 15, 1}, {9, 31, 32}, {10, 31, 64}, {4, 31, 0}, {7, 31, 0}, {9, 31, 1}, {10, 31, 0}} {
		pi := append([]Field32(nil), base...)
		pi[tc.index][tc.byteIndex] = tc.value
		if _, err := NewAuditContext(KindDeposit, pi); err == nil {
			t.Fatalf("PI %d out-of-range/count/zero accepted", tc.index)
		}
	}
	if _, err := (AuditContext{}).T(Nonce128{}); err == nil {
		t.Fatal("zero context accepted")
	}
	for _, n := range []int{0, 20, 23, 25} {
		if _, err := NewAuditContext(KindDeposit, make([]Field32, n)); err == nil {
			t.Fatal("wrong PI size accepted")
		}
	}
	if _, err := (EnvelopeFrame{}).Bytes(); err == nil {
		t.Fatal("zero frame accepted")
	}
}

func TestAuditContextPublicAmountUint128(t *testing.T) {
	for _, kind := range []Kind{KindDeposit, KindWithdraw} {
		inputs, outputs := uint8(0), uint8(1)
		if kind == KindWithdraw {
			inputs, outputs = 1, 0
		}
		pi := testContext(t, kind, inputs, outputs).PublicInputs()
		for _, raw := range []Field32{{23: 1}, {16: 255, 17: 255, 18: 255, 19: 255, 20: 255, 21: 255, 22: 255, 23: 255, 24: 255, 25: 255, 26: 255, 27: 255, 28: 255, 29: 255, 30: 255, 31: 255}} {
			pi[15] = raw
			if _, err := NewAuditContext(kind, pi); err != nil {
				t.Fatalf("uint128 amount rejected: %v", err)
			}
		}
		pi[15] = Field32{15: 1}
		if _, err := NewAuditContext(kind, pi); err == nil {
			t.Fatal("2^128 amount accepted")
		}
	}
}

func TestT19PlaintextCopiesAndRedaction(t *testing.T) {
	fields := []Field32{Field32FromUint64(77), Field32FromUint64(88)}
	plain, err := NewAuditPlain(KindWithdraw, fields)
	if err != nil {
		t.Fatal(err)
	}
	fields[0] = Field32{}
	out := plain.Fields()
	out[1] = Field32{}
	if plain.Fields()[0] != Field32FromUint64(77) || plain.Fields()[1] != Field32FromUint64(88) {
		t.Fatal("plaintext mutable alias")
	}
	for _, verb := range []string{"%v", "%+v", "%#v", "%d", "%b", "%x", "%s"} {
		for _, value := range []any{plain, &plain} {
			if got := fmt.Sprintf(verb, value); got != "auditfield.AuditPlain(<redacted>)" {
				t.Fatalf("plaintext not redacted for %s", verb)
			}
		}
	}
	if _, err := json.Marshal(plain); err == nil {
		t.Fatal("plaintext JSON serialization permitted")
	}
	if _, err := plain.MarshalText(); err == nil {
		t.Fatal("plaintext text serialization permitted")
	}
}
