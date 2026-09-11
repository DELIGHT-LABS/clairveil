package auditfield

import (
	"encoding/binary"
	"testing"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/ctbn254/edwardsct"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/ctbn254/scalarct"
)

func testPoint(t testing.TB) Point64 {
	t.Helper()
	base := edwardsct.Base()
	raw, err := base.EncodePoint64()
	if err != nil {
		t.Fatal(err)
	}
	point, err := ParsePoint64(raw[:])
	if err != nil {
		t.Fatal(err)
	}
	return point
}

func testLegacyPoint(t testing.TB) []byte {
	t.Helper()
	base := edwardsct.Base()
	raw, err := base.CompressLegacy()
	if err != nil {
		t.Fatal(err)
	}
	return append([]byte(nil), raw[:]...)
}

func testContext(t testing.TB, kind Kind, inputs, outputs uint8) AuditContext {
	t.Helper()
	key, err := NewAuditKey(testPoint(t))
	if err != nil {
		t.Fatal(err)
	}
	pi := make([]Field32, AuditContextPublicFieldCount)
	hi, lo := DigestFields(key.ID())
	pi[2], pi[3] = hi, lo
	x, y, err := key.Point().Coordinates()
	if err != nil {
		t.Fatal(err)
	}
	pi[4], pi[5], pi[6], pi[7] = Field32FromUint64(1), x, y, Field32FromUint64(1)
	pi[9], pi[10] = Field32FromUint64(uint64(inputs)), Field32FromUint64(uint64(outputs))
	context, err := NewAuditContext(kind, pi)
	if err != nil {
		t.Fatal(err)
	}
	return context
}

func testNonzeroScalar(t testing.TB, value uint64) scalarct.NonzeroScalar {
	t.Helper()
	var raw [32]byte
	binary.BigEndian.PutUint64(raw[24:], value)
	s, err := scalarct.ParseNonzeroScalarBE32(raw[:])
	if err != nil {
		t.Fatal(err)
	}
	return s
}
