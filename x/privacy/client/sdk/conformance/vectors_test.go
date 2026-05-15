package conformance_test

import (
	"encoding/hex"
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	cmttypes "github.com/cometbft/cometbft/rpc/core/types"
	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
	"github.com/stretchr/testify/require"

	privacydisclosure "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/disclosure"
	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
	privacyidentity "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/identity"
	privacyscan "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/scan"
	privacytransfer "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/transfer"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

const updateGoldenVectorFixtureEnv = "PRIVACY_UPDATE_GOLDEN_VECTOR_FIXTURES"

type goldenVectors struct {
	SchemaVersion     string            `json:"schema_version"`
	SenderRootSeed    rootSeedFixture   `json:"sender_root_seed"`
	RecipientRootSeed rootSeedFixture   `json:"recipient_root_seed"`
	Sender            identityFixture   `json:"sender"`
	Recipient         identityFixture   `json:"recipient"`
	Note              noteFixture       `json:"note"`
	Disclosure        disclosureFixture `json:"disclosure"`
	Scan              scanFixture       `json:"scan"`
}

type rootSeedFixture struct {
	Address              string `json:"address"`
	TransparentPubKeyHex string `json:"transparent_pubkey_hex"`
	SignatureHex         string `json:"signature_hex"`
	SigningMessageHex    string `json:"signing_message_hex"`
	RootSeedHex          string `json:"root_seed_hex"`
}

type identityFixture struct {
	SpendScalarHex      string `json:"spend_scalar_hex"`
	SpendPubKeyHex      string `json:"spend_pubkey_hex"`
	ViewScalarHex       string `json:"view_scalar_hex"`
	ViewPubKeyHex       string `json:"view_pubkey_hex"`
	DisclosureScalarHex string `json:"disclosure_scalar_hex"`
	DisclosurePubKeyHex string `json:"disclosure_pubkey_hex"`
	ShieldedAddress     string `json:"shielded_address"`
}

type noteFixture struct {
	Amount           string `json:"amount"`
	Denom            string `json:"denom"`
	Memo             string `json:"memo"`
	NoteJSONHex      string `json:"note_json_hex"`
	EncryptedNoteHex string `json:"encrypted_note_hex"`
	CommitmentHex    string `json:"commitment_hex"`
	NullifierHex     string `json:"nullifier_hex"`
}

type disclosureFixture struct {
	Policy         string `json:"policy"`
	Mode           string `json:"mode"`
	PayloadJSONHex string `json:"payload_json_hex"`
	CiphertextHex  string `json:"ciphertext_hex"`
	DigestHex      string `json:"digest_hex"`
}

type scanFixture struct {
	TxHashHex string `json:"tx_hash_hex"`
	Height    int64  `json:"height"`
}

func TestGoldenVectorsRootSeedDerivation(t *testing.T) {
	vectors := loadGoldenVectors(t)

	assertRootSeedFixture(t, vectors.SenderRootSeed)
	assertRootSeedFixture(t, vectors.RecipientRootSeed)
}

func TestGoldenVectorsIdentityDerivation(t *testing.T) {
	vectors := loadGoldenVectors(t)

	assertIdentityFixture(t, mustDecodeHex(t, vectors.SenderRootSeed.RootSeedHex), vectors.Sender)
	assertIdentityFixture(t, mustDecodeHex(t, vectors.RecipientRootSeed.RootSeedHex), vectors.Recipient)
}

func TestGoldenVectorsNoteAndScanContracts(t *testing.T) {
	vectors := loadGoldenVectors(t)

	rootSeed := mustDecodeHex(t, vectors.SenderRootSeed.RootSeedHex)
	spendScalar, _, _ := privacyidentity.DeriveSpendKeys(rootSeed)
	viewScalar, viewPubKey, _ := privacyidentity.DeriveViewKeys(rootSeed)

	noteBytes, err := privacycrypto.Decrypt(mustDecodeHex(t, vectors.Note.EncryptedNoteHex), rootSeed)
	require.NoError(t, err)
	require.Equal(t, vectors.Note.NoteJSONHex, hex.EncodeToString(noteBytes))

	note, err := privacyscan.ParseNoteBytes(noteBytes)
	require.NoError(t, err)
	require.Equal(t, vectors.Note.Amount, note.Amount.String())
	require.Equal(t, vectors.Note.Memo, note.Memo)
	require.Equal(t, vectors.Note.Denom, assetDenomFromNote(t, note))

	receiverAddress, err := note.ReceiverShieldedAddress()
	require.NoError(t, err)
	require.Equal(t, vectors.Sender.ShieldedAddress, receiverAddress)

	commitmentHex, err := privacyfield.CanonicalHexFromBigInt(note.ComputeCommitment())
	require.NoError(t, err)
	require.Equal(t, vectors.Note.CommitmentHex, commitmentHex)

	nullifierHex, err := privacyfield.CanonicalHexFromBigInt(note.ComputeNullifier())
	require.NoError(t, err)
	require.Equal(t, vectors.Note.NullifierHex, nullifierHex)

	depositTx := newPrivacyTx(
		t,
		vectors.Scan.TxHashHex,
		vectors.Scan.Height,
		privacytypes.EventTypeDeposit,
		privacytypes.AttributeKeyEncryptedNote,
		vectors.Note.EncryptedNoteHex,
	)
	depositFound := privacyscan.ProcessTx(depositTx, rootSeed, spendScalar, viewScalar)
	require.Len(t, depositFound, 1)
	require.Equal(t, vectors.Note.NullifierHex, depositFound[0].Nullifier)
	require.Equal(t, vectors.Scan.TxHashHex, depositFound[0].TxHash)
	require.Equal(t, vectors.Scan.Height, depositFound[0].Height)

	transferCipherText, err := privacycrypto.AsymEncrypt(noteBytes, *viewPubKey)
	require.NoError(t, err)

	transferTx := newPrivacyTx(
		t,
		vectors.Scan.TxHashHex,
		vectors.Scan.Height,
		privacytypes.EventTypeShieldedTransfer,
		privacytypes.AttributeKeyCipherText1,
		hex.EncodeToString(transferCipherText),
	)
	transferFound := privacyscan.ProcessTx(transferTx, rootSeed, spendScalar, viewScalar)
	require.Len(t, transferFound, 1)
	require.Equal(t, vectors.Note.NullifierHex, transferFound[0].Nullifier)
	require.Equal(t, vectors.Scan.TxHashHex, transferFound[0].TxHash)
	require.Equal(t, vectors.Scan.Height, transferFound[0].Height)
}

func TestGoldenVectorsDisclosureContracts(t *testing.T) {
	vectors := loadGoldenVectors(t)

	payload, err := privacydisclosure.DecodePublicPayloadHex(vectors.Disclosure.PayloadJSONHex)
	require.NoError(t, err)
	require.Equal(t, privacydisclosure.PayloadVersion, payload.Version)
	require.Equal(t, privacydisclosure.PlaneUser, payload.Plane)
	require.Equal(t, vectors.Note.Amount, payload.Amount)
	require.Equal(t, vectors.Note.Denom, payload.AssetDenom)
	require.Equal(t, vectors.Sender.ShieldedAddress, payload.FromShieldedAddress)
	require.Equal(t, vectors.Recipient.ShieldedAddress, payload.ToShieldedAddress)
	require.Equal(t, []string{"amount", "from_shielded_address", "to_shielded_address"}, privacydisclosure.DisclosedFields(payload))

	verification, err := privacydisclosure.VerifyPayload(payload, vectors.Disclosure.DigestHex)
	require.NoError(t, err)
	require.True(t, verification.Verified)
	require.True(t, verification.LocalDisclosureDigestMatch)
	require.True(t, verification.OnChainDisclosureDigestUsed)
	require.True(t, verification.OnChainDisclosureDigestMatch)

	disclosureScalar, _, _ := privacyidentity.DeriveDisclosureKeys(mustDecodeHex(t, vectors.RecipientRootSeed.RootSeedHex))
	decrypted, err := privacydisclosure.DecryptPayloadHex(vectors.Disclosure.CiphertextHex, disclosureScalar)
	require.NoError(t, err)
	require.Equal(t, payload, decrypted)
}

func TestWriteGoldenVectorsFixture(t *testing.T) {
	if os.Getenv(updateGoldenVectorFixtureEnv) != "1" {
		t.Skipf("set %s=1 to rewrite the golden vector fixture", updateGoldenVectorFixtureEnv)
	}

	payload, err := json.MarshalIndent(buildGoldenVectorsFixture(t), "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(goldenVectorsFixturePath(t), append(payload, '\n'), 0o644))
}

func buildGoldenVectorsFixture(t *testing.T) goldenVectors {
	t.Helper()

	senderRootSeed := buildRootSeedFixture(
		t,
		"clair1lm4z6g0p7n0jq3m2t0y8m7s8u6x5w4v3c2r9q8",
		"0123456789abcdef",
		"73656e6465722d726f6f742d7369676e61747572652d7631",
	)
	recipientRootSeed := buildRootSeedFixture(
		t,
		"clair1z8v9c0x2l4m6n8p0q2r4s6t8u0w2y4z6a8s0d2",
		"deadbeef10203040",
		"726563697069656e742d726f6f742d7369676e61747572652d7631",
	)

	senderSeed := mustDecodeHex(t, senderRootSeed.RootSeedHex)
	recipientSeed := mustDecodeHex(t, recipientRootSeed.RootSeedHex)
	sender := buildIdentityFixture(t, senderSeed)
	recipient := buildIdentityFixture(t, recipientSeed)

	_, senderSpendPubKey, _ := privacyidentity.DeriveSpendKeys(senderSeed)
	_, senderViewPubKey, _ := privacyidentity.DeriveViewKeys(senderSeed)
	_, recipientSpendPubKey, _ := privacyidentity.DeriveSpendKeys(recipientSeed)
	_, recipientViewPubKey, _ := privacyidentity.DeriveViewKeys(recipientSeed)
	_, recipientDisclosurePubKey, _ := privacyidentity.DeriveDisclosureKeys(recipientSeed)

	note := privacytypes.Note{
		ReceiverSpendPubKeyX: pointX(senderSpendPubKey),
		ReceiverSpendPubKeyY: pointY(senderSpendPubKey),
		ReceiverViewPubKeyX:  pointX(senderViewPubKey),
		ReceiverViewPubKeyY:  pointY(senderViewPubKey),
		Amount:               big.NewInt(7),
		AssetID:              privacycrypto.HashString("uclair"),
		Randomness:           big.NewInt(1771001),
		Memo:                 "golden-vector",
	}
	noteBytes := note.Bytes()
	encryptedNote, err := privacycrypto.Encrypt(noteBytes, senderSeed)
	require.NoError(t, err)
	commitmentHex, err := privacyfield.CanonicalHexFromBigInt(note.ComputeCommitment())
	require.NoError(t, err)
	nullifierHex, err := privacyfield.CanonicalHexFromBigInt(note.ComputeNullifier())
	require.NoError(t, err)

	recipientOutputNote := privacytypes.Note{
		ReceiverSpendPubKeyX: pointX(recipientSpendPubKey),
		ReceiverSpendPubKeyY: pointY(recipientSpendPubKey),
		ReceiverViewPubKeyX:  pointX(recipientViewPubKey),
		ReceiverViewPubKeyY:  pointY(recipientViewPubKey),
		Amount:               big.NewInt(7),
		AssetID:              privacycrypto.HashString("uclair"),
		Randomness:           big.NewInt(1771002),
		Memo:                 "golden-vector-recipient-output",
	}
	recipientOutputCommitment, err := privacyfield.CanonicalBytesFromBigInt(recipientOutputNote.ComputeCommitment())
	require.NoError(t, err)
	disclosure, err := privacytransfer.BuildUserDisclosureData(
		privacytransfer.DisclosureBuildInput{
			OutputCommitment: recipientOutputCommitment,
			TransferDenom:    "uclair",
			FromNote:         note,
			RecipientNote:    recipientOutputNote,
		},
		privacytypes.TransferPrivacyPolicyDiscloseAmountToFrom,
		privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_RECIPIENT_ENCRYPTED,
		recipientDisclosurePubKey,
	)
	require.NoError(t, err)

	return goldenVectors{
		SchemaVersion:     "v1",
		SenderRootSeed:    senderRootSeed,
		RecipientRootSeed: recipientRootSeed,
		Sender:            sender,
		Recipient:         recipient,
		Note: noteFixture{
			Amount:           note.Amount.String(),
			Denom:            "uclair",
			Memo:             note.Memo,
			NoteJSONHex:      hex.EncodeToString(noteBytes),
			EncryptedNoteHex: hex.EncodeToString(encryptedNote),
			CommitmentHex:    commitmentHex,
			NullifierHex:     nullifierHex,
		},
		Disclosure: disclosureFixture{
			Policy:         "amount-from-to",
			Mode:           "recipient-encrypted",
			PayloadJSONHex: hex.EncodeToString(disclosure.PayloadJSON),
			CiphertextHex:  hex.EncodeToString(disclosure.CipherText),
			DigestHex:      hex.EncodeToString(disclosure.Digest),
		},
		Scan: scanFixture{
			TxHashHex: "AABBCC",
			Height:    42,
		},
	}
}

func buildRootSeedFixture(t *testing.T, address string, pubKeyHex string, signatureHex string) rootSeedFixture {
	t.Helper()

	pubKey := mustDecodeHex(t, pubKeyHex)
	signature := mustDecodeHex(t, signatureHex)
	signingMessage := privacyidentity.BuildRootSigningMessage(address, pubKey)
	rootSeed := privacyidentity.ComputeRootSeed(address, pubKey, signature)

	return rootSeedFixture{
		Address:              address,
		TransparentPubKeyHex: pubKeyHex,
		SignatureHex:         signatureHex,
		SigningMessageHex:    hex.EncodeToString(signingMessage),
		RootSeedHex:          hex.EncodeToString(rootSeed),
	}
}

func buildIdentityFixture(t *testing.T, rootSeed []byte) identityFixture {
	t.Helper()

	spendScalar, spendPubKey, _ := privacyidentity.DeriveSpendKeys(rootSeed)
	viewScalar, viewPubKey, _ := privacyidentity.DeriveViewKeys(rootSeed)
	disclosureScalar, disclosurePubKey, _ := privacyidentity.DeriveDisclosureKeys(rootSeed)
	shieldedAddress, err := privacytypes.EncodeShieldedAddressWithView(spendPubKey, viewPubKey)
	require.NoError(t, err)

	return identityFixture{
		SpendScalarHex:      privacyidentity.ScalarToFixedHex(spendScalar),
		SpendPubKeyHex:      pointHex(spendPubKey),
		ViewScalarHex:       privacyidentity.ScalarToFixedHex(viewScalar),
		ViewPubKeyHex:       pointHex(viewPubKey),
		DisclosureScalarHex: privacyidentity.ScalarToFixedHex(disclosureScalar),
		DisclosurePubKeyHex: pointHex(disclosurePubKey),
		ShieldedAddress:     shieldedAddress,
	}
}

func pointX(point *crypto_tedwards.PointAffine) *big.Int {
	out := new(big.Int)
	point.X.BigInt(out)
	return out
}

func pointY(point *crypto_tedwards.PointAffine) *big.Int {
	out := new(big.Int)
	point.Y.BigInt(out)
	return out
}

func loadGoldenVectors(t *testing.T) goldenVectors {
	t.Helper()

	bz, err := os.ReadFile(goldenVectorsFixturePath(t))
	require.NoError(t, err)

	var vectors goldenVectors
	require.NoError(t, json.Unmarshal(bz, &vectors))
	require.Equal(t, "v1", vectors.SchemaVersion)
	return vectors
}

func goldenVectorsFixturePath(t *testing.T) string {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)

	return filepath.Join(filepath.Dir(filename), "testdata", "privacy_wallet_golden_vectors.json")
}

func assertRootSeedFixture(t *testing.T, fixture rootSeedFixture) {
	t.Helper()

	pubKey := mustDecodeHex(t, fixture.TransparentPubKeyHex)
	signature := mustDecodeHex(t, fixture.SignatureHex)

	signingMessage := privacyidentity.BuildRootSigningMessage(fixture.Address, pubKey)
	require.Equal(t, fixture.SigningMessageHex, hex.EncodeToString(signingMessage))

	rootSeed := privacyidentity.ComputeRootSeed(fixture.Address, pubKey, signature)
	require.Equal(t, fixture.RootSeedHex, hex.EncodeToString(rootSeed))
}

func assertIdentityFixture(t *testing.T, rootSeed []byte, fixture identityFixture) {
	t.Helper()

	spendScalar, spendPubKey, _ := privacyidentity.DeriveSpendKeys(rootSeed)
	viewScalar, viewPubKey, _ := privacyidentity.DeriveViewKeys(rootSeed)
	disclosureScalar, disclosurePubKey, _ := privacyidentity.DeriveDisclosureKeys(rootSeed)

	require.Equal(t, fixture.SpendScalarHex, privacyidentity.ScalarToFixedHex(spendScalar))
	require.Equal(t, fixture.SpendPubKeyHex, pointHex(spendPubKey))
	require.Equal(t, fixture.ViewScalarHex, privacyidentity.ScalarToFixedHex(viewScalar))
	require.Equal(t, fixture.ViewPubKeyHex, pointHex(viewPubKey))
	require.Equal(t, fixture.DisclosureScalarHex, privacyidentity.ScalarToFixedHex(disclosureScalar))
	require.Equal(t, fixture.DisclosurePubKeyHex, pointHex(disclosurePubKey))

	shieldedAddress, err := privacytypes.EncodeShieldedAddressWithView(spendPubKey, viewPubKey)
	require.NoError(t, err)
	require.Equal(t, fixture.ShieldedAddress, shieldedAddress)

	bundle, err := privacytypes.DecodeShieldedAddressBundle(fixture.ShieldedAddress)
	require.NoError(t, err)
	require.Equal(t, fixture.SpendPubKeyHex, pointHex(bundle.SpendPubKey))
	require.Equal(t, fixture.ViewPubKeyHex, pointHex(bundle.ViewPubKey))
}

func pointHex(point interface{ Bytes() [32]byte }) string {
	bz := point.Bytes()
	return hex.EncodeToString(bz[:])
}

func mustDecodeHex(t *testing.T, value string) []byte {
	t.Helper()

	bz, err := hex.DecodeString(value)
	require.NoError(t, err)
	return bz
}

func assetDenomFromNote(t *testing.T, note *privacytypes.Note) string {
	t.Helper()

	assetIDHex, err := privacyfield.CanonicalHexFromBigInt(note.AssetID)
	require.NoError(t, err)
	expectedHex, err := privacyfield.CanonicalHexFromBigInt(privacycrypto.HashString("uclair"))
	require.NoError(t, err)
	require.Equal(t, expectedHex, assetIDHex)
	return "uclair"
}

func newPrivacyTx(t *testing.T, txHashHex string, height int64, eventType string, attrKey string, attrValue string) *cmttypes.ResultTx {
	t.Helper()

	return &cmttypes.ResultTx{
		Hash:   mustDecodeHex(t, txHashHex),
		Height: height,
		TxResult: abci.ExecTxResult{
			Events: []abci.Event{
				{
					Type: eventType,
					Attributes: []abci.EventAttribute{
						{Key: attrKey, Value: attrValue},
					},
				},
			},
		},
	}
}
