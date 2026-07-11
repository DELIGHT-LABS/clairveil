package payroll

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/consensys/gnark-crypto/ecc/bn254/fr/mimc"
	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
	cryptoeddsa "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards/eddsa"
	"github.com/stretchr/testify/require"

	privacybatchtransfer "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/batchtransfer"
	privacyreservation "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/reservation"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestBatchReconcileDurableRestartRetryTxHashFirstAndItemEvidenceSeparate(t *testing.T) {
	ctx := context.Background()
	payload := batchReconcileTestPayload(t)
	now := time.Date(2026, 7, 11, 2, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "batch-reconcile.json")
	store, err := privacyreservation.OpenDurableFileStore(path)
	require.NoError(t, err)
	reservations, graph := batchReconcileTestGraph(payload, now)
	_, err = store.CreateBatchOperation(ctx, reservations, graph)
	require.NoError(t, err)

	lease, err := store.AcquireBatchOperationLease(ctx, graph.Operation.OperationID, "proof-worker", "proof-lease", now.Add(time.Minute), now)
	require.NoError(t, err)
	_, err = store.CompareAndSetBatchOperationStatus(ctx, graph.Operation.OperationID, lease.LeaseToken, privacyreservation.OperationStatusPlanned, privacyreservation.OperationStatusProving, now)
	require.NoError(t, err)
	_, err = store.SaveBatchProofArtifacts(ctx, graph.Operation.OperationID, lease.LeaseToken, privacyreservation.BatchProofArtifactUpdate{ProofCiphertext: []byte("sealed-proof"), ProofHash: "proof-hash"}, now)
	require.NoError(t, err)
	lease, err = store.AcquireBatchOperationLease(ctx, graph.Operation.OperationID, "broadcast-worker", "broadcast-lease", now.Add(time.Minute), now)
	require.NoError(t, err)
	_, err = store.SaveBatchSignedTx(ctx, graph.Operation.OperationID, lease.LeaseToken, privacyreservation.BatchSignedTxUpdate{
		SignedTxBytesCiphertext: []byte("sealed-signed-tx"), TxBytesHash: "tx-bytes-hash", SignDocHash: "sign-doc-hash", TxHash: "aabb", AccountSequence: 9,
	}, now)
	require.NoError(t, err)
	_, err = store.RecordBatchBroadcast(ctx, graph.Operation.OperationID, lease.LeaseToken, privacyreservation.BatchBroadcastUpdate{
		TxBytesHash: "tx-bytes-hash", SignDocHash: "sign-doc-hash", TxHash: "aabb", AccountSequence: 9,
		LastBroadcastError: "rpc timeout", Unknown: true,
	}, now)
	require.NoError(t, err)

	// Re-open the durable store, then retry the exact staged signed bytes. A
	// restart must not allocate a new operation or transaction envelope.
	reopened, err := privacyreservation.OpenDurableFileStore(path)
	require.NoError(t, err)
	lease, err = reopened.AcquireBatchOperationLease(ctx, graph.Operation.OperationID, "broadcast-worker", "retry-lease", now.Add(2*time.Minute), now.Add(time.Second))
	require.NoError(t, err)
	_, err = reopened.RecordBatchBroadcast(ctx, graph.Operation.OperationID, lease.LeaseToken, privacyreservation.BatchBroadcastUpdate{
		TxBytesHash: "tx-bytes-hash", SignDocHash: "sign-doc-hash", TxHash: "aabb", AccountSequence: 9, Unknown: true,
	}, now.Add(time.Second))
	require.NoError(t, err)

	nullifier := hex.EncodeToString(payload.Inputs[0].Nullifier)
	chain := &orderedBatchReconcileChain{
		lookup:     &BatchTxLookupResult{Found: true, Succeeded: true, TxHash: "0xAABB", Height: 17},
		nullifiers: map[string]bool{"0x" + strings.ToUpper(nullifier): true},
	}
	worker := BatchReconcileWorker{Store: reopened, Reconciler: chain, Now: func() time.Time { return now.Add(2 * time.Second) }}
	observed := batchMatchingObservedOutput(graph.Evidence[0])
	result, err := worker.Reconcile(ctx, BatchReconcileRequest{OperationID: graph.Operation.OperationID, Payload: payload, ObservedOutputs: []privacyreservation.ObservedOutputEvidence{observed}})
	require.NoError(t, err)
	require.Equal(t, []string{"lookup:aabb", "nullifiers"}, chain.calls)
	require.False(t, result.PendingChainEvidence)
	require.Equal(t, []string{reservations[0].ReservationID}, result.SpentReservationIDs)
	require.Equal(t, privacyreservation.OperationStatusSucceeded, result.Graph.Operation.Status)
	require.Equal(t, 2, result.Graph.Operation.BroadcastAttemptCount)
	require.Equal(t, privacyreservation.BatchItemEvidenceSucceeded, result.Graph.Items[0].EvidenceStatus)
	require.Equal(t, privacyreservation.BatchItemEvidenceManualReview, result.Graph.Items[1].EvidenceStatus)
	require.Contains(t, result.Graph.Items[1].ManualReviewReason, "missing")
	storedReservation, err := reopened.GetReservation(ctx, reservations[0].ReservationID)
	require.NoError(t, err)
	require.Equal(t, privacyreservation.StatusConfirmedSpent, storedReservation.Status)

	effectID := strings.Repeat("01", 32)
	report, err := BuildBatchOperationReport(*result.Graph, "0x"+effectID)
	require.NoError(t, err)
	require.Equal(t, privacyreservation.OperationStatusSucceeded, report.ChainStatus)
	require.Equal(t, 1, report.ProofCount)
	require.Equal(t, 1, report.TxEnvelopeCount)
	require.Equal(t, 2, report.BroadcastAttemptCount)
	require.Equal(t, 1, report.RetryCount)
	require.Equal(t, 2, report.PaymentItems)
	require.Equal(t, 1, report.SucceededItems)
	require.Equal(t, 1, report.ManualReviewItems)
	require.Equal(t, effectID, report.Outputs[0].EffectID)
	require.Equal(t, 0, report.Outputs[0].OutputIndex)
	require.Equal(t, "item-0", report.Outputs[0].ItemID)
	require.Equal(t, []string{"expected output evidence is missing"}, report.Outputs[1].DisclosureFindings)

	// A later chain-only reconcile must preserve already verified/manual item
	// evidence instead of degrading every output merely because no new scan
	// records were supplied.
	repeated, err := worker.Reconcile(ctx, BatchReconcileRequest{OperationID: graph.Operation.OperationID, Payload: payload})
	require.NoError(t, err)
	require.Equal(t, privacyreservation.BatchItemEvidenceSucceeded, repeated.Graph.Items[0].EvidenceStatus)
	require.Equal(t, privacyreservation.BatchItemEvidenceManualReview, repeated.Graph.Items[1].EvidenceStatus)
	require.Contains(t, repeated.Graph.Items[1].ManualReviewReason, "missing")
}

func TestBatchReconcileFindsSucceededHistoricalTxAfterResignStaging(t *testing.T) {
	ctx := context.Background()
	payload := batchReconcileTestPayload(t)
	now := time.Date(2026, 7, 11, 3, 0, 0, 0, time.UTC)
	store := privacyreservation.NewMemoryStore()
	reservations, graph := batchReconcileTestGraph(payload, now)
	_, err := store.CreateBatchOperation(ctx, reservations, graph)
	require.NoError(t, err)

	lease, err := store.AcquireBatchOperationLease(ctx, graph.Operation.OperationID, "proof-worker", "proof-lease", now.Add(time.Minute), now)
	require.NoError(t, err)
	_, err = store.CompareAndSetBatchOperationStatus(ctx, graph.Operation.OperationID, lease.LeaseToken, privacyreservation.OperationStatusPlanned, privacyreservation.OperationStatusProving, now)
	require.NoError(t, err)
	_, err = store.SaveBatchProofArtifacts(ctx, graph.Operation.OperationID, lease.LeaseToken, privacyreservation.BatchProofArtifactUpdate{
		ProofCiphertext: []byte("sealed-proof"), ProofHash: "proof-hash",
	}, now)
	require.NoError(t, err)

	lease, err = store.AcquireBatchOperationLease(ctx, graph.Operation.OperationID, "broadcast-worker", "broadcast-a", now.Add(time.Minute), now)
	require.NoError(t, err)
	_, err = store.SaveBatchSignedTx(ctx, graph.Operation.OperationID, lease.LeaseToken, privacyreservation.BatchSignedTxUpdate{
		SignedTxBytesCiphertext: []byte("sealed-signed-a"), TxBytesHash: "bytes-a", SignDocHash: "sign-a", TxHash: "aaaa", AccountSequence: 10,
	}, now)
	require.NoError(t, err)
	_, err = store.RecordBatchBroadcast(ctx, graph.Operation.OperationID, lease.LeaseToken, privacyreservation.BatchBroadcastUpdate{
		TxBytesHash: "bytes-a", SignDocHash: "sign-a", TxHash: "aaaa", AccountSequence: 10,
		LastBroadcastError: "rpc response lost", Unknown: true,
	}, now)
	require.NoError(t, err)

	// Explicitly stage a new-sequence transaction B after A was absent. If A
	// lands in the gap before B's final nullifier check, reconciliation must
	// still attribute A rather than losing it behind the current B hash.
	lease, err = store.AcquireBatchOperationLease(ctx, graph.Operation.OperationID, "broadcast-worker", "broadcast-b", now.Add(2*time.Minute), now.Add(time.Second))
	require.NoError(t, err)
	_, err = store.SaveBatchSignedTx(ctx, graph.Operation.OperationID, lease.LeaseToken, privacyreservation.BatchSignedTxUpdate{
		SignedTxBytesCiphertext: []byte("sealed-signed-b"), TxBytesHash: "bytes-b", SignDocHash: "sign-b", TxHash: "bbbb", AccountSequence: 11,
	}, now.Add(time.Second))
	require.NoError(t, err)

	nullifier := hex.EncodeToString(payload.Inputs[0].Nullifier)
	chain := &orderedBatchReconcileChain{
		lookups: map[string]*BatchTxLookupResult{
			"bbbb": {Found: false, TxHash: "bbbb"},
			"aaaa": {Found: true, Succeeded: true, TxHash: "0xAAAA", Height: 23},
		},
		nullifiers: map[string]bool{nullifier: true},
	}
	observed := make([]privacyreservation.ObservedOutputEvidence, len(graph.Evidence))
	for i, expected := range graph.Evidence {
		observed[i] = batchMatchingObservedOutput(expected)
	}
	result, err := (BatchReconcileWorker{Store: store, Reconciler: chain, Now: func() time.Time { return now.Add(2 * time.Second) }}).Reconcile(
		ctx,
		BatchReconcileRequest{OperationID: graph.Operation.OperationID, Payload: payload, ObservedOutputs: observed},
	)
	require.NoError(t, err)
	require.Equal(t, []string{"lookup:bbbb", "lookup:aaaa", "nullifiers"}, chain.calls)
	require.Equal(t, privacyreservation.OperationStatusSucceeded, result.Graph.Operation.Status)
	require.Equal(t, "0xAAAA", result.Graph.Operation.TxHash)
	require.Equal(t, "bytes-a", result.Graph.Operation.TxBytesHash)
	require.Equal(t, "sign-a", result.Graph.Operation.SignDocHash)
	require.Equal(t, uint64(10), result.Graph.Operation.AccountSequence)
	require.Equal(t, []byte("sealed-signed-a"), result.Graph.Operation.SignedTxBytesCiphertext)
	require.Len(t, result.Graph.Operation.BroadcastHistory, 1)
}

func TestBatchReconcileLeavesUnknownUnspentOperationPending(t *testing.T) {
	payload := batchReconcileTestPayload(t)
	_, graph := batchReconcileTestGraph(payload, time.Now().UTC())
	graph.Operation.TxHash = "ccdd"
	store := &captureBatchReconcileStore{graph: graph}
	nullifier := hex.EncodeToString(payload.Inputs[0].Nullifier)
	chain := &orderedBatchReconcileChain{lookup: &BatchTxLookupResult{Found: false, TxHash: "ccdd"}, nullifiers: map[string]bool{nullifier: false}}
	result, err := (BatchReconcileWorker{Store: store, Reconciler: chain}).Reconcile(context.Background(), BatchReconcileRequest{OperationID: graph.Operation.OperationID, Payload: payload})
	require.NoError(t, err)
	require.True(t, result.PendingChainEvidence)
	require.False(t, store.reconciled)
	require.Equal(t, []string{"lookup:ccdd", "nullifiers"}, chain.calls)
}

func TestBatchReconcilePersistsReviewWhenTerminalChainEvidenceDisappears(t *testing.T) {
	payload := batchReconcileTestPayload(t)
	now := time.Now().UTC()
	reservations, graph := batchReconcileTestGraph(payload, now)
	store := privacyreservation.NewMemoryStore()
	_, err := store.CreateBatchOperation(context.Background(), reservations, graph)
	require.NoError(t, err)
	observed := make([]privacyreservation.ObservedOutputEvidence, len(graph.Evidence))
	for i, expected := range graph.Evidence {
		observed[i] = batchMatchingObservedOutput(expected)
	}
	terminal, err := store.ReconcileBatchOperation(context.Background(), graph.Operation.OperationID, privacyreservation.BatchReconcileUpdate{
		TxHash: "ccdd", TxSucceeded: true,
		SpentReservationIDs: []string{reservations[0].ReservationID}, ObservedOutputs: observed,
	}, now)
	require.NoError(t, err)
	require.Equal(t, privacyreservation.OperationStatusSucceeded, terminal.Operation.Status)

	nullifier := hex.EncodeToString(payload.Inputs[0].Nullifier)
	chain := &orderedBatchReconcileChain{
		lookup: &BatchTxLookupResult{Found: false, TxHash: "ccdd"},
		nullifiers: map[string]bool{
			nullifier: false,
		},
	}
	result, err := (BatchReconcileWorker{Store: store, Reconciler: chain, Now: func() time.Time { return now.Add(time.Second) }}).Reconcile(
		context.Background(), BatchReconcileRequest{OperationID: graph.Operation.OperationID, Payload: payload},
	)
	require.NoError(t, err)
	require.True(t, result.PendingChainEvidence)
	require.Equal(t, privacyreservation.OperationStatusManualReview, result.Graph.Operation.Status)
	require.Contains(t, result.Graph.Operation.LastBroadcastError, "no current transaction or spent-nullifier evidence")
	for _, item := range result.Graph.Items {
		require.Equal(t, privacyreservation.BatchItemEvidenceManualReview, item.EvidenceStatus)
	}
	input, err := store.GetReservation(context.Background(), reservations[0].ReservationID)
	require.NoError(t, err)
	require.Equal(t, privacyreservation.StatusConfirmedSpent, input.Status)
}

func TestBatchOperationReportClassifiesDisclosureAndStateCosts(t *testing.T) {
	now := time.Now().UTC()
	graph := privacyreservation.BatchOperationGraph{
		Operation: privacyreservation.BatchOperation{
			SchemaVersion: privacyreservation.BatchOperationSchemaVersionV1, OperationID: "op-report", CompanyID: "company", PayrollID: "payroll", BatchID: "batch",
			InputCount: 1, OutputCount: 3, Status: privacyreservation.OperationStatusSucceeded,
			ProofCiphertext: []byte("sealed-proof"), ProofHash: "proof", SignedTxBytesCiphertext: []byte("sealed-tx"), TxBytesHash: "tx-bytes", TxHash: "ABCD",
			BroadcastAttemptCount: 2, BroadcastHistory: []privacyreservation.BatchBroadcastAttempt{{Unknown: true}, {BroadcastError: "second timeout", Unknown: true}},
		},
		Inputs: []privacyreservation.OperationInputReservation{{SchemaVersion: privacyreservation.BatchOperationSchemaVersionV1, OperationID: "op-report", ReservationID: "res-0", InputIndex: 0, Commitment: strings.Repeat("01", 32)}},
		Items: []privacyreservation.PayrollItemOutput{
			{SchemaVersion: privacyreservation.BatchOperationSchemaVersionV1, OperationID: "op-report", OutputIndex: 0, Role: privacyreservation.BatchOutputRolePayment, ItemID: "item-0", EmployeeID: "employee-0", EvidenceStatus: privacyreservation.BatchItemEvidenceManualReview, ManualReviewReason: "disclosure delivery requires manual review"},
			{SchemaVersion: privacyreservation.BatchOperationSchemaVersionV1, OperationID: "op-report", OutputIndex: 1, Role: privacyreservation.BatchOutputRoleChange, EvidenceStatus: privacyreservation.BatchItemEvidenceSucceeded},
			{SchemaVersion: privacyreservation.BatchOperationSchemaVersionV1, OperationID: "op-report", OutputIndex: 2, Role: privacyreservation.BatchOutputRolePadding, EvidenceStatus: privacyreservation.BatchItemEvidenceSucceeded},
		},
		Evidence: []privacyreservation.ExpectedOutputEvidence{
			{SchemaVersion: privacyreservation.BatchOperationSchemaVersionV1, OperationID: "op-report", OutputIndex: 0, Role: privacyreservation.BatchOutputRolePayment, Commitment: "01", UserDisclosureDigest: "02", FullDisclosureDigest: "03", RecipientHash: "04", ObservedCommitment: "01", ObservedUserDigest: "ff", ObservedFullDigest: "03", ObservedRecipientHash: "04", AuditDeliveryFailed: true, SelfViewDeliveryFailed: true, CreatedAt: now},
			{SchemaVersion: privacyreservation.BatchOperationSchemaVersionV1, OperationID: "op-report", OutputIndex: 1, Role: privacyreservation.BatchOutputRoleChange, Commitment: "11", FullDisclosureDigest: "12", ObservedCommitment: "11", ObservedFullDigest: "12", CreatedAt: now},
			{SchemaVersion: privacyreservation.BatchOperationSchemaVersionV1, OperationID: "op-report", OutputIndex: 2, Role: privacyreservation.BatchOutputRolePadding, Commitment: "21", FullDisclosureDigest: "22", ObservedCommitment: "21", ObservedFullDigest: "22", CreatedAt: now},
		},
	}
	report, err := BuildBatchOperationReport(graph, strings.Repeat("12", 32))
	require.NoError(t, err)
	require.Equal(t, BatchOutputStateCost{PaymentOutputs: 1, ChangeOutputs: 1, PaddingOutputs: 1, PersistedCommitments: 3}, report.OutputStateCost)
	require.Equal(t, 1, report.PaymentItems)
	require.Equal(t, 1, report.ManualReviewItems)
	require.Equal(t, []string{
		"user disclosure digest mismatch",
		"audit disclosure delivery failed",
		"self-view disclosure delivery failed",
	}, report.Outputs[0].DisclosureFindings)
	require.Equal(t, []string{"broadcast outcome unknown; exact signed bytes retained for retry", "second timeout"}, report.RetryReasons)
}

type orderedBatchReconcileChain struct {
	lookup     *BatchTxLookupResult
	lookups    map[string]*BatchTxLookupResult
	nullifiers map[string]bool
	calls      []string
}

func (c *orderedBatchReconcileChain) LookupBatchTx(_ context.Context, txHash string) (*BatchTxLookupResult, error) {
	c.calls = append(c.calls, "lookup:"+txHash)
	if c.lookups != nil {
		lookup := c.lookups[normalizeBatchEvidenceHex(txHash)]
		if lookup == nil {
			return &BatchTxLookupResult{Found: false, TxHash: txHash}, nil
		}
		copy := *lookup
		return &copy, nil
	}
	return c.lookup, nil
}

func (c *orderedBatchReconcileChain) CheckBatchNullifiers(_ context.Context, _ []string) (map[string]bool, error) {
	c.calls = append(c.calls, "nullifiers")
	return c.nullifiers, nil
}

type captureBatchReconcileStore struct {
	privacyreservation.BatchOperationStore
	graph      privacyreservation.BatchOperationGraph
	reconciled bool
}

func (s *captureBatchReconcileStore) GetBatchOperation(_ context.Context, _ string) (*privacyreservation.BatchOperationGraph, error) {
	copy := s.graph
	return &copy, nil
}

func (s *captureBatchReconcileStore) ReconcileBatchOperation(_ context.Context, _ string, _ privacyreservation.BatchReconcileUpdate, _ time.Time) (*privacyreservation.BatchOperationGraph, error) {
	s.reconciled = true
	copy := s.graph
	return &copy, nil
}

type batchReconcileSigner struct{ key *cryptoeddsa.PrivateKey }

func (s batchReconcileSigner) SignBatchTransfer(request privacybatchtransfer.BatchTransferSigningRequest) ([]byte, error) {
	if err := privacybatchtransfer.ValidateBatchTransferSigningRequest(request); err != nil {
		return nil, err
	}
	return s.key.Sign(request.ExpectedIntent.FillBytes(make([]byte, 32)), mimc.NewMiMC())
}

type batchReconcilePathProvider struct{}

func (batchReconcilePathProvider) LookupMerklePath(_ context.Context, commitmentHex string) (*privacybatchtransfer.MerklePathResult, error) {
	commitment := new(big.Int)
	if _, ok := commitment.SetString(commitmentHex, 16); !ok {
		return nil, fmt.Errorf("invalid commitment")
	}
	siblings := privacytypes.EmptyNoteTreeRootsV1(32)
	current := new(big.Int).Set(commitment)
	path := make([]string, 32)
	helper := make([]uint32, 32)
	for i := 0; i < 32; i++ {
		path[i] = fmt.Sprintf("%x", siblings[i].FillBytes(make([]byte, 32)))
		current = privacytypes.ComputeNoteTreeNodeV1(uint32(i), current, siblings[i])
	}
	return &privacybatchtransfer.MerklePathResult{Root: current.FillBytes(make([]byte, 32)), Path: path, PathHelper: helper}, nil
}

func batchReconcileTestPayload(t *testing.T) *privacybatchtransfer.PreparedBatchTransferPayload {
	t.Helper()
	ownerKey, err := cryptoeddsa.GenerateKey(bytes.NewReader(bytes.Repeat([]byte{0x42}, 32)))
	require.NoError(t, err)
	ownerBytes := ownerKey.PublicKey.Bytes()
	var owner crypto_tedwards.PointAffine
	_, err = owner.SetBytes(ownerBytes)
	require.NoError(t, err)
	view := batchReconcileTestPoint(2)
	recipientA, recipientB := batchReconcileTestPoint(3), batchReconcileTestPoint(4)
	note := batchReconcileTestNote(t, &owner, view, 2, 11)
	plan, err := privacybatchtransfer.PlanBatchTransfer(privacybatchtransfer.PlanBatchTransferInput{
		Inputs: []privacybatchtransfer.InputNote{{Note: note}}, OwnerSpendPubKey: &owner, OwnerViewPubKey: view,
		Payments: []privacybatchtransfer.Payment{
			{SpendPubKey: recipientA, ViewPubKey: recipientA, Amount: big.NewInt(1), PrivacyPolicy: privacytypes.TransferPrivacyPolicyDiscloseAmount, DisclosureMode: privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_PUBLIC},
			{SpendPubKey: recipientB, ViewPubKey: recipientB, Amount: big.NewInt(1)},
		},
		Mode: privacybatchtransfer.OutputModeCompact,
	})
	require.NoError(t, err)
	prepared, err := privacybatchtransfer.PrepareBatchTransfer(context.Background(), batchReconcilePathProvider{}, plan)
	require.NoError(t, err)
	payload, err := privacybatchtransfer.BuildPreparedBatchTransferPayload(prepared, batchReconcileSigner{key: ownerKey}, privacybatchtransfer.BuildPreparedBatchTransferPayloadInput{
		ChainID: "clairveil-test-1", ExpiresAtUnix: time.Now().Add(time.Hour).Unix(), AuditKeyID: "audit-test", AuditKeyEpoch: 1,
		AuditDisclosureTargetPubKey: batchReconcileTestPoint(9), SelfViewDisclosureTargetPubKey: view,
	})
	require.NoError(t, err)
	return payload
}

func batchReconcileTestPoint(scalar int64) *crypto_tedwards.PointAffine {
	curve := crypto_tedwards.GetEdwardsCurve()
	var point crypto_tedwards.PointAffine
	point.ScalarMultiplication(&curve.Base, big.NewInt(scalar))
	return &point
}

func batchReconcileTestNote(t *testing.T, spend, view *crypto_tedwards.PointAffine, amount, randomness int64) privacytypes.Note {
	t.Helper()
	sx, sy, vx, vy := new(big.Int), new(big.Int), new(big.Int), new(big.Int)
	spend.X.BigInt(sx)
	spend.Y.BigInt(sy)
	view.X.BigInt(vx)
	view.Y.BigInt(vy)
	note := privacytypes.Note{ReceiverSpendPubKeyX: sx, ReceiverSpendPubKeyY: sy, ReceiverViewPubKeyX: vx, ReceiverViewPubKeyY: vy, Amount: big.NewInt(amount), AssetID: privacytypes.ComputeAssetIDV1("uclair"), Randomness: big.NewInt(randomness)}
	require.NoError(t, note.ValidateV1())
	return note
}

func batchReconcileTestGraph(payload *privacybatchtransfer.PreparedBatchTransferPayload, now time.Time) ([]privacyreservation.NoteReservation, privacyreservation.BatchOperationGraph) {
	operationID := "batch-reconcile-op"
	reservationID := "batch-reconcile-res-0"
	reservation := privacyreservation.NoteReservation{
		ReservationID: reservationID, BatchID: "batch", PayrollID: "payroll", CompanyID: "company", NoteID: "note-0", OwnerKeyID: "owner",
		NullifierLookupKey: hex.EncodeToString(payload.Inputs[0].Nullifier), NullifierLookupKeyID: "lookup-v1", EncryptedNullifier: []byte("sealed-nullifier"),
		Status: privacyreservation.StatusReserved, OperationID: operationID, CreatedAt: now, UpdatedAt: now,
	}
	items := make([]privacyreservation.PayrollItemOutput, len(payload.Outputs))
	evidence := make([]privacyreservation.ExpectedOutputEvidence, len(payload.Outputs))
	assetID := hex.EncodeToString(payload.AssetID.FillBytes(make([]byte, 32)))
	for i, wire := range payload.MessageOutputs {
		items[i] = privacyreservation.PayrollItemOutput{SchemaVersion: privacyreservation.BatchOperationSchemaVersionV1, OperationID: operationID, ItemID: fmt.Sprintf("item-%d", i), EmployeeID: fmt.Sprintf("employee-%d", i), OutputIndex: i, Role: privacyreservation.BatchOutputRolePayment, EvidenceStatus: privacyreservation.BatchItemEvidencePending, CreatedAt: now, UpdatedAt: now}
		evidence[i] = privacyreservation.ExpectedOutputEvidence{
			SchemaVersion: privacyreservation.BatchOperationSchemaVersionV1, OperationID: operationID, OutputIndex: i, Role: privacyreservation.BatchOutputRolePayment,
			Commitment: hex.EncodeToString(wire.Commitment), UserPrivacyPolicy: wire.UserPrivacyPolicy, UserDisclosureMode: wire.UserDisclosureMode,
			UserDisclosureDigest: hex.EncodeToString(wire.UserDisclosureDigest), FullDisclosureDigest: hex.EncodeToString(wire.FullDisclosureDigest), RecipientHash: fmt.Sprintf("recipient-%d", i),
			Denom: "uclair", AssetID: assetID, AuditKeyID: payload.AuditKeyID, AuditKeyEpoch: payload.AuditKeyEpoch, CreatedAt: now, UpdatedAt: now,
		}
	}
	graph := privacyreservation.BatchOperationGraph{
		Operation: privacyreservation.BatchOperation{SchemaVersion: privacyreservation.BatchOperationSchemaVersionV1, OperationID: operationID, CompanyID: "company", PayrollID: "payroll", BatchID: "batch", OwnerKeyID: "owner", AssetID: assetID, Denom: "uclair", InputCount: 1, OutputCount: len(items), Status: privacyreservation.OperationStatusPlanned, PreparedPayloadCiphertext: []byte("sealed-payload"), PreparedPayloadHash: payload.PayloadHash, CreatedAt: now, UpdatedAt: now},
		Inputs: []privacyreservation.OperationInputReservation{{
			SchemaVersion: privacyreservation.BatchOperationSchemaVersionV1, OperationID: operationID, ReservationID: reservationID, InputIndex: 0,
			Commitment: hex.EncodeToString(payload.Inputs[0].Note.ComputeCommitment().FillBytes(make([]byte, 32))), CreatedAt: now,
		}},
		Items: items, Evidence: evidence,
	}
	return []privacyreservation.NoteReservation{reservation}, graph
}

func batchMatchingObservedOutput(expected privacyreservation.ExpectedOutputEvidence) privacyreservation.ObservedOutputEvidence {
	return privacyreservation.ObservedOutputEvidence{OutputIndex: expected.OutputIndex, Commitment: expected.Commitment, UserDisclosureDigest: expected.UserDisclosureDigest, FullDisclosureDigest: expected.FullDisclosureDigest, RecipientHash: expected.RecipientHash}
}
