package types

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

const testChainID = "clairveil-local-1"
const testExpiresAtUnix int64 = 4102444800

func testCreatorAddress() string {
	return sdk.AccAddress(bytes.Repeat([]byte{0x1}, 20)).String()
}

func validFieldBytes() []byte {
	bz := make([]byte, expectedFieldElementBytes)
	bz[expectedFieldElementBytes-1] = 0x01
	return bz
}

func distinctFieldBytesPair() [][]byte {
	first := validFieldBytes()
	second := validFieldBytes()
	second[len(second)-1] = 0x02
	return [][]byte{first, second}
}

func nonCanonicalFieldBytes() []byte {
	bz := fr.Modulus().Bytes()
	out := make([]byte, len(bz))
	copy(out, bz[:])
	return out
}

func validViewTags() [][]byte {
	return [][]byte{{0x01, 0x02}, {0x03, 0x04}}
}

func validDisclosurePubKeyBytes(t *testing.T) []byte {
	t.Helper()

	curve := crypto_tedwards.GetEdwardsCurve()
	var pubKey crypto_tedwards.PointAffine
	pubKey.ScalarMultiplication(&curve.Base, big.NewInt(7))
	pubKeyBytes := pubKey.Bytes()
	return append([]byte(nil), pubKeyBytes[:]...)
}

func validEnvelopeBytes(t *testing.T, kind EncryptedEnvelopeKindV1) []byte {
	t.Helper()
	size, err := encryptedCiphertextSizeV1(kind)
	require.NoError(t, err)
	wrapped, err := WrapEncryptedEnvelopeV1(kind, make([]byte, size))
	require.NoError(t, err)
	return wrapped
}

func validTransferCipherTexts(t *testing.T) [][]byte {
	return [][]byte{
		validEnvelopeBytes(t, EnvelopeTransferNoteV1),
		validEnvelopeBytes(t, EnvelopeTransferNoteV1),
	}
}

func validPublicDisclosurePlaintext(t *testing.T, policy uint32) []byte {
	t.Helper()
	curve := crypto_tedwards.GetEdwardsCurve()
	var point crypto_tedwards.PointAffine
	point.ScalarMultiplication(&curve.Base, big.NewInt(7))
	x, y := new(big.Int), new(big.Int)
	point.X.BigInt(x)
	point.Y.BigInt(y)
	payload := &DisclosurePlaintextV1{
		Plane: DisclosurePlaneUserV1, OutputIndex: TransferDisclosureRecipientOutputIndex,
		Policy: policy, DisclosedFieldBitmap: policy,
		Commitment: big.NewInt(1), Amount: big.NewInt(0), AssetID: ComputeAssetIDV1("uclair"),
		SenderSpendKeyX: big.NewInt(0), SenderSpendKeyY: big.NewInt(0),
		SenderViewKeyX: big.NewInt(0), SenderViewKeyY: big.NewInt(0),
		RecipientSpendKeyX: big.NewInt(0), RecipientSpendKeyY: big.NewInt(0),
		RecipientViewKeyX: big.NewInt(0), RecipientViewKeyY: big.NewInt(0),
		DisclosureBlinding: big.NewInt(3),
	}
	if policy&TransferPrivacyPolicyDiscloseAmount != 0 {
		payload.Amount = big.NewInt(1)
	}
	if policy&TransferPrivacyPolicyDiscloseFrom != 0 {
		payload.SenderSpendKeyX, payload.SenderSpendKeyY = new(big.Int).Set(x), new(big.Int).Set(y)
		payload.SenderViewKeyX, payload.SenderViewKeyY = new(big.Int).Set(x), new(big.Int).Set(y)
	}
	if policy&TransferPrivacyPolicyDiscloseTo != 0 {
		payload.RecipientSpendKeyX, payload.RecipientSpendKeyY = new(big.Int).Set(x), new(big.Int).Set(y)
		payload.RecipientViewKeyX, payload.RecipientViewKeyY = new(big.Int).Set(x), new(big.Int).Set(y)
	}
	encoded, err := MarshalDisclosurePlaintextV1(payload)
	require.NoError(t, err)
	return encoded
}

func TestValidateBasicInvalidCreator(t *testing.T) {
	deposit := NewMsgDeposit("invalid", "1uclair", []byte{1}, []byte{2}, []byte{3})
	withdraw := NewMsgWithdraw("invalid", []byte{1}, []byte{2}, []byte{3}, "1uclair", "clair1test", testChainID, testExpiresAtUnix)
	transfer := NewMsgTransfer("invalid", []byte{1}, []byte{2}, [][]byte{{1}, {2}}, [][]byte{{3}, {4}}, [][]byte{{5}, {6}}, validViewTags(), testExpiresAtUnix)

	require.Error(t, deposit.ValidateBasic())
	require.Error(t, withdraw.ValidateBasic())
	require.Error(t, transfer.ValidateBasic())
}

func TestMsgDepositValidateBasicFieldBytes(t *testing.T) {
	creator := testCreatorAddress()

	valid := NewMsgDeposit(creator, "1uclair", validFieldBytes(), validEnvelopeBytes(t, EnvelopeDepositNoteV1), []byte{3})
	require.NoError(t, valid.ValidateBasic())

	invalidLen := NewMsgDeposit(creator, "1uclair", []byte{0x01}, []byte{2}, []byte{3})
	require.Error(t, invalidLen.ValidateBasic())

	nonCanonical := NewMsgDeposit(creator, "1uclair", nonCanonicalFieldBytes(), []byte{2}, []byte{3})
	require.Error(t, nonCanonical.ValidateBasic())

	zero := NewMsgDeposit(creator, "1uclair", make([]byte, expectedFieldElementBytes), []byte{2}, []byte{3})
	require.ErrorContains(t, zero.ValidateBasic(), "note commitment must be non-zero")

	missingProof := NewMsgDeposit(creator, "1uclair", validFieldBytes(), []byte{2}, nil)
	require.Error(t, missingProof.ValidateBasic())
}

func TestMsgWithdrawValidateBasicReplayGuardFields(t *testing.T) {
	creator := testCreatorAddress()
	recipient := sdk.AccAddress(bytes.Repeat([]byte{0x2}, 20)).String()

	valid := NewMsgWithdraw(
		creator,
		[]byte{1},
		validFieldBytes(),
		validFieldBytes(),
		"1uclair",
		recipient,
		testChainID,
		testExpiresAtUnix,
	)
	require.NoError(t, valid.ValidateBasic())

	zeroNullifier := NewMsgWithdraw(
		creator,
		[]byte{1},
		validFieldBytes(),
		make([]byte, expectedFieldElementBytes),
		"1uclair",
		recipient,
		testChainID,
		testExpiresAtUnix,
	)
	require.ErrorContains(t, zeroNullifier.ValidateBasic(), "nullifier must be non-zero")

	missingChainID := NewMsgWithdraw(
		creator,
		[]byte{1},
		validFieldBytes(),
		validFieldBytes(),
		"1uclair",
		recipient,
		"",
		testExpiresAtUnix,
	)
	require.Error(t, missingChainID.ValidateBasic())

	nonPositiveExpiry := NewMsgWithdraw(
		creator,
		[]byte{1},
		validFieldBytes(),
		validFieldBytes(),
		"1uclair",
		recipient,
		testChainID,
		0,
	)
	err := missingChainID.ValidateBasic()
	require.ErrorContains(t, err, "chain id is required for withdraw")

	err = nonPositiveExpiry.ValidateBasic()
	require.ErrorContains(t, err, "expires_at_unix must be positive for withdraw")
}

func TestMsgTransferValidateBasicLengthChecks(t *testing.T) {
	creator := testCreatorAddress()

	valid := NewMsgTransferWithDisclosure(
		creator,
		[]byte{1},
		validFieldBytes(),
		distinctFieldBytesPair(),
		distinctFieldBytesPair(),
		validTransferCipherTexts(t),
		validViewTags(),
		TransferPrivacyPolicyAllPrivate,
		nil,
		UserDisclosureMode_USER_DISCLOSURE_MODE_NONE,
		nil,
		nil,
		validFieldBytes(),
		validDisclosurePubKeyBytes(t),
		validEnvelopeBytes(t, EnvelopeAuditDisclosureV1),
		validFieldBytes(),
		validEnvelopeBytes(t, EnvelopeSelfViewDisclosureV1),
		testExpiresAtUnix,
	)
	require.NoError(t, valid.ValidateBasic())

	invalidNullifier := NewMsgTransferWithDisclosure(
		creator,
		[]byte{1},
		validFieldBytes(),
		[][]byte{validFieldBytes()},
		distinctFieldBytesPair(),
		[][]byte{{5}, {6}},
		validViewTags(),
		TransferPrivacyPolicyAllPrivate,
		nil,
		UserDisclosureMode_USER_DISCLOSURE_MODE_NONE,
		nil,
		nil,
		validFieldBytes(),
		validDisclosurePubKeyBytes(t),
		[]byte("audit"),
		nil,
		nil,
		testExpiresAtUnix,
	)
	err := invalidNullifier.ValidateBasic()
	require.ErrorContains(t, err, "transfer requires exactly 2 nullifiers")
}

func TestMsgTransferValidateBasicRejectsDuplicateNullifiersAndCommitments(t *testing.T) {
	creator := testCreatorAddress()
	auditPubKey := validDisclosurePubKeyBytes(t)
	fieldOne := validFieldBytes()
	fieldTwo := append([]byte(nil), fieldOne...)
	fieldTwo[len(fieldTwo)-1] = 2

	base := func() *MsgTransfer {
		return NewMsgTransferWithDisclosure(
			creator,
			[]byte{1},
			fieldOne,
			[][]byte{fieldOne, fieldTwo},
			[][]byte{fieldOne, fieldTwo},
			validTransferCipherTexts(t),
			validViewTags(),
			TransferPrivacyPolicyAllPrivate,
			nil,
			UserDisclosureMode_USER_DISCLOSURE_MODE_NONE,
			nil,
			nil,
			fieldOne,
			auditPubKey,
			validEnvelopeBytes(t, EnvelopeAuditDisclosureV1),
			fieldTwo,
			validEnvelopeBytes(t, EnvelopeSelfViewDisclosureV1),
			testExpiresAtUnix,
		)
	}

	duplicateNullifier := base()
	duplicateNullifier.Nullifiers[1] = append([]byte(nil), duplicateNullifier.Nullifiers[0]...)
	require.ErrorContains(t, duplicateNullifier.ValidateBasic(), "nullifier index 1 duplicates index 0")

	duplicateCommitment := base()
	duplicateCommitment.NewCommitments[1] = append([]byte(nil), duplicateCommitment.NewCommitments[0]...)
	require.ErrorContains(t, duplicateCommitment.ValidateBasic(), "commitment index 1 duplicates index 0")

	zeroNullifier := base()
	zeroNullifier.Nullifiers[0] = make([]byte, expectedFieldElementBytes)
	require.ErrorContains(t, zeroNullifier.ValidateBasic(), "nullifier must be non-zero")

	zeroCommitment := base()
	zeroCommitment.NewCommitments[0] = make([]byte, expectedFieldElementBytes)
	require.ErrorContains(t, zeroCommitment.ValidateBasic(), "commitment must be non-zero")
}

func TestMsgTransferValidateBasicUserDisclosureModes(t *testing.T) {
	creator := testCreatorAddress()
	auditPubKey := validDisclosurePubKeyBytes(t)
	userPubKey := validDisclosurePubKeyBytes(t)

	base := func() *MsgTransfer {
		return NewMsgTransferWithDisclosure(
			creator,
			[]byte{1},
			validFieldBytes(),
			distinctFieldBytesPair(),
			distinctFieldBytesPair(),
			validTransferCipherTexts(t),
			validViewTags(),
			TransferPrivacyPolicyAllPrivate,
			nil,
			UserDisclosureMode_USER_DISCLOSURE_MODE_NONE,
			nil,
			nil,
			validFieldBytes(),
			auditPubKey,
			validEnvelopeBytes(t, EnvelopeAuditDisclosureV1),
			validFieldBytes(),
			validEnvelopeBytes(t, EnvelopeSelfViewDisclosureV1),
			testExpiresAtUnix,
		)
	}

	require.NoError(t, base().ValidateBasic())

	publicMsg := base()
	publicMsg.UserPrivacyPolicy = TransferPrivacyPolicyDiscloseAmountTo
	publicMsg.UserDisclosureDigest = validFieldBytes()
	publicMsg.UserDisclosureMode = UserDisclosureMode_USER_DISCLOSURE_MODE_PUBLIC
	publicMsg.UserDisclosurePayload = validPublicDisclosurePlaintext(t, publicMsg.UserPrivacyPolicy)
	require.NoError(t, publicMsg.ValidateBasic())

	encryptedMsg := base()
	encryptedMsg.UserPrivacyPolicy = TransferPrivacyPolicyDiscloseAmountFrom
	encryptedMsg.UserDisclosureDigest = validFieldBytes()
	encryptedMsg.UserDisclosureMode = UserDisclosureMode_USER_DISCLOSURE_MODE_RECIPIENT_ENCRYPTED
	encryptedMsg.UserDisclosureTargetPubkey = userPubKey
	encryptedMsg.UserDisclosurePayload = validEnvelopeBytes(t, EnvelopeUserDisclosureV1)
	require.NoError(t, encryptedMsg.ValidateBasic())

	invalidTarget := base()
	invalidTarget.UserPrivacyPolicy = TransferPrivacyPolicyDiscloseTo
	invalidTarget.UserDisclosureDigest = validFieldBytes()
	invalidTarget.UserDisclosureMode = UserDisclosureMode_USER_DISCLOSURE_MODE_PUBLIC
	invalidTarget.UserDisclosureTargetPubkey = userPubKey
	invalidTarget.UserDisclosurePayload = validPublicDisclosurePlaintext(t, invalidTarget.UserPrivacyPolicy)
	err := invalidTarget.ValidateBasic()
	require.ErrorContains(t, err, "public user disclosure must not include a target pubkey")

	missingAudit := base()
	missingAudit.AuditDisclosureDigest = nil
	err = missingAudit.ValidateBasic()
	require.ErrorContains(t, err, "audit disclosure digest must be exactly 32 bytes")

	missingSelfViewPayload := base()
	missingSelfViewPayload.SelfViewDisclosurePayload = nil
	err = missingSelfViewPayload.ValidateBasic()
	require.ErrorContains(t, err, "self-view disclosure digest and payload must be provided together")
}
