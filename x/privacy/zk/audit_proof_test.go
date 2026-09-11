package zk

import (
	"bytes"
	"github.com/consensys/gnark-crypto/ecc/bn254"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestAuditProofRejectsG2OutsideSubgroup(t *testing.T) {
	// Construct an on-curve point without clearing the cofactor. Disabling the
	// check is confined to this adversarial fixture, never the product decoder.
	var point bn254.G2Affine
	found := false
	for x := 0; x < 256; x++ {
		var candidate [64]byte
		candidate[0] = 0x80
		candidate[63] = byte(x)
		err := bn254.NewDecoder(bytes.NewReader(candidate[:]), bn254.NoSubgroupChecks()).Decode(&point)
		if err == nil && point.IsOnCurve() && !point.IsInSubGroup() {
			found = true
			break
		}
	}
	require.True(t, found)
	frame := make([]byte, CanonicalBN254Groth16ProofSize)
	frame[0], frame[96], frame[132] = 0x40, 0x40, 0x40
	g2 := point.Bytes()
	copy(frame[32:96], g2[:])
	require.NoError(t, ValidateCanonicalProofFramingBN254(frame))
	_, err := ReadCanonicalProofBN254(frame)
	require.Error(t, err)
}
func TestAuditProofRejectsNoncanonicalInfinity(t *testing.T) {
	frame := make([]byte, CanonicalBN254Groth16ProofSize)
	for _, offset := range compressedProofPointOffsets {
		frame[offset] = 0x40
	}
	require.NoError(t, ValidateCanonicalProofBN254(frame))
	frame[1] = 1
	_, err := ReadCanonicalProofBN254(frame)
	require.Error(t, err)
}
