package identity

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestBuildRootSigningMessageStable(t *testing.T) {
	message := BuildRootSigningMessage("clair1rootseedtest", []byte{0x01, 0x02, 0x03})

	require.Equal(t, "clairveil-root-v1\naddress:clair1rootseedtest\npubkey:010203", string(message))
}

func TestComputeRootSeedBindsTransparentMaterial(t *testing.T) {
	address := "clair1rootseedtest"
	pubKey := []byte("transparent-pubkey")
	signature := []byte("deterministic-signature")

	base := ComputeRootSeed(address, pubKey, signature)
	require.Len(t, base, RootSeedLength)
	require.NotEqual(t, base, ComputeRootSeed("clair1other", pubKey, signature))
	require.NotEqual(t, base, ComputeRootSeed(address, []byte("other-pubkey"), signature))
	require.NotEqual(t, base, ComputeRootSeed(address, pubKey, []byte("other-signature")))
}

func TestDeriveDomainSeedSeparated(t *testing.T) {
	rootSeed := []byte("root-seed-material")

	spendSeed := DeriveDomainSeed(rootSeed, SpendDomain)
	viewSeed := DeriveDomainSeed(rootSeed, ViewDomain)
	disclosureSeed := DeriveDomainSeed(rootSeed, DisclosureDomain)

	require.NotEqual(t, spendSeed, viewSeed)
	require.NotEqual(t, spendSeed, disclosureSeed)
	require.NotEqual(t, viewSeed, disclosureSeed)
}

func TestDeriveDisclosureKeysDeterministic(t *testing.T) {
	rootSeed := []byte("root-seed-material")

	scalar1, pubKey1, seed1 := DeriveDisclosureKeys(rootSeed)
	scalar2, pubKey2, seed2 := DeriveDisclosureKeys(rootSeed)

	require.Equal(t, scalar1, scalar2)
	require.Equal(t, pubKey1.Bytes(), pubKey2.Bytes())
	require.Equal(t, seed1, seed2)
}

func TestDeriveShieldedAddress(t *testing.T) {
	address, err := DeriveShieldedAddress([]byte("root-seed-material"))
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(address, privacytypes.ShieldedBech32Prefix))
}
