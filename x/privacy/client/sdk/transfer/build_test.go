package transfer

import (
	"bytes"
	"context"
	"math/big"
	"testing"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
	privacyscan "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/scan"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestBuildTransferMessageAssemblesLatestTransfer(t *testing.T) {
	input, merkleProvider, signer, artifacts, runner := testBuildTransferMessageDeps(t)

	msg, err := BuildTransferMessage(context.Background(), merkleProvider, signer, artifacts, runner, input)
	require.NoError(t, err)
	require.NotNil(t, msg)
	require.NoError(t, msg.ValidateBasic())
	require.Equal(t, input.Creator, msg.Creator)
	require.Equal(t, input.UserPrivacyPolicy, msg.UserPrivacyPolicy)
	require.Equal(t, input.UserDisclosureMode, msg.UserDisclosureMode)
	require.Len(t, msg.Nullifiers, 2)
	require.Len(t, msg.NewCommitments, 2)
	require.Len(t, msg.CipherTexts, 2)
	require.NotEmpty(t, msg.UserDisclosureDigest)
	require.NotEmpty(t, msg.UserDisclosureTargetPubkey)
	require.NotEmpty(t, msg.UserDisclosurePayload)
	require.NotEmpty(t, msg.AuditDisclosureDigest)
	require.NotEmpty(t, msg.AuditDisclosureTargetPubkey)
	require.NotEmpty(t, msg.AuditDisclosurePayload)
	require.Len(t, merkleProvider.requests, 2)
	require.Len(t, signer.hashes, 2)
	require.True(t, artifacts.r1csCalled)
	require.True(t, artifacts.provingKeyCalled)
	require.NotNil(t, runner.witness)
}

func TestBuildTransferMessageAllPrivateLeavesUserDisclosureEmpty(t *testing.T) {
	input, merkleProvider, signer, artifacts, runner := testBuildTransferMessageDeps(t)
	input.UserPrivacyPolicy = privacytypes.TransferPrivacyPolicyAllPrivate
	input.UserDisclosureMode = privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_NONE
	input.UserDisclosureTargetPubKey = nil
	input.UserDisclosureTargetPubKeyBz = nil

	msg, err := BuildTransferMessage(context.Background(), merkleProvider, signer, artifacts, runner, input)
	require.NoError(t, err)
	require.NoError(t, msg.ValidateBasic())
	require.Empty(t, msg.UserDisclosureDigest)
	require.Empty(t, msg.UserDisclosureTargetPubkey)
	require.Empty(t, msg.UserDisclosurePayload)
	require.NotEmpty(t, msg.AuditDisclosureDigest)
}

func testBuildTransferMessageDeps(
	t *testing.T,
) (BuildTransferMessageInput, *stubMerklePathProvider, *stubNoteHashSigner, *stubJoinSplitArtifactProvider, *stubJoinSplitProofRunner) {
	t.Helper()

	senderSpendScalar, senderSpendPubKey := testScalarAndPubKey(61)
	senderViewScalar, senderViewPubKey := testScalarAndPubKey(67)
	recipientSpendScalar, recipientSpendPubKey := testScalarAndPubKey(71)
	recipientViewScalar, recipientViewPubKey := testScalarAndPubKey(73)
	disclosureScalar, disclosurePubKey := testScalarAndPubKey(79)
	auditScalar, auditPubKey := testScalarAndPubKey(83)

	inputs := [2]privacyscan.FoundNote{
		{
			Note: privacytypes.Note{
				ReceiverSpendPubKeyX: pointCoordinate(senderSpendPubKey, true),
				ReceiverSpendPubKeyY: pointCoordinate(senderSpendPubKey, false),
				ReceiverViewPubKeyX:  pointCoordinate(senderViewPubKey, true),
				ReceiverViewPubKeyY:  pointCoordinate(senderViewPubKey, false),
				Amount:               big.NewInt(7),
				AssetID:              privacycrypto.HashString("uclair"),
				Randomness:           big.NewInt(701),
				Memo:                 "input-1",
			},
		},
		{
			Note: privacytypes.Note{
				ReceiverSpendPubKeyX: pointCoordinate(senderSpendPubKey, true),
				ReceiverSpendPubKeyY: pointCoordinate(senderSpendPubKey, false),
				ReceiverViewPubKeyX:  pointCoordinate(senderViewPubKey, true),
				ReceiverViewPubKeyY:  pointCoordinate(senderViewPubKey, false),
				Amount:               big.NewInt(5),
				AssetID:              privacycrypto.HashString("uclair"),
				Randomness:           big.NewInt(702),
				Memo:                 "input-2",
			},
		},
	}

	rootBytes, err := privacyfield.CanonicalBytesFromBigInt(big.NewInt(909))
	require.NoError(t, err)

	merkleProvider := &stubMerklePathProvider{paths: map[string]*MerklePathResult{}}
	for _, transferInput := range inputs {
		commitmentHex, err := privacyfield.CanonicalHexFromBigInt(transferInput.Note.ComputeCommitment())
		require.NoError(t, err)
		merkleProvider.paths[commitmentHex] = &MerklePathResult{
			Root:       rootBytes,
			Path:       []string{"01", "02"},
			PathHelper: []uint32{0, 1},
		}
	}

	signature := testSignatureBytes(t)
	signer := &stubNoteHashSigner{signature: signature}
	artifacts := &stubJoinSplitArtifactProvider{
		r1cs:       groth16.NewCS(ecc.BN254),
		provingKey: groth16.NewProvingKey(ecc.BN254),
	}
	runner := &stubJoinSplitProofRunner{
		proof: groth16.NewProof(ecc.BN254),
	}

	disclosurePubKeyBytes := disclosurePubKey.Bytes()
	auditPubKeyBytes := auditPubKey.Bytes()

	require.NotNil(t, senderSpendScalar)
	require.NotNil(t, senderViewScalar)
	require.NotNil(t, recipientSpendScalar)
	require.NotNil(t, recipientViewScalar)
	require.NotNil(t, disclosureScalar)
	require.NotNil(t, auditScalar)

	return BuildTransferMessageInput{
			Creator:                       sdk.AccAddress(bytes.Repeat([]byte{0x1}, 20)).String(),
			Inputs:                        inputs,
			RecipientSpendPubKey:          recipientSpendPubKey,
			RecipientViewPubKey:           recipientViewPubKey,
			TransferAmount:                big.NewInt(7),
			TransferDenom:                 "uclair",
			SenderSpendPubKey:             senderSpendPubKey,
			SenderViewPubKey:              senderViewPubKey,
			UserPrivacyPolicy:             privacytypes.TransferPrivacyPolicyDiscloseAmountToFrom,
			UserDisclosureMode:            privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_RECIPIENT_ENCRYPTED,
			UserDisclosureTargetPubKey:    disclosurePubKey,
			UserDisclosureTargetPubKeyBz:  append([]byte(nil), disclosurePubKeyBytes[:]...),
			AuditDisclosureTargetPubKey:   auditPubKey,
			AuditDisclosureTargetPubKeyBz: append([]byte(nil), auditPubKeyBytes[:]...),
		},
		merkleProvider,
		signer,
		artifacts,
		runner
}
