package conformance_test

import (
	"encoding/hex"
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
	"github.com/stretchr/testify/require"

	privacydisclosure "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/disclosure"
	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
	privacyprovertransport "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/provertransport"
	privacytransfer "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/transfer"
	privacywithdraw "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/withdraw"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

const updateProverExampleFixtureEnv = "PRIVACY_UPDATE_PROVER_EXAMPLE_FIXTURE"
const referenceCanonicalIntentSignatureHex = "3153c1da87e13085b53a042a4255416a3db2355fa64cadc79c757275eb04942a0000000000000000000000000000000000000000000000000000000000000013"

type proverExampleBundleFixture struct {
	SchemaVersion string                 `json:"schema_version"`
	Transfer      transferExampleFixture `json:"transfer"`
	Withdraw      withdrawExampleFixture `json:"withdraw"`
}

type transferExampleFixture struct {
	Request  privacyprovertransport.TransferProofRequest  `json:"request"`
	Response privacyprovertransport.TransferProofResponse `json:"response"`
}

type withdrawExampleFixture struct {
	ValidationNowUnix int64                                        `json:"validation_now_unix"`
	Request           privacyprovertransport.WithdrawProofRequest  `json:"request"`
	Response          privacyprovertransport.WithdrawProofResponse `json:"response"`
}

func TestProverExampleBundleFixture(t *testing.T) {
	fixture := loadProverExampleBundleFixture(t)
	require.Equal(t, "v2", fixture.SchemaVersion)

	transferRequestJSON, err := fixture.Transfer.Request.MarshalIndentedJSON()
	require.NoError(t, err)
	decodedTransferRequest, err := privacyprovertransport.DecodeTransferProofRequestJSON(transferRequestJSON)
	require.NoError(t, err)
	require.NoError(t, privacyprovertransport.ValidateTransferProofRequest(*decodedTransferRequest))

	transferResponseJSON, err := fixture.Transfer.Response.MarshalIndentedJSON()
	require.NoError(t, err)
	decodedTransferResponse, err := privacyprovertransport.DecodeTransferProofResponseJSON(transferResponseJSON)
	require.NoError(t, err)
	require.NoError(t, privacyprovertransport.ValidateTransferProofResponse(*decodedTransferRequest, *decodedTransferResponse))

	transferMsg, err := decodedTransferRequest.Payload.ToMsg(decodedTransferResponse.Proof)
	require.NoError(t, err)
	require.NoError(t, transferMsg.ValidateBasic())

	validationNow := time.Unix(fixture.Withdraw.ValidationNowUnix, 0).UTC()

	withdrawRequestJSON, err := fixture.Withdraw.Request.MarshalIndentedJSON()
	require.NoError(t, err)
	decodedWithdrawRequest, err := privacyprovertransport.DecodeWithdrawProofRequestJSON(withdrawRequestJSON)
	require.NoError(t, err)
	require.NoError(t, privacyprovertransport.ValidateWithdrawProofRequest(*decodedWithdrawRequest, validationNow))

	withdrawResponseJSON, err := fixture.Withdraw.Response.MarshalIndentedJSON()
	require.NoError(t, err)
	decodedWithdrawResponse, err := privacyprovertransport.DecodeWithdrawProofResponseJSON(withdrawResponseJSON)
	require.NoError(t, err)
	require.NoError(t, privacyprovertransport.ValidateWithdrawProofResponse(*decodedWithdrawRequest, *decodedWithdrawResponse, validationNow))

	finalWithdrawPayload, err := decodedWithdrawRequest.Payload.ToPreparedWithdrawPayload(decodedWithdrawResponse.Proof, validationNow)
	require.NoError(t, err)
	require.NoError(t, privacywithdraw.ValidatePreparedWithdrawPayloadMetadata(*finalWithdrawPayload, validationNow))
}

func TestWriteProverExampleBundleFixture(t *testing.T) {
	if os.Getenv(updateProverExampleFixtureEnv) != "1" {
		t.Skipf("set %s=1 to rewrite the prover example fixture", updateProverExampleFixtureEnv)
	}

	fixture := loadProverExampleBundleFixture(t)
	fixture.SchemaVersion = "v2"
	payload := &fixture.Transfer.Request.Payload
	userBlinding := big.NewInt(1901)
	fullBlinding := big.NewInt(1907)
	assetID := privacytypes.ComputeAssetIDV1("uclair")
	payload.AssetIDHex = mustCanonicalBigIntHex(t, assetID)
	rewriteTransferNoteContracts(t, payload, assetID)
	recipientCommitment := payload.Outputs[0].CommitmentHex

	payload.UserDisclosureDigestHex, payload.UserDisclosurePayloadHex = rewriteDisclosureEnvelope(
		t,
		payload.UserDisclosurePayloadHex,
		big.NewInt(referenceUserDisclosureScalar),
		userBlinding,
		recipientCommitment,
		payload.AssetIDHex,
	)
	payload.AuditDisclosureDigestHex, payload.AuditDisclosurePayloadHex = rewriteDisclosureEnvelope(
		t,
		payload.AuditDisclosurePayloadHex,
		big.NewInt(referenceAuditDisclosureScalar),
		fullBlinding,
		recipientCommitment,
		payload.AssetIDHex,
	)
	payload.SelfViewDisclosureDigestHex, payload.SelfViewDisclosurePayloadHex = rewriteDisclosureEnvelope(
		t,
		payload.SelfViewDisclosurePayloadHex,
		big.NewInt(referenceSelfViewDisclosureScalar),
		fullBlinding,
		recipientCommitment,
		payload.AssetIDHex,
	)
	require.Equal(t, payload.AuditDisclosureDigestHex, payload.SelfViewDisclosureDigestHex)

	payload.Version = privacytransfer.PreparedTransferPayloadVersion
	payload.ChainID = "clairveil-local-1"
	payload.ExpiresAtUnix = 4_102_448_400
	payload.UserDisclosureBlindingHex = mustCanonicalBigIntHex(t, userBlinding)
	payload.FullDisclosureBlindingHex = mustCanonicalBigIntHex(t, fullBlinding)
	payload.OwnerSignatureHex = referenceCanonicalIntentSignatureHex
	payload.PayloadHash = privacytransfer.ComputePreparedTransferPayloadHash(*payload)
	fixture.Transfer.Request.Version = privacyprovertransport.TransferProofRequestVersion
	fixture.Transfer.Response.Version = privacyprovertransport.TransferProofResponseVersion
	fixture.Transfer.Response.Proof.Version = privacytransfer.PreparedTransferProofVersion
	fixture.Transfer.Response.Proof.PayloadHash = payload.PayloadHash

	withdrawPayload := &fixture.Withdraw.Request.Payload
	withdrawPayload.AssetIDHex = payload.AssetIDHex
	withdrawNote := noteFromPreparedFields(
		t,
		withdrawPayload.Amount,
		withdrawPayload.NoteRandomnessHex,
		withdrawPayload.SpendPubKeyHex,
		withdrawPayload.ViewPubKeyHex,
		assetID,
	)
	withdrawPayload.NullifierHex = mustCanonicalBigIntHex(t, withdrawNote.ComputeNullifier())
	withdrawPayload.Version = privacywithdraw.PreparedWithdrawProverPayloadVersion
	withdrawPayload.SpendIntentSignatureHex = referenceCanonicalIntentSignatureHex
	withdrawPayload.PayloadHash = privacywithdraw.ComputePreparedWithdrawProverPayloadHash(*withdrawPayload)
	fixture.Withdraw.Request.Version = privacyprovertransport.WithdrawProofRequestVersion
	fixture.Withdraw.Response.Version = privacyprovertransport.WithdrawProofResponseVersion
	fixture.Withdraw.Response.Proof.Version = privacywithdraw.PreparedWithdrawProofVersion
	fixture.Withdraw.Response.Proof.PayloadHash = withdrawPayload.PayloadHash

	bz, err := json.MarshalIndent(fixture, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(proverExampleBundleFixturePath(t), append(bz, '\n'), 0o644))
}

func rewriteDisclosureEnvelope(
	t *testing.T,
	cipherHex string,
	scalar, blinding *big.Int,
	commitmentHex, assetIDHex string,
) (string, string) {
	t.Helper()
	payload, err := privacydisclosure.DecryptPayloadHex(cipherHex, scalar)
	if err != nil {
		legacyCipherText := mustDecodeHex(t, cipherHex)
		legacyPlainText, decryptErr := privacycrypto.AsymDecrypt(legacyCipherText, scalar)
		require.NoError(t, decryptErr)
		payload = new(privacydisclosure.Payload)
		require.NoError(t, json.Unmarshal(legacyPlainText, payload))
	}
	payload.Version = privacydisclosure.PayloadVersion
	payload.BlindingHex = mustCanonicalBigIntHex(t, blinding)
	payload.CommitmentHex = commitmentHex
	if payload.Amount != "" {
		payload.AssetIDHex = assetIDHex
	}
	payload.AssetDenom = ""
	digestHex, _, err := privacydisclosure.ComputeExpectedDisclosureDigest(payload)
	require.NoError(t, err)
	payload.DisclosureDigestHex = digestHex

	plainText, kind := marshalDisclosureFixtureV1(t, payload, blinding)
	curve := crypto_tedwards.GetEdwardsCurve()
	var publicKey crypto_tedwards.PointAffine
	publicKey.ScalarMultiplication(&curve.Base, scalar)
	cipherText, err := privacycrypto.AsymEncrypt(plainText, publicKey)
	require.NoError(t, err)
	envelope, err := privacytypes.WrapEncryptedEnvelopeV1(kind, cipherText)
	require.NoError(t, err)
	return digestHex, hex.EncodeToString(envelope)
}

func rewriteTransferNoteContracts(t *testing.T, payload *privacytransfer.PreparedTransferPayload, assetID *big.Int) {
	t.Helper()
	for i := range payload.Inputs {
		note := noteFromPreparedFields(
			t,
			payload.Inputs[i].Amount,
			payload.Inputs[i].RandomnessHex,
			payload.Inputs[i].SpendPubKeyHex,
			payload.Inputs[i].ViewPubKeyHex,
			assetID,
		)
		payload.Inputs[i].NullifierHex = mustCanonicalBigIntHex(t, note.ComputeNullifier())
	}

	payload.CipherTextHexes = make([]string, len(payload.Outputs))
	payload.ViewTagHexes = make([]string, len(payload.Outputs))
	for i := range payload.Outputs {
		note := noteFromPreparedFields(
			t,
			payload.Outputs[i].Amount,
			payload.Outputs[i].RandomnessHex,
			payload.Outputs[i].SpendPubKeyHex,
			payload.Outputs[i].ViewPubKeyHex,
			assetID,
		)
		commitment := mustCanonicalBigIntBytes(t, note.ComputeCommitment())
		payload.Outputs[i].CommitmentHex = hex.EncodeToString(commitment)
		plainText, err := privacytypes.MarshalNotePlaintextV1(&note)
		require.NoError(t, err)
		viewKey, err := privacycrypto.DecodeCanonicalPoint(mustDecodeHex(t, payload.Outputs[i].ViewPubKeyHex))
		require.NoError(t, err)
		rawCipherText, viewTag, err := privacycrypto.AsymEncryptWithViewTag(plainText, *viewKey, commitment, uint32(i))
		require.NoError(t, err)
		envelope, err := privacytypes.WrapEncryptedEnvelopeV1(privacytypes.EnvelopeTransferNoteV1, rawCipherText)
		require.NoError(t, err)
		payload.CipherTextHexes[i] = hex.EncodeToString(envelope)
		payload.ViewTagHexes[i] = hex.EncodeToString(viewTag)
	}
}

func noteFromPreparedFields(
	t *testing.T,
	amountDecimal, randomnessHex, spendPubKeyHex, viewPubKeyHex string,
	assetID *big.Int,
) privacytypes.Note {
	t.Helper()
	amount, ok := new(big.Int).SetString(amountDecimal, 10)
	require.True(t, ok)
	spendKey, err := privacycrypto.DecodeCanonicalPoint(mustDecodeHex(t, spendPubKeyHex))
	require.NoError(t, err)
	viewKey, err := privacycrypto.DecodeCanonicalPoint(mustDecodeHex(t, viewPubKeyHex))
	require.NoError(t, err)
	return privacytypes.Note{
		ReceiverSpendPubKeyX: pointX(spendKey),
		ReceiverSpendPubKeyY: pointY(spendKey),
		ReceiverViewPubKeyX:  pointX(viewKey),
		ReceiverViewPubKeyY:  pointY(viewKey),
		Amount:               amount,
		AssetID:              new(big.Int).Set(assetID),
		Randomness:           new(big.Int).SetBytes(mustDecodeHex(t, randomnessHex)),
	}
}

func marshalDisclosureFixtureV1(
	t *testing.T,
	payload *privacydisclosure.Payload,
	blinding *big.Int,
) ([]byte, privacytypes.EncryptedEnvelopeKindV1) {
	t.Helper()
	fixed := &privacytypes.DisclosurePlaintextV1{
		OutputIndex:     payload.OutputIndex,
		Commitment:      new(big.Int).SetBytes(mustDecodeHex(t, payload.CommitmentHex)),
		Amount:          new(big.Int),
		AssetID:         new(big.Int),
		SenderSpendKeyX: new(big.Int), SenderSpendKeyY: new(big.Int),
		SenderViewKeyX: new(big.Int), SenderViewKeyY: new(big.Int),
		RecipientSpendKeyX: new(big.Int), RecipientSpendKeyY: new(big.Int),
		RecipientViewKeyX: new(big.Int), RecipientViewKeyY: new(big.Int),
		DisclosureBlinding: new(big.Int).Set(blinding),
	}
	var kind privacytypes.EncryptedEnvelopeKindV1
	if payload.Plane == privacydisclosure.PlaneUser {
		fixed.Plane = privacytypes.DisclosurePlaneUserV1
		fixed.Policy = payload.Policy
		fixed.DisclosedFieldBitmap = payload.Policy
		kind = privacytypes.EnvelopeUserDisclosureV1
	} else {
		fixed.Plane = privacytypes.DisclosurePlaneFullV1
		fixed.Policy = privacytypes.DisclosureFullMarkerV1
		fixed.DisclosedFieldBitmap = privacytypes.TransferPrivacyPolicyDiscloseAmountToFrom
		if payload.Plane == privacydisclosure.PlaneAudit {
			kind = privacytypes.EnvelopeAuditDisclosureV1
		} else {
			kind = privacytypes.EnvelopeSelfViewDisclosureV1
		}
	}
	if payload.Amount != "" {
		amount, ok := new(big.Int).SetString(payload.Amount, 10)
		require.True(t, ok)
		fixed.Amount = amount
		fixed.AssetID = new(big.Int).SetBytes(mustDecodeHex(t, payload.AssetIDHex))
	}
	if payload.FromShieldedAddress != "" {
		bundle, err := privacytypes.DecodeShieldedAddressBundle(payload.FromShieldedAddress)
		require.NoError(t, err)
		fixed.SenderSpendKeyX, fixed.SenderSpendKeyY = pointX(bundle.SpendPubKey), pointY(bundle.SpendPubKey)
		fixed.SenderViewKeyX, fixed.SenderViewKeyY = pointX(bundle.ViewPubKey), pointY(bundle.ViewPubKey)
	}
	if payload.ToShieldedAddress != "" {
		bundle, err := privacytypes.DecodeShieldedAddressBundle(payload.ToShieldedAddress)
		require.NoError(t, err)
		fixed.RecipientSpendKeyX, fixed.RecipientSpendKeyY = pointX(bundle.SpendPubKey), pointY(bundle.SpendPubKey)
		fixed.RecipientViewKeyX, fixed.RecipientViewKeyY = pointX(bundle.ViewPubKey), pointY(bundle.ViewPubKey)
	}
	plainText, err := privacytypes.MarshalDisclosurePlaintextV1(fixed)
	require.NoError(t, err)
	return plainText, kind
}

func mustCanonicalBigIntBytes(t *testing.T, value *big.Int) []byte {
	t.Helper()
	encoded, err := privacyfield.CanonicalBytesFromBigInt(value)
	require.NoError(t, err)
	return encoded
}

func mustCanonicalBigIntHex(t *testing.T, value *big.Int) string {
	t.Helper()
	encoded, err := privacyfield.CanonicalHexFromBigInt(value)
	require.NoError(t, err)
	return encoded
}

func loadProverExampleBundleFixture(t *testing.T) proverExampleBundleFixture {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)

	fixturePath := filepath.Join(filepath.Dir(filename), "testdata", "privacy_prover_example_bundle.json")
	bz, err := os.ReadFile(fixturePath)
	require.NoError(t, err)

	var fixture proverExampleBundleFixture
	require.NoError(t, json.Unmarshal(bz, &fixture))
	return fixture
}

func proverExampleBundleFixturePath(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Join(filepath.Dir(filename), "testdata", "privacy_prover_example_bundle.json")
}
