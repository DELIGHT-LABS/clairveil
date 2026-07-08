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

func TestLiveDaemonRollsBackProvingReservationsWhenProofReadyFails(t *testing.T) {
	ctx := context.Background()
	store := privacyreservation.NewMemoryStore()
	reservationService := privacyreservation.Service{Store: store, Now: testNow}
	planner := Service{Reservation: reservationService, Now: testNow}

	input := testPayrollInput()
	input.Items[0].Amount = big.NewInt(70)
	input.Items[0].ExpectedOutputCommitment = "expected-commitment"
	plan, err := planner.CreatePlan(ctx, input, []TreasuryNote{
		testTreasuryNote("large", "uclair", 100, false, ""),
		testTreasuryNote("zero", "uclair", 0, false, ""),
	})
	require.NoError(t, err)
	confirmed, err := planner.ConfirmPlan(ctx, *plan)
	require.NoError(t, err)

	_, err = (LiveDaemon{
		Reservation: reservationService,
		Executor:    testLiveExecutor{item: confirmed.Items[0]},
		LeaseOwner:  "live-worker-a",
		LeaseTTL:    time.Minute,
		Now:         testNow,
	}).RunOnce(ctx)
	require.Error(t, err)

	for _, note := range confirmed.Items[0].InputNotes {
		reservation, err := store.GetReservation(ctx, note.ReservationID)
		require.NoError(t, err)
		require.Equal(t, privacyreservation.StatusReserved, reservation.Status)
	}
}

func TestLiveDaemonDoesNotReuseAnotherWorkersProofReadyLease(t *testing.T) {
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

	refs := make([]privacyreservation.SubmittedReservationRef, 0, len(confirmed.Items[0].InputNotes))
	for _, note := range confirmed.Items[0].InputNotes {
		lease, err := reservationService.AcquireLeaseForStatus(ctx, note.ReservationID, "worker-a", privacyreservation.StatusReserved, time.Minute)
		require.NoError(t, err)
		_, err = reservationService.TransitionWithLease(ctx, note.ReservationID, lease.Token, privacyreservation.StatusReserved, privacyreservation.StatusProving)
		require.NoError(t, err)
		refs = append(refs, privacyreservation.SubmittedReservationRef{ReservationID: note.ReservationID, LeaseToken: lease.Token})
	}
	_, _, err = reservationService.MarkProofReadyBatch(ctx, refs, privacyreservation.ProofReadyOperationUpdate{
		OperationID:                   confirmed.Items[0].OperationID,
		ExpectedOutputCommitment:      "commitment-a",
		ExpectedDisclosureDigest:      "digest-a",
		ExpectedAuditDisclosureDigest: "digest-a",
	})
	require.NoError(t, err)

	report, err := (LiveDaemon{
		Reservation: reservationService,
		Executor:    testLiveExecutor{item: confirmed.Items[0]},
		LeaseOwner:  "worker-b",
		LeaseTTL:    time.Minute,
		Now:         testNow,
	}).RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, report.Skipped)
	require.Equal(t, 0, report.Submitted)
	for _, ref := range refs {
		reservation, err := store.GetReservation(ctx, ref.ReservationID)
		require.NoError(t, err)
		require.Equal(t, privacyreservation.StatusProofReady, reservation.Status)
		require.Equal(t, "worker-a", reservation.LeaseOwner)
		require.Equal(t, ref.LeaseToken, reservation.LeaseToken)
	}
}

func TestLiveDaemonClearsAcquiredProofReadyLeasesWhenBroadcastSkips(t *testing.T) {
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

	refs := make([]privacyreservation.SubmittedReservationRef, 0, len(confirmed.Items[0].InputNotes))
	for _, note := range confirmed.Items[0].InputNotes {
		lease, err := reservationService.AcquireLeaseForStatus(ctx, note.ReservationID, "worker-a", privacyreservation.StatusReserved, time.Minute)
		require.NoError(t, err)
		_, err = reservationService.TransitionWithLease(ctx, note.ReservationID, lease.Token, privacyreservation.StatusReserved, privacyreservation.StatusProving)
		require.NoError(t, err)
		refs = append(refs, privacyreservation.SubmittedReservationRef{ReservationID: note.ReservationID, LeaseToken: lease.Token})
	}
	_, _, err = reservationService.MarkProofReadyBatch(ctx, refs, privacyreservation.ProofReadyOperationUpdate{
		OperationID:                   confirmed.Items[0].OperationID,
		ExpectedOutputCommitment:      "commitment-a",
		ExpectedDisclosureDigest:      "digest-a",
		ExpectedAuditDisclosureDigest: "digest-a",
	})
	require.NoError(t, err)
	for _, ref := range refs {
		_, err = reservationService.ClearLease(ctx, ref.ReservationID, ref.LeaseToken)
		require.NoError(t, err)
	}

	report, err := (LiveDaemon{
		Reservation: reservationService,
		Executor:    skippingBroadcastExecutor{item: confirmed.Items[0]},
		LeaseOwner:  "worker-b",
		LeaseTTL:    time.Minute,
		Now:         testNow,
	}).RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, report.Skipped)
	for _, ref := range refs {
		reservation, err := store.GetReservation(ctx, ref.ReservationID)
		require.NoError(t, err)
		require.Equal(t, privacyreservation.StatusProofReady, reservation.Status)
		require.Empty(t, reservation.LeaseOwner)
		require.Empty(t, reservation.LeaseToken)
	}
}

func TestLiveDaemonClearsSameRunProofReadyLeasesWhenBroadcastSkips(t *testing.T) {
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
		Executor:    skippingBroadcastExecutor{item: confirmed.Items[0]},
		LeaseOwner:  "worker-b",
		LeaseTTL:    time.Minute,
		Now:         testNow,
	}).RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, report.ProofReady)
	require.Equal(t, 1, report.Skipped)
	for _, note := range confirmed.Items[0].InputNotes {
		reservation, err := store.GetReservation(ctx, note.ReservationID)
		require.NoError(t, err)
		require.Equal(t, privacyreservation.StatusProofReady, reservation.Status)
		require.Empty(t, reservation.LeaseOwner)
		require.Empty(t, reservation.LeaseToken)
	}
}

func TestLiveDaemonSkipsReservedLeaseContention(t *testing.T) {
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
	_, err = reservationService.AcquireLeaseForStatus(ctx, confirmed.Items[0].InputNotes[0].ReservationID, "external-worker", privacyreservation.StatusReserved, time.Minute)
	require.NoError(t, err)

	report, err := (LiveDaemon{
		Reservation: reservationService,
		Executor:    testLiveExecutor{item: confirmed.Items[0]},
		LeaseOwner:  "worker-b",
		LeaseTTL:    time.Minute,
		Now:         testNow,
	}).RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, report.Skipped)
	require.Equal(t, 0, report.ProofReady)
}

func TestLiveDaemonReportsNonzeroBroadcastAsRequiresReview(t *testing.T) {
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
		Executor:    rejectingBroadcastExecutor{item: confirmed.Items[0]},
		LeaseOwner:  "worker-b",
		LeaseTTL:    time.Minute,
		Now:         testNow,
	}).RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, report.ProofReady)
	require.Equal(t, 0, report.Submitted)
	require.Equal(t, 1, report.RequiresReview)

	operation, err := store.GetOperation(ctx, confirmed.Items[0].OperationID)
	require.NoError(t, err)
	require.Equal(t, privacyreservation.OperationStatusUnknown, operation.Status)
	for _, note := range confirmed.Items[0].InputNotes {
		reservation, err := store.GetReservation(ctx, note.ReservationID)
		require.NoError(t, err)
		require.Equal(t, privacyreservation.StatusUnknown, reservation.Status)
		require.Equal(t, "REJECTED_TX", reservation.TxHash)
		require.Contains(t, reservation.LastBroadcastError, "code 17")
	}
}

func TestLiveDaemonRollsBackExpiredMixedProvingReservations(t *testing.T) {
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
	require.Len(t, confirmed.Items[0].InputNotes, 2)
	markPayrollNotesProvingForDaemonTest(t, ctx, reservationService, confirmed.Items[0].InputNotes[:1], "worker-a", time.Minute)
	futureNow := func() time.Time { return testNow().Add(2 * time.Minute) }

	report, err := (LiveDaemon{
		Reservation: privacyreservation.Service{Store: store, Now: futureNow},
		Executor:    testLiveExecutor{item: confirmed.Items[0]},
		LeaseOwner:  "worker-b",
		LeaseTTL:    time.Minute,
		Now:         futureNow,
	}).RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, report.ProofReady)
	require.Equal(t, 0, report.Submitted)
	require.Equal(t, 2, report.Skipped)

	for _, note := range confirmed.Items[0].InputNotes {
		reservation, err := store.GetReservation(ctx, note.ReservationID)
		require.NoError(t, err)
		require.Equal(t, privacyreservation.StatusReserved, reservation.Status)
		require.Empty(t, reservation.LeaseOwner)
		require.Empty(t, reservation.LeaseToken)
	}
}

type testLiveExecutor struct {
	item PayrollPlanItem
}

type skippingBroadcastExecutor struct {
	item PayrollPlanItem
}

type rejectingBroadcastExecutor struct {
	item PayrollPlanItem
}

func (e skippingBroadcastExecutor) BuildProofReady(context.Context, LiveOperationGroup) (privacyreservation.ProofReadyOperationUpdate, string, error) {
	return testLiveExecutor{item: e.item}.BuildProofReady(context.Background(), LiveOperationGroup{})
}

func (e skippingBroadcastExecutor) BroadcastProofReady(context.Context, LiveOperationGroup) (*BroadcastResult, string, error) {
	return nil, "external broadcaster handles this operation", ErrLiveDaemonSkip
}

func (e skippingBroadcastExecutor) ScanSubmitted(ctx context.Context, group LiveOperationGroup) (map[string]privacyreservation.OperationEvidence, string, error) {
	return testLiveExecutor{item: e.item}.ScanSubmitted(ctx, group)
}

func (e rejectingBroadcastExecutor) BuildProofReady(context.Context, LiveOperationGroup) (privacyreservation.ProofReadyOperationUpdate, string, error) {
	return testLiveExecutor{item: e.item}.BuildProofReady(context.Background(), LiveOperationGroup{})
}

func (e rejectingBroadcastExecutor) BroadcastProofReady(context.Context, LiveOperationGroup) (*BroadcastResult, string, error) {
	return &BroadcastResult{TxHash: "REJECTED_TX", TxBytesHash: "tx-bytes", SignDocHash: "sign-doc", AccountSequence: 4, Code: 17, RawLog: "out of gas"}, "test rejected broadcast", nil
}

func (e rejectingBroadcastExecutor) ScanSubmitted(context.Context, LiveOperationGroup) (map[string]privacyreservation.OperationEvidence, string, error) {
	return nil, "broadcast result requires operator review", ErrLiveDaemonSkip
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

func markPayrollNotesProvingForDaemonTest(t *testing.T, ctx context.Context, service privacyreservation.Service, notes []TreasuryNote, owner string, ttl time.Duration) []privacyreservation.SubmittedReservationRef {
	t.Helper()
	refs := make([]privacyreservation.SubmittedReservationRef, 0, len(notes))
	for _, note := range notes {
		lease, err := service.AcquireLeaseForStatus(ctx, note.ReservationID, owner, privacyreservation.StatusReserved, ttl)
		require.NoError(t, err)
		_, err = service.TransitionWithLease(ctx, note.ReservationID, lease.Token, privacyreservation.StatusReserved, privacyreservation.StatusProving)
		require.NoError(t, err)
		refs = append(refs, privacyreservation.SubmittedReservationRef{ReservationID: note.ReservationID, LeaseToken: lease.Token})
	}
	return refs
}
