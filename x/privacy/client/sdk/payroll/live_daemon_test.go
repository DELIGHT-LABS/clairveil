package payroll

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	privacyreservation "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/reservation"
)

func TestLiveDaemonRunsInjectedProofBroadcastAndScan(t *testing.T) {
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

	report, err := (LiveDaemon{
		Reservation: reservationService,
		Executor:    testLiveExecutor{item: confirmed.Items[0]},
		LeaseOwner:  "live-worker-a",
		LeaseTTL:    time.Minute,
		Now:         testNow,
	}).RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, "live", report.Mode)
	require.Equal(t, 1, report.ProofReady)
	require.Equal(t, 1, report.Submitted)
	require.Equal(t, 2, report.Reconciled)
	require.Equal(t, 0, report.RequiresReview)

	operation, err := store.GetOperation(ctx, confirmed.Items[0].OperationID)
	require.NoError(t, err)
	require.Equal(t, privacyreservation.OperationStatusSucceeded, operation.Status)
	require.Equal(t, "commitment-a", operation.ExpectedOutputCommitment)
	for _, note := range confirmed.Items[0].InputNotes {
		reservation, err := store.GetReservation(ctx, note.ReservationID)
		require.NoError(t, err)
		require.Equal(t, privacyreservation.StatusConfirmedSpent, reservation.Status)
		require.Equal(t, "TXHASH", reservation.TxHash)
	}
}

type testLiveExecutor struct {
	item PayrollPlanItem
}

func (e testLiveExecutor) BuildProofReady(context.Context, LiveOperationGroup) (privacyreservation.ProofReadyOperationUpdate, string, error) {
	return privacyreservation.ProofReadyOperationUpdate{
		ExpectedOutputCommitment:      "commitment-a",
		ExpectedDisclosureDigest:      "digest-a",
		ExpectedAuditDisclosureDigest: "digest-a",
	}, "test proof", nil
}

func (e testLiveExecutor) BroadcastProofReady(context.Context, LiveOperationGroup) (*BroadcastResult, string, error) {
	return &BroadcastResult{TxHash: "TXHASH", TxBytesHash: "tx-bytes", SignDocHash: "sign-doc", AccountSequence: 3}, "test broadcast", nil
}

func (e testLiveExecutor) ScanSubmitted(_ context.Context, group LiveOperationGroup) (map[string]privacyreservation.OperationEvidence, string, error) {
	out := make(map[string]privacyreservation.OperationEvidence, len(group.Reservations))
	for _, reservation := range group.Reservations {
		out[reservation.ReservationID] = privacyreservation.OperationEvidence{
			TxHash:                "TXHASH",
			TxBytesHash:           "tx-bytes",
			SignDocHash:           "sign-doc",
			OutputCommitment:      "commitment-a",
			DisclosureDigest:      "digest-a",
			AuditDisclosureDigest: "digest-a",
			RecipientHash:         e.item.ExpectedRecipientHash,
			AmountHash:            e.item.ExpectedAmountHash,
			Denom:                 e.item.Denom,
			BatchItemIndex:        0,
			BatchItemIndexKnown:   true,
			NullifierSpent:        true,
			TxSucceeded:           true,
			TxKnown:               true,
		}
	}
	return out, "test scan", nil
}
