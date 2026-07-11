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
			ExpectedRecipientHash: HashRecipient(address), Amount: new(big.Int).Set(output.Note.Amount),
			ExpectedAmountHash: HashAmount("uclair", output.Note.Amount), Denom: "uclair",
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
	plan.InputNotes[0].NullifierLookupKey = strings.Repeat("00", 32)
	_, _, err = BuildBatchOperationGraph(context.Background(), plan, payload, testPayrollCipher{}, now)
	require.ErrorContains(t, err, "nullifier does not match")
	plan.InputNotes[0].NullifierLookupKey = derivedLookupKey

	wrong := batchReconcileTestPoint(77)
	wrongAddress, err := privacytypes.EncodeShieldedAddressWithView(wrong, wrong)
	require.NoError(t, err)
	plan.Items[0].RecipientAddress = wrongAddress
	plan.Items[0].ExpectedRecipientHash = HashRecipient(wrongAddress)
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
	_, err = store.SaveBatchProofArtifacts(context.Background(), graph.Operation.OperationID, lease.LeaseToken, privacyreservation.BatchProofArtifactUpdate{ProofCiphertext: []byte("sealed-proof"), ProofHash: "proof-hash"}, now)
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
