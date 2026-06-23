package transfer

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestEffectiveStepDisclosureConfigKeepsFinalTransferDisclosure(t *testing.T) {
	input, _, _, _, _ := testBuildTransferMessageDeps(t)

	effective := EffectiveStepDisclosureConfig(testStepDisclosureConfig(input), true)

	require.Equal(t, input.UserPrivacyPolicy, effective.UserPrivacyPolicy)
	require.Equal(t, input.UserDisclosureMode, effective.UserDisclosureMode)
	require.Equal(t, input.UserDisclosureTargetPubKey.Bytes(), effective.UserDisclosureTargetPubKey.Bytes())
	require.Equal(t, input.UserDisclosureTargetPubKeyBz, effective.UserDisclosureTargetPubKeyBz)
	require.Equal(t, input.AuditDisclosureTargetPubKey.Bytes(), effective.AuditDisclosureTargetPubKey.Bytes())
	require.Equal(t, input.AuditDisclosureTargetPubKeyBz, effective.AuditDisclosureTargetPubKeyBz)
	require.Equal(t, input.SelfViewDisclosureTargetPubKey.Bytes(), effective.SelfViewDisclosureTargetPubKey.Bytes())
}

func TestEffectiveStepDisclosureConfigForcesAllPrivateForSelfMerge(t *testing.T) {
	input, _, _, _, _ := testBuildTransferMessageDeps(t)

	effective := EffectiveStepDisclosureConfig(testStepDisclosureConfig(input), false)

	require.Equal(t, privacytypes.TransferPrivacyPolicyAllPrivate, effective.UserPrivacyPolicy)
	require.Equal(t, privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_NONE, effective.UserDisclosureMode)
	require.Nil(t, effective.UserDisclosureTargetPubKey)
	require.Nil(t, effective.UserDisclosureTargetPubKeyBz)
	require.Equal(t, input.AuditDisclosureTargetPubKey.Bytes(), effective.AuditDisclosureTargetPubKey.Bytes())
	require.Equal(t, input.AuditDisclosureTargetPubKeyBz, effective.AuditDisclosureTargetPubKeyBz)
	require.Equal(t, input.SelfViewDisclosureTargetPubKey.Bytes(), effective.SelfViewDisclosureTargetPubKey.Bytes())
}

func TestBuildTransferStepMessageForSelfMergeClearsUserDisclosure(t *testing.T) {
	input, merkleProvider, signer, artifacts, runner := testBuildTransferMessageDeps(t)

	msg, err := BuildTransferStepMessage(
		context.Background(),
		merkleProvider,
		signer,
		artifacts,
		runner,
		BuildTransferStepMessageInput{
			Creator:              input.Creator,
			Inputs:               input.Inputs,
			RecipientSpendPubKey: input.RecipientSpendPubKey,
			RecipientViewPubKey:  input.RecipientViewPubKey,
			TransferAmount:       input.TransferAmount,
			TransferDenom:        input.TransferDenom,
			SenderSpendPubKey:    input.SenderSpendPubKey,
			SenderViewPubKey:     input.SenderViewPubKey,
			IsFinal:              false,
			Disclosure:           testStepDisclosureConfig(input),
		},
	)
	require.NoError(t, err)
	require.NoError(t, msg.ValidateBasic())
	require.Equal(t, privacytypes.TransferPrivacyPolicyAllPrivate, msg.UserPrivacyPolicy)
	require.Equal(t, privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_NONE, msg.UserDisclosureMode)
	require.Empty(t, msg.UserDisclosureDigest)
	require.Empty(t, msg.UserDisclosureTargetPubkey)
	require.Empty(t, msg.UserDisclosurePayload)
	require.NotEmpty(t, msg.AuditDisclosureDigest)
	require.NotEmpty(t, msg.SelfViewDisclosureDigest)
	require.NotEmpty(t, msg.SelfViewDisclosurePayload)
}

func TestBuildTransferStepMessageForFinalTransferKeepsUserDisclosure(t *testing.T) {
	input, merkleProvider, signer, artifacts, runner := testBuildTransferMessageDeps(t)

	msg, err := BuildTransferStepMessage(
		context.Background(),
		merkleProvider,
		signer,
		artifacts,
		runner,
		BuildTransferStepMessageInput{
			Creator:              input.Creator,
			Inputs:               input.Inputs,
			RecipientSpendPubKey: input.RecipientSpendPubKey,
			RecipientViewPubKey:  input.RecipientViewPubKey,
			TransferAmount:       input.TransferAmount,
			TransferDenom:        input.TransferDenom,
			SenderSpendPubKey:    input.SenderSpendPubKey,
			SenderViewPubKey:     input.SenderViewPubKey,
			IsFinal:              true,
			Disclosure:           testStepDisclosureConfig(input),
		},
	)
	require.NoError(t, err)
	require.NoError(t, msg.ValidateBasic())
	require.Equal(t, input.UserPrivacyPolicy, msg.UserPrivacyPolicy)
	require.Equal(t, input.UserDisclosureMode, msg.UserDisclosureMode)
	require.NotEmpty(t, msg.UserDisclosureDigest)
	require.NotEmpty(t, msg.UserDisclosureTargetPubkey)
	require.NotEmpty(t, msg.UserDisclosurePayload)
	require.NotEmpty(t, msg.SelfViewDisclosureDigest)
	require.NotEmpty(t, msg.SelfViewDisclosurePayload)
}
