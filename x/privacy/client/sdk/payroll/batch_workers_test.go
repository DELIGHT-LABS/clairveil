package payroll

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math/big"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	privacybatchtransfer "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/batchtransfer"
	privacyreservation "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/reservation"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestIdempotentBatchBroadcastStagesBeforeSendAndRetriesExactBytes(t *testing.T) {
	ctx := context.Background()
	payload := batchReconcileTestPayload(t)
	proof := testPreparedBatchProof(payload)
	store, graph := testProofReadyBatchStore(t, payload)
	creator := sdk.AccAddress(bytes.Repeat([]byte{1}, 20)).String()
	builder := &testSignedBatchBuilder{bytes: []byte("immutable-signed-batch")}
	sender := &testSignedBatchSender{errors: []error{errors.New("rpc response lost"), nil}}
	chain := &testBatchBroadcastChain{statuses: unspentBatchStatuses(payload)}
	worker := IdempotentBatchBroadcastWorker{
		Store: store, Builder: builder, Sender: sender, Reconciler: chain, Cipher: testPayrollCipher{},
		LeaseOwner: "broadcast-worker", LeaseTTL: time.Minute,
	}

	first, err := worker.Submit(ctx, graph.Operation.OperationID, payload, proof, creator, BatchBroadcastOptions{})
	require.ErrorContains(t, err, "rpc response lost")
	require.Equal(t, 1, builder.calls)
	require.Equal(t, 1, sender.calls)
	require.False(t, first.UsedStoredSignedBytes)
	afterFirst, err := store.GetBatchOperation(ctx, graph.Operation.OperationID)
	require.NoError(t, err)
	require.Equal(t, privacyreservation.OperationStatusUnknown, afterFirst.Operation.Status)
	require.NotEmpty(t, afterFirst.Operation.SignedTxBytesCiphertext)
	require.Equal(t, 1, afterFirst.Operation.BroadcastAttemptCount)
	require.Len(t, afterFirst.Operation.BroadcastHistory, 1)

	second, err := worker.Submit(ctx, graph.Operation.OperationID, payload, proof, creator, BatchBroadcastOptions{})
	require.NoError(t, err)
	require.True(t, second.UsedStoredSignedBytes)
	require.Equal(t, 1, builder.calls, "an ambiguous retry must not sign new bytes")
	require.Equal(t, 2, sender.calls)
	require.Equal(t, sender.sent[0], sender.sent[1])
	afterSecond, err := store.GetBatchOperation(ctx, graph.Operation.OperationID)
	require.NoError(t, err)
	require.Equal(t, privacyreservation.OperationStatusUnknown, afterSecond.Operation.Status, "an earlier ambiguous attempt remains Unknown until canonical reconcile")
	require.Equal(t, 2, afterSecond.Operation.BroadcastAttemptCount)
	require.Len(t, afterSecond.Operation.BroadcastHistory, 2)

	chain.lookup = &BatchTxLookupResult{Found: true, Succeeded: true, TxHash: afterSecond.Operation.TxHash, Height: 19}
	third, err := worker.Submit(ctx, graph.Operation.OperationID, payload, proof, creator, BatchBroadcastOptions{})
	require.NoError(t, err)
	require.True(t, third.ReconciledExistingTx)
	require.Equal(t, 2, sender.calls, "a tx-hash hit must never rebroadcast")
}

func TestIdempotentBatchBroadcastRechecksNullifiersImmediatelyBeforeSend(t *testing.T) {
	payload := batchReconcileTestPayload(t)
	store, graph := testProofReadyBatchStore(t, payload)
	chain := &testBatchBroadcastChain{statuses: unspentBatchStatuses(payload), spendOnCheck: 2}
	builder := &testSignedBatchBuilder{bytes: []byte("staged-but-never-broadcast")}
	sender := &testSignedBatchSender{}
	worker := IdempotentBatchBroadcastWorker{
		Store: store, Builder: builder, Sender: sender, Reconciler: chain, Cipher: testPayrollCipher{},
		LeaseOwner: "broadcast-worker", LeaseTTL: time.Minute,
	}
	creator := sdk.AccAddress(bytes.Repeat([]byte{2}, 20)).String()
	outcome, err := worker.Submit(context.Background(), graph.Operation.OperationID, payload, testPreparedBatchProof(payload), creator, BatchBroadcastOptions{})
	require.NoError(t, err)
	require.True(t, outcome.NullifierEvidenceExists)
	require.Equal(t, 0, sender.calls)
	stored, err := store.GetBatchOperation(context.Background(), graph.Operation.OperationID)
	require.NoError(t, err)
	require.NotEmpty(t, stored.Operation.SignedTxBytesCiphertext, "signed bytes must be staged before the final nullifier check")
	require.Zero(t, stored.Operation.BroadcastAttemptCount)
	require.Empty(t, stored.Operation.LeaseToken)
}

func TestIdempotentBatchBroadcastRejectsProofDifferentFromDurableArtifact(t *testing.T) {
	payload := batchReconcileTestPayload(t)
	store, graph := testProofReadyBatchStore(t, payload)
	differentProof := testPreparedBatchProof(payload)
	differentProof.Proof[0] = 1
	builder := &testSignedBatchBuilder{bytes: []byte("must-not-be-signed")}
	sender := &testSignedBatchSender{}
	worker := IdempotentBatchBroadcastWorker{
		Store: store, Builder: builder, Sender: sender,
		Reconciler: &testBatchBroadcastChain{statuses: unspentBatchStatuses(payload)}, Cipher: testPayrollCipher{},
		LeaseOwner: "broadcast-worker", LeaseTTL: time.Minute,
	}
	creator := sdk.AccAddress(bytes.Repeat([]byte{8}, 20)).String()

	_, err := worker.Submit(context.Background(), graph.Operation.OperationID, payload, differentProof, creator, BatchBroadcastOptions{})
	require.ErrorContains(t, err, "does not match the durable proof artifact")
	require.Zero(t, builder.calls)
	require.Zero(t, sender.calls)
	stored, err := store.GetBatchOperation(context.Background(), graph.Operation.OperationID)
	require.NoError(t, err)
	require.Equal(t, privacyreservation.OperationStatusProofReady, stored.Operation.Status)
	require.Empty(t, stored.Operation.LeaseToken)
}

func TestIdempotentBatchBroadcastNeverResignsSubmittedOperation(t *testing.T) {
	payload := batchReconcileTestPayload(t)
	store, graph := testProofReadyBatchStore(t, payload)
	creator := sdk.AccAddress(bytes.Repeat([]byte{3}, 20)).String()
	builder := &testSignedBatchBuilder{bytes: []byte("first-signed-batch")}
	sender := &testSignedBatchSender{}
	chain := &testBatchBroadcastChain{statuses: unspentBatchStatuses(payload)}
	worker := IdempotentBatchBroadcastWorker{Store: store, Builder: builder, Sender: sender, Reconciler: chain, Cipher: testPayrollCipher{}, LeaseOwner: "worker", LeaseTTL: time.Minute}
	_, err := worker.Submit(context.Background(), graph.Operation.OperationID, payload, testPreparedBatchProof(payload), creator, BatchBroadcastOptions{})
	require.NoError(t, err)
	_, err = worker.Submit(context.Background(), graph.Operation.OperationID, payload, testPreparedBatchProof(payload), creator, BatchBroadcastOptions{ResignWithNewSequence: true})
	require.ErrorContains(t, err, "requires an Unknown prior broadcast")
	require.Equal(t, 1, builder.calls)
	require.Equal(t, 1, sender.calls)
}

func TestIdempotentBatchBroadcastReturnsFoundChainFailure(t *testing.T) {
	payload := batchReconcileTestPayload(t)
	store, graph := testProofReadyBatchStore(t, payload)
	creator := sdk.AccAddress(bytes.Repeat([]byte{4}, 20)).String()
	builder := &testSignedBatchBuilder{bytes: []byte("failed-chain-batch")}
	sender := &testSignedBatchSender{}
	chain := &testBatchBroadcastChain{statuses: unspentBatchStatuses(payload)}
	worker := IdempotentBatchBroadcastWorker{Store: store, Builder: builder, Sender: sender, Reconciler: chain, Cipher: testPayrollCipher{}, LeaseOwner: "worker", LeaseTTL: time.Minute}
	_, err := worker.Submit(context.Background(), graph.Operation.OperationID, payload, testPreparedBatchProof(payload), creator, BatchBroadcastOptions{})
	require.NoError(t, err)
	stored, err := store.GetBatchOperation(context.Background(), graph.Operation.OperationID)
	require.NoError(t, err)
	chain.lookup = &BatchTxLookupResult{Found: true, Failed: true, TxHash: stored.Operation.TxHash, Code: 17}
	outcome, err := worker.Submit(context.Background(), graph.Operation.OperationID, payload, testPreparedBatchProof(payload), creator, BatchBroadcastOptions{})
	require.ErrorContains(t, err, "failed on chain")
	require.True(t, outcome.ReconciledExistingTx)
	require.Equal(t, 1, sender.calls)
}

func TestIdempotentBatchBroadcastExplicitlyResignsConfirmedChainFailure(t *testing.T) {
	payload := batchReconcileTestPayload(t)
	store, graph := testProofReadyBatchStore(t, payload)
	creator := sdk.AccAddress(bytes.Repeat([]byte{9}, 20)).String()
	builder := &testSignedBatchBuilder{bytes: []byte("first-sequence-batch")}
	sender := &testSignedBatchSender{}
	chain := &testBatchBroadcastChain{statuses: unspentBatchStatuses(payload)}
	worker := IdempotentBatchBroadcastWorker{
		Store: store, Builder: builder, Sender: sender, Reconciler: chain, Cipher: testPayrollCipher{},
		LeaseOwner: "worker", LeaseTTL: time.Minute,
	}

	_, err := worker.Submit(context.Background(), graph.Operation.OperationID, payload, testPreparedBatchProof(payload), creator, BatchBroadcastOptions{})
	require.NoError(t, err)
	first, err := store.GetBatchOperation(context.Background(), graph.Operation.OperationID)
	require.NoError(t, err)
	require.Equal(t, privacyreservation.OperationStatusSubmitted, first.Operation.Status)
	firstTxHash := first.Operation.TxHash
	chain.lookup = &BatchTxLookupResult{Found: true, Failed: true, TxHash: firstTxHash, Code: 32}
	builder.bytes = []byte("replacement-sequence-batch")

	outcome, err := worker.Submit(
		context.Background(),
		graph.Operation.OperationID,
		payload,
		testPreparedBatchProof(payload),
		creator,
		BatchBroadcastOptions{ResignWithNewSequence: true},
	)
	require.NoError(t, err)
	require.NotNil(t, outcome.Receipt)
	require.NotEqual(t, firstTxHash, outcome.Receipt.TxHash)
	require.Equal(t, 2, builder.calls)
	require.Equal(t, 2, sender.calls)
	require.Equal(t, 4, chain.checks, "both attempts must check nullifiers before signing and immediately before broadcast")

	updated, err := store.GetBatchOperation(context.Background(), graph.Operation.OperationID)
	require.NoError(t, err)
	require.Equal(t, privacyreservation.OperationStatusUnknown, updated.Operation.Status, "replacement submission stays conservative until canonical reconciliation")
	require.Equal(t, outcome.Receipt.TxHash, updated.Operation.TxHash)
	require.Len(t, updated.Operation.BroadcastHistory, 2)
	require.Equal(t, firstTxHash, updated.Operation.BroadcastHistory[0].TxHash)
	require.Equal(t, outcome.Receipt.TxHash, updated.Operation.BroadcastHistory[1].TxHash)
	for _, input := range updated.Inputs {
		reservation, getErr := store.GetReservation(context.Background(), input.ReservationID)
		require.NoError(t, getErr)
		require.Equal(t, privacyreservation.StatusUnknown, reservation.Status)
	}
}

func TestIdempotentBatchBroadcastHeartbeatsLeaseUntilUninterruptibleSendReturns(t *testing.T) {
	payload := batchReconcileTestPayload(t)
	store, graph := testProofReadyBatchStore(t, payload)
	creator := sdk.AccAddress(bytes.Repeat([]byte{5}, 20)).String()
	sender := &blockingSignedBatchSender{started: make(chan struct{}), release: make(chan struct{})}
	worker := IdempotentBatchBroadcastWorker{
		Store: store, Builder: &testSignedBatchBuilder{bytes: []byte("slow-signed-batch")}, Sender: sender,
		Reconciler: &testBatchBroadcastChain{statuses: unspentBatchStatuses(payload)}, Cipher: testPayrollCipher{},
		LeaseOwner: "slow-broadcast-worker", LeaseTTL: 90 * time.Millisecond,
	}
	callerCtx, cancel := context.WithCancel(context.Background())
	resultCh := make(chan error, 1)
	go func() {
		_, submitErr := worker.Submit(callerCtx, graph.Operation.OperationID, payload, testPreparedBatchProof(payload), creator, BatchBroadcastOptions{})
		resultCh <- submitErr
	}()
	select {
	case <-sender.started:
	case <-time.After(2 * time.Second):
		t.Fatal("broadcast did not start")
	}
	// The sender deliberately ignores cancellation, matching an HSM/RPC call
	// that cannot be interrupted once it starts.
	cancel()
	time.Sleep(180 * time.Millisecond)
	leased, err := store.GetBatchOperation(context.Background(), graph.Operation.OperationID)
	require.NoError(t, err)
	require.Equal(t, privacyreservation.OperationStatusProofReady, leased.Operation.Status)
	require.True(t, leased.Operation.LeaseUntil.After(time.Now()), "heartbeat must keep the broadcast lease alive")
	_, err = store.AcquireBatchOperationLease(context.Background(), graph.Operation.OperationID, "competitor", "competitor-token", time.Now().Add(time.Minute), time.Now())
	require.ErrorIs(t, err, privacyreservation.ErrLeaseUnavailable)
	close(sender.release)
	require.NoError(t, <-resultCh)
	finished, err := store.GetBatchOperation(context.Background(), graph.Operation.OperationID)
	require.NoError(t, err)
	require.Equal(t, privacyreservation.OperationStatusSubmitted, finished.Operation.Status)
	require.Empty(t, finished.Operation.LeaseToken)
	require.Equal(t, 1, sender.calls)
}

func TestBatchProofWorkerKeepsSharedLeaseUntilUninterruptibleProveReturns(t *testing.T) {
	payload := batchReconcileTestPayload(t)
	now := time.Now().UTC()
	store := privacyreservation.NewMemoryStore()
	reservations, graph := batchReconcileTestGraph(payload, now)
	_, err := store.CreateBatchOperation(context.Background(), reservations, graph)
	require.NoError(t, err)
	prover := &blockingBatchPayrollProver{started: make(chan struct{}), release: make(chan struct{}), proof: testPreparedBatchProof(payload)}
	worker := BatchProofWorker{
		Store: store, Prover: prover, Sealer: testPayrollCipher{}, LeaseOwner: "proof-worker", LeaseTTL: 90 * time.Millisecond,
	}
	callerCtx, cancel := context.WithCancel(context.Background())
	resultCh := make(chan error, 1)
	go func() {
		_, processErr := worker.Process(callerCtx, graph.Operation.OperationID, payload)
		resultCh <- processErr
	}()
	select {
	case <-prover.started:
	case <-time.After(2 * time.Second):
		t.Fatal("prover did not start")
	}
	cancel()
	time.Sleep(180 * time.Millisecond)
	leased, err := store.GetBatchOperation(context.Background(), graph.Operation.OperationID)
	require.NoError(t, err)
	require.Equal(t, privacyreservation.OperationStatusProving, leased.Operation.Status)
	require.True(t, leased.Operation.LeaseUntil.After(time.Now()), "heartbeat must outlive caller cancellation while prove is still running")
	_, err = store.AcquireBatchOperationLease(context.Background(), graph.Operation.OperationID, "competitor", "competitor-token", time.Now().Add(time.Minute), time.Now())
	require.ErrorIs(t, err, privacyreservation.ErrLeaseUnavailable)
	close(prover.release)
	require.NoError(t, <-resultCh)
	finished, err := store.GetBatchOperation(context.Background(), graph.Operation.OperationID)
	require.NoError(t, err)
	require.Equal(t, privacyreservation.OperationStatusProofReady, finished.Operation.Status)
	require.NotEmpty(t, finished.Operation.ProofCiphertext)
	require.Empty(t, finished.Operation.LeaseToken)
}

func TestBatchProofWorkerReleasesLeaseWhenProvingClaimFails(t *testing.T) {
	payload := batchReconcileTestPayload(t)
	now := time.Now().UTC()
	store := privacyreservation.NewMemoryStore()
	reservations, graph := batchReconcileTestGraph(payload, now)
	_, err := store.CreateBatchOperation(context.Background(), reservations, graph)
	require.NoError(t, err)
	claimErr := errors.New("durable proving claim failed")
	failingStore := &failingBatchProvingClaimStore{BatchOperationStore: store, err: claimErr}
	worker := BatchProofWorker{
		Store: failingStore, Prover: &blockingBatchPayrollProver{}, Sealer: testPayrollCipher{},
		LeaseOwner: "proof-worker", LeaseTTL: time.Minute,
	}

	_, err = worker.Process(context.Background(), graph.Operation.OperationID, payload)
	require.ErrorIs(t, err, claimErr)
	stored, err := store.GetBatchOperation(context.Background(), graph.Operation.OperationID)
	require.NoError(t, err)
	require.Equal(t, privacyreservation.OperationStatusPlanned, stored.Operation.Status)
	require.Empty(t, stored.Operation.LeaseToken)
	takeover, err := store.AcquireBatchOperationLease(context.Background(), graph.Operation.OperationID, "retry-worker", "retry-token", now.Add(2*time.Minute), now.Add(time.Second))
	require.NoError(t, err, "claim failure must not block an immediate retry")
	_, err = store.ReleaseBatchOperationLease(context.Background(), graph.Operation.OperationID, takeover.LeaseToken, now.Add(time.Second))
	require.NoError(t, err)
}

func TestBatchProofWorkerRecoversExpiredProvingLeaseAfterDurableRestart(t *testing.T) {
	payload := batchReconcileTestPayload(t)
	oldNow := time.Now().UTC().Add(-2 * time.Minute)
	path := filepath.Join(t.TempDir(), "batch-proof-restart.json")
	store, err := privacyreservation.OpenDurableFileStore(path)
	require.NoError(t, err)
	reservations, graph := batchReconcileTestGraph(payload, oldNow)
	_, err = store.CreateBatchOperation(context.Background(), reservations, graph)
	require.NoError(t, err)
	lease, err := store.AcquireBatchOperationLease(context.Background(), graph.Operation.OperationID, "crashed-worker", "expired-token", oldNow.Add(30*time.Second), oldNow)
	require.NoError(t, err)
	_, err = store.CompareAndSetBatchOperationStatus(context.Background(), graph.Operation.OperationID, lease.LeaseToken, privacyreservation.OperationStatusPlanned, privacyreservation.OperationStatusProving, oldNow)
	require.NoError(t, err)

	reopened, err := privacyreservation.OpenDurableFileStore(path)
	require.NoError(t, err)
	release := make(chan struct{})
	close(release)
	prover := &blockingBatchPayrollProver{started: make(chan struct{}), release: release, proof: testPreparedBatchProof(payload)}
	worker := BatchProofWorker{Store: reopened, Prover: prover, Sealer: testPayrollCipher{}, LeaseOwner: "restart-worker", LeaseTTL: time.Minute}
	_, err = worker.Process(context.Background(), graph.Operation.OperationID, payload)
	require.NoError(t, err)
	finished, err := reopened.GetBatchOperation(context.Background(), graph.Operation.OperationID)
	require.NoError(t, err)
	require.Equal(t, privacyreservation.OperationStatusProofReady, finished.Operation.Status)
	require.NotEmpty(t, finished.Operation.ProofCiphertext)
	require.Empty(t, finished.Operation.LeaseToken)
}

func TestBuildBatchOperationGraphBindsRecipientAndDisclosurePlan(t *testing.T) {
	payload := batchReconcileTestPayload(t)
	now := time.Now().UTC()
	items := make([]PayrollPlanItem, len(payload.Outputs))
	for i, output := range payload.Outputs {
		address := shieldedAddressForBatchNote(t, output.Note)
		items[i] = PayrollPlanItem{
			CompanyID: "company", PayrollID: "payroll", BatchID: "batch", ItemID: "item-" + string(rune('a'+i)),
			EmployeeID: "employee", OperationID: "bound-operation", RecipientAddress: address,
			ExpectedRecipientHash: mustHashRecipient(t, address), Amount: new(big.Int).Set(output.Note.Amount),
			ExpectedAmountHash: mustHashAmount(t, "uclair", output.Note.Amount), Denom: "uclair",
			DisclosurePolicy: PayrollDisclosurePolicy{UserPrivacyPolicy: output.PrivacyPolicy, UserDisclosureMode: output.DisclosureMode, UserDisclosureTargetPubKeyHex: hex.EncodeToString(output.DisclosureTargetPubKey)},
		}
	}
	plan := BatchPayrollOperationPlan{
		OperationID: "bound-operation", Items: items,
		InputNotes: []TreasuryNote{{NoteID: "note-a", OwnerKeyID: "owner", NullifierLookupKey: "lookup", NullifierLookupKeyID: "lookup-v1", Denom: "uclair", Amount: new(big.Int).Set(payload.Inputs[0].Note.Amount)}},
		InputTotal: new(big.Int).Set(payload.Inputs[0].Note.Amount), PaymentTotal: new(big.Int).Set(payload.Inputs[0].Note.Amount),
		Change: new(big.Int), OutputCount: len(items), HasChange: false,
	}
	derivedLookupKey, err := testPayrollCipher{}.PayrollNullifierLookupKey(context.Background(), plan.InputNotes[0].NullifierLookupKeyID, payload.Inputs[0].Nullifier)
	require.NoError(t, err)
	plan.InputNotes[0].NullifierLookupKey = derivedLookupKey
	reservations, graph, err := BuildBatchOperationGraph(context.Background(), plan, payload, testPayrollCipher{}, now)
	require.NoError(t, err)
	require.Len(t, reservations, 1)
	require.Len(t, graph.Items, len(items))
	require.Equal(t, payload.Outputs[0].PrivacyPolicy, graph.Evidence[0].UserPrivacyPolicy)
	_, _, err = buildBatchOperationGraph(context.Background(), plan, payload, testPayrollCipher{}, time.Time{}, func() time.Time {
		return time.Unix(payload.ExpiresAtUnix+1, 0)
	})
	require.ErrorContains(t, err, "expired")
	plan.InputNotes[0].NullifierLookupKey = strings.Repeat("00", 32)
	_, _, err = BuildBatchOperationGraph(context.Background(), plan, payload, testPayrollCipher{}, now)
	require.ErrorContains(t, err, "nullifier does not match")
	plan.InputNotes[0].NullifierLookupKey = derivedLookupKey

	wrong := batchReconcileTestPoint(77)
	wrongAddress, err := privacytypes.EncodeShieldedAddressWithView(wrong, wrong)
	require.NoError(t, err)
	plan.Items[0].RecipientAddress = wrongAddress
	plan.Items[0].ExpectedRecipientHash = mustHashRecipient(t, wrongAddress)
	_, _, err = BuildBatchOperationGraph(context.Background(), plan, payload, testPayrollCipher{}, now)
	require.ErrorContains(t, err, "recipient does not match")
}

func testPreparedBatchProof(payload *privacybatchtransfer.PreparedBatchTransferPayload) *privacybatchtransfer.PreparedBatchTransferProof {
	return &privacybatchtransfer.PreparedBatchTransferProof{
		Version: privacybatchtransfer.PreparedBatchTransferProofVersion, RequestPayloadHash: payload.PayloadHash,
		CircuitSetID: payload.CircuitSetID, Proof: make([]byte, privacytypes.BatchTransferProofSizeV1),
	}
}

func testProofReadyBatchStore(t *testing.T, payload *privacybatchtransfer.PreparedBatchTransferPayload) (*privacyreservation.MemoryStore, privacyreservation.BatchOperationGraph) {
	t.Helper()
	now := time.Now().UTC()
	store := privacyreservation.NewMemoryStore()
	reservations, graph := batchReconcileTestGraph(payload, now)
	_, err := store.CreateBatchOperation(context.Background(), reservations, graph)
	require.NoError(t, err)
	lease, err := store.AcquireBatchOperationLease(context.Background(), graph.Operation.OperationID, "proof-worker", "proof-token", now.Add(time.Minute), now)
	require.NoError(t, err)
	_, err = store.CompareAndSetBatchOperationStatus(context.Background(), graph.Operation.OperationID, lease.LeaseToken, privacyreservation.OperationStatusPlanned, privacyreservation.OperationStatusProving, now)
	require.NoError(t, err)
	proofDigest := sha256.Sum256(testPreparedBatchProof(payload).Proof)
	_, err = store.SaveBatchProofArtifacts(context.Background(), graph.Operation.OperationID, lease.LeaseToken, privacyreservation.BatchProofArtifactUpdate{ProofCiphertext: []byte("sealed-proof"), ProofHash: hex.EncodeToString(proofDigest[:])}, now)
	require.NoError(t, err)
	return store, graph
}

type testPayrollCipher struct{}

func (testPayrollCipher) SealPayrollEvidence(_ context.Context, plaintext []byte) ([]byte, error) {
	return append([]byte("sealed:"), plaintext...), nil
}

func (testPayrollCipher) OpenPayrollEvidence(_ context.Context, ciphertext []byte) ([]byte, error) {
	if !bytes.HasPrefix(ciphertext, []byte("sealed:")) {
		return nil, errors.New("invalid sealed payroll artifact")
	}
	return append([]byte(nil), ciphertext[len("sealed:"):]...), nil
}

func (testPayrollCipher) PayrollNullifierLookupKey(_ context.Context, keyID string, nullifier []byte) (string, error) {
	return privacyreservation.NullifierLookupKey([]byte("test-index-key:"+keyID), nullifier)
}

type testSignedBatchBuilder struct {
	bytes []byte
	calls int
}

func (b *testSignedBatchBuilder) BuildSignedBatchTx(_ context.Context, _ *privacytypes.MsgBatchTransfer) (*SignedBatchTx, error) {
	b.calls++
	value := append([]byte(nil), b.bytes...)
	digest := sha256.Sum256(value)
	hash := hex.EncodeToString(digest[:])
	return &SignedBatchTx{Bytes: value, TxBytesHash: hash, SignDocHash: "sign-doc", TxHash: hash, AccountSequence: uint64(b.calls)}, nil
}

type testSignedBatchSender struct {
	mu     sync.Mutex
	errors []error
	calls  int
	sent   [][]byte
}

type blockingSignedBatchSender struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
	calls   int
}

func (s *blockingSignedBatchSender) BroadcastSignedBatchTx(_ context.Context, signed []byte) (*BatchBroadcastReceipt, error) {
	s.once.Do(func() { close(s.started) })
	<-s.release
	s.calls++
	digest := sha256.Sum256(signed)
	return &BatchBroadcastReceipt{TxHash: hex.EncodeToString(digest[:])}, nil
}

func (s *testSignedBatchSender) BroadcastSignedBatchTx(_ context.Context, signed []byte) (*BatchBroadcastReceipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.sent = append(s.sent, append([]byte(nil), signed...))
	var err error
	if s.calls <= len(s.errors) {
		err = s.errors[s.calls-1]
	}
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(signed)
	return &BatchBroadcastReceipt{TxHash: hex.EncodeToString(digest[:])}, nil
}

type testBatchBroadcastChain struct {
	lookup       *BatchTxLookupResult
	statuses     map[string]bool
	checks       int
	spendOnCheck int
}

func (c *testBatchBroadcastChain) LookupBatchTx(_ context.Context, _ string) (*BatchTxLookupResult, error) {
	if c.lookup == nil {
		return &BatchTxLookupResult{Found: false}, nil
	}
	copy := *c.lookup
	return &copy, nil
}

func (c *testBatchBroadcastChain) CheckBatchNullifiers(_ context.Context, nullifiers []string) (map[string]bool, error) {
	c.checks++
	out := make(map[string]bool, len(nullifiers))
	for _, nullifier := range nullifiers {
		out[nullifier] = c.statuses[nullifier]
		if c.spendOnCheck > 0 && c.checks >= c.spendOnCheck {
			out[nullifier] = true
		}
	}
	return out, nil
}

func unspentBatchStatuses(payload *privacybatchtransfer.PreparedBatchTransferPayload) map[string]bool {
	statuses := make(map[string]bool, len(payload.Inputs))
	for _, input := range payload.Inputs {
		statuses[hex.EncodeToString(input.Nullifier)] = false
	}
	return statuses
}

type blockingBatchPayrollProver struct {
	started chan struct{}
	release chan struct{}
	proof   *privacybatchtransfer.PreparedBatchTransferProof
	once    sync.Once
}

type failingBatchProvingClaimStore struct {
	privacyreservation.BatchOperationStore
	err error
}

func (s *failingBatchProvingClaimStore) CompareAndSetBatchOperationStatus(context.Context, string, string, privacyreservation.OperationStatus, privacyreservation.OperationStatus, time.Time) (*privacyreservation.BatchOperation, error) {
	return nil, s.err
}

func shieldedAddressForBatchNote(t *testing.T, note privacytypes.Note) string {
	t.Helper()
	spend := &crypto_tedwards.PointAffine{}
	view := &crypto_tedwards.PointAffine{}
	spend.X.SetBigInt(note.ReceiverSpendPubKeyX)
	spend.Y.SetBigInt(note.ReceiverSpendPubKeyY)
	view.X.SetBigInt(note.ReceiverViewPubKeyX)
	view.Y.SetBigInt(note.ReceiverViewPubKeyY)
	address, err := privacytypes.EncodeShieldedAddressWithView(spend, view)
	require.NoError(t, err)
	return address
}

func (p *blockingBatchPayrollProver) ProveBatchPayroll(_ context.Context, _ *privacybatchtransfer.PreparedBatchTransferPayload, _ time.Time) (*privacybatchtransfer.PreparedBatchTransferProof, error) {
	p.once.Do(func() { close(p.started) })
	<-p.release
	copy := *p.proof
	copy.Proof = append([]byte(nil), p.proof.Proof...)
	return &copy, nil
}
