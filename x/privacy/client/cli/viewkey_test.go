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
	spendScalar, err := deriveScalarFromSeed(derivePrivacyDomainSeed(rootSeed, privacySpendDomain))
	require.NoError(t, err)
	viewScalar, viewPubKey, _, deriveErr := deriveViewKeys(rootSeed)
	require.NoError(t, deriveErr)

	require.NotEqual(t, spendScalar.Bytes(), viewScalar.Bytes())
	require.NotNil(t, viewPubKey)
}

func TestScalarToFixedHex(t *testing.T) {
	scalar, err := deriveScalarFromSeed(derivePrivacyDomainSeed([]byte("hex-seed"), privacySpendDomain))
	require.NoError(t, err)
	hexValue := scalarToFixedHex(scalar)
	require.Len(t, hexValue, 64)
	_, err = hex.DecodeString(hexValue)
	require.NoError(t, err)
}
