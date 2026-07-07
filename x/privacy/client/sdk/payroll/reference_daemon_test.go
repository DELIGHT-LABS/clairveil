package payroll

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	privacyreservation "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/reservation"
)

func TestReferenceDaemonRunOnceCompletesSimulatedPayrollOperation(t *testing.T) {
	ctx := context.Background()
	store := privacyreservation.NewMemoryStore()
	svc := Service{
		Reservation: privacyreservation.Service{Store: store, Now: testNow},
		Now:         testNow,
	}
	input := testPayrollInput()
	input.Items[0].Amount = big.NewInt(70)
	plan, err := svc.CreatePlan(ctx, input, []TreasuryNote{
		testTreasuryNote("large", "uclair", 100, false, ""),
		testTreasuryNote("zero", "uclair", 0, false, ""),
	})
	require.NoError(t, err)
	confirmed, err := svc.ConfirmPlan(ctx, *plan)
	require.NoError(t, err)

	daemon := ReferenceDaemon{
		Reservation: privacyreservation.Service{Store: store, Now: testNow},
		LeaseOwner:  "test-payrolld",
		LeaseTTL:    time.Minute,
		Now:         testNow,
	}
	report, err := daemon.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, report.ProofReady)
	require.Equal(t, 1, report.Submitted)
	require.Equal(t, 2, report.Reconciled)
	require.Equal(t, 0, report.RequiresReview)

	for _, note := range confirmed.Items[0].InputNotes {
		reservation, err := store.GetReservation(ctx, note.ReservationID)
		require.NoError(t, err)
		require.Equal(t, privacyreservation.StatusConfirmedSpent, reservation.Status)
		require.NotEmpty(t, reservation.TxHash)
	}
	operation, err := store.GetOperation(ctx, confirmed.Items[0].OperationID)
	require.NoError(t, err)
	require.Equal(t, privacyreservation.OperationStatusSucceeded, operation.Status)
	require.NotEmpty(t, operation.ExpectedOutputCommitment)
	require.NotEmpty(t, operation.ExpectedDisclosureDigest)
}

func TestReferenceDaemonRunOnceIsIdleAfterTerminalState(t *testing.T) {
	ctx := context.Background()
	store := privacyreservation.NewMemoryStore()
	svc := Service{
		Reservation: privacyreservation.Service{Store: store, Now: testNow},
		Now:         testNow,
	}
	input := testPayrollInput()
	input.Items[0].Amount = big.NewInt(70)
	plan, err := svc.CreatePlan(ctx, input, []TreasuryNote{
		testTreasuryNote("large", "uclair", 100, false, ""),
		testTreasuryNote("zero", "uclair", 0, false, ""),
	})
	require.NoError(t, err)
	_, err = svc.ConfirmPlan(ctx, *plan)
	require.NoError(t, err)

	daemon := ReferenceDaemon{Reservation: privacyreservation.Service{Store: store, Now: testNow}, Now: testNow}
	_, err = daemon.RunOnce(ctx)
	require.NoError(t, err)
	second, err := daemon.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, second.ProofReady)
	require.Equal(t, 0, second.Submitted)
	require.Equal(t, 0, second.Reconciled)
	require.Empty(t, second.Items)
}
