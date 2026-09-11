package crypto

import (
	"crypto/sha256"
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLegacyMiMCPinnedConstantsHash(t *testing.T) {
	h := sha256.New()
	for i := range legacyMiMCConstants {
		b := legacyMiMCConstants[i].Bytes()
		_, _ = h.Write(b[:])
	}
	require.Equal(t, "2ed5ea434af73b6d62b0cdd47f4c8eb3b7592a80ca9f670aceafbbcf1dade368", hex.EncodeToString(h.Sum(nil)))
}

func TestLegacyMiMCMatchesCompatibilityHash(t *testing.T) {
	inputs := []*big.Int{big.NewInt(0), big.NewInt(1), big.NewInt(42), HashString("clairveil.mimc.kat")}
	values := make([]FieldValue, len(inputs))
	for i, input := range inputs {
		b := input.FillBytes(make([]byte, 32))
		v, err := ParseFieldValueBE32(b)
		require.NoError(t, err)
		values[i] = v
	}
	got, err := LegacyMiMCHash(values...)
	require.NoError(t, err)
	want := MimcHash(inputs...)
	gotBytes := got.Bytes()
	require.Equal(t, want.FillBytes(make([]byte, 32)), gotBytes[:])
}
