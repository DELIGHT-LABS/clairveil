package transfer

import (
	"context"
	"errors"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestBroadcastTransferStepBuildsAndBroadcasts(t *testing.T) {
	input, merkleProvider, signer, artifacts, runner := testBuildTransferMessageDeps(t)
	broadcaster := &stubTransferMessageBroadcaster{
		response: &sdk.TxResponse{TxHash: "tx-ok", Height: 11},
	}

	res, err := BroadcastTransferStep(context.Background(), merkleProvider, signer, artifacts, runner, broadcaster, BuildTransferStepMessageInput{
		Creator:              input.Creator,
		Inputs:               input.Inputs,
		RecipientSpendPubKey: input.RecipientSpendPubKey,
		RecipientViewPubKey:  input.RecipientViewPubKey,
		TransferAmount:       input.TransferAmount,
		TransferDenom:        input.TransferDenom,
		SenderSpendPubKey:    input.SenderSpendPubKey,
		SenderViewPubKey:     input.SenderViewPubKey,
		IsFinal:              true,
		Disclosure: StepDisclosureConfig{
			UserPrivacyPolicy:             input.UserPrivacyPolicy,
			UserDisclosureMode:            input.UserDisclosureMode,
			UserDisclosureTargetPubKey:    input.UserDisclosureTargetPubKey,
			UserDisclosureTargetPubKeyBz:  input.UserDisclosureTargetPubKeyBz,
			AuditDisclosureTargetPubKey:   input.AuditDisclosureTargetPubKey,
			AuditDisclosureTargetPubKeyBz: input.AuditDisclosureTargetPubKeyBz,
		},
	})
	require.NoError(t, err)
	require.Equal(t, "tx-ok", res.TxHash)
	require.NotNil(t, broadcaster.msg)
	require.Equal(t, input.Creator, broadcaster.msg.Creator)
}

func TestBroadcastTransferStepPropagatesBroadcasterError(t *testing.T) {
	input, merkleProvider, signer, artifacts, runner := testBuildTransferMessageDeps(t)
	broadcaster := &stubTransferMessageBroadcaster{
		err: errors.New("broadcast boom"),
	}

	_, err := BroadcastTransferStep(context.Background(), merkleProvider, signer, artifacts, runner, broadcaster, BuildTransferStepMessageInput{
		Creator:              input.Creator,
		Inputs:               input.Inputs,
		RecipientSpendPubKey: input.RecipientSpendPubKey,
		RecipientViewPubKey:  input.RecipientViewPubKey,
		TransferAmount:       input.TransferAmount,
		TransferDenom:        input.TransferDenom,
		SenderSpendPubKey:    input.SenderSpendPubKey,
		SenderViewPubKey:     input.SenderViewPubKey,
		IsFinal:              true,
		Disclosure: StepDisclosureConfig{
			UserPrivacyPolicy:             input.UserPrivacyPolicy,
			UserDisclosureMode:            input.UserDisclosureMode,
			UserDisclosureTargetPubKey:    input.UserDisclosureTargetPubKey,
			UserDisclosureTargetPubKeyBz:  input.UserDisclosureTargetPubKeyBz,
			AuditDisclosureTargetPubKey:   input.AuditDisclosureTargetPubKey,
			AuditDisclosureTargetPubKeyBz: input.AuditDisclosureTargetPubKeyBz,
		},
	})
	require.ErrorContains(t, err, "broadcast boom")
}

func TestBroadcastTransferStepReturnsFailedTxResponse(t *testing.T) {
	input, merkleProvider, signer, artifacts, runner := testBuildTransferMessageDeps(t)
	broadcaster := &stubTransferMessageBroadcaster{
		response: &sdk.TxResponse{Code: 7, RawLog: "denied"},
	}

	res, err := BroadcastTransferStep(context.Background(), merkleProvider, signer, artifacts, runner, broadcaster, BuildTransferStepMessageInput{
		Creator:              input.Creator,
		Inputs:               input.Inputs,
		RecipientSpendPubKey: input.RecipientSpendPubKey,
		RecipientViewPubKey:  input.RecipientViewPubKey,
		TransferAmount:       input.TransferAmount,
		TransferDenom:        input.TransferDenom,
		SenderSpendPubKey:    input.SenderSpendPubKey,
		SenderViewPubKey:     input.SenderViewPubKey,
		IsFinal:              true,
		Disclosure: StepDisclosureConfig{
			UserPrivacyPolicy:             input.UserPrivacyPolicy,
			UserDisclosureMode:            input.UserDisclosureMode,
			UserDisclosureTargetPubKey:    input.UserDisclosureTargetPubKey,
			UserDisclosureTargetPubKeyBz:  input.UserDisclosureTargetPubKeyBz,
			AuditDisclosureTargetPubKey:   input.AuditDisclosureTargetPubKey,
			AuditDisclosureTargetPubKeyBz: input.AuditDisclosureTargetPubKeyBz,
		},
	})
	require.ErrorContains(t, err, "tx failed with code 7")
	require.Equal(t, uint32(7), res.Code)
}

type stubTransferMessageBroadcaster struct {
	msg      *privacytypes.MsgTransfer
	response *sdk.TxResponse
	err      error
}

func (s *stubTransferMessageBroadcaster) BroadcastTransferMessage(_ context.Context, msg *privacytypes.MsgTransfer) (*sdk.TxResponse, error) {
	s.msg = msg
	if s.err != nil {
		return nil, s.err
	}
	if s.response == nil {
		return &sdk.TxResponse{}, nil
	}
	return s.response, nil
}
