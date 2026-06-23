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

func nonCanonicalFieldBytes() []byte {
	bz := fr.Modulus().Bytes()
	out := make([]byte, len(bz))
	copy(out, bz[:])
	return out
}

func validDisclosurePubKeyBytes(t *testing.T) []byte {
	t.Helper()

	curve := crypto_tedwards.GetEdwardsCurve()
	var pubKey crypto_tedwards.PointAffine
	pubKey.ScalarMultiplication(&curve.Base, big.NewInt(7))
	pubKeyBytes := pubKey.Bytes()
	return append([]byte(nil), pubKeyBytes[:]...)
}

func TestValidateBasicInvalidCreator(t *testing.T) {
	deposit := NewMsgDeposit("invalid", "1uclair", []byte{1}, []byte{2}, []byte{3})
	withdraw := NewMsgWithdraw("invalid", []byte{1}, []byte{2}, []byte{3}, "1uclair", "clair1test", testChainID, testExpiresAtUnix)
	transfer := NewMsgTransfer("invalid", []byte{1}, []byte{2}, [][]byte{{1}, {2}}, [][]byte{{3}, {4}}, [][]byte{{5}, {6}})

	require.Error(t, deposit.ValidateBasic())
	require.Error(t, withdraw.ValidateBasic())
	require.Error(t, transfer.ValidateBasic())
}

func TestMsgDepositValidateBasicFieldBytes(t *testing.T) {
	creator := testCreatorAddress()

	valid := NewMsgDeposit(creator, "1uclair", validFieldBytes(), []byte{2}, []byte{3})
	require.NoError(t, valid.ValidateBasic())

	invalidLen := NewMsgDeposit(creator, "1uclair", []byte{0x01}, []byte{2}, []byte{3})
	require.Error(t, invalidLen.ValidateBasic())

	nonCanonical := NewMsgDeposit(creator, "1uclair", nonCanonicalFieldBytes(), []byte{2}, []byte{3})
	require.Error(t, nonCanonical.ValidateBasic())

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
		[][]byte{validFieldBytes(), validFieldBytes()},
		[][]byte{validFieldBytes(), validFieldBytes()},
		[][]byte{{5}, {6}},
		TransferPrivacyPolicyAllPrivate,
		nil,
		UserDisclosureMode_USER_DISCLOSURE_MODE_NONE,
		nil,
		nil,
		validFieldBytes(),
		validDisclosurePubKeyBytes(t),
		[]byte("audit"),
		validFieldBytes(),
		[]byte("self-view"),
	)
	require.NoError(t, valid.ValidateBasic())

	invalidNullifier := NewMsgTransferWithDisclosure(
		creator,
		[]byte{1},
		validFieldBytes(),
		[][]byte{validFieldBytes()},
		[][]byte{validFieldBytes(), validFieldBytes()},
		[][]byte{{5}, {6}},
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
	)
	err := invalidNullifier.ValidateBasic()
	require.ErrorContains(t, err, "transfer requires exactly 2 nullifiers")
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
			[][]byte{validFieldBytes(), validFieldBytes()},
			[][]byte{validFieldBytes(), validFieldBytes()},
			[][]byte{{5}, {6}},
			TransferPrivacyPolicyAllPrivate,
			nil,
			UserDisclosureMode_USER_DISCLOSURE_MODE_NONE,
			nil,
			nil,
			validFieldBytes(),
			auditPubKey,
			[]byte("audit"),
			validFieldBytes(),
			[]byte("self-view"),
		)
	}

	require.NoError(t, base().ValidateBasic())

	publicMsg := base()
	publicMsg.UserPrivacyPolicy = TransferPrivacyPolicyDiscloseAmountTo
	publicMsg.UserDisclosureDigest = validFieldBytes()
	publicMsg.UserDisclosureMode = UserDisclosureMode_USER_DISCLOSURE_MODE_PUBLIC
	publicMsg.UserDisclosurePayload = []byte("public")
	require.NoError(t, publicMsg.ValidateBasic())

	encryptedMsg := base()
	encryptedMsg.UserPrivacyPolicy = TransferPrivacyPolicyDiscloseAmountFrom
	encryptedMsg.UserDisclosureDigest = validFieldBytes()
	encryptedMsg.UserDisclosureMode = UserDisclosureMode_USER_DISCLOSURE_MODE_RECIPIENT_ENCRYPTED
	encryptedMsg.UserDisclosureTargetPubkey = userPubKey
	encryptedMsg.UserDisclosurePayload = []byte("cipher")
	require.NoError(t, encryptedMsg.ValidateBasic())

	invalidTarget := base()
	invalidTarget.UserPrivacyPolicy = TransferPrivacyPolicyDiscloseTo
	invalidTarget.UserDisclosureDigest = validFieldBytes()
	invalidTarget.UserDisclosureMode = UserDisclosureMode_USER_DISCLOSURE_MODE_PUBLIC
	invalidTarget.UserDisclosureTargetPubkey = userPubKey
	invalidTarget.UserDisclosurePayload = []byte("public")
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
