package crypto

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
	"github.com/consensys/gnark-crypto/ecc/bn254/fr/mimc"
	"github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
	cryptoeddsa "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards/eddsa"
	"github.com/stretchr/testify/require"
)

func TestDecodeCanonicalPointAcceptsPrimeSubgroupPoint(t *testing.T) {
	curve := twistededwards.GetEdwardsCurve()
	var point twistededwards.PointAffine
	point.ScalarMultiplication(&curve.Base, big.NewInt(17))
	encoded := point.Bytes()

	decoded, err := DecodeCanonicalPoint(encoded[:])
	require.NoError(t, err)
	require.True(t, decoded.Equal(&point))
}

func TestDecodeCanonicalPointRejectsMalformedPoints(t *testing.T) {
	curve := twistededwards.GetEdwardsCurve()
	var valid twistededwards.PointAffine
	valid.ScalarMultiplication(&curve.Base, big.NewInt(17))
	validEncoded := valid.Bytes()

	identity := twistededwards.PointAffine{}
	identity.Y.SetOne()
	identityEncoded := identity.Bytes()

	orderTwo := twistededwards.PointAffine{}
	orderTwo.Y.SetOne()
	orderTwo.Y.Neg(&orderTwo.Y)
	require.True(t, orderTwo.IsOnCurve())
	require.False(t, orderTwo.IsZero())
	orderTwoEncoded := orderTwo.Bytes()

	nonCanonical := nonCanonicalEncoding(t, valid)
	offCurve := offCurveEncoding(t)

	tests := []struct {
		name    string
		encoded []byte
		want    string
	}{
		{name: "truncated", encoded: validEncoded[:CanonicalPointSize-1], want: "expected exactly 32 bytes"},
		{name: "oversized", encoded: append(validEncoded[:], 0), want: "expected exactly 32 bytes"},
		{name: "non-canonical", encoded: nonCanonical, want: "non-canonical compressed encoding"},
		{name: "off-curve", encoded: offCurve, want: "point is not on the curve"},
		{name: "identity", encoded: identityEncoded[:], want: "identity point is not allowed"},
		{name: "non-subgroup", encoded: orderTwoEncoded[:], want: "point is not in the prime-order subgroup"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeCanonicalPoint(tt.encoded)
			require.ErrorIs(t, err, ErrInvalidPointEncoding)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestValidatePrimeSubgroupPointRejectsNil(t *testing.T) {
	require.ErrorIs(t, ValidatePrimeSubgroupPoint(nil), ErrInvalidPointEncoding)
}

func TestDecodeCanonicalEdDSASignatureAcceptsCanonicalSignature(t *testing.T) {
	encoded := canonicalSignature(t)

	decoded, err := DecodeCanonicalEdDSASignature(encoded)
	require.NoError(t, err)
	require.Equal(t, encoded, decoded.Bytes())
}

func TestDecodeCanonicalEdDSASignatureRejectsMalformedSignature(t *testing.T) {
	valid := canonicalSignature(t)

	identity := twistededwards.PointAffine{}
	identity.Y.SetOne()
	identityEncoded := identity.Bytes()

	orderTwo := twistededwards.PointAffine{}
	orderTwo.Y.SetOne()
	orderTwo.Y.Neg(&orderTwo.Y)
	orderTwoEncoded := orderTwo.Bytes()

	nonCanonicalR := nonCanonicalEncoding(t, signaturePoint(t, valid))
	scalarAtOrder := make([]byte, fr.Bytes)
	curve := twistededwards.GetEdwardsCurve()
	curve.Order.FillBytes(scalarAtOrder)

	tests := []struct {
		name      string
		signature []byte
		want      string
	}{
		{name: "truncated", signature: valid[:CanonicalEdDSASignatureSize-1], want: "expected exactly 64 bytes"},
		{name: "oversized", signature: append(append([]byte(nil), valid...), 0), want: "expected exactly 64 bytes"},
		{name: "non-canonical R", signature: replaceSignaturePart(valid, 0, nonCanonicalR), want: "non-canonical compressed encoding"},
		{name: "identity R", signature: replaceSignaturePart(valid, 0, identityEncoded[:]), want: "identity point is not allowed"},
		{name: "non-subgroup R", signature: replaceSignaturePart(valid, 0, orderTwoEncoded[:]), want: "point is not in the prime-order subgroup"},
		{name: "zero S", signature: replaceSignaturePart(valid, CanonicalPointSize, make([]byte, fr.Bytes)), want: "S must be greater than zero"},
		{name: "S at subgroup order", signature: replaceSignaturePart(valid, CanonicalPointSize, scalarAtOrder), want: "S must be smaller than the subgroup order"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeCanonicalEdDSASignature(tt.signature)
			require.ErrorIs(t, err, ErrInvalidEdDSASignature)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func FuzzDecodeCanonicalPointNoPanic(f *testing.F) {
	curve := twistededwards.GetEdwardsCurve()
	valid := curve.Base.Bytes()
	f.Add([]byte(nil))
	f.Add(valid[:])
	f.Add(make([]byte, CanonicalPointSize))
	f.Add(make([]byte, CanonicalPointSize+1))

	f.Fuzz(func(t *testing.T, encoded []byte) {
		_, _ = DecodeCanonicalPoint(encoded)
	})
}

func FuzzDecodeCanonicalEdDSASignatureNoPanic(f *testing.F) {
	f.Add([]byte(nil))
	f.Add(make([]byte, CanonicalEdDSASignatureSize))
	f.Add(make([]byte, CanonicalEdDSASignatureSize+1))

	f.Fuzz(func(t *testing.T, encoded []byte) {
		_, _ = DecodeCanonicalEdDSASignature(encoded)
	})
}

func canonicalSignature(t *testing.T) []byte {
	t.Helper()
	privateKey, err := cryptoeddsa.GenerateKey(bytes.NewReader(bytes.Repeat([]byte{0x42}, 32)))
	require.NoError(t, err)
	message := make([]byte, fr.Bytes)
	message[len(message)-1] = 42
	signature, err := privateKey.Sign(message, mimc.NewMiMC())
	require.NoError(t, err)
	require.Len(t, signature, CanonicalEdDSASignatureSize)
	return signature
}

func signaturePoint(t *testing.T, signature []byte) twistededwards.PointAffine {
	t.Helper()
	var point twistededwards.PointAffine
	_, err := point.SetBytes(signature[:CanonicalPointSize])
	require.NoError(t, err)
	return point
}

func replaceSignaturePart(signature []byte, offset int, replacement []byte) []byte {
	result := append([]byte(nil), signature...)
	copy(result[offset:offset+len(replacement)], replacement)
	return result
}

func nonCanonicalEncoding(t *testing.T, point twistededwards.PointAffine) []byte {
	t.Helper()
	y := point.Y.BigInt(new(big.Int))
	y.Add(y, fr.Modulus())
	require.Less(t, y.BitLen(), 256)

	encoded := make([]byte, CanonicalPointSize)
	y.FillBytes(encoded)
	for i, j := 0, len(encoded)-1; i < j; i, j = i+1, j-1 {
		encoded[i], encoded[j] = encoded[j], encoded[i]
	}
	if point.X.LexicographicallyLargest() {
		encoded[len(encoded)-1] |= 0x80
	}
	return encoded
}

func offCurveEncoding(t *testing.T) []byte {
	t.Helper()
	for value := uint64(2); value < 1<<16; value++ {
		encoded := make([]byte, CanonicalPointSize)
		new(big.Int).SetUint64(value).FillBytes(encoded)
		for i, j := 0, len(encoded)-1; i < j; i, j = i+1, j-1 {
			encoded[i], encoded[j] = encoded[j], encoded[i]
		}

		var point twistededwards.PointAffine
		_, err := point.SetBytes(encoded)
		require.NoError(t, err)
		canonical := point.Bytes()
		if !point.IsOnCurve() && bytes.Equal(canonical[:], encoded) {
			return encoded
		}
	}
	t.Fatal("failed to find a canonical off-curve encoding")
	return nil
}
