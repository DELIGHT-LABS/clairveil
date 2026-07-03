package payroll

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	privacyreservation "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/reservation"
)

func TestServiceCreateAndConfirmPlanCreatesReservations(t *testing.T) {
	ctx := context.Background()
	reservationStore := privacyreservation.NewMemoryStore()
	svc := Service{
		Reservation: privacyreservation.Service{Store: reservationStore, Now: testNow},
		Now:         testNow,
	}
	input := testPayrollInput()
	input.Items[0].Amount = big.NewInt(70)
	notes := []TreasuryNote{
		testTreasuryNote("large", "uclair", 100, false, ""),
		testTreasuryNote("zero", "uclair", 0, false, ""),
	}

	plan, err := svc.CreatePlan(ctx, input, notes)
	require.NoError(t, err)
	require.Equal(t, PlanStatusDraft, plan.Status)
	require.Len(t, plan.Items, 1)

	confirmed, err := svc.ConfirmPlan(ctx, *plan)
	require.NoError(t, err)
	require.Equal(t, PlanStatusConfirmed, confirmed.Status)
	require.Equal(t, ItemStatusReserved, confirmed.Items[0].Status)
	require.Len(t, confirmed.Items[0].InputNotes, 2)
	require.NotEmpty(t, confirmed.Items[0].InputNotes[0].ReservationID)

	reservations, err := reservationStore.ListReservations(ctx, privacyreservation.ReservationFilter{
		Statuses: []privacyreservation.ReservationStatus{privacyreservation.StatusReserved},
	})
	require.NoError(t, err)
	require.Len(t, reservations, 2)

	operation, err := reservationStore.GetOperation(ctx, confirmed.Items[0].OperationID)
	require.NoError(t, err)
	require.Equal(t, input.Items[0].ItemID, operation.ItemID)
	require.Equal(t, HashRecipient(input.Items[0].RecipientAddress), operation.ExpectedRecipientHash)
	require.Equal(t, HashAmount("uclair", input.Items[0].Amount), operation.ExpectedAmountHash)
}

func TestServiceConfirmPlanDoesNotPartiallyReserveOnBatchConflict(t *testing.T) {
	ctx := context.Background()
	reservationStore := privacyreservation.NewMemoryStore()
	svc := Service{
		Reservation: privacyreservation.Service{Store: reservationStore, Now: testNow},
		Now:         testNow,
	}
	plan := PayrollPlan{
		CompanyID: "company-a",
		PayrollID: "payroll-a",
		BatchID:   "batch-a",
		Denom:     "uclair",
		Status:    PlanStatusDraft,
		Items: []PayrollPlanItem{
			{
				CompanyID:        "company-a",
				PayrollID:        "payroll-a",
				BatchID:          "batch-a",
				ChunkID:          "chunk-a",
				ItemID:           "item-1",
				OperationID:      "op-1",
				RecipientAddress: testRecipientAddress("1"),
				Amount:           big.NewInt(10),
				Denom:            "uclair",
				InputNotes:       []TreasuryNote{testTreasuryNote("shared", "uclair", 10, false, "")},
			},
			{
				CompanyID:        "company-a",
				PayrollID:        "payroll-a",
				BatchID:          "batch-a",
				ChunkID:          "chunk-a",
				ItemID:           "item-2",
				OperationID:      "op-2",
				RecipientAddress: testRecipientAddress("2"),
				Amount:           big.NewInt(10),
				Denom:            "uclair",
				InputNotes:       []TreasuryNote{testTreasuryNote("shared-again", "uclair", 10, false, "")},
			},
		},
	}
	plan.Items[1].InputNotes[0].NullifierLookupKey = plan.Items[0].InputNotes[0].NullifierLookupKey

	_, err := svc.ConfirmPlan(ctx, plan)
	require.ErrorIs(t, err, privacyreservation.ErrActiveReservationExists)

	reservations, listErr := reservationStore.ListReservations(ctx, privacyreservation.ReservationFilter{})
	require.NoError(t, listErr)
	require.Empty(t, reservations)
}

func TestBuildPlanReportCountsItemStatuses(t *testing.T) {
	plan := PayrollPlan{
		PayrollID: "payroll-a",
		Items: []PayrollPlanItem{
			{Status: ItemStatusReserved},
			{Status: ItemStatusSubmitted},
			{Status: ItemStatusConfirmed},
			{Status: ItemStatusReplanRequired},
		},
	}

	report := BuildPlanReport(plan)
	require.Equal(t, 4, report.TotalItems)
	require.Equal(t, 1, report.ReservedItems)
	require.Equal(t, 1, report.SubmittedItems)
	require.Equal(t, 1, report.ConfirmedItems)
	require.Equal(t, 1, report.ReplanRequiredItems)
}

func testPayrollInput() PayrollInput {
	return PayrollInput{
		CompanyID: "company-a",
		PayrollID: "payroll-a",
		BatchID:   "batch-a",
		Denom:     "uclair",
		Items: []PayrollItemInput{
			{
				ItemID:           "item-1",
				EmployeeID:       "employee-1",
				RecipientAddress: testRecipientAddress("1"),
				Amount:           big.NewInt(10),
			},
		},
	}
}

func testRecipientAddress(suffix string) string {
	return "clairs1testrecipient" + suffix
}

func testTreasuryNote(noteID string, denom string, amount int64, spent bool, reservationID string) TreasuryNote {
	return TreasuryNote{
		NoteID:               noteID,
		OwnerKeyID:           "owner-a",
		NullifierLookupKey:   "lookup-" + noteID,
		NullifierLookupKeyID: "lookup-v1",
		Denom:                denom,
		Amount:               big.NewInt(amount),
		IsSpent:              spent,
		ReservationID:        reservationID,
	}
}

func testNow() time.Time {
	return time.Date(2026, 7, 3, 9, 0, 0, 0, time.UTC)
}
