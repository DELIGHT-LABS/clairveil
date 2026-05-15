package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	sdkkeyring "github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	"github.com/stretchr/testify/require"

	privacyidentity "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/identity"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

const (
	rootSeedQualificationKeyName    = "alice"
	rootSeedQualificationPassphrase = "root-seed-passphrase"
	rootSeedQualificationMnemonic   = "bounce success option birth apple portion aunt rural episode solution hockey pencil lend session cause hedgehog slender journey system canvas decorate razor catch empty"
)

func TestComputePrivacyRootSeedDeterministic(t *testing.T) {
	address := "clair1rootseedtest"
	pubKey := []byte("transparent-pubkey")
	signature := []byte("deterministic-signature")

	rootSeed1 := computePrivacyRootSeed(address, pubKey, signature)
	rootSeed2 := computePrivacyRootSeed(address, pubKey, signature)

	require.Len(t, rootSeed1, privacyRootSeedLength)
	require.Equal(t, rootSeed1, rootSeed2)
}

func TestBuildPrivacyRootSigningMessageStable(t *testing.T) {
	message := buildPrivacyRootSigningMessage("clair1rootseedtest", []byte{0x01, 0x02, 0x03})

	require.Equal(t, "clairveil-root-v1\naddress:clair1rootseedtest\npubkey:010203", string(message))
}

func TestComputePrivacyRootSeedBindsTransparentMaterial(t *testing.T) {
	address := "clair1rootseedtest"
	pubKey := []byte("transparent-pubkey")
	signature := []byte("deterministic-signature")

	base := computePrivacyRootSeed(address, pubKey, signature)
	require.NotEqual(t, base, computePrivacyRootSeed("clair1other", pubKey, signature))
	require.NotEqual(t, base, computePrivacyRootSeed(address, []byte("other-pubkey"), signature))
	require.NotEqual(t, base, computePrivacyRootSeed(address, pubKey, []byte("other-signature")))
}

func TestDerivePrivacyDomainSeedSeparated(t *testing.T) {
	rootSeed := []byte("root-seed-material")

	spendSeed := derivePrivacyDomainSeed(rootSeed, privacySpendDomain)
	viewSeed := derivePrivacyDomainSeed(rootSeed, privacyViewDomain)
	disclosureSeed := derivePrivacyDomainSeed(rootSeed, privacyDisclosureDomain)

	require.NotEqual(t, spendSeed, viewSeed)
	require.NotEqual(t, spendSeed, disclosureSeed)
	require.NotEqual(t, viewSeed, disclosureSeed)
}

func TestDeriveDisclosureKeysDeterministic(t *testing.T) {
	rootSeed := []byte("root-seed-material")

	scalar1, pubKey1, seed1 := deriveDisclosureKeys(rootSeed)
	scalar2, pubKey2, seed2 := deriveDisclosureKeys(rootSeed)

	require.Equal(t, scalar1, scalar2)
	require.Equal(t, pubKey1.Bytes(), pubKey2.Bytes())
	require.Equal(t, seed1, seed2)
}

func TestResolveClientFromAddressRequiresFromAccount(t *testing.T) {
	_, err := resolveClientFromAddress(client.Context{})

	require.ErrorContains(t, err, "a transparent --from account is required to derive the privacy root seed")
}

func TestResolveClientFromAddressRequiresKeyring(t *testing.T) {
	_, err := resolveClientFromAddress(client.Context{}.WithFromName("alice"))

	require.ErrorContains(t, err, "a keyring is required to resolve --from \"alice\"")
}

func TestDerivePrivacyRootSeedDeterministicForQualifiedSoftwareBackends(t *testing.T) {
	for _, backend := range []string{sdkkeyring.BackendTest, sdkkeyring.BackendFile} {
		t.Run(backend, func(t *testing.T) {
			clientCtx, expectedAddress := newRootSeedQualificationClientContext(t, backend)

			rootSeed1, fromAddress1, err := derivePrivacyRootSeed(clientCtx)
			require.NoError(t, err)

			rootSeed2, fromAddress2, err := derivePrivacyRootSeed(clientCtx)
			require.NoError(t, err)

			require.Len(t, rootSeed1, privacyRootSeedLength)
			require.Equal(t, rootSeed1, rootSeed2)
			require.Equal(t, expectedAddress, fromAddress1.String())
			require.Equal(t, fromAddress1.String(), fromAddress2.String())
		})
	}
}

func TestNewKeyringPrivacyRootSignerRequiresKeyring(t *testing.T) {
	clientCtx := client.Context{}.
		WithFromAddress(sdk.AccAddress(bytes.Repeat([]byte{0x2}, 20))).
		WithFromName("alice")

	_, _, err := newKeyringPrivacyRootSigner(clientCtx)

	require.ErrorContains(t, err, "a keyring is required to derive the privacy root seed")
}

func TestKeyringPrivacyRootSignerUnavailableErrors(t *testing.T) {
	var signer *keyringPrivacyRootSigner

	_, err := signer.Address()
	require.ErrorContains(t, err, "privacy root signer address is unavailable")

	_, err = signer.PubKeyBytes()
	require.ErrorContains(t, err, "privacy root signer pubkey is unavailable")

	err = signer.VerifyPrivacyRoot([]byte("msg"), []byte("sig"))
	require.ErrorContains(t, err, "privacy root signer pubkey is unavailable")
}

func TestDerivePrivacyRootSeedMatchesAcrossTestAndFileBackends(t *testing.T) {
	testClientCtx, testAddress := newRootSeedQualificationClientContext(t, sdkkeyring.BackendTest)
	fileClientCtx, fileAddress := newRootSeedQualificationClientContext(t, sdkkeyring.BackendFile)

	testRootSeed, _, err := derivePrivacyRootSeed(testClientCtx)
	require.NoError(t, err)

	fileRootSeed, _, err := derivePrivacyRootSeed(fileClientCtx)
	require.NoError(t, err)

	require.Equal(t, testAddress, fileAddress)
	require.Equal(t, testRootSeed, fileRootSeed)
	require.Equal(t, deriveQualifiedShieldedAddress(t, testClientCtx), deriveQualifiedShieldedAddress(t, fileClientCtx))
	require.Equal(t, deriveQualifiedDisclosurePubKey(t, testClientCtx), deriveQualifiedDisclosurePubKey(t, fileClientCtx))
}

func TestKeyringPrivacyRootSignerDerivesAndVerifiesAcrossQualifiedBackends(t *testing.T) {
	for _, backend := range []string{sdkkeyring.BackendTest, sdkkeyring.BackendFile} {
		t.Run(backend, func(t *testing.T) {
			clientCtx, expectedAddress := newRootSeedQualificationClientContext(t, backend)

			signer, fromAddress, err := newKeyringPrivacyRootSigner(clientCtx)
			require.NoError(t, err)
			require.Equal(t, expectedAddress, fromAddress.String())

			rootSeedFromSigner, material, err := privacyidentity.DeriveRootSeedFromSigner(signer)
			require.NoError(t, err)
			require.NoError(t, privacyidentity.VerifyRootSeedMaterial(signer, material))

			rootSeedFromCLI, fromAddressFromCLI, err := derivePrivacyRootSeed(clientCtx)
			require.NoError(t, err)

			require.Equal(t, expectedAddress, fromAddressFromCLI.String())
			require.Equal(t, rootSeedFromSigner, rootSeedFromCLI)
		})
	}
}

func newRootSeedQualificationClientContext(t *testing.T, backend string) (client.Context, string) {
	t.Helper()

	encodingConfig := moduletestutil.MakeTestEncodingConfig()
	userInput := strings.NewReader("")
	if backend == sdkkeyring.BackendFile {
		userInput = strings.NewReader(strings.Repeat(rootSeedQualificationPassphrase+"\n", 32))
	}

	keyring, err := sdkkeyring.New("privacy-root-seed-qualification", backend, t.TempDir(), userInput, encodingConfig.Codec)
	require.NoError(t, err)

	record, err := keyring.NewAccount(
		rootSeedQualificationKeyName,
		rootSeedQualificationMnemonic,
		sdkkeyring.DefaultBIP39Passphrase,
		sdk.FullFundraiserPath,
		hd.Secp256k1,
	)
	require.NoError(t, err)

	address, err := record.GetAddress()
	require.NoError(t, err)

	return client.Context{}.
		WithKeyring(keyring).
		WithFrom(rootSeedQualificationKeyName).
		WithFromName(rootSeedQualificationKeyName), address.String()
}

func deriveQualifiedShieldedAddress(t *testing.T, clientCtx client.Context) string {
	t.Helper()

	_, spendPubKey, rootSeed, err := getExplicitKeys(clientCtx)
	require.NoError(t, err)

	_, viewPubKey, _ := deriveViewKeys(rootSeed)
	shieldedAddress, err := privacytypes.EncodeShieldedAddressWithView(spendPubKey, viewPubKey)
	require.NoError(t, err)

	return shieldedAddress
}

func deriveQualifiedDisclosurePubKey(t *testing.T, clientCtx client.Context) string {
	t.Helper()

	rootSeed, _, err := derivePrivacyRootSeed(clientCtx)
	require.NoError(t, err)

	_, disclosurePubKey, _ := deriveDisclosureKeys(rootSeed)
	return encodePointHex(disclosurePubKey)
}
