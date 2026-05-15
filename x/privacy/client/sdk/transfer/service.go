package transfer

import (
	"context"
	"fmt"
	"math/big"

	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

type ExecuteTransferDependencies struct {
	MerklePaths MerklePathProvider
	Signer      NoteHashSigner
	Artifacts   JoinSplitArtifactProvider
	Runner      JoinSplitProofRunner
	Broadcaster TransferMessageBroadcaster
}

type ExecuteTransferInput struct {
	Creator              string
	RecipientSpendPubKey *crypto_tedwards.PointAffine
	RecipientViewPubKey  *crypto_tedwards.PointAffine
	SenderSpendPubKey    *crypto_tedwards.PointAffine
	SenderViewPubKey     *crypto_tedwards.PointAffine
	TransferAmount       *big.Int
	TransferDenom        string
	Disclosure           StepDisclosureConfig
	StartStep            int
	MaxSteps             int
	AutoDummy            bool
	Runtime              *RecursivePlannerRuntime
}

func ExecuteTransfer(
	ctx context.Context,
	source RecursiveTransferNoteSource,
	dummyPreparer RecursiveTransferDummyPreparer,
	waiter RecursiveTransferBlockWaiter,
	observer RecursiveTransferObserver,
	deps ExecuteTransferDependencies,
	input ExecuteTransferInput,
) (*sdk.TxResponse, error) {
	executor := &transferServiceStepExecutor{
		deps:  deps,
		input: input,
	}

	_, err := ExecuteRecursiveTransfer(
		ctx,
		source,
		dummyPreparer,
		executor,
		waiter,
		observer,
		ExecuteRecursiveTransferInput{
			FinalRecipientSpendPubKey: input.RecipientSpendPubKey,
			FinalRecipientViewPubKey:  input.RecipientViewPubKey,
			SelfSpendPubKey:           input.SenderSpendPubKey,
			SelfViewPubKey:            input.SenderViewPubKey,
			TargetAmount:              input.TransferAmount,
			TargetDenom:               input.TransferDenom,
			StartStep:                 input.StartStep,
			MaxSteps:                  input.MaxSteps,
			AutoDummy:                 input.AutoDummy,
			Runtime:                   input.Runtime,
		},
	)
	if err != nil {
		return nil, err
	}
	if executor.lastResponse == nil {
		return nil, fmt.Errorf("recursive transfer did not return a final tx response")
	}
	return executor.lastResponse, nil
}

type transferServiceStepExecutor struct {
	deps         ExecuteTransferDependencies
	input        ExecuteTransferInput
	lastResponse *sdk.TxResponse
}

func (e *transferServiceStepExecutor) ExecuteTransferStep(
	ctx context.Context,
	decision *RecursivePlannerDecision,
) (*RecursiveTransferTxResult, error) {
	res, err := BroadcastTransferStep(
		ctx,
		e.deps.MerklePaths,
		e.deps.Signer,
		e.deps.Artifacts,
		e.deps.Runner,
		e.deps.Broadcaster,
		BuildTransferStepMessageInput{
			Creator:              e.input.Creator,
			Inputs:               decision.Inputs,
			RecipientSpendPubKey: decision.RecipientSpendPubKey,
			RecipientViewPubKey:  decision.RecipientViewPubKey,
			TransferAmount:       decision.SendAmount,
			TransferDenom:        e.input.TransferDenom,
			SenderSpendPubKey:    e.input.SenderSpendPubKey,
			SenderViewPubKey:     e.input.SenderViewPubKey,
			IsFinal:              decision.IsFinal,
			Disclosure:           e.input.Disclosure,
		},
	)
	if err != nil {
		return nil, err
	}

	e.lastResponse = res
	return &RecursiveTransferTxResult{
		TxHash: res.TxHash,
		Height: res.Height,
	}, nil
}
