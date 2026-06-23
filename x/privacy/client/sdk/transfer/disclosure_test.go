package transfer

import (
	"encoding/hex"
	"math/big"
	"testing"

	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
	"github.com/stretchr/testify/require"

	privacydisclosure "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/disclosure"
	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestBuildUserDisclosureDataAllPrivateReturnsNil(t *testing.T) {
	input := testDisclosureBuildInput(t)

	data, err := BuildUserDisclosureData(
		input,
		privacytypes.TransferPrivacyPolicyAllPrivate,
		privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_PUBLIC,
		nil,
	)
	require.NoError(t, err)
	require.Nil(t, data)
}

func TestBuildUserDisclosureDataPublicPayloadVerifies(t *testing.T) {
	input := testDisclosureBuildInput(t)

	data, err := BuildUserDisclosureData(
		input,
		privacytypes.TransferPrivacyPolicyDiscloseAmountTo,
		privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_PUBLIC,
		nil,
	)
	require.NoError(t, err)
	require.NotNil(t, data)
	require.Equal(t, data.PayloadJSON, data.CipherText)
	require.Equal(t, privacydisclosure.PlaneUser, data.Payload.Plane)
	require.Equal(t, "", data.Payload.FromShieldedAddress)
	require.NotEmpty(t, data.Payload.ToShieldedAddress)

	report, err := privacydisclosure.VerifyPayload(&data.Payload, hex.EncodeToString(data.Digest))
	require.NoError(t, err)
	require.True(t, report.Verified)
}

func TestBuildUserDisclosureDataEncryptedPayloadDecrypts(t *testing.T) {
	input := testDisclosureBuildInput(t)
	disclosureScalar, disclosurePubKey := testScalarAndPubKey(19)

	data, err := BuildUserDisclosureData(
		input,
		privacytypes.TransferPrivacyPolicyDiscloseAmountToFrom,
		privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_RECIPIENT_ENCRYPTED,
		disclosurePubKey,
	)
	require.NoError(t, err)
	require.NotNil(t, data)
	require.NotEqual(t, string(data.PayloadJSON), string(data.CipherText))

	payload, err := privacydisclosure.DecryptPayload(data.CipherText, disclosureScalar)
	require.NoError(t, err)
	require.Equal(t, data.Payload, *payload)

	report, err := privacydisclosure.VerifyPayload(payload, hex.EncodeToString(data.Digest))
	require.NoError(t, err)
	require.True(t, report.Verified)
}

func TestBuildAuditDisclosureDataEncryptedPayloadDecrypts(t *testing.T) {
	input := testDisclosureBuildInput(t)
	auditScalar, auditPubKey := testScalarAndPubKey(23)

	data, err := BuildAuditDisclosureData(input, auditPubKey)
	require.NoError(t, err)
	require.NotNil(t, data)
	require.Equal(t, privacydisclosure.PlaneAudit, data.Payload.Plane)
	require.NotEmpty(t, data.Payload.FromShieldedAddress)
	require.NotEmpty(t, data.Payload.ToShieldedAddress)
	require.Equal(t, "uclair", data.Payload.AssetDenom)

	payload, err := privacydisclosure.DecryptPayload(data.CipherText, auditScalar)
	require.NoError(t, err)
	require.Equal(t, data.Payload, *payload)

	report, err := privacydisclosure.VerifyPayload(payload, hex.EncodeToString(data.Digest))
	require.NoError(t, err)
	require.True(t, report.Verified)
}

func TestBuildSelfViewDisclosureDataEncryptedPayloadDecrypts(t *testing.T) {
	input := testDisclosureBuildInput(t)
	selfViewScalar, selfViewPubKey := testScalarAndPubKey(29)

	data, err := BuildSelfViewDisclosureData(input, selfViewPubKey)
	require.NoError(t, err)
	require.NotNil(t, data)
	require.Equal(t, privacydisclosure.PlaneSelfView, data.Payload.Plane)
	require.NotEmpty(t, data.Payload.FromShieldedAddress)
	require.NotEmpty(t, data.Payload.ToShieldedAddress)
	require.Equal(t, "uclair", data.Payload.AssetDenom)

	payload, err := privacydisclosure.DecryptPayload(data.CipherText, selfViewScalar)
	require.NoError(t, err)
	require.Equal(t, data.Payload, *payload)

	report, err := privacydisclosure.VerifyPayload(payload, hex.EncodeToString(data.Digest))
	require.NoError(t, err)
	require.True(t, report.Verified)
}

func TestBuildUserDisclosureDataEncryptedRequiresTargetKey(t *testing.T) {
	input := testDisclosureBuildInput(t)

	_, err := BuildUserDisclosureData(
		input,
		privacytypes.TransferPrivacyPolicyDiscloseAmount,
		privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_RECIPIENT_ENCRYPTED,
		nil,
	)
	require.ErrorContains(t, err, "requires a disclosure target public key")
}

func TestBuildAuditDisclosureDataRequiresTargetKey(t *testing.T) {
	input := testDisclosureBuildInput(t)

	_, err := BuildAuditDisclosureData(input, nil)
	require.ErrorContains(t, err, "audit disclosure target public key is required")
}

func TestBuildSelfViewDisclosureDataRequiresTargetKey(t *testing.T) {
	input := testDisclosureBuildInput(t)

	_, err := BuildSelfViewDisclosureData(input, nil)
	require.ErrorContains(t, err, "self-view disclosure target public key is required")
}

func testDisclosureBuildInput(t *testing.T) DisclosureBuildInput {
	t.Helper()

	fromSpendScalar, fromSpendPubKey := testScalarAndPubKey(3)
	fromViewScalar, fromViewPubKey := testScalarAndPubKey(5)
	toSpendScalar, toSpendPubKey := testScalarAndPubKey(7)
	toViewScalar, toViewPubKey := testScalarAndPubKey(11)

	fromNote := privacytypes.Note{
		ReceiverSpendPubKeyX: pointCoordinate(fromSpendPubKey, true),
		ReceiverSpendPubKeyY: pointCoordinate(fromSpendPubKey, false),
		ReceiverViewPubKeyX:  pointCoordinate(fromViewPubKey, true),
		ReceiverViewPubKeyY:  pointCoordinate(fromViewPubKey, false),
		Amount:               big.NewInt(9),
		AssetID:              privacycrypto.HashString("uclair"),
		Randomness:           big.NewInt(101),
		Memo:                 "from",
	}
	recipientNote := privacytypes.Note{
		ReceiverSpendPubKeyX: pointCoordinate(toSpendPubKey, true),
		ReceiverSpendPubKeyY: pointCoordinate(toSpendPubKey, false),
		ReceiverViewPubKeyX:  pointCoordinate(toViewPubKey, true),
		ReceiverViewPubKeyY:  pointCoordinate(toViewPubKey, false),
		Amount:               big.NewInt(7),
		AssetID:              privacycrypto.HashString("uclair"),
		Randomness:           big.NewInt(202),
		Memo:                 "recipient",
	}

	commitmentBytes, err := privacyfield.CanonicalBytesFromBigInt(recipientNote.ComputeCommitment())
	require.NoError(t, err)

	require.NotNil(t, fromSpendScalar)
	require.NotNil(t, fromViewScalar)
	require.NotNil(t, toSpendScalar)
	require.NotNil(t, toViewScalar)

	return DisclosureBuildInput{
		OutputCommitment: commitmentBytes,
		TransferDenom:    "uclair",
		FromNote:         fromNote,
		RecipientNote:    recipientNote,
	}
}

func testScalarAndPubKey(value int64) (*big.Int, *crypto_tedwards.PointAffine) {
	curve := crypto_tedwards.GetEdwardsCurve()
	scalar := big.NewInt(value)

	var pubKey crypto_tedwards.PointAffine
	pubKey.ScalarMultiplication(&curve.Base, scalar)

	return scalar, &pubKey
}

func pointCoordinate(point *crypto_tedwards.PointAffine, x bool) *big.Int {
	value := new(big.Int)
	if x {
		point.X.BigInt(value)
		return value
	}
	point.Y.BigInt(value)
	return value
}
