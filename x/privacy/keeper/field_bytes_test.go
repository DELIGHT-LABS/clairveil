package keeper

import (
	"testing"

	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
	"github.com/stretchr/testify/require"
)

func TestCanonicalizeFieldBytes(t *testing.T) {
	canonical, err := canonicalizeFieldBytes([]byte{0x01})
	require.NoError(t, err)
	require.Len(t, canonical, fieldElementByteSize)
	require.Equal(t, byte(0x01), canonical[fieldElementByteSize-1])
}

func TestCanonicalizeFieldBytesTooLarge(t *testing.T) {
	tooLarge := make([]byte, fieldElementByteSize+1)
	tooLarge[0] = 0x01

	_, err := canonicalizeFieldBytes(tooLarge)
	require.Error(t, err)
}

func TestCanonicalizeFieldBytesOrOriginal(t *testing.T) {
	tooLarge := make([]byte, fieldElementByteSize+1)
	tooLarge[0] = 0x01

	out := canonicalizeFieldBytesOrOriginal(tooLarge)
	require.Equal(t, tooLarge, out)
}

func TestValidateFieldElementBytesStrict(t *testing.T) {
	valid := make([]byte, fieldElementByteSize)
	valid[fieldElementByteSize-1] = 0x01

	canonical, err := validateFieldElementBytesStrict(valid)
	require.NoError(t, err)
	require.Equal(t, valid, canonical)
}

func TestValidateFieldElementBytesStrictRejectsBadLength(t *testing.T) {
	_, err := validateFieldElementBytesStrict([]byte{0x01})
	require.Error(t, err)
}

func TestValidateFieldElementBytesStrictRejectsNonCanonical(t *testing.T) {
	outOfRange := fr.Modulus().Bytes()
	_, err := validateFieldElementBytesStrict(outOfRange[:])
	require.Error(t, err)
}
