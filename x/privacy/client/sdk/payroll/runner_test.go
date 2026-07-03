package payroll

import (
	"context"
	"math/big"
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

	proofWorker := ProofWorker{
		Reservation:    reservationService,
		PayloadBuilder: fakePayloadBuilder{},
		ProofRunner:    fakeProofRunner{},
		Assembler:      fakeAssembler{},
		LeaseOwner:     "proof-worker-a",
		LeaseTTL:       time.Minute,
	}
	proofResult, err := proofWorker.Process(ctx, item)
	require.NoError(t, err)
	require.NotNil(t, proofResult.Message)

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

func TestClassifyBroadcastError(t *testing.T) {
	require.Equal(t, RetryActionRetrySameTx, ClassifyBroadcastError("rpc timeout").Action)
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

type fakeProofRunner struct{}

func (fakeProofRunner) BuildPreparedTransferProof(_ context.Context, payload privacytransfer.PreparedTransferPayload) (*privacytransfer.PreparedTransferProof, error) {
	return &privacytransfer.PreparedTransferProof{
		Version:     privacytransfer.PreparedTransferProofVersion,
		PayloadHash: payload.PayloadHash,
		ProofHex:    "proof-a",
	}, nil
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
