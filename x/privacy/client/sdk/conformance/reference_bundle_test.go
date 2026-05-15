package conformance_test

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	privacydisclosure "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/disclosure"
	privacyidentity "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/identity"
	privacyscan "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/scan"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

const updateReadonlyReferenceFixtureEnv = "PRIVACY_UPDATE_READONLY_REFERENCE_FIXTURES"

type readonlyReferenceBundle struct {
	SchemaVersion string                      `json:"schema_version"`
	Phase         string                      `json:"phase"`
	SourceFixture string                      `json:"source_fixture"`
	Sender        readonlyIdentityBundle      `json:"sender"`
	Recipient     readonlyIdentityBundle      `json:"recipient"`
	Disclosure    readonlyDisclosureBundle    `json:"disclosure"`
	Scan          readonlyReferenceScanBundle `json:"scan"`
}

type readonlyIdentityBundle struct {
	TransparentAddress   string                         `json:"transparent_address"`
	ShowAddress          readonlyShieldedAddressSummary `json:"show_address"`
	ShowViewKey          readonlyViewingKeySummary      `json:"show_view_key"`
	ShowDisclosurePubKey readonlyDisclosureKeySummary   `json:"show_disclosure_pubkey"`
}

type readonlyShieldedAddressSummary struct {
	FromAddress string `json:"from_address"`
	Address     string `json:"address"`
	DerivedFrom string `json:"derived_from"`
	Usage       string `json:"usage"`
}

type readonlyViewingKeySummary struct {
	FromAddress        string `json:"from_address"`
	IncomingViewKeyHex string `json:"incoming_viewing_key_hex"`
	ViewPublicKeyHex   string `json:"view_public_key_hex"`
	DerivedFrom        string `json:"derived_from"`
}

type readonlyDisclosureKeySummary struct {
	FromAddress  string `json:"from_address"`
	PublicKeyHex string `json:"public_key_hex"`
	DerivedFrom  string `json:"derived_from"`
}

type readonlyDisclosureBundle struct {
	Plane            string `json:"plane"`
	Delivery         string `json:"delivery"`
	Policy           string `json:"policy"`
	Amount           string `json:"amount"`
	AssetDenom       string `json:"asset_denom"`
	FromShieldedAddr string `json:"from_shielded_address"`
	ToShieldedAddr   string `json:"to_shielded_address"`
	Verified         bool   `json:"verified"`
	DigestHex        string `json:"digest_hex"`
}

type readonlyReferenceScanBundle struct {
	DepositFound  []readonlyFoundNote `json:"deposit_found"`
	TransferFound []readonlyFoundNote `json:"transfer_found"`
}

type readonlyFoundNote struct {
	TxHash                  string `json:"tx_hash"`
	Height                  int64  `json:"height"`
	Nullifier               string `json:"nullifier"`
	Amount                  string `json:"amount"`
	AssetDenom              string `json:"asset_denom"`
	ReceiverShieldedAddress string `json:"receiver_shielded_address"`
}

func TestReadonlyReferenceBundleFixture(t *testing.T) {
	expected := buildReadonlyReferenceBundle(t)
	actual := loadReadonlyReferenceBundle(t)

	require.Equal(t, expected, actual)
}

func TestWriteReadonlyReferenceBundleFixture(t *testing.T) {
	if os.Getenv(updateReadonlyReferenceFixtureEnv) != "1" {
		t.Skip("set PRIVACY_UPDATE_READONLY_REFERENCE_FIXTURES=1 to rewrite the read-only reference bundle fixture")
	}

	fixturePath := readonlyReferenceBundleFixturePath(t)
	payload, err := json.MarshalIndent(buildReadonlyReferenceBundle(t), "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(fixturePath, append(payload, '\n'), 0o644))
}

func buildReadonlyReferenceBundle(t *testing.T) readonlyReferenceBundle {
	t.Helper()

	vectors := loadGoldenVectors(t)

	senderRootSeed := mustDecodeHex(t, vectors.SenderRootSeed.RootSeedHex)
	recipientRootSeed := mustDecodeHex(t, vectors.RecipientRootSeed.RootSeedHex)

	senderSpendScalar, senderSpendPubKey, _ := privacyidentity.DeriveSpendKeys(senderRootSeed)
	senderViewScalar, senderViewPubKey, _ := privacyidentity.DeriveViewKeys(senderRootSeed)
	_, senderDisclosurePubKey, _ := privacyidentity.DeriveDisclosureKeys(senderRootSeed)
	senderViewPubKeyBytes := senderViewPubKey.Bytes()
	senderDisclosurePubKeyBytes := senderDisclosurePubKey.Bytes()
	senderShieldedAddress, err := privacytypes.EncodeShieldedAddressWithView(senderSpendPubKey, senderViewPubKey)
	require.NoError(t, err)

	_, recipientSpendPubKey, _ := privacyidentity.DeriveSpendKeys(recipientRootSeed)
	recipientViewScalar, recipientViewPubKey, _ := privacyidentity.DeriveViewKeys(recipientRootSeed)
	_, recipientDisclosurePubKey, _ := privacyidentity.DeriveDisclosureKeys(recipientRootSeed)
	recipientViewPubKeyBytes := recipientViewPubKey.Bytes()
	recipientDisclosurePubKeyBytes := recipientDisclosurePubKey.Bytes()
	recipientShieldedAddress, err := privacytypes.EncodeShieldedAddressWithView(recipientSpendPubKey, recipientViewPubKey)
	require.NoError(t, err)

	payload, err := privacydisclosure.DecodePublicPayloadHex(vectors.Disclosure.PayloadJSONHex)
	require.NoError(t, err)
	verification, err := privacydisclosure.VerifyPayload(payload, vectors.Disclosure.DigestHex)
	require.NoError(t, err)

	noteBytes, err := privacycrypto.Decrypt(mustDecodeHex(t, vectors.Note.EncryptedNoteHex), senderRootSeed)
	require.NoError(t, err)
	note, err := privacyscan.ParseNoteBytes(noteBytes)
	require.NoError(t, err)
	assetDenom := assetDenomFromNote(t, note)
	receiverAddress, err := note.ReceiverShieldedAddress()
	require.NoError(t, err)

	depositFound := privacyscan.ProcessTx(
		newPrivacyTx(t, vectors.Scan.TxHashHex, vectors.Scan.Height, privacytypes.EventTypeDeposit, privacytypes.AttributeKeyEncryptedNote, vectors.Note.EncryptedNoteHex),
		senderRootSeed,
		senderSpendScalar,
		senderViewScalar,
	)
	require.Len(t, depositFound, 1)

	transferCipherText, err := privacycrypto.AsymEncrypt(noteBytes, *senderViewPubKey)
	require.NoError(t, err)
	transferFound := privacyscan.ProcessTx(
		newPrivacyTx(t, vectors.Scan.TxHashHex, vectors.Scan.Height, privacytypes.EventTypeShieldedTransfer, privacytypes.AttributeKeyCipherText1, hex.EncodeToString(transferCipherText)),
		senderRootSeed,
		senderSpendScalar,
		senderViewScalar,
	)
	require.Len(t, transferFound, 1)

	return readonlyReferenceBundle{
		SchemaVersion: "v1",
		Phase:         "phase-a-read-only",
		SourceFixture: "privacy_wallet_golden_vectors.json",
		Sender: readonlyIdentityBundle{
			TransparentAddress: vectors.SenderRootSeed.Address,
			ShowAddress: readonlyShieldedAddressSummary{
				FromAddress: vectors.SenderRootSeed.Address,
				Address:     senderShieldedAddress,
				DerivedFrom: "transparent-keyring-root",
				Usage:       "share this full shielded address when someone needs to send you private funds",
			},
			ShowViewKey: readonlyViewingKeySummary{
				FromAddress:        vectors.SenderRootSeed.Address,
				IncomingViewKeyHex: privacyidentity.ScalarToFixedHex(senderViewScalar),
				ViewPublicKeyHex:   hex.EncodeToString(senderViewPubKeyBytes[:]),
				DerivedFrom:        "transparent-keyring-root",
			},
			ShowDisclosurePubKey: readonlyDisclosureKeySummary{
				FromAddress:  vectors.SenderRootSeed.Address,
				PublicKeyHex: hex.EncodeToString(senderDisclosurePubKeyBytes[:]),
				DerivedFrom:  "transparent-keyring-root",
			},
		},
		Recipient: readonlyIdentityBundle{
			TransparentAddress: vectors.RecipientRootSeed.Address,
			ShowAddress: readonlyShieldedAddressSummary{
				FromAddress: vectors.RecipientRootSeed.Address,
				Address:     recipientShieldedAddress,
				DerivedFrom: "transparent-keyring-root",
				Usage:       "share this full shielded address when someone needs to send you private funds",
			},
			ShowViewKey: readonlyViewingKeySummary{
				FromAddress:        vectors.RecipientRootSeed.Address,
				IncomingViewKeyHex: privacyidentity.ScalarToFixedHex(recipientViewScalar),
				ViewPublicKeyHex:   hex.EncodeToString(recipientViewPubKeyBytes[:]),
				DerivedFrom:        "transparent-keyring-root",
			},
			ShowDisclosurePubKey: readonlyDisclosureKeySummary{
				FromAddress:  vectors.RecipientRootSeed.Address,
				PublicKeyHex: hex.EncodeToString(recipientDisclosurePubKeyBytes[:]),
				DerivedFrom:  "transparent-keyring-root",
			},
		},
		Disclosure: readonlyDisclosureBundle{
			Plane:            string(payload.Plane),
			Delivery:         vectors.Disclosure.Mode,
			Policy:           vectors.Disclosure.Policy,
			Amount:           payload.Amount,
			AssetDenom:       payload.AssetDenom,
			FromShieldedAddr: payload.FromShieldedAddress,
			ToShieldedAddr:   payload.ToShieldedAddress,
			Verified:         verification.Verified,
			DigestHex:        vectors.Disclosure.DigestHex,
		},
		Scan: readonlyReferenceScanBundle{
			DepositFound: []readonlyFoundNote{
				{
					TxHash:                  depositFound[0].TxHash,
					Height:                  depositFound[0].Height,
					Nullifier:               depositFound[0].Nullifier,
					Amount:                  depositFound[0].Note.Amount.String(),
					AssetDenom:              assetDenom,
					ReceiverShieldedAddress: receiverAddress,
				},
			},
			TransferFound: []readonlyFoundNote{
				{
					TxHash:                  transferFound[0].TxHash,
					Height:                  transferFound[0].Height,
					Nullifier:               transferFound[0].Nullifier,
					Amount:                  transferFound[0].Note.Amount.String(),
					AssetDenom:              assetDenom,
					ReceiverShieldedAddress: receiverAddress,
				},
			},
		},
	}
}

func loadReadonlyReferenceBundle(t *testing.T) readonlyReferenceBundle {
	t.Helper()

	bz, err := os.ReadFile(readonlyReferenceBundleFixturePath(t))
	require.NoError(t, err)

	var bundle readonlyReferenceBundle
	require.NoError(t, json.Unmarshal(bz, &bundle))
	require.Equal(t, "v1", bundle.SchemaVersion)
	return bundle
}

func readonlyReferenceBundleFixturePath(t *testing.T) string {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)

	return filepath.Join(filepath.Dir(filename), "testdata", "privacy_wallet_readonly_reference_bundle.json")
}
