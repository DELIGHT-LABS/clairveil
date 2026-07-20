package payroll

import (
	"context"
	"fmt"
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

	refs := markPayrollNotesProvingForDaemonTest(t, ctx, reservationService, confirmed.Items[0].OperationID, confirmed.Items[0].InputNotes, "worker-a", time.Minute)
	_, _, err = reservationService.MarkProofReadyBatch(ctx, refs, privacyreservation.ProofReadyOperationUpdate{
		OperationID:                   confirmed.Items[0].OperationID,
		PayloadHash:                   "test-proof-ready-payload",
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

func TestLiveDaemonLeavesAnotherWorkersProofReadyLeaseWhenBroadcastSkips(t *testing.T) {
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

	refs := markPayrollNotesProvingForDaemonTest(t, ctx, reservationService, confirmed.Items[0].OperationID, confirmed.Items[0].InputNotes, "worker-a", time.Minute)
	_, _, err = reservationService.MarkProofReadyBatch(ctx, refs, privacyreservation.ProofReadyOperationUpdate{
		OperationID:                   confirmed.Items[0].OperationID,
		PayloadHash:                   "test-proof-ready-payload",
		ExpectedOutputCommitment:      "commitment-a",
		ExpectedDisclosureDigest:      "digest-a",
		ExpectedAuditDisclosureDigest: "digest-a",
	})
	require.NoError(t, err)
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
		require.Equal(t, "worker-a", reservation.LeaseOwner)
		require.Equal(t, ref.LeaseToken, reservation.LeaseToken)
	}
}

func TestLiveDaemonDoesNotRecordAttemptWhenBroadcastPreparationSkips(t *testing.T) {
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
		require.Equal(t, "worker-b", reservation.LeaseOwner)
		require.NotEmpty(t, reservation.LeaseToken)
		require.False(t, reservation.BroadcastInFlight)
		require.Zero(t, reservation.BroadcastAttemptCount)
	}
}

func TestLiveDaemonMarksAttemptBeforeExternalBroadcast(t *testing.T) {
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

	sawAttempt := false
	report, err := (LiveDaemon{
		Reservation: reservationService,
		Executor: testLiveExecutor{
			item: confirmed.Items[0],
			beforeSubmit: func(ctx context.Context, group LiveOperationGroup) error {
				for _, note := range group.Reservations {
					reservation, err := store.GetReservation(ctx, note.ReservationID)
					if err != nil {
						return err
					}
					if !reservation.BroadcastInFlight || reservation.BroadcastAttemptCount != 1 {
						return fmt.Errorf("reservation %s was not durably marked before submit", note.ReservationID)
					}
				}
				sawAttempt = true
				return nil
			},
		},
		LeaseOwner: "live-worker-a",
		LeaseTTL:   time.Minute,
		Now:        testNow,
	}).RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, report.Submitted)
	require.True(t, sawAttempt)
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

func TestLiveDaemonMarksExpiredProvingReservationsReplanRequired(t *testing.T) {
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
	markPayrollNotesProvingForDaemonTest(t, ctx, reservationService, confirmed.Items[0].OperationID, confirmed.Items[0].InputNotes, "worker-a", time.Minute)
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
	require.Equal(t, 1, report.Skipped)

	for _, note := range confirmed.Items[0].InputNotes {
		reservation, err := store.GetReservation(ctx, note.ReservationID)
		require.NoError(t, err)
		require.Equal(t, privacyreservation.StatusReplanRequired, reservation.Status)
		require.Empty(t, reservation.LeaseOwner)
		require.Empty(t, reservation.LeaseToken)
	}
}

type testLiveExecutor struct {
	item         PayrollPlanItem
	beforeSubmit func(context.Context, LiveOperationGroup) error
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

func (e skippingBroadcastExecutor) PrepareBroadcastProofReady(context.Context, LiveOperationGroup) (LiveBroadcastSubmit, string, error) {
	return nil, "external broadcaster handles this operation", ErrLiveDaemonSkip
}

func (e skippingBroadcastExecutor) ScanSubmitted(ctx context.Context, group LiveOperationGroup) (map[string]privacyreservation.OperationEvidence, string, error) {
	return testLiveExecutor{item: e.item}.ScanSubmitted(ctx, group)
}

func (e rejectingBroadcastExecutor) BuildProofReady(context.Context, LiveOperationGroup) (privacyreservation.ProofReadyOperationUpdate, string, error) {
	return testLiveExecutor{item: e.item}.BuildProofReady(context.Background(), LiveOperationGroup{})
}

func (e rejectingBroadcastExecutor) PrepareBroadcastProofReady(context.Context, LiveOperationGroup) (LiveBroadcastSubmit, string, error) {
	return func(context.Context) (*BroadcastResult, error) {
		return &BroadcastResult{TxHash: "REJECTED_TX", TxBytesHash: "tx-bytes", SignDocHash: "sign-doc", AccountSequence: 4, Code: 17, RawLog: "out of gas"}, nil
	}, "test rejected broadcast", nil
}

func (e rejectingBroadcastExecutor) ScanSubmitted(context.Context, LiveOperationGroup) (map[string]privacyreservation.OperationEvidence, string, error) {
	return nil, "broadcast result requires operator review", ErrLiveDaemonSkip
}

func (e testLiveExecutor) BuildProofReady(context.Context, LiveOperationGroup) (privacyreservation.ProofReadyOperationUpdate, string, error) {
	return privacyreservation.ProofReadyOperationUpdate{
		PayloadHash:                   "test-live-payload",
		ExpectedOutputCommitment:      "commitment-a",
		ExpectedDisclosureDigest:      "digest-a",
		ExpectedAuditDisclosureDigest: "digest-a",
	}, "test proof", nil
}

func (e testLiveExecutor) PrepareBroadcastProofReady(_ context.Context, group LiveOperationGroup) (LiveBroadcastSubmit, string, error) {
	return func(ctx context.Context) (*BroadcastResult, error) {
		if e.beforeSubmit != nil {
			if err := e.beforeSubmit(ctx, group); err != nil {
				return nil, err
			}
		}
		return &BroadcastResult{TxHash: "TXHASH", TxBytesHash: "tx-bytes", SignDocHash: "sign-doc", AccountSequence: 3}, nil
	}, "test broadcast", nil
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

func markPayrollNotesProvingForDaemonTest(t *testing.T, ctx context.Context, service privacyreservation.Service, operationID string, notes []TreasuryNote, owner string, ttl time.Duration) []privacyreservation.SubmittedReservationRef {
	t.Helper()
	reservationIDs := make([]string, 0, len(notes))
	for _, note := range notes {
		reservationIDs = append(reservationIDs, note.ReservationID)
	}
	refs, _, err := service.BeginProvingOperation(ctx, operationID, reservationIDs, owner, ttl)
	require.NoError(t, err)
	return refs
}
