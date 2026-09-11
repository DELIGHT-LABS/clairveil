package auditfield

import (
	"errors"
	"testing"
)

func TestT11PoP96ExactTranscriptAndZeroResponseEncoding(t *testing.T) {
	key, err := NewAuditKey(testPoint(t)) // [1]G
	if err != nil {
		t.Fatal(err)
	}
	var network [32]byte
	network[0] = 7
	proof, err := CreatePoP96(key, testNonzeroScalar(t, 1), testNonzeroScalar(t, 9), network, 8, 99)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyPoP96(key, network, 8, 99, proof); err != nil {
		t.Fatal(err)
	}
	if err := VerifyPoP96(key, network, 9, 99, proof); err == nil {
		t.Fatal("epoch replacement accepted")
	}
	if err := VerifyPoP96(key, network, 8, 100, proof); err == nil {
		t.Fatal("activation replacement accepted")
	}
	raw, err := proof.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	for i := PointSize; i < len(raw); i++ {
		raw[i] = 0
	}
	parsed, err := ParsePoP96(raw)
	if err != nil {
		t.Fatalf("zero PoP response rejected: %v", err)
	}
	if parsed.s.IsZero() != 1 {
		t.Fatal("zero PoP response was not retained")
	}
	if _, err := ParsePoP96(raw[:len(raw)-1]); err == nil {
		t.Fatal("short PoP accepted")
	}
	if _, err := ParsePoP96(append(make([]byte, PointSize), make([]byte, FieldSize)...)); !errors.Is(err, ErrInvalidPoint) {
		t.Fatal("identity R accepted")
	}
}
