package auditfield

import (
	"errors"
	"testing"
)

func TestT01T03Point64AndKeyIDAreStrictlyBound(t *testing.T) {
	point := testPoint(t)
	raw := point.Bytes()
	parsed, err := ParsePoint64(raw)
	if err != nil {
		t.Fatal(err)
	}
	key, err := NewAuditKey(parsed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseAuditKey(raw, key.IDBytes()); err != nil {
		t.Fatal(err)
	}
	wrongID := key.IDBytes()
	wrongID[0] ^= 1
	if _, err := ParseAuditKey(raw, wrongID); !errors.Is(err, ErrInvalidAuditKey) {
		t.Fatal("independent KeyID accepted")
	}

	identity := make([]byte, PointSize)
	identity[PointSize-1] = 1
	if _, err := ParsePoint64(identity); !errors.Is(err, ErrInvalidPoint) {
		t.Fatal("identity accepted")
	}
	nonCanonical := append([]byte(nil), raw...)
	copy(nonCanonical[:FieldSize], fieldModulusBE[:])
	if _, err := ParsePoint64(nonCanonical); !errors.Is(err, ErrInvalidPoint) {
		t.Fatal("noncanonical coordinate accepted")
	}
	offCurve := make([]byte, PointSize)
	offCurve[FieldSize-1], offCurve[PointSize-1] = 1, 2
	if _, err := ParsePoint64(offCurve); !errors.Is(err, ErrInvalidPoint) {
		t.Fatal("off-curve point accepted")
	}
	if _, err := NewAuditKey(Point64{}); !errors.Is(err, ErrInvalidPoint) {
		t.Fatal("zero audit key accepted")
	}
}
