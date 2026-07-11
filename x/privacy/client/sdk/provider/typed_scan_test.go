package provider

import (
	"context"
	"fmt"
	"math/big"
	"testing"

	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

type stubPrivacyScanV2Querier struct {
	responses []*privacytypes.QueryPrivacyScanResponse
	requests  []*privacytypes.QueryPrivacyScanRequest
}

type stubPathsAtRootQuerier struct {
	response *privacytypes.QueryCommitmentPathsAtRootResponse
}

func (s stubPathsAtRootQuerier) CommitmentPathsAtRoot(context.Context, *privacytypes.QueryCommitmentPathsAtRootRequest, ...grpc.CallOption) (*privacytypes.QueryCommitmentPathsAtRootResponse, error) {
	return s.response, nil
}

type stubAssetByIDQuerier struct {
	response *privacytypes.QueryAssetByIDResponse
}

func (s stubAssetByIDQuerier) AssetByID(context.Context, *privacytypes.QueryAssetByIDRequest, ...grpc.CallOption) (*privacytypes.QueryAssetByIDResponse, error) {
	return s.response, nil
}

func (s *stubPrivacyScanV2Querier) PrivacyScan(_ context.Context, req *privacytypes.QueryPrivacyScanRequest, _ ...grpc.CallOption) (*privacytypes.QueryPrivacyScanResponse, error) {
	s.requests = append(s.requests, req)
	response := s.responses[0]
	s.responses = s.responses[1:]
	return response, nil
}

func TestPrivacyScanV2Paginates32OutputsWithoutLoss(t *testing.T) {
	effectID := make([]byte, 32)
	effectID[31] = 1
	txHash := make([]byte, 32)
	txHash[31] = 2
	summary := typedBatchSummary(effectID, txHash, 32)
	outputs := make([]*privacytypes.PrivacyScanOutputV2, 32)
	for i := range outputs {
		outputs[i] = typedBatchOutput(effectID, txHash, uint32(i))
	}
	q := &stubPrivacyScanV2Querier{responses: []*privacytypes.QueryPrivacyScanResponse{
		{Summaries: []*privacytypes.PrivacyScanSummaryV2{summary}, Outputs: outputs[:17], NextCursor: &privacytypes.PrivacyScanCursorV1{Height: 9, GlobalSequence: 7, OutputIndex: 16}, HasMore: true, ScanSchemaVersion: privacytypes.PrivacyScanSchemaVersionV2},
		{Summaries: []*privacytypes.PrivacyScanSummaryV2{summary}, Outputs: outputs[17:], NextCursor: &privacytypes.PrivacyScanCursorV1{Height: 9, GlobalSequence: 7, OutputIndex: 31}, ScanSchemaVersion: privacytypes.PrivacyScanSchemaVersionV2},
	}}
	p := ScanQueryProvider{PrivacyScanQuerier: q}
	first, err := p.PrivacyScan(context.Background(), nil, 17, 0, 0, nil)
	require.NoError(t, err)
	require.Len(t, first.Outputs, 17)
	second, err := p.PrivacyScan(context.Background(), first.NextCursor, 17, 0, 0, nil)
	require.NoError(t, err)
	require.Len(t, second.Outputs, 15)
	require.Equal(t, uint32(17), second.Outputs[0].OutputIndex)
	require.Equal(t, first.NextCursor, q.requests[1].After)
}

func TestPrivacyScanV2AllowsCursorPastLastOutputForZeroOutputTail(t *testing.T) {
	effectID := make([]byte, 32)
	effectID[31] = 1
	txHash := make([]byte, 32)
	txHash[31] = 2
	output := typedBatchOutput(effectID, txHash, 0)
	q := &stubPrivacyScanV2Querier{responses: []*privacytypes.QueryPrivacyScanResponse{{
		Summaries:         []*privacytypes.PrivacyScanSummaryV2{typedBatchSummary(effectID, txHash, 1), typedZeroOutputSummary(10, 8)},
		Outputs:           []*privacytypes.PrivacyScanOutputV2{output},
		NextCursor:        &privacytypes.PrivacyScanCursorV1{Height: 10, GlobalSequence: 8, OutputIndex: 0},
		ScannedEventCount: 2,
		ScanSchemaVersion: privacytypes.PrivacyScanSchemaVersionV2,
	}}}

	response, err := (ScanQueryProvider{PrivacyScanQuerier: q}).PrivacyScan(
		context.Background(), nil, 10, 10, 0,
		nil,
	)
	require.NoError(t, err)
	require.Empty(t, q.responses)
	require.Equal(t, int64(10), response.NextCursor.Height)
	require.Equal(t, uint64(8), response.NextCursor.GlobalSequence)
}

func TestPrivacyScanV2RejectsCursorAdvancePastIncompleteOutputEvent(t *testing.T) {
	effectID := make([]byte, 32)
	effectID[31] = 1
	txHash := make([]byte, 32)
	txHash[31] = 2
	q := &stubPrivacyScanV2Querier{responses: []*privacytypes.QueryPrivacyScanResponse{{
		Summaries:         []*privacytypes.PrivacyScanSummaryV2{typedBatchSummary(effectID, txHash, 2), typedZeroOutputSummary(10, 8)},
		Outputs:           []*privacytypes.PrivacyScanOutputV2{typedBatchOutput(effectID, txHash, 0)},
		NextCursor:        &privacytypes.PrivacyScanCursorV1{Height: 10, GlobalSequence: 8, OutputIndex: 0},
		ScannedEventCount: 2,
		ScanSchemaVersion: privacytypes.PrivacyScanSchemaVersionV2,
	}}}

	_, err := (ScanQueryProvider{PrivacyScanQuerier: q}).PrivacyScan(context.Background(), nil, 10, 10, 0, nil)
	require.ErrorContains(t, err, "incomplete output event")
}

func TestPrivacyScanV2RejectsOutputGapFromNilStartCursor(t *testing.T) {
	effectID := make([]byte, 32)
	effectID[31] = 1
	txHash := make([]byte, 32)
	txHash[31] = 2
	q := &stubPrivacyScanV2Querier{responses: []*privacytypes.QueryPrivacyScanResponse{{
		Summaries:         []*privacytypes.PrivacyScanSummaryV2{typedBatchSummary(effectID, txHash, 2)},
		Outputs:           []*privacytypes.PrivacyScanOutputV2{typedBatchOutput(effectID, txHash, 1)},
		NextCursor:        &privacytypes.PrivacyScanCursorV1{Height: 9, GlobalSequence: 7, OutputIndex: 1},
		ScannedEventCount: 1,
		ScanSchemaVersion: privacytypes.PrivacyScanSchemaVersionV2,
	}}}

	_, err := (ScanQueryProvider{PrivacyScanQuerier: q}).PrivacyScan(context.Background(), nil, 10, 10, 0, nil)
	require.ErrorContains(t, err, "new event output must start at index zero")
}

func TestPrivacyScanV2RejectsEffectIDCorruption(t *testing.T) {
	effectID := make([]byte, 32)
	effectID[31] = 1
	txHash := make([]byte, 32)
	txHash[31] = 2
	output := typedBatchOutput(effectID, txHash, 0)
	output.EffectId = append([]byte(nil), effectID...)
	output.EffectId[30] = 9
	q := &stubPrivacyScanV2Querier{responses: []*privacytypes.QueryPrivacyScanResponse{{Summaries: []*privacytypes.PrivacyScanSummaryV2{typedBatchSummary(effectID, txHash, 1)}, Outputs: []*privacytypes.PrivacyScanOutputV2{output}, NextCursor: &privacytypes.PrivacyScanCursorV1{Height: 9, GlobalSequence: 7}, ScanSchemaVersion: privacytypes.PrivacyScanSchemaVersionV2}}}
	_, err := (ScanQueryProvider{PrivacyScanQuerier: q}).PrivacyScan(context.Background(), nil, 1, 0, 0, nil)
	require.ErrorContains(t, err, "effect identity mismatch")
}

func TestCommitmentPathsAtRootReconstructsOptionalHeightSnapshot(t *testing.T) {
	commitment := big.NewInt(23)
	empty := privacytypes.EmptyNoteTreeRootsV1(32)
	path := make([]string, 32)
	helper := make([]uint32, 32)
	root := new(big.Int).Set(commitment)
	for level := 0; level < 32; level++ {
		path[level] = fmt.Sprintf("%x", empty[level].FillBytes(make([]byte, 32)))
		root = privacytypes.ComputeNoteTreeNodeV1(uint32(level), root, empty[level])
	}
	commitmentHex := fmt.Sprintf("%x", commitment.FillBytes(make([]byte, 32)))
	response := &privacytypes.QueryCommitmentPathsAtRootResponse{
		RootHex: fmt.Sprintf("%x", root.FillBytes(make([]byte, 32))), SnapshotHeight: 44, LeafCount: 1,
		Paths: []*privacytypes.QueryCommitmentPathAtRoot{{CommitmentHex: commitmentHex, LeafIndex: 0, Path: path, PathHelper: helper}},
	}
	provider := ScanQueryProvider{PathsAtRootQuerier: stubPathsAtRootQuerier{response}}
	snapshot, err := provider.CommitmentPathsAtRoot(context.Background(), []string{commitmentHex}, "", 0)
	require.NoError(t, err)
	require.Equal(t, int64(44), snapshot.SnapshotHeight)
	response.Paths[0].Path[0] = fmt.Sprintf("%064x", 99)
	_, err = provider.CommitmentPathsAtRoot(context.Background(), []string{commitmentHex}, "", 0)
	require.ErrorContains(t, err, "does not reconstruct")
}

func TestAssetDenomByIDRecomputesRegistryMapping(t *testing.T) {
	asset := privacytypes.ComputeAssetIDV1("uclair")
	assetBytes := asset.FillBytes(make([]byte, 32))
	provider := ScanQueryProvider{AssetByIDQuerier: stubAssetByIDQuerier{&privacytypes.QueryAssetByIDResponse{MappingVersion: privacytypes.AssetRegistryVersionV1, Asset: &privacytypes.AssetRegistryEntryV1{CanonicalDenom: "uclair", AssetId: assetBytes}}}}
	denom, err := provider.AssetDenomByID(context.Background(), assetBytes)
	require.NoError(t, err)
	require.Equal(t, "uclair", denom)
	provider.AssetByIDQuerier = stubAssetByIDQuerier{&privacytypes.QueryAssetByIDResponse{MappingVersion: privacytypes.AssetRegistryVersionV1, Asset: &privacytypes.AssetRegistryEntryV1{CanonicalDenom: "uforged", AssetId: assetBytes}}}
	_, err = provider.AssetDenomByID(context.Background(), assetBytes)
	require.ErrorContains(t, err, "does not derive")
}

func typedBatchSummary(effectID, txHash []byte, count uint32) *privacytypes.PrivacyScanSummaryV2 {
	nullifier := make([]byte, 32)
	nullifier[31] = 7
	return &privacytypes.PrivacyScanSummaryV2{Height: 9, GlobalSequence: 7, TxHash: txHash, EventType: privacytypes.EventTypeBatchTransferV1, Nullifiers: [][]byte{nullifier}, OutputCount: count, EffectId: effectID, CircuitSetId: privacytypes.ActiveCircuitSetID, PayloadVersion: privacytypes.FixedPayloadVersionV1, ScanSchemaVersion: privacytypes.PrivacyScanSchemaVersionV2, AuditKeyId: "audit-default", AuditKeyEpoch: 1, AuditTargetPubkey: typedScanAuditTarget()}
}

func typedZeroOutputSummary(height int64, sequence uint64) *privacytypes.PrivacyScanSummaryV2 {
	txHash := make([]byte, 32)
	txHash[31] = 3
	return &privacytypes.PrivacyScanSummaryV2{
		Height: height, GlobalSequence: sequence, TxHash: txHash, EventType: privacytypes.EventTypeWithdraw,
		CircuitSetId: privacytypes.ActiveCircuitSetID, PayloadVersion: privacytypes.FixedPayloadVersionV1, ScanSchemaVersion: privacytypes.PrivacyScanSchemaVersionV2,
	}
}

func typedBatchOutput(effectID, txHash []byte, index uint32) *privacytypes.PrivacyScanOutputV2 {
	commitment := make([]byte, 32)
	commitment[31] = byte(index + 1)
	// NotePlaintextV1 (350) + ECIES point/nonce/tag overhead (60).
	ciphertext, err := privacytypes.WrapEncryptedEnvelopeV1(privacytypes.EnvelopeTransferNoteV1, make([]byte, 410))
	if err != nil {
		panic(err)
	}
	fullDigest := make([]byte, 32)
	fullDigest[31] = byte(index + 1)
	auditPayload, err := privacytypes.WrapEncryptedEnvelopeV1(privacytypes.EnvelopeAuditDisclosureV1, make([]byte, 452))
	if err != nil {
		panic(err)
	}
	return &privacytypes.PrivacyScanOutputV2{Height: 9, GlobalSequence: 7, OutputIndex: index, EffectId: effectID, Commitment: commitment, Ciphertext: ciphertext, ViewTag: []byte{1, 2}, LeafIndexFound: true, LeafIndex: uint64(index), UserDisclosureMode: privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_NONE.String(), FullDisclosureDigest: fullDigest, AuditDisclosurePayload: auditPayload, CircuitSetId: privacytypes.ActiveCircuitSetID, PayloadVersion: privacytypes.FixedPayloadVersionV1, ScanSchemaVersion: privacytypes.PrivacyScanSchemaVersionV2, AuditKeyId: "audit-default", AuditKeyEpoch: 1, AuditTargetPubkey: typedScanAuditTarget(), TxHash: txHash, EventType: privacytypes.EventTypeBatchTransferV1}
}

func typedScanAuditTarget() []byte {
	curve := crypto_tedwards.GetEdwardsCurve()
	var point crypto_tedwards.PointAffine
	point.ScalarMultiplication(&curve.Base, big.NewInt(17))
	encoded := point.Bytes()
	return append([]byte(nil), encoded[:]...)
}
