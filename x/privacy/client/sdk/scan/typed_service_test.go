package scan

import (
	"context"
	"errors"
	"math/big"
	"testing"

	cmttypes "github.com/cometbft/cometbft/rpc/core/types"
	"github.com/stretchr/testify/require"

	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
	privacyidentity "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/identity"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

type typedPrivacyScanSource struct {
	latest    int64
	responses []*privacytypes.QueryPrivacyScanResponse
	errors    []error
	requests  []*privacytypes.PrivacyScanCursorV1
}

func (s *typedPrivacyScanSource) LatestBlockHeight(context.Context) (int64, error) {
	return s.latest, nil
}
func (s *typedPrivacyScanSource) SearchPrivacyTxs(context.Context, int64, int, int) ([]*cmttypes.ResultTx, error) {
	return nil, errors.New("lossy ABCI fallback must not be called")
}
func (s *typedPrivacyScanSource) PrivacyScan(_ context.Context, after *privacytypes.PrivacyScanCursorV1, _, _ uint32, _ uint64, _ []string) (*privacytypes.QueryPrivacyScanResponse, error) {
	s.requests = append(s.requests, &privacytypes.PrivacyScanCursorV1{Height: after.Height, GlobalSequence: after.GlobalSequence, OutputIndex: after.OutputIndex})
	if len(s.errors) > 0 {
		err := s.errors[0]
		s.errors = s.errors[1:]
		if err != nil {
			return nil, err
		}
	}
	response := s.responses[0]
	s.responses = s.responses[1:]
	return response, nil
}

func TestTypedScannerRecovers32OutputsAndRetriesTransactionally(t *testing.T) {
	rootSeed := []byte("typed-batch-32-output-wallet")
	outputs, used := typedOwnedBatchOutputs(t, rootSeed, 32)
	first := &privacytypes.QueryPrivacyScanResponse{
		Outputs: outputs[:17], NextCursor: &privacytypes.PrivacyScanCursorV1{Height: 10, GlobalSequence: 7, OutputIndex: 16}, HasMore: true, ScanSchemaVersion: privacytypes.PrivacyScanSchemaVersionV2,
	}
	second := &privacytypes.QueryPrivacyScanResponse{
		Outputs: outputs[17:], NextCursor: &privacytypes.PrivacyScanCursorV1{Height: 10, GlobalSequence: 7, OutputIndex: 31}, ScanSchemaVersion: privacytypes.PrivacyScanSchemaVersionV2,
	}
	wallet := &LocalWalletData{}
	checker := &stubBatchNullifierUsageChecker{batchUsed: used}
	failing := &typedPrivacyScanSource{latest: 10, responses: []*privacytypes.QueryPrivacyScanResponse{first}, errors: []error{nil, errors.New("temporary typed query failure")}}
	_, err := SyncNotes(context.Background(), failing, checker, nil, SyncInput{UserAddress: "clair1typed", RootSeed: rootSeed, Wallet: wallet, PageLimit: 17})
	require.ErrorContains(t, err, "ABCI fallback disabled")
	require.Empty(t, wallet.Notes)
	require.Zero(t, wallet.LastHeight)
	require.Zero(t, wallet.LastSequence)
	require.Zero(t, wallet.LastOutputIndex)

	retry := &typedPrivacyScanSource{latest: 10, responses: []*privacytypes.QueryPrivacyScanResponse{first, second}}
	result, err := SyncNotes(context.Background(), retry, checker, nil, SyncInput{UserAddress: "clair1typed", RootSeed: rootSeed, Wallet: wallet, PageLimit: 17})
	require.NoError(t, err)
	require.Len(t, result.Notes, 32)
	require.Equal(t, uint32(31), result.Wallet.LastOutputIndex)
	require.Equal(t, uint64(7), result.Wallet.LastSequence)
	require.Len(t, retry.requests, 2)
	require.Equal(t, uint32(16), retry.requests[1].OutputIndex)
	seen := make(map[string]struct{}, 32)
	for _, note := range result.Notes {
		_, duplicate := seen[note.Commitment]
		require.False(t, duplicate)
		seen[note.Commitment] = struct{}{}
	}
}

func TestTypedScannerAllowsCursorPastLastOutputForZeroOutputTail(t *testing.T) {
	rootSeed := []byte("typed-zero-output-tail-wallet")
	outputs, used := typedOwnedBatchOutputs(t, rootSeed, 1)
	source := &typedPrivacyScanSource{
		latest: 11,
		responses: []*privacytypes.QueryPrivacyScanResponse{{
			Summaries: []*privacytypes.PrivacyScanSummaryV2{
				{Height: 10, GlobalSequence: 7, OutputCount: 1},
				{Height: 11, GlobalSequence: 8, OutputCount: 0},
			},
			Outputs:           outputs,
			NextCursor:        &privacytypes.PrivacyScanCursorV1{Height: 11, GlobalSequence: 8, OutputIndex: 0},
			ScannedEventCount: 2,
			ScanSchemaVersion: privacytypes.PrivacyScanSchemaVersionV2,
		}},
	}

	result, err := SyncNotes(context.Background(), source, &stubBatchNullifierUsageChecker{batchUsed: used}, nil, SyncInput{UserAddress: "clair1typed", RootSeed: rootSeed, Wallet: &LocalWalletData{}})
	require.NoError(t, err)
	require.Len(t, result.Notes, 1)
	require.Equal(t, int64(11), result.Wallet.LastHeight)
	require.Equal(t, uint64(8), result.Wallet.LastSequence)
	require.Zero(t, result.Wallet.LastOutputIndex)
}

func TestTypedScannerRejectsCursorAdvancePastIncompleteOutputEvent(t *testing.T) {
	rootSeed := []byte("typed-incomplete-event-wallet")
	outputs, used := typedOwnedBatchOutputs(t, rootSeed, 1)
	source := &typedPrivacyScanSource{
		latest: 11,
		responses: []*privacytypes.QueryPrivacyScanResponse{{
			Summaries: []*privacytypes.PrivacyScanSummaryV2{
				{Height: 10, GlobalSequence: 7, OutputCount: 2},
				{Height: 11, GlobalSequence: 8, OutputCount: 0},
			},
			Outputs:           outputs,
			NextCursor:        &privacytypes.PrivacyScanCursorV1{Height: 11, GlobalSequence: 8, OutputIndex: 0},
			ScannedEventCount: 2,
			ScanSchemaVersion: privacytypes.PrivacyScanSchemaVersionV2,
		}},
	}

	wallet := &LocalWalletData{}
	_, err := SyncNotes(context.Background(), source, &stubBatchNullifierUsageChecker{batchUsed: used}, nil, SyncInput{UserAddress: "clair1typed", RootSeed: rootSeed, Wallet: wallet})
	require.ErrorContains(t, err, "incomplete output event")
	require.Empty(t, wallet.Notes)
	require.Zero(t, wallet.LastHeight)
}

func TestTypedScannerFailsClosedAfterOwnedPlaintextCommitmentMismatch(t *testing.T) {
	rootSeed := []byte("typed-batch-corrupt-wallet")
	outputs, used := typedOwnedBatchOutputs(t, rootSeed, 1)
	outputs[0].Commitment = append([]byte(nil), outputs[0].Commitment...)
	outputs[0].Commitment[31] ^= 1
	source := &typedPrivacyScanSource{latest: 10, responses: []*privacytypes.QueryPrivacyScanResponse{{Outputs: outputs, NextCursor: &privacytypes.PrivacyScanCursorV1{Height: 10, GlobalSequence: 7}, ScanSchemaVersion: privacytypes.PrivacyScanSchemaVersionV2}}}
	_, err := SyncNotes(context.Background(), source, &stubBatchNullifierUsageChecker{batchUsed: used}, nil, SyncInput{UserAddress: "clair1typed", RootSeed: rootSeed, Wallet: &LocalWalletData{}})
	require.ErrorContains(t, err, "commitment mismatch")
}

func TestTypedScannerAllowsLegacy2x2SelfViewOnlyOnRealOutput(t *testing.T) {
	outputs := []*privacytypes.PrivacyScanOutputV2{
		{Height: 10, GlobalSequence: 7, OutputIndex: 0, EventType: privacytypes.EventTypeShieldedTransfer, SelfViewDisclosurePayload: []byte{1}},
		{Height: 10, GlobalSequence: 7, OutputIndex: 1, EventType: privacytypes.EventTypeShieldedTransfer},
	}
	response := &privacytypes.QueryPrivacyScanResponse{
		Outputs: outputs, NextCursor: &privacytypes.PrivacyScanCursorV1{Height: 10, GlobalSequence: 7, OutputIndex: 1}, ScanSchemaVersion: privacytypes.PrivacyScanSchemaVersionV2,
	}
	require.NoError(t, validateTypedScanResponseForSync(response, &privacytypes.PrivacyScanCursorV1{}, make(map[string]bool)))
}

func TestTypedScannerRejectsBatchSelfViewMismatchAcrossPages(t *testing.T) {
	state := make(map[string]bool)
	first := &privacytypes.QueryPrivacyScanResponse{
		Outputs:    []*privacytypes.PrivacyScanOutputV2{{Height: 10, GlobalSequence: 7, OutputIndex: 0, EventType: privacytypes.EventTypeBatchTransferV1, SelfViewDisclosurePayload: []byte{1}}},
		NextCursor: &privacytypes.PrivacyScanCursorV1{Height: 10, GlobalSequence: 7}, HasMore: true, ScanSchemaVersion: privacytypes.PrivacyScanSchemaVersionV2,
	}
	require.NoError(t, validateTypedScanResponseForSync(first, &privacytypes.PrivacyScanCursorV1{}, state))
	second := &privacytypes.QueryPrivacyScanResponse{
		Outputs:    []*privacytypes.PrivacyScanOutputV2{{Height: 10, GlobalSequence: 7, OutputIndex: 1, EventType: privacytypes.EventTypeBatchTransferV1}},
		NextCursor: &privacytypes.PrivacyScanCursorV1{Height: 10, GlobalSequence: 7, OutputIndex: 1}, ScanSchemaVersion: privacytypes.PrivacyScanSchemaVersionV2,
	}
	require.ErrorContains(t, validateTypedScanResponseForSync(second, first.NextCursor, state), "batch self-view disclosure is not all-or-none")
}

func typedOwnedBatchOutputs(t *testing.T, rootSeed []byte, count int) ([]*privacytypes.PrivacyScanOutputV2, map[string]bool) {
	t.Helper()
	spendScalar, spendPubKey, _ := privacyidentity.DeriveSpendKeys(rootSeed)
	_, viewPubKey, _ := privacyidentity.DeriveViewKeys(rootSeed)
	_ = spendScalar
	outputs := make([]*privacytypes.PrivacyScanOutputV2, count)
	used := make(map[string]bool, count)
	for i := 0; i < count; i++ {
		note, err := privacytypes.NewNote(pointBigInt(&spendPubKey.X), pointBigInt(&spendPubKey.Y), pointBigInt(&viewPubKey.X), pointBigInt(&viewPubKey.Y), big.NewInt(int64(i+1)), "uclair", "typed-batch")
		require.NoError(t, err)
		commitment, err := privacyfield.CanonicalBytesFromBigInt(note.ComputeCommitment())
		require.NoError(t, err)
		raw, tag, err := privacycrypto.AsymEncryptWithViewTag(mustNoteBytes(t, note), *viewPubKey, commitment, uint32(i))
		require.NoError(t, err)
		ciphertext, err := privacytypes.WrapEncryptedEnvelopeV1(privacytypes.EnvelopeTransferNoteV1, raw)
		require.NoError(t, err)
		outputs[i] = &privacytypes.PrivacyScanOutputV2{Height: 10, GlobalSequence: 7, OutputIndex: uint32(i), EventType: privacytypes.EventTypeBatchTransferV1, Commitment: commitment, Ciphertext: ciphertext, ViewTag: tag, TxHash: make([]byte, 32)}
		nullifier, err := privacyfield.CanonicalHexFromBigInt(note.ComputeNullifier())
		require.NoError(t, err)
		used[nullifier] = false
	}
	return outputs, used
}
