package edwardsct

import (
	"math/big"
	"testing"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/ctbn254/scalarct"
	"github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
)

func TestScalarMultAndLegacyCompressionMatchGnark(t *testing.T) {
	curve := twistededwards.GetEdwardsCurve()
	for _, n := range []int64{1, 2, 3, 17, 255, 1 << 20} {
		var be [32]byte
		big.NewInt(n).FillBytes(be[:])
		s, ok := scalarct.FromCanonicalBE(be)
		if !ok {
			t.Fatal("scalar")
		}
		base, got := Base(), Point{}
		got.ScalarMult(&base, s)
		encoded, err := got.CompressLegacy()
		if err != nil {
			t.Fatal(err)
		}
		var want twistededwards.PointAffine
		want.ScalarMultiplication(&curve.Base, big.NewInt(n))
		wb := want.Bytes()
		if encoded != wb {
			t.Fatalf("scalar %d legacy compression mismatch", n)
		}
		decoded, err := DecodeLegacyCompressed(encoded[:])
		if err != nil {
			t.Fatalf("scalar %d decode: %v", n, err)
		}
		if !decoded.Equal(&got) {
			t.Fatalf("scalar %d decoded point mismatch", n)
		}
	}
}

func TestDecodePoint64RejectsIdentity(t *testing.T) {
	base := Base()
	encoded, err := base.EncodePoint64()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodePoint64(encoded[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodePoint64(make([]byte, 64)); err == nil {
		t.Fatal("identity accepted")
	}
}
