package transfer

import (
	"context"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	privacyscan "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/scan"
)

func TestExecuteTransferRunsFinalStepAndReturnsResponse(t *testing.T) {
	input, merkleProvider, signer, artifacts, runner := testBuildTransferMessageDeps(t)
	source := &stubRecursiveTransferNoteSource{
		responses: [][]privacyscan.FoundNote{
			append([]privacyscan.FoundNote(nil), input.Inputs[:]...),
		},
	}
	waiter := &stubRecursiveTransferBlockWaiter{}
	observer := &stubRecursiveTransferObserver{}
	broadcaster := &stubTransferMessageBroadcaster{
		response: &sdk.TxResponse{TxHash: "tx-final", Height: 11},
	}

	res, err := ExecuteTransfer(
		context.Background(),
		source,
		nil,
		waiter,
		observer,
		ExecuteTransferDependencies{
			MerklePaths: merkleProvider,
			Signer:      signer,
			Artifacts:   artifacts,
			Runner:      runner,
			Broadcaster: broadcaster,
		},
		ExecuteTransferInput{
			Creator:              input.Creator,
			RecipientSpendPubKey: input.RecipientSpendPubKey,
			RecipientViewPubKey:  input.RecipientViewPubKey,
			SenderSpendPubKey:    input.SenderSpendPubKey,
			SenderViewPubKey:     input.SenderViewPubKey,
			TransferAmount:       input.TransferAmount,
			TransferDenom:        input.TransferDenom,
			Disclosure: StepDisclosureConfig{
				UserPrivacyPolicy:             input.UserPrivacyPolicy,
				UserDisclosureMode:            input.UserDisclosureMode,
				UserDisclosureTargetPubKey:    input.UserDisclosureTargetPubKey,
				UserDisclosureTargetPubKeyBz:  input.UserDisclosureTargetPubKeyBz,
				AuditDisclosureTargetPubKey:   input.AuditDisclosureTargetPubKey,
				AuditDisclosureTargetPubKeyBz: input.AuditDisclosureTargetPubKeyBz,
			},
			StartStep: 1,
			MaxSteps:  4,
			AutoDummy: true,
		},
	)
	require.NoError(t, err)
	require.Equal(t, "tx-final", res.TxHash)
	require.Len(t, source.calls, 1)
	require.Empty(t, waiter.calls)
	require.Equal(t, []int{1}, observer.scans)
	require.Equal(t, []int{1}, observer.broadcastFinal)
	require.Equal(t, []string{"tx-final"}, observer.completions)
	require.NotNil(t, broadcaster.msg)
	require.Equal(t, input.Creator, broadcaster.msg.Creator)
}

func TestExecuteTransferPropagatesBroadcastError(t *testing.T) {
	input, merkleProvider, signer, artifacts, runner := testBuildTransferMessageDeps(t)
	source := &stubRecursiveTransferNoteSource{
		responses: [][]privacyscan.FoundNote{
			append([]privacyscan.FoundNote(nil), input.Inputs[:]...),
		},
	}
	broadcaster := &stubTransferMessageBroadcaster{err: context.DeadlineExceeded}

	_, err := ExecuteTransfer(
		context.Background(),
		source,
		nil,
		nil,
		nil,
		ExecuteTransferDependencies{
			MerklePaths: merkleProvider,
			Signer:      signer,
			Artifacts:   artifacts,
			Runner:      runner,
			Broadcaster: broadcaster,
		},
		ExecuteTransferInput{
			Creator:              input.Creator,
			RecipientSpendPubKey: input.RecipientSpendPubKey,
			RecipientViewPubKey:  input.RecipientViewPubKey,
			SenderSpendPubKey:    input.SenderSpendPubKey,
			SenderViewPubKey:     input.SenderViewPubKey,
			TransferAmount:       input.TransferAmount,
			TransferDenom:        input.TransferDenom,
			Disclosure: StepDisclosureConfig{
				UserPrivacyPolicy:             input.UserPrivacyPolicy,
				UserDisclosureMode:            input.UserDisclosureMode,
				UserDisclosureTargetPubKey:    input.UserDisclosureTargetPubKey,
				UserDisclosureTargetPubKeyBz:  input.UserDisclosureTargetPubKeyBz,
				AuditDisclosureTargetPubKey:   input.AuditDisclosureTargetPubKey,
				AuditDisclosureTargetPubKeyBz: input.AuditDisclosureTargetPubKeyBz,
			},
			StartStep: 1,
			MaxSteps:  4,
			AutoDummy: true,
		},
	)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}
