package cli

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeriveViewSeedDeterministic(t *testing.T) {
	rootSeed := []byte("seed-for-test")
	v1 := derivePrivacyDomainSeed(rootSeed, privacyViewDomain)
	v2 := derivePrivacyDomainSeed(rootSeed, privacyViewDomain)

	require.Equal(t, v1, v2)
	require.NotEqual(t, rootSeed, v1)
}

func TestDeriveViewKeysDistinctFromSpendSeed(t *testing.T) {
	rootSeed := []byte("another-seed")
	spendScalar := deriveScalarFromSeed(derivePrivacyDomainSeed(rootSeed, privacySpendDomain))
	viewScalar, viewPubKey, _ := deriveViewKeys(rootSeed)

	require.NotZero(t, spendScalar.Cmp(viewScalar))
	require.NotNil(t, viewPubKey)
}

func TestScalarToFixedHex(t *testing.T) {
	hexValue := scalarToFixedHex(deriveScalarFromSeed([]byte("hex-seed")))
	require.Len(t, hexValue, 64)
	_, err := hex.DecodeString(hexValue)
	require.NoError(t, err)
}
