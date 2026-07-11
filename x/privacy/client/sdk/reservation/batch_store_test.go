package reservation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestBatchOperationGraphIsAtomicAndConflictsWithOrdinaryReservation(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	reservations, graph := testBatchOperationGraph("batch-op-a", 3, 4)
	created, err := store.CreateBatchOperation(ctx, reservations, graph)
	require.NoError(t, err)
	require.Len(t, created.Inputs, 3)
	require.Len(t, created.Items, 4)
	require.Len(t, created.Evidence, 4)

	conflict := testReservation("ordinary-conflict", "ordinary-note", "ordinary-op")
	conflict.OwnerKeyID = reservations[0].OwnerKeyID
	conflict.NullifierLookupKey = reservations[0].NullifierLookupKey
	_, err = store.CreateReservation(ctx, conflict)
	require.ErrorIs(t, err, ErrActiveReservationExists)

	invalidReservations, invalidGraph := testBatchOperationGraph("batch-op-b", 2, 2)
	invalidGraph.Evidence = invalidGraph.Evidence[:1]
	_, err = store.CreateBatchOperation(ctx, invalidReservations, invalidGraph)
	require.Error(t, err)
	_, err = store.GetReservation(ctx, invalidReservations[0].ReservationID)
	require.ErrorIs(t, err, ErrReservationNotFound)
	_, err = store.GetBatchOperation(ctx, invalidGraph.Operation.OperationID)
	require.ErrorIs(t, err, ErrOperationNotFound)
}

func TestBatchOperationLeaseProofBroadcastAndIdempotentRetry(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	reservations, graph := testBatchOperationGraph("batch-op-flow", 2, 2)
	_, err := store.CreateBatchOperation(ctx, reservations, graph)
	require.NoError(t, err)
	now := fixedNow()
	lease, err := store.AcquireBatchOperationLease(ctx, graph.Operation.OperationID, "worker", "lease-token", now.Add(time.Minute), now)
	require.NoError(t, err)
	_, err = store.CompareAndSetBatchOperationStatus(ctx, graph.Operation.OperationID, lease.LeaseToken, OperationStatusPlanned, OperationStatusProving, now)
	require.NoError(t, err)
	_, err = store.SaveBatchProofArtifacts(ctx, graph.Operation.OperationID, lease.LeaseToken, BatchProofArtifactUpdate{
		PreparedPayloadCiphertext: []byte("encrypted-payload"), PreparedPayloadHash: "payload-hash",
		ProofCiphertext: []byte("encrypted-proof"), ProofHash: "proof-hash",
	}, now)
	require.NoError(t, err)
	lease, err = store.AcquireBatchOperationLease(ctx, graph.Operation.OperationID, "broadcaster", "broadcast-lease", now.Add(time.Minute), now)
	require.NoError(t, err)
	update := BatchBroadcastUpdate{
		SignedTxBytesCiphertext: []byte("encrypted-signed-tx"), TxBytesHash: "tx-bytes-hash",
		SignDocHash: "sign-doc", TxHash: "tx-hash", AccountSequence: 7, Unknown: true,
	}
	_, err = store.SaveBatchSignedTx(ctx, graph.Operation.OperationID, lease.LeaseToken, BatchSignedTxUpdate{
		SignedTxBytesCiphertext: update.SignedTxBytesCiphertext, TxBytesHash: update.TxBytesHash,
		SignDocHash: update.SignDocHash, TxHash: update.TxHash, AccountSequence: update.AccountSequence,
	}, now)
	require.NoError(t, err)
	first, err := store.RecordBatchBroadcast(ctx, graph.Operation.OperationID, lease.LeaseToken, update, now)
	require.NoError(t, err)
	require.Equal(t, OperationStatusUnknown, first.Status)
	require.Equal(t, 1, first.BroadcastAttemptCount)
	lease, err = store.AcquireBatchOperationLease(ctx, graph.Operation.OperationID, "reconcile", "retry-lease", now.Add(time.Minute), now.Add(time.Second))
	require.NoError(t, err)
	second, err := store.RecordBatchBroadcast(ctx, graph.Operation.OperationID, lease.LeaseToken, update, now.Add(time.Second))
	require.NoError(t, err)
	require.Equal(t, 2, second.BroadcastAttemptCount)
	update.TxBytesHash = "different-signed-bytes"
	_, err = store.SaveBatchSignedTx(ctx, graph.Operation.OperationID, lease.LeaseToken, BatchSignedTxUpdate{
		SignedTxBytesCiphertext: update.SignedTxBytesCiphertext, TxBytesHash: update.TxBytesHash,
		SignDocHash: update.SignDocHash, TxHash: "different-tx-hash", AccountSequence: 8,
	}, now.Add(2*time.Second))
	require.Error(t, err)
}

func TestBatchOperationCASRejectsUnmappedRelationTransitions(t *testing.T) {
	for _, target := range []OperationStatus{OperationStatusReplanRequired, OperationStatusManualReview} {
		t.Run(string(target), func(t *testing.T) {
			ctx := context.Background()
			store := NewMemoryStore()
			reservations, graph := testBatchOperationGraph("batch-op-cas-"+string(target), 2, 2)
			_, err := store.CreateBatchOperation(ctx, reservations, graph)
			require.NoError(t, err)
			now := fixedNow()
			lease, err := store.AcquireBatchOperationLease(ctx, graph.Operation.OperationID, "worker", "lease-token", now.Add(time.Minute), now)
			require.NoError(t, err)
			_, err = store.CompareAndSetBatchOperationStatus(ctx, graph.Operation.OperationID, lease.LeaseToken, OperationStatusPlanned, OperationStatusProving, now)
			require.NoError(t, err)

			_, err = store.CompareAndSetBatchOperationStatus(ctx, graph.Operation.OperationID, lease.LeaseToken, OperationStatusProving, target, now)
			require.ErrorIs(t, err, ErrInvalidTransition)
			stored, err := store.GetBatchOperation(ctx, graph.Operation.OperationID)
			require.NoError(t, err)
			require.Equal(t, OperationStatusProving, stored.Operation.Status)
			require.Equal(t, lease.LeaseToken, stored.Operation.LeaseToken)
			for _, reservation := range reservations {
				input, getErr := store.GetReservation(ctx, reservation.ReservationID)
				require.NoError(t, getErr)
				require.Equal(t, StatusProving, input.Status)
				require.Equal(t, lease.LeaseToken, input.LeaseToken)
			}
		})
	}
}

func TestBatchOperationHeartbeatNeverRegressesSharedLease(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	reservations, graph := testBatchOperationGraph("batch-op-monotonic-heartbeat", 2, 2)
	_, err := store.CreateBatchOperation(ctx, reservations, graph)
	require.NoError(t, err)
	now := fixedNow()
	lease, err := store.AcquireBatchOperationLease(ctx, graph.Operation.OperationID, "worker", "lease-token", now.Add(5*time.Minute), now)
	require.NoError(t, err)

	newerAt := now.Add(2 * time.Minute)
	newerUntil := now.Add(7 * time.Minute)
	_, err = store.HeartbeatBatchOperationLease(ctx, graph.Operation.OperationID, lease.LeaseToken, newerUntil, newerAt)
	require.NoError(t, err)

	staleAt := now.Add(time.Minute)
	staleUntil := now.Add(6 * time.Minute)
	stale, err := store.HeartbeatBatchOperationLease(ctx, graph.Operation.OperationID, lease.LeaseToken, staleUntil, staleAt)
	require.NoError(t, err)
	require.Equal(t, newerAt, stale.LastHeartbeatAt)
	require.Equal(t, newerUntil, stale.LeaseUntil)

	laterAt := now.Add(3 * time.Minute)
	shorterUntil := now.Add(6*time.Minute + 30*time.Second)
	later, err := store.HeartbeatBatchOperationLease(ctx, graph.Operation.OperationID, lease.LeaseToken, shorterUntil, laterAt)
	require.NoError(t, err)
	require.Equal(t, laterAt, later.LastHeartbeatAt)
	require.Equal(t, newerUntil, later.LeaseUntil)
	for _, input := range reservations {
		stored, getErr := store.GetReservation(ctx, input.ReservationID)
		require.NoError(t, getErr)
		require.Equal(t, laterAt, stored.LastHeartbeatAt)
		require.Equal(t, newerUntil, stored.LeaseUntil)
	}
}

func TestBatchReconcileSeparatesSpentNotesFromItemEvidence(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	reservations, graph := testBatchOperationGraph("batch-op-reconcile", 2, 3)
	_, err := store.CreateBatchOperation(ctx, reservations, graph)
	require.NoError(t, err)

	observed := []ObservedOutputEvidence{
		{OutputIndex: 0, Commitment: graph.Evidence[0].Commitment, UserDisclosureDigest: graph.Evidence[0].UserDisclosureDigest, FullDisclosureDigest: graph.Evidence[0].FullDisclosureDigest, RecipientHash: graph.Evidence[0].RecipientHash},
		// Output 1 intentionally lacks evidence even though both nullifiers are spent.
		{OutputIndex: 2, Commitment: graph.Evidence[2].Commitment, UserDisclosureDigest: graph.Evidence[2].UserDisclosureDigest, FullDisclosureDigest: graph.Evidence[2].FullDisclosureDigest, RecipientHash: graph.Evidence[2].RecipientHash, AuditDeliveryFailed: true},
	}
	updated, err := store.ReconcileBatchOperation(ctx, graph.Operation.OperationID, BatchReconcileUpdate{
		TxHash: "tx-hash", TxSucceeded: true,
		SpentReservationIDs: []string{reservations[0].ReservationID, reservations[1].ReservationID},
		ObservedOutputs:     observed,
	}, fixedNow())
	require.NoError(t, err)
	require.Equal(t, OperationStatusSucceeded, updated.Operation.Status)
	require.Equal(t, BatchItemEvidenceSucceeded, updated.Items[0].EvidenceStatus)
	require.Equal(t, BatchItemEvidenceManualReview, updated.Items[1].EvidenceStatus)
	require.Equal(t, BatchItemEvidenceManualReview, updated.Items[2].EvidenceStatus)
	for _, reservation := range reservations {
		stored, getErr := store.GetReservation(ctx, reservation.ReservationID)
		require.NoError(t, getErr)
		require.Equal(t, StatusConfirmedSpent, stored.Status)
	}
}

func TestBatchReconcileMovesContradictoryTerminalEvidenceToManualReview(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	reservations, graph := testBatchOperationGraph("batch-op-terminal-conflict", 1, 2)
	_, err := store.CreateBatchOperation(ctx, reservations, graph)
	require.NoError(t, err)
	observed := make([]ObservedOutputEvidence, len(graph.Evidence))
	for i, expected := range graph.Evidence {
		observed[i] = ObservedOutputEvidence{
			OutputIndex: expected.OutputIndex, Commitment: expected.Commitment,
			UserDisclosureDigest: expected.UserDisclosureDigest, FullDisclosureDigest: expected.FullDisclosureDigest,
			RecipientHash: expected.RecipientHash,
		}
	}
	succeeded, err := store.ReconcileBatchOperation(ctx, graph.Operation.OperationID, BatchReconcileUpdate{
		TxHash: "terminal-tx", TxSucceeded: true,
		SpentReservationIDs: []string{reservations[0].ReservationID}, ObservedOutputs: observed,
	}, fixedNow())
	require.NoError(t, err)
	require.Equal(t, OperationStatusSucceeded, succeeded.Operation.Status)

	contradicted, err := store.ReconcileBatchOperation(ctx, graph.Operation.OperationID, BatchReconcileUpdate{
		TxHash: "terminal-tx", TxFailed: true, FailureReason: "stale node reported failure",
	}, fixedNow().Add(time.Second))
	require.NoError(t, err)
	require.Equal(t, OperationStatusManualReview, contradicted.Operation.Status)
	require.Contains(t, contradicted.Operation.LastBroadcastError, batchTerminalReconcileConflictReason)
	for _, item := range contradicted.Items {
		require.Equal(t, BatchItemEvidenceManualReview, item.EvidenceStatus)
		require.Equal(t, batchTerminalReconcileConflictReason, item.ManualReviewReason)
	}
	input, err := store.GetReservation(ctx, reservations[0].ReservationID)
	require.NoError(t, err)
	require.Equal(t, StatusConfirmedSpent, input.Status, "contradictory evidence must not partially rewrite terminal input state")
}

func TestBatchReconcilePromotesSpentEvidenceAfterFailureToConflictSpent(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	reservations, graph := testBatchOperationGraph("batch-op-failed-then-spent", 1, 1)
	_, err := store.CreateBatchOperation(ctx, reservations, graph)
	require.NoError(t, err)
	failed, err := store.ReconcileBatchOperation(ctx, graph.Operation.OperationID, BatchReconcileUpdate{
		TxHash: "failed-tx", TxFailed: true,
	}, fixedNow())
	require.NoError(t, err)
	require.Equal(t, OperationStatusFailed, failed.Operation.Status)
	input, err := store.GetReservation(ctx, reservations[0].ReservationID)
	require.NoError(t, err)
	require.Equal(t, StatusReplanRequired, input.Status)

	conflict, err := store.ReconcileBatchOperation(ctx, graph.Operation.OperationID, BatchReconcileUpdate{
		TxHash: "failed-tx", SpentReservationIDs: []string{reservations[0].ReservationID},
		FailureReason: "nullifier became spent after failed transaction evidence",
	}, fixedNow().Add(time.Second))
	require.NoError(t, err)
	require.Equal(t, OperationStatusConflictSpent, conflict.Operation.Status)
	require.Contains(t, conflict.Operation.LastBroadcastError, batchTerminalReconcileConflictReason)
	input, err = store.GetReservation(ctx, reservations[0].ReservationID)
	require.NoError(t, err)
	require.Equal(t, StatusConfirmedSpent, input.Status)
	require.Equal(t, BatchItemEvidenceManualReview, conflict.Items[0].EvidenceStatus)
}

func TestBatchReconcileClearsSharedOperationAndInputLeases(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	reservations, graph := testBatchOperationGraph("batch-op-reconcile-lease", 2, 2)
	_, err := store.CreateBatchOperation(ctx, reservations, graph)
	require.NoError(t, err)
	now := fixedNow()
	lease, err := store.AcquireBatchOperationLease(ctx, graph.Operation.OperationID, "proof-worker", "shared-lease", now.Add(time.Minute), now)
	require.NoError(t, err)
	_, err = store.CompareAndSetBatchOperationStatus(ctx, graph.Operation.OperationID, lease.LeaseToken, OperationStatusPlanned, OperationStatusProving, now)
	require.NoError(t, err)

	updated, err := store.ReconcileBatchOperation(ctx, graph.Operation.OperationID, BatchReconcileUpdate{
		SpentReservationIDs: []string{reservations[0].ReservationID},
	}, now.Add(time.Second))
	require.NoError(t, err)
	require.Equal(t, OperationStatusManualReview, updated.Operation.Status)
	require.Empty(t, updated.Operation.LeaseToken)
	for _, reservation := range reservations {
		stored, getErr := store.GetReservation(ctx, reservation.ReservationID)
		require.NoError(t, getErr)
		require.Empty(t, stored.LeaseToken)
		require.True(t, stored.LeaseUntil.IsZero())
	}
}

func TestBatchOperationDurableFileRestartRoundTrip(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "batch-reservations.json")
	store, err := OpenDurableFileStore(path)
	require.NoError(t, err)
	reservations, graph := testBatchOperationGraph("batch-op-restart", 3, 4)
	_, err = store.CreateBatchOperation(ctx, reservations, graph)
	require.NoError(t, err)
	lease, err := store.AcquireBatchOperationLease(ctx, graph.Operation.OperationID, "worker", "lease-restart", fixedNow().Add(time.Minute), fixedNow())
	require.NoError(t, err)
	_, err = store.CompareAndSetBatchOperationStatus(ctx, graph.Operation.OperationID, lease.LeaseToken, OperationStatusPlanned, OperationStatusProving, fixedNow())
	require.NoError(t, err)

	reopened, err := OpenDurableFileStore(path)
	require.NoError(t, err)
	loaded, err := reopened.GetBatchOperation(ctx, graph.Operation.OperationID)
	require.NoError(t, err)
	require.Equal(t, OperationStatusProving, loaded.Operation.Status)
	require.Equal(t, lease.LeaseToken, loaded.Operation.LeaseToken)
	require.Len(t, loaded.Inputs, 3)
	require.Len(t, loaded.Items, 4)
	require.Len(t, loaded.Evidence, 4)
}

func TestBatchOperationSQLSchemaIsVersionedAndRelational(t *testing.T) {
	for _, schema := range []string{PostgreSQLSchema(), SQLiteSchema()} {
		require.Contains(t, schema, "batch_operation_store_meta")
		require.Contains(t, schema, "batch_operations")
		require.Contains(t, schema, "batch_operation_inputs")
		require.Contains(t, schema, "payroll_item_outputs")
		require.Contains(t, schema, "expected_output_evidence")
	}
	require.Contains(t, sqlBatchSchemaSeedStatement(SQLDialectSQLite), BatchOperationSchemaVersionV1)
	var _ BatchOperationStore = (*SQLStore)(nil)
}

func TestBatchOperationFileRejectsUnknownSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad-schema.json")
	store, err := OpenDurableFileStore(path)
	require.NoError(t, err)
	reservations, graph := testBatchOperationGraph("batch-op-schema", 1, 1)
	_, err = store.CreateBatchOperation(context.Background(), reservations, graph)
	require.NoError(t, err)
	bz, err := os.ReadFile(path)
	require.NoError(t, err)
	bz = []byte(strings.Replace(string(bz), BatchOperationSchemaVersionV1, "unknown-batch-schema", 1))
	require.NoError(t, os.WriteFile(path, bz, 0o600))
	_, err = OpenDurableFileStore(path)
	require.True(t, errors.Is(err, ErrInvalidReservation))
}

func testBatchOperationGraph(operationID string, inputCount, outputCount int) ([]NoteReservation, BatchOperationGraph) {
	now := fixedNow()
	reservations := make([]NoteReservation, inputCount)
	inputs := make([]OperationInputReservation, inputCount)
	for i := 0; i < inputCount; i++ {
		reservations[i] = testReservation(fmt.Sprintf("%s-res-%d", operationID, i), fmt.Sprintf("note-%d", i), operationID)
		reservations[i].ItemID = ""
		reservations[i].NullifierLookupKey = fmt.Sprintf("nullifier-%s-%d", operationID, i)
		reservations[i].CreatedAt = now
		reservations[i].UpdatedAt = now
		inputs[i] = OperationInputReservation{SchemaVersion: BatchOperationSchemaVersionV1, OperationID: operationID, ReservationID: reservations[i].ReservationID, InputIndex: i, Commitment: fmt.Sprintf("%064x", i+1), CreatedAt: now}
	}
	items := make([]PayrollItemOutput, outputCount)
	evidence := make([]ExpectedOutputEvidence, outputCount)
	for i := 0; i < outputCount; i++ {
		role := BatchOutputRolePayment
		itemID := fmt.Sprintf("item-%d", i)
		policy := uint32(privacytypes.TransferPrivacyPolicyDiscloseAmount)
		mode := privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_PUBLIC
		userDigest := fmt.Sprintf("user-%d", i)
		if i == outputCount-1 && outputCount > 1 {
			role = BatchOutputRoleChange
			itemID = ""
			policy = 0
			mode = privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_NONE
			userDigest = ""
		}
		items[i] = PayrollItemOutput{SchemaVersion: BatchOperationSchemaVersionV1, OperationID: operationID, ItemID: itemID, OutputIndex: i, Role: role, EvidenceStatus: BatchItemEvidencePending, CreatedAt: now, UpdatedAt: now}
		evidence[i] = ExpectedOutputEvidence{
			SchemaVersion: BatchOperationSchemaVersionV1, OperationID: operationID, OutputIndex: i,
			Commitment: fmt.Sprintf("commitment-%d", i), UserPrivacyPolicy: policy, UserDisclosureMode: mode,
			UserDisclosureDigest: userDigest, FullDisclosureDigest: fmt.Sprintf("full-%d", i),
			RecipientHash: fmt.Sprintf("recipient-%d", i), Denom: "uclv", AssetID: "asset", Role: role,
			AuditKeyID: "audit-default", AuditKeyEpoch: 1, CreatedAt: now, UpdatedAt: now,
		}
	}
	graph := BatchOperationGraph{
		Operation: BatchOperation{SchemaVersion: BatchOperationSchemaVersionV1, OperationID: operationID, CompanyID: "company", PayrollID: "payroll", BatchID: "batch", OwnerKeyID: "owner", AssetID: "asset", Denom: "uclv", InputCount: inputCount, OutputCount: outputCount, Status: OperationStatusPlanned, PreparedPayloadCiphertext: []byte("encrypted-prepared"), PreparedPayloadHash: "payload-hash", CreatedAt: now, UpdatedAt: now},
		Inputs:    inputs, Items: items, Evidence: evidence,
	}
	return reservations, graph
}
