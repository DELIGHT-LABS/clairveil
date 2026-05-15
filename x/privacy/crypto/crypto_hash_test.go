package crypto

import (
	"testing"

	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
	"github.com/stretchr/testify/require"
)

func TestHashStringReturnsCanonicalFieldElement(t *testing.T) {
	assetID := HashString("uclair")
	require.Less(t, assetID.Cmp(fr.Modulus()), 0)
}
