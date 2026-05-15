package transfer

import (
	"context"
	"fmt"
	"math/big"

	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"

	privacyscan "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/scan"
)

type RecursiveTransferNoteSource interface {
	LoadFoundNotes(ctx context.Context) ([]privacyscan.FoundNote, error)
}

type RecursiveTransferDummyPreparer interface {
	PrepareDummyNote(ctx context.Context, denom string) error
}

type RecursiveTransferStepExecutor interface {
	ExecuteTransferStep(ctx context.Context, decision *RecursivePlannerDecision) (*RecursiveTransferTxResult, error)
}

type RecursiveTransferBlockWaiter interface {
	WaitForNextBlock(ctx context.Context, currentHeight int64) error
}

type RecursiveTransferObserver interface {
	OnScan(step int)
	OnBroadcastFinal(step int)
	OnBroadcastSelfMerge(step int, total *big.Int)
	OnTransferComplete(step int, txHash string)
	OnWaitForBlock(step int, txHash string, height int64)
}

type RecursiveTransferTxResult struct {
	TxHash string
	Height int64
}

type ExecuteRecursiveTransferInput struct {
	FinalRecipientSpendPubKey *crypto_tedwards.PointAffine
	FinalRecipientViewPubKey  *crypto_tedwards.PointAffine
	SelfSpendPubKey           *crypto_tedwards.PointAffine
	SelfViewPubKey            *crypto_tedwards.PointAffine
	TargetAmount              *big.Int
	TargetDenom               string
	StartStep                 int
	MaxSteps                  int
	AutoDummy                 bool
	Runtime                   *RecursivePlannerRuntime
}

func ExecuteRecursiveTransfer(
	ctx context.Context,
	source RecursiveTransferNoteSource,
	dummyPreparer RecursiveTransferDummyPreparer,
	executor RecursiveTransferStepExecutor,
	waiter RecursiveTransferBlockWaiter,
	observer RecursiveTransferObserver,
	input ExecuteRecursiveTransferInput,
) (*RecursiveTransferTxResult, error) {
	if source == nil {
		return nil, fmt.Errorf("a recursive transfer note source is required")
	}
	if executor == nil {
		return nil, fmt.Errorf("a recursive transfer step executor is required")
	}
	if input.StartStep <= 0 {
		return nil, fmt.Errorf("recursive transfer start step must be positive")
	}
	if input.MaxSteps <= 0 {
		return nil, fmt.Errorf("recursive transfer max steps must be positive")
	}

	runtime := input.Runtime
	if runtime == nil {
		runtime = NewRecursivePlannerRuntime()
	}

	for step := input.StartStep; step <= input.MaxSteps; step++ {
		if observer != nil {
			observer.OnScan(step)
		}

		foundNotes, err := source.LoadFoundNotes(ctx)
		if err != nil {
			return nil, err
		}

		decision, err := runtime.DecideNextStep(RecursivePlannerInput{
			FoundNotes:                foundNotes,
			TargetDenom:               input.TargetDenom,
			TargetAmount:              input.TargetAmount,
			Step:                      step,
			AutoDummy:                 input.AutoDummy,
			FinalRecipientSpendPubKey: input.FinalRecipientSpendPubKey,
			FinalRecipientViewPubKey:  input.FinalRecipientViewPubKey,
			SelfSpendPubKey:           input.SelfSpendPubKey,
			SelfViewPubKey:            input.SelfViewPubKey,
		})
		if err != nil {
			return nil, err
		}

		if decision.Action == RecursivePlannerActionPrepareDummy {
			if dummyPreparer == nil {
				return nil, fmt.Errorf("a recursive transfer dummy preparer is required")
			}
			if err := dummyPreparer.PrepareDummyNote(ctx, input.TargetDenom); err != nil {
				return nil, fmt.Errorf("automatic dummy-note preparation failed: %w", err)
			}
			continue
		}

		if decision.IsFinal {
			if observer != nil {
				observer.OnBroadcastFinal(step)
			}
		} else if observer != nil {
			observer.OnBroadcastSelfMerge(step, decision.InputsTotal)
		}

		txResult, err := executor.ExecuteTransferStep(ctx, decision)
		if err != nil {
			return nil, err
		}
		if txResult == nil {
			return nil, fmt.Errorf("recursive transfer step executor returned a nil result")
		}

		if decision.IsFinal {
			if observer != nil {
				observer.OnTransferComplete(step, txResult.TxHash)
			}
			return txResult, nil
		}

		if waiter == nil {
			return nil, fmt.Errorf("a recursive transfer block waiter is required")
		}
		if observer != nil {
			observer.OnWaitForBlock(step, txResult.TxHash, txResult.Height)
		}
		if err := waiter.WaitForNextBlock(ctx, txResult.Height); err != nil {
			return nil, fmt.Errorf("polling failed while waiting for the next block: %w", err)
		}
	}

	return nil, fmt.Errorf(
		"planner exceeded %d steps without reaching a final transfer; inspect note state with list-notes and retry with explicit flags if you need manual control",
		input.MaxSteps,
	)
}
