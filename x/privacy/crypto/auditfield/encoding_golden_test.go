package auditfield

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// Independent golden hashes commit the complete byte order, nonzero nonce,
// coordinates, ciphertext and tag. These are framing fixtures, not valid DH
// points or encryption KATs.
func TestT01EnvelopeEncodingGolden(t *testing.T) {
	cases := []struct {
		kind            Kind
		inputs, outputs uint8
		n               int
		digest          string
	}{
		{KindDeposit, 0, 1, 6, "fe6e4431f84d1df9af4dd2a3d67772a237310440bb79b134196e3f849d6865f1"},
		{KindWithdraw, 1, 0, 2, "7d36cf36da4f6b5b0cc96e52c207204376025c500b0e8913a95513a5a7826176"},
		{KindTransfer2x2, 2, 2, 13, "363a8f49c25cbfee00969636757c210dc7f3f4c1cdac98bb5c25310c77b6434b"},
		{KindBatch16x32, 16, 32, 177, "9e426dfa26c5a4ab4a1c21d31849377d6e13948781ed3bf5db53adc6d7eeef7d"},
	}
	for _, tc := range cases {
		fields := make([]Field32, tc.n)
		for i := range fields {
			fields[i] = Field32FromUint64(uint64(100 + i))
		}
		point := testPoint(t)
		var nonce Nonce128
		for i := range nonce {
			nonce[i] = byte(i)
		}
		e, err := NewEnvelopeFrame(tc.kind, tc.inputs, tc.outputs, nonce, point, fields, Field32FromUint64(999))
		if err != nil {
			t.Fatal(err)
		}
		raw, err := e.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(raw)
		if hex.EncodeToString(sum[:]) != tc.digest {
			t.Fatalf("kind %d golden changed: %x", tc.kind, sum)
		}
		decoded, err := ParseEnvelopeFrame(tc.kind, tc.inputs, tc.outputs, raw)
		if err != nil {
			t.Fatal(err)
		}
		// Mutations must neither alias the decoder's state nor bypass validation.
		raw[len(raw)-1] ^= 1
		if decoded.Tag() != Field32FromUint64(999) {
			t.Fatal("decoder retained raw input")
		}
		for _, offset := range []int{16, 18, 20, 21, 22, 23, 31} {
			malformed, _ := e.Bytes()
			malformed[offset] ^= 0xff
			if _, err := ParseEnvelopeFrame(tc.kind, tc.inputs, tc.outputs, malformed); err == nil {
				t.Fatalf("invalid header offset %d accepted", offset)
			}
		}
		p, _ := hex.DecodeString("30644e72e131a029b85045b68181585d2833e84879b9709143e1f593f0000001")
		for _, offset := range []int{48, 80, 112, len(raw) - 32} {
			malformed, _ := e.Bytes()
			copy(malformed[offset:], p)
			if _, err := ParseEnvelopeFrame(tc.kind, tc.inputs, tc.outputs, malformed); err == nil {
				t.Fatalf("Fr modulus at %d accepted", offset)
			}
		}
	}
}

func TestT01CipherRootUsesFullFr(t *testing.T) {
	pMinusOne, _ := hex.DecodeString("30644e72e131a029b85045b68181585d2833e84879b9709143e1f593f0000000")
	raw := append(append([]byte(nil), pMinusOne...), pMinusOne...)
	root, err := ParseCipherRoot(raw)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := root.Bytes()
	if err != nil || hex.EncodeToString(encoded[:]) != hex.EncodeToString(raw) {
		t.Fatal("full Fr root codec mismatch")
	}
	for _, n := range []int{0, 32, 63, 65} {
		if _, err := ParseCipherRoot(make([]byte, n)); err == nil {
			t.Fatal("wrong root size accepted")
		}
	}
	raw[63]++
	if _, err := ParseCipherRoot(raw); err == nil {
		t.Fatal("root modulus accepted")
	}
}
