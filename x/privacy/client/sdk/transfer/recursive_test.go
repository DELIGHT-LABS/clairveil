package transfer

import (
	"context"
	"fmt"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	privacyscan "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/scan"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestExecuteRecursiveTransferFinishesWithFinalTransfer(t *testing.T) {
	selfSpendScalar, selfSpendPubKey := testScalarAndPubKey(241)
	selfViewScalar, selfViewPubKey := testScalarAndPubKey(251)
	finalSpendScalar, finalSpendPubKey := testScalarAndPubKey(257)
	finalViewScalar, finalViewPubKey := testScalarAndPubKey(263)

	source := &stubRecursiveTransferNoteSource{
		responses: [][]privacyscan.FoundNote{
			{
				recursiveTransferFoundNote(7, "uclair", "a", 1),
				recursiveTransferFoundNote(5, "uclair", "b", 2),
			},
		},
	}
	executor := &stubRecursiveTransferStepExecutor{
		results: []*RecursiveTransferTxResult{{TxHash: "tx-final", Height: 11}},
	}
	waiter := &stubRecursiveTransferBlockWaiter{}
	observer := &stubRecursiveTransferObserver{}

	result, err := ExecuteRecursiveTransfer(
		context.Background(),
		source,
		nil,
		executor,
		waiter,
		observer,
		ExecuteRecursiveTransferInput{
			FinalRecipientSpendPubKey: finalSpendPubKey,
			FinalRecipientViewPubKey:  finalViewPubKey,
			SelfSpendPubKey:           selfSpendPubKey,
			SelfViewPubKey:            selfViewPubKey,
			TargetAmount:              big.NewInt(12),
			TargetDenom:               "uclair",
			StartStep:                 1,
			MaxSteps:                  4,
			AutoDummy:                 true,
		},
	)
	require.NoError(t, err)
	require.Equal(t, "tx-final", result.TxHash)
	require.Len(t, source.calls, 1)
	require.Len(t, executor.calls, 1)
	require.True(t, executor.calls[0].IsFinal)
	require.Empty(t, waiter.calls)
	require.Equal(t, []int{1}, observer.scans)
	require.Equal(t, []int{1}, observer.broadcastFinal)
	require.Equal(t, []string{"tx-final"}, observer.completions)

	require.NotNil(t, selfSpendScalar)
	require.NotNil(t, selfViewScalar)
	require.NotNil(t, finalSpendScalar)
	require.NotNil(t, finalViewScalar)
}

func TestExecuteRecursiveTransferPreparesDummyAndRetries(t *testing.T) {
	selfSpendScalar, selfSpendPubKey := testScalarAndPubKey(269)
	selfViewScalar, selfViewPubKey := testScalarAndPubKey(271)
	finalSpendScalar, finalSpendPubKey := testScalarAndPubKey(277)
	finalViewScalar, finalViewPubKey := testScalarAndPubKey(281)

	source := &stubRecursiveTransferNoteSource{
		responses: [][]privacyscan.FoundNote{
			{
				recursiveTransferFoundNote(10, "uclair", "a", 1),
			},
			{
				recursiveTransferFoundNote(10, "uclair", "a", 1),
				recursiveTransferFoundNote(0, "uclair", "z", 3),
			},
		},
	}
	dummyPreparer := &stubRecursiveTransferDummyPreparer{}
	executor := &stubRecursiveTransferStepExecutor{
		results: []*RecursiveTransferTxResult{{TxHash: "tx-final", Height: 12}},
	}

	result, err := ExecuteRecursiveTransfer(
		context.Background(),
		source,
		dummyPreparer,
		executor,
		nil,
		nil,
		ExecuteRecursiveTransferInput{
			FinalRecipientSpendPubKey: finalSpendPubKey,
			FinalRecipientViewPubKey:  finalViewPubKey,
			SelfSpendPubKey:           selfSpendPubKey,
			SelfViewPubKey:            selfViewPubKey,
			TargetAmount:              big.NewInt(10),
			TargetDenom:               "uclair",
			StartStep:                 1,
			MaxSteps:                  4,
			AutoDummy:                 true,
		},
	)
	require.NoError(t, err)
	require.Equal(t, "tx-final", result.TxHash)
	require.Len(t, source.calls, 2)
	require.Equal(t, []string{"uclair"}, dummyPreparer.denoms)
	require.Len(t, executor.calls, 1)

	require.NotNil(t, selfSpendScalar)
	require.NotNil(t, selfViewScalar)
	require.NotNil(t, finalSpendScalar)
	require.NotNil(t, finalViewScalar)
}

func TestExecuteRecursiveTransferMergesThenWaitsThenFinishes(t *testing.T) {
	selfSpendScalar, selfSpendPubKey := testScalarAndPubKey(283)
	selfViewScalar, selfViewPubKey := testScalarAndPubKey(293)
	finalSpendScalar, finalSpendPubKey := testScalarAndPubKey(307)
	finalViewScalar, finalViewPubKey := testScalarAndPubKey(311)

	source := &stubRecursiveTransferNoteSource{
		responses: [][]privacyscan.FoundNote{
			{
				recursiveTransferFoundNote(2, "uclair", "a", 1),
				recursiveTransferFoundNote(3, "uclair", "b", 2),
				recursiveTransferFoundNote(9, "uclair", "c", 3),
			},
			{
				recursiveTransferFoundNote(12, "uclair", "d", 4),
				recursiveTransferFoundNote(2, "uclair", "a", 1),
				recursiveTransferFoundNote(0, "uclair", "z", 5),
			},
		},
	}
	executor := &stubRecursiveTransferStepExecutor{
		results: []*RecursiveTransferTxResult{
			{TxHash: "tx-merge", Height: 20},
			{TxHash: "tx-final", Height: 21},
		},
	}
	waiter := &stubRecursiveTransferBlockWaiter{}
	observer := &stubRecursiveTransferObserver{}

	result, err := ExecuteRecursiveTransfer(
		context.Background(),
		source,
		nil,
		executor,
		waiter,
		observer,
		ExecuteRecursiveTransferInput{
			FinalRecipientSpendPubKey: finalSpendPubKey,
			FinalRecipientViewPubKey:  finalViewPubKey,
			SelfSpendPubKey:           selfSpendPubKey,
			SelfViewPubKey:            selfViewPubKey,
			TargetAmount:              big.NewInt(13),
			TargetDenom:               "uclair",
			StartStep:                 1,
			MaxSteps:                  4,
			AutoDummy:                 true,
		},
	)
	require.NoError(t, err)
	require.Equal(t, "tx-final", result.TxHash)
	require.Len(t, source.calls, 2)
	require.Len(t, executor.calls, 2)
	require.False(t, executor.calls[0].IsFinal)
	require.True(t, executor.calls[1].IsFinal)
	require.Equal(t, []int64{20}, waiter.calls)
	require.Equal(t, []int{1, 2}, observer.scans)
	require.Equal(t, []string{"12"}, observer.broadcastMergeTotals)
	require.Equal(t, []int{2}, observer.broadcastFinal)
	require.Equal(t, []string{"tx-final"}, observer.completions)

	require.NotNil(t, selfSpendScalar)
	require.NotNil(t, selfViewScalar)
	require.NotNil(t, finalSpendScalar)
	require.NotNil(t, finalViewScalar)
}

type stubRecursiveTransferNoteSource struct {
	responses [][]privacyscan.FoundNote
	calls     []struct{}
}

func (s *stubRecursiveTransferNoteSource) LoadFoundNotes(_ context.Context) ([]privacyscan.FoundNote, error) {
	s.calls = append(s.calls, struct{}{})
	index := len(s.calls) - 1
	if index >= len(s.responses) {
		index = len(s.responses) - 1
	}
	return append([]privacyscan.FoundNote(nil), s.responses[index]...), nil
}

type stubRecursiveTransferDummyPreparer struct {
	denoms []string
	err    error
}

func (s *stubRecursiveTransferDummyPreparer) PrepareDummyNote(_ context.Context, denom string) error {
	s.denoms = append(s.denoms, denom)
	return s.err
}

type stubRecursiveTransferStepExecutor struct {
	calls   []RecursivePlannerDecision
	results []*RecursiveTransferTxResult
	err     error
}

func (s *stubRecursiveTransferStepExecutor) ExecuteTransferStep(_ context.Context, decision *RecursivePlannerDecision) (*RecursiveTransferTxResult, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.calls = append(s.calls, *decision)
	index := len(s.calls) - 1
	if index >= len(s.results) {
		return nil, fmt.Errorf("missing recursive transfer result for call %d", index)
	}
	return s.results[index], nil
}

type stubRecursiveTransferBlockWaiter struct {
	calls []int64
	err   error
}

func (s *stubRecursiveTransferBlockWaiter) WaitForNextBlock(_ context.Context, currentHeight int64) error {
	s.calls = append(s.calls, currentHeight)
	return s.err
}

type stubRecursiveTransferObserver struct {
	scans                []int
	broadcastFinal       []int
	broadcastMergeTotals []string
	completions          []string
}

func (s *stubRecursiveTransferObserver) OnScan(step int) {
	s.scans = append(s.scans, step)
}

func (s *stubRecursiveTransferObserver) OnBroadcastFinal(step int) {
	s.broadcastFinal = append(s.broadcastFinal, step)
}

func (s *stubRecursiveTransferObserver) OnBroadcastSelfMerge(_ int, total *big.Int) {
	s.broadcastMergeTotals = append(s.broadcastMergeTotals, total.String())
}

func (s *stubRecursiveTransferObserver) OnTransferComplete(_ int, txHash string) {
	s.completions = append(s.completions, txHash)
}

func (s *stubRecursiveTransferObserver) OnWaitForBlock(_ int, _ string, _ int64) {}

func recursiveTransferFoundNote(amount int64, denom string, nullifier string, height int64) privacyscan.FoundNote {
	return privacyscan.FoundNote{
		Note: privacytypes.Note{
			Amount:  big.NewInt(amount),
			AssetID: privacycrypto.HashString(denom),
		},
		Nullifier: nullifier,
		Height:    height,
		IsSpent:   false,
	}
}
