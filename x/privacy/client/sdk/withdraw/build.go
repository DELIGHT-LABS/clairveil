package withdraw

import (
	"context"
	"fmt"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"

	privacyscan "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/scan"
)

type BuildWithdrawPayloadInput struct {
	TargetCoin sdk.Coin
	Recipient  sdk.AccAddress
	ChainID    string
	ExpiresAt  time.Time
	AutoPlan   bool
}

type BuildWithdrawPayloadResult struct {
	SelectedNote privacyscan.FoundNote
	Payload      *PreparedWithdrawPayload
}

func BuildWithdrawPayload(
	ctx context.Context,
	source ExactMatchNoteSource,
	planner ExactMatchAutoPlanner,
	merklePaths MerklePathProvider,
	signer SpendNoteHashSigner,
	artifacts SpendArtifactProvider,
	runner SpendProofRunner,
	input BuildWithdrawPayloadInput,
) (*BuildWithdrawPayloadResult, error) {
	if artifacts == nil {
		return nil, fmt.Errorf("a spend artifact provider is required to build a withdraw payload")
	}
	if runner == nil {
		return nil, fmt.Errorf("a spend proof runner is required to build a withdraw payload")
	}
	prepared, err := BuildPreparedWithdrawProverPayload(ctx, source, planner, merklePaths, signer, input)
	if err != nil {
		return nil, err
	}
	proof, err := BuildPreparedWithdrawProof(*prepared.Payload, artifacts, runner)
	if err != nil {
		return nil, err
	}
	payload, err := prepared.Payload.ToPreparedWithdrawPayload(*proof, time.Now())
	if err != nil {
		return nil, err
	}

	return &BuildWithdrawPayloadResult{
		SelectedNote: prepared.SelectedNote,
		Payload:      payload,
	}, nil
}
