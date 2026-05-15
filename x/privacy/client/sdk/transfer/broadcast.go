package transfer

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

type TransferMessageBroadcaster interface {
	BroadcastTransferMessage(ctx context.Context, msg *privacytypes.MsgTransfer) (*sdk.TxResponse, error)
}

func BroadcastTransferStep(
	ctx context.Context,
	merklePaths MerklePathProvider,
	signer NoteHashSigner,
	artifacts JoinSplitArtifactProvider,
	runner JoinSplitProofRunner,
	broadcaster TransferMessageBroadcaster,
	input BuildTransferStepMessageInput,
) (*sdk.TxResponse, error) {
	if broadcaster == nil {
		return nil, fmt.Errorf("a transfer message broadcaster is required")
	}

	msg, err := BuildTransferStepMessage(ctx, merklePaths, signer, artifacts, runner, input)
	if err != nil {
		return nil, err
	}

	res, err := broadcaster.BroadcastTransferMessage(ctx, msg)
	if err != nil {
		return nil, err
	}
	if res.Code != 0 {
		return res, fmt.Errorf("tx failed with code %d: %s", res.Code, res.RawLog)
	}
	return res, nil
}
