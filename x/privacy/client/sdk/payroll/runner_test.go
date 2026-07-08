package payroll

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	privacyreservation "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/reservation"
	privacytransfer "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/transfer"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestProofAndBroadcastWorkersAdvanceReservationLifecycle(t *testing.T) {
	ctx := context.Background()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: testNow}
	planner := Service{Reservation: reservationService, Now: testNow}

	input := testPayrollInput()
	input.Items[0].Amount = big.NewInt(70)
	plan, err := planner.CreatePlan(ctx, input, []TreasuryNote{
		testTreasuryNote("large", "uclair", 100, false, ""),
		testTreasuryNote("zero", "uclair", 0, false, ""),
	})
	require.NoError(t, err)
	confirmed, err := planner.ConfirmPlan(ctx, *plan)
	require.NoError(t, err)
	item := confirmed.Items[0]

	proofStore := NewMemoryProofResultStore()
	proofWorker := ProofWorker{
		Reservation:     reservationService,
		PayloadBuilder:  fakePayloadBuilder{},
		ProofRunner:     fakeProofRunner{},
		Assembler:       fakeAssembler{},
		ProofResultSink: proofStore,
		LeaseOwner:      "proof-worker-a",
		LeaseTTL:        time.Minute,
	}
	proofResult, err := proofWorker.Process(ctx, item)
	require.NoError(t, err)
	require.NotNil(t, proofResult.Message)
	storedProofResult, err := proofStore.GetProofResult(ctx, item.OperationID)
	require.NoError(t, err)
	require.NotNil(t, storedProofResult.Message)
	require.Equal(t, proofResult.Proof.PayloadHash, storedProofResult.Proof.PayloadHash)

	for _, note := range item.InputNotes {
		reservation, err := store.GetReservation(ctx, note.ReservationID)
		require.NoError(t, err)
		require.Equal(t, privacyreservation.StatusProofReady, reservation.Status)
		require.NotEmpty(t, reservation.LeaseToken)
	}
	operation, err := store.GetOperation(ctx, item.OperationID)
	require.NoError(t, err)
	require.Equal(t, privacyreservation.OperationStatusProofReady, operation.Status)
	require.Equal(t, "commitment-a", operation.ExpectedOutputCommitment)
	require.Equal(t, "audit-digest-a", operation.ExpectedDisclosureDigest)

	broadcastWorker := BroadcastWorker{
		Reservation: reservationService,
		Broadcaster: fakeBroadcaster{},
	}
	broadcastResult, err := broadcastWorker.SubmitProofResult(ctx, *proofResult)
	require.NoError(t, err)
	require.Equal(t, "TXHASH", broadcastResult.TxHash)

	for _, note := range item.InputNotes {
		reservation, err := store.GetReservation(ctx, note.ReservationID)
		require.NoError(t, err)
		require.Equal(t, privacyreservation.StatusSubmitted, reservation.Status)
		require.Equal(t, "TXHASH", reservation.TxHash)
	}
	operation, err = store.GetOperation(ctx, item.OperationID)
	require.NoError(t, err)
	require.Equal(t, privacyreservation.OperationStatusSubmitted, operation.Status)
	require.Equal(t, "TXHASH", operation.TxHash)
}

func TestProofWorkerStoresAuditDisclosureDigestWhenMultiplePlanesExist(t *testing.T) {
	ctx := context.Background()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: testNow}
	planner := Service{Reservation: reservationService, Now: testNow}

	input := testPayrollInput()
	input.Items[0].Amount = big.NewInt(70)
	plan, err := planner.CreatePlan(ctx, input, []TreasuryNote{
		testTreasuryNote("large", "uclair", 100, false, ""),
		testTreasuryNote("zero", "uclair", 0, false, ""),
	})
	require.NoError(t, err)
	confirmed, err := planner.ConfirmPlan(ctx, *plan)
	require.NoError(t, err)
	item := confirmed.Items[0]

	proofWorker := ProofWorker{
		Reservation: reservationService,
		PayloadBuilder: multiPlanePayloadBuilder{
			payload: privacytransfer.PreparedTransferPayload{
				Version:                     privacytransfer.PreparedTransferPayloadVersion,
				PayloadHash:                 "payload-hash-a",
				UserDisclosureDigestHex:     "user-digest-a",
				AuditDisclosureDigestHex:    "audit-digest-a",
				SelfViewDisclosureDigestHex: "self-view-digest-a",
				Inputs: []privacytransfer.PreparedTransferInput{
					{NullifierHex: "nullifier-a"},
					{NullifierHex: "nullifier-b"},
				},
				Outputs: []privacytransfer.PreparedTransferOutput{
					{CommitmentHex: "commitment-a"},
					{CommitmentHex: "change-a"},
				},
			},
		},
		ProofRunner:     fakeProofRunner{},
		Assembler:       fakeAssembler{},
		ProofResultSink: NewMemoryProofResultStore(),
		LeaseOwner:      "proof-worker-a",
		LeaseTTL:        time.Minute,
	}
	_, err = proofWorker.Process(ctx, item)
	require.NoError(t, err)

	operation, err := store.GetOperation(ctx, item.OperationID)
	require.NoError(t, err)
	require.Equal(t, privacyreservation.OperationStatusProofReady, operation.Status)
	require.Equal(t, "audit-digest-a", operation.ExpectedDisclosureDigest)
	require.Equal(t, "user-digest-a", operation.ExpectedUserDisclosureDigest)
	require.Equal(t, "audit-digest-a", operation.ExpectedAuditDisclosureDigest)
	require.Equal(t, "self-view-digest-a", operation.ExpectedSelfViewDisclosureDigest)
}

func TestBroadcastWorkerRecordsUnknownAttemptOnNonzeroCode(t *testing.T) {
	ctx := context.Background()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: testNow}
	chunk := prepareTestMessageChunk(t, ctx, reservationService)
	result := chunk.Results[0]

	broadcastWorker := BroadcastWorker{
		Reservation: reservationService,
		Broadcaster: codeErrorBroadcaster{},
	}
	_, err := broadcastWorker.SubmitProofResult(ctx, result)
	require.ErrorContains(t, err, "tx failed with code 17")

	operation, err := store.GetOperation(ctx, result.Item.OperationID)
	require.NoError(t, err)
	require.Equal(t, privacyreservation.OperationStatusUnknown, operation.Status)
	require.Equal(t, "TXHASH", operation.TxHash)
	for _, note := range result.Item.InputNotes {
		reservation, err := store.GetReservation(ctx, note.ReservationID)
		require.NoError(t, err)
		require.Equal(t, privacyreservation.StatusUnknown, reservation.Status)
		require.Equal(t, "tx failed with code 17: out of gas", reservation.LastBroadcastError)
	}
}

func TestBroadcastWorkerKeepsProofReadyLeaseOnBroadcastErrorWithoutMetadata(t *testing.T) {
	ctx := context.Background()
	now := testNow()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: func() time.Time { return now }}
	planner := Service{Reservation: reservationService, Now: func() time.Time { return now }}

	input := testPayrollInput()
	input.Items[0].Amount = big.NewInt(70)
	plan, err := planner.CreatePlan(ctx, input, []TreasuryNote{
		testTreasuryNote("large", "uclair", 100, false, ""),
		testTreasuryNote("zero", "uclair", 0, false, ""),
	})
	require.NoError(t, err)
	confirmed, err := planner.ConfirmPlan(ctx, *plan)
	require.NoError(t, err)
	item := confirmed.Items[0]

	proofWorker := ProofWorker{
		Reservation:     reservationService,
		PayloadBuilder:  fakePayloadBuilder{},
		ProofRunner:     fakeProofRunner{},
		Assembler:       fakeAssembler{},
		ProofResultSink: NewMemoryProofResultStore(),
		LeaseOwner:      "proof-worker-a",
		LeaseTTL:        time.Minute,
	}
	result, err := proofWorker.Process(ctx, item)
	require.NoError(t, err)

	now = now.Add(2 * time.Minute)
	broadcastWorker := BroadcastWorker{
		Reservation: reservationService,
		Broadcaster: noMetadataErrorBroadcaster{err: errors.New("rpc connection reset")},
		LeaseOwner:  "broadcast-worker-a",
		LeaseTTL:    time.Minute,
	}
	_, err = broadcastWorker.SubmitProofResult(ctx, *result)
	require.ErrorContains(t, err, "rpc connection reset")

	operation, err := store.GetOperation(ctx, item.OperationID)
	require.NoError(t, err)
	require.Equal(t, privacyreservation.OperationStatusProofReady, operation.Status)
	require.Empty(t, operation.TxHash)
	require.Empty(t, operation.TxBytesHash)
	for _, note := range item.InputNotes {
		reservation, err := store.GetReservation(ctx, note.ReservationID)
		require.NoError(t, err)
		require.Equal(t, privacyreservation.StatusProofReady, reservation.Status)
		require.Equal(t, "broadcast-worker-a", reservation.LeaseOwner)
		require.NotEqual(t, result.ReservationLeases[note.ReservationID], reservation.LeaseToken)
		require.Empty(t, reservation.TxHash)
		require.Empty(t, reservation.TxBytesHash)
		require.Empty(t, reservation.LastBroadcastError)
		require.Equal(t, 0, reservation.BroadcastAttemptCount)
	}
}

func TestProofWorkerRollsBackProvingReservationsOnPayloadFailure(t *testing.T) {
	ctx := context.Background()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: testNow}
	planner := Service{Reservation: reservationService, Now: testNow}

	input := testPayrollInput()
	input.Items[0].Amount = big.NewInt(70)
	plan, err := planner.CreatePlan(ctx, input, []TreasuryNote{
		testTreasuryNote("large", "uclair", 100, false, ""),
		testTreasuryNote("zero", "uclair", 0, false, ""),
	})
	require.NoError(t, err)
	confirmed, err := planner.ConfirmPlan(ctx, *plan)
	require.NoError(t, err)
	item := confirmed.Items[0]

	proofWorker := ProofWorker{
		Reservation:     reservationService,
		PayloadBuilder:  failingPayloadBuilder{},
		ProofRunner:     fakeProofRunner{},
		ProofResultSink: NewMemoryProofResultStore(),
		LeaseOwner:      "proof-worker-a",
		LeaseTTL:        time.Minute,
	}
	_, err = proofWorker.Process(ctx, item)
	require.ErrorContains(t, err, "payload failed")

	for _, note := range item.InputNotes {
		reservation, err := store.GetReservation(ctx, note.ReservationID)
		require.NoError(t, err)
		require.Equal(t, privacyreservation.StatusReserved, reservation.Status)
		require.Empty(t, reservation.LeaseToken)
	}
}

func TestProofWorkerDoesNotRollbackExpiredProvingLeaseWithoutToken(t *testing.T) {
	ctx := context.Background()
	now := testNow()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: func() time.Time { return now }}
	planner := Service{Reservation: reservationService, Now: func() time.Time { return now }}

	input := testPayrollInput()
	input.Items[0].Amount = big.NewInt(70)
	plan, err := planner.CreatePlan(ctx, input, []TreasuryNote{
		testTreasuryNote("large", "uclair", 100, false, ""),
		testTreasuryNote("zero", "uclair", 0, false, ""),
	})
	require.NoError(t, err)
	confirmed, err := planner.ConfirmPlan(ctx, *plan)
	require.NoError(t, err)
	item := confirmed.Items[0]

	proofWorker := ProofWorker{
		Reservation: reservationService,
		PayloadBuilder: expiringPayloadBuilder{advance: func() {
			now = now.Add(2 * time.Minute)
		}},
		ProofRunner:     fakeProofRunner{},
		ProofResultSink: NewMemoryProofResultStore(),
		LeaseOwner:      "proof-worker-a",
		LeaseTTL:        time.Minute,
	}
	_, err = proofWorker.Process(ctx, item)
	require.ErrorContains(t, err, "payload failed after lease expiry")
	require.ErrorIs(t, err, privacyreservation.ErrLeaseUnavailable)

	for _, note := range item.InputNotes {
		reservation, err := store.GetReservation(ctx, note.ReservationID)
		require.NoError(t, err)
		require.Equal(t, privacyreservation.StatusProving, reservation.Status)
		require.Equal(t, "proof-worker-a", reservation.LeaseOwner)
	}
}

func TestProofWorkerHeartbeatsDuringLongProof(t *testing.T) {
	ctx := context.Background()
	store := privacyreservation.NewMemoryStore()
	now := func() time.Time { return time.Now().UTC() }
	reservationService := privacyreservation.Service{Store: store, Now: now}
	planner := Service{Reservation: reservationService, Now: now}

	input := testPayrollInput()
	input.Items[0].Amount = big.NewInt(70)
	plan, err := planner.CreatePlan(ctx, input, []TreasuryNote{
		testTreasuryNote("large", "uclair", 100, false, ""),
		testTreasuryNote("zero", "uclair", 0, false, ""),
	})
	require.NoError(t, err)
	confirmed, err := planner.ConfirmPlan(ctx, *plan)
	require.NoError(t, err)
	item := confirmed.Items[0]

	proofWorker := ProofWorker{
		Reservation:     reservationService,
		PayloadBuilder:  fakePayloadBuilder{},
		ProofRunner:     slowProofRunner{},
		ProofResultSink: NewMemoryProofResultStore(),
		Assembler:       fakeAssembler{},
		LeaseOwner:      "proof-worker-a",
		LeaseTTL:        30 * time.Millisecond,
	}
	_, err = proofWorker.Process(ctx, item)
	require.NoError(t, err)

	for _, note := range item.InputNotes {
		reservation, err := store.GetReservation(ctx, note.ReservationID)
		require.NoError(t, err)
		require.Equal(t, privacyreservation.StatusProofReady, reservation.Status)
		require.True(t, reservation.LeaseUntil.After(time.Now().UTC()), "expected heartbeat to extend lease")
	}
}

func TestProofWorkerDoesNotPersistProofResultAfterHeartbeatFailure(t *testing.T) {
	ctx := context.Background()
	store := &heartbeatFailingReservationStore{
		MemoryStore:   privacyreservation.NewMemoryStore(),
		heartbeatSeen: make(chan struct{}),
	}
	now := func() time.Time { return time.Now().UTC() }
	reservationService := privacyreservation.Service{Store: store, Now: now}
	planner := Service{Reservation: reservationService, Now: now}

	input := testPayrollInput()
	input.Items[0].Amount = big.NewInt(70)
	plan, err := planner.CreatePlan(ctx, input, []TreasuryNote{
		testTreasuryNote("large", "uclair", 100, false, ""),
		testTreasuryNote("zero", "uclair", 0, false, ""),
	})
	require.NoError(t, err)
	confirmed, err := planner.ConfirmPlan(ctx, *plan)
	require.NoError(t, err)
	item := confirmed.Items[0]
	proofStore := NewMemoryProofResultStore()

	proofWorker := ProofWorker{
		Reservation:     reservationService,
		PayloadBuilder:  fakePayloadBuilder{},
		ProofRunner:     heartbeatAwareProofRunner{heartbeatSeen: store.heartbeatSeen},
		ProofResultSink: proofStore,
		Assembler:       fakeAssembler{},
		LeaseOwner:      "proof-worker-a",
		LeaseTTL:        200 * time.Millisecond,
	}
	_, err = proofWorker.Process(ctx, item)
	require.ErrorIs(t, err, context.Canceled)

	_, err = proofStore.GetProofResult(ctx, item.OperationID)
	require.ErrorContains(t, err, "not found")
	for _, note := range item.InputNotes {
		reservation, err := store.GetReservation(ctx, note.ReservationID)
		require.NoError(t, err)
		require.Equal(t, privacyreservation.StatusReserved, reservation.Status)
		require.Empty(t, reservation.LeaseToken)
	}
}

func TestProofWorkerDoesNotTakeOverProofReadyLease(t *testing.T) {
	ctx := context.Background()
	now := testNow()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: func() time.Time { return now }}
	planner := Service{Reservation: reservationService, Now: func() time.Time { return now }}

	input := testPayrollInput()
	input.Items[0].Amount = big.NewInt(70)
	plan, err := planner.CreatePlan(ctx, input, []TreasuryNote{
		testTreasuryNote("large", "uclair", 100, false, ""),
		testTreasuryNote("zero", "uclair", 0, false, ""),
	})
	require.NoError(t, err)
	confirmed, err := planner.ConfirmPlan(ctx, *plan)
	require.NoError(t, err)
	item := confirmed.Items[0]

	proofWorker := ProofWorker{
		Reservation:     reservationService,
		PayloadBuilder:  fakePayloadBuilder{},
		ProofRunner:     fakeProofRunner{},
		Assembler:       fakeAssembler{},
		ProofResultSink: NewMemoryProofResultStore(),
		LeaseOwner:      "proof-worker-a",
		LeaseTTL:        time.Minute,
	}
	result, err := proofWorker.Process(ctx, item)
	require.NoError(t, err)
	firstToken := result.ReservationLeases[item.InputNotes[0].ReservationID]

	now = now.Add(2 * time.Minute)
	_, err = proofWorker.Process(ctx, item)
	require.ErrorIs(t, err, privacyreservation.ErrCompareAndSetFailed)

	reservation, err := store.GetReservation(ctx, item.InputNotes[0].ReservationID)
	require.NoError(t, err)
	require.Equal(t, privacyreservation.StatusProofReady, reservation.Status)
	require.Equal(t, firstToken, reservation.LeaseToken)
}

func TestClassifyBroadcastError(t *testing.T) {
	require.Equal(t, RetryActionReconcileUnknown, ClassifyBroadcastError("rpc timeout").Action)
	require.Equal(t, RetryActionRebuildTx, ClassifyBroadcastError("account sequence mismatch").Action)
	require.Equal(t, RetryActionReplan, ClassifyBroadcastError("invalid proof").Action)
	require.Equal(t, RetryActionMarkConflictSpent, ClassifyBroadcastError("nullifier already spent").Action)
	require.Equal(t, RetryActionManualReview, ClassifyBroadcastError("something else").Action)
}

type fakePayloadBuilder struct{}

func (fakePayloadBuilder) BuildPreparedTransferPayload(_ context.Context, _ PayrollPlanItem) (*privacytransfer.PreparedTransferPayload, error) {
	return &privacytransfer.PreparedTransferPayload{
		Version:                  privacytransfer.PreparedTransferPayloadVersion,
		PayloadHash:              "payload-hash-a",
		AuditDisclosureDigestHex: "audit-digest-a",
		Inputs: []privacytransfer.PreparedTransferInput{
			{NullifierHex: "nullifier-a"},
			{NullifierHex: "nullifier-b"},
		},
		Outputs: []privacytransfer.PreparedTransferOutput{
			{CommitmentHex: "commitment-a"},
			{CommitmentHex: "change-a"},
		},
	}, nil
}

type multiPlanePayloadBuilder struct {
	payload privacytransfer.PreparedTransferPayload
}

func (b multiPlanePayloadBuilder) BuildPreparedTransferPayload(_ context.Context, _ PayrollPlanItem) (*privacytransfer.PreparedTransferPayload, error) {
	payload := b.payload
	payload.Inputs = append([]privacytransfer.PreparedTransferInput(nil), b.payload.Inputs...)
	payload.Outputs = append([]privacytransfer.PreparedTransferOutput(nil), b.payload.Outputs...)
	return &payload, nil
}

type failingPayloadBuilder struct{}

func (failingPayloadBuilder) BuildPreparedTransferPayload(_ context.Context, _ PayrollPlanItem) (*privacytransfer.PreparedTransferPayload, error) {
	return nil, fmt.Errorf("payload failed")
}

type expiringPayloadBuilder struct {
	advance func()
}

func (b expiringPayloadBuilder) BuildPreparedTransferPayload(_ context.Context, _ PayrollPlanItem) (*privacytransfer.PreparedTransferPayload, error) {
	if b.advance != nil {
		b.advance()
	}
	return nil, fmt.Errorf("payload failed after lease expiry")
}

type fakeProofRunner struct{}

func (fakeProofRunner) BuildPreparedTransferProof(_ context.Context, payload privacytransfer.PreparedTransferPayload) (*privacytransfer.PreparedTransferProof, error) {
	return &privacytransfer.PreparedTransferProof{
		Version:     privacytransfer.PreparedTransferProofVersion,
		PayloadHash: payload.PayloadHash,
		ProofHex:    "proof-a",
	}, nil
}

type slowProofRunner struct{}

func (r slowProofRunner) BuildPreparedTransferProof(ctx context.Context, payload privacytransfer.PreparedTransferPayload) (*privacytransfer.PreparedTransferProof, error) {
	for i := 0; i < 6; i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	return &privacytransfer.PreparedTransferProof{
		Version:     privacytransfer.PreparedTransferProofVersion,
		PayloadHash: payload.PayloadHash,
		ProofHex:    "proof-a",
	}, nil
}

type heartbeatAwareProofRunner struct {
	heartbeatSeen <-chan struct{}
}

func (r heartbeatAwareProofRunner) BuildPreparedTransferProof(_ context.Context, payload privacytransfer.PreparedTransferPayload) (*privacytransfer.PreparedTransferProof, error) {
	select {
	case <-r.heartbeatSeen:
	case <-time.After(time.Second):
		return nil, fmt.Errorf("heartbeat was not observed")
	}
	time.Sleep(20 * time.Millisecond)
	return &privacytransfer.PreparedTransferProof{
		Version:     privacytransfer.PreparedTransferProofVersion,
		PayloadHash: payload.PayloadHash,
		ProofHex:    "proof-a",
	}, nil
}

type heartbeatFailingReservationStore struct {
	*privacyreservation.MemoryStore
	heartbeatSeen chan struct{}
	closeOnce     sync.Once
}

func (s *heartbeatFailingReservationStore) HeartbeatReservationLeaseForStatus(_ context.Context, _ string, _ string, _ privacyreservation.ReservationStatus, _ time.Time, _ time.Time) (*privacyreservation.NoteReservation, error) {
	s.closeOnce.Do(func() {
		close(s.heartbeatSeen)
	})
	return nil, privacyreservation.ErrLeaseUnavailable
}

type fakeAssembler struct{}

func (fakeAssembler) BuildTransferMessage(_ privacytransfer.PreparedTransferPayload, _ privacytransfer.PreparedTransferProof) (*privacytypes.MsgTransfer, error) {
	return &privacytypes.MsgTransfer{
		Nullifiers: [][]byte{[]byte("nullifier-a"), []byte("nullifier-b")},
	}, nil
}

type fakeBroadcaster struct{}

func (fakeBroadcaster) BroadcastMessages(_ context.Context, msgs ...sdk.Msg) (*BroadcastResult, error) {
	if len(msgs) != 1 {
		return nil, nil
	}
	return &BroadcastResult{
		TxHash:          "TXHASH",
		TxBytesHash:     "tx-bytes-hash",
		SignDocHash:     "sign-doc-hash",
		AccountSequence: 7,
		Height:          11,
	}, nil
}

func (fakeBroadcaster) CheckNullifiersUsed(_ context.Context, nullifierHexes []string) (map[string]bool, error) {
	return allNullifiersUnspent(nullifierHexes), nil
}
