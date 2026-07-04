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

func TestServiceCreatePlanCanonicalizesStringInputs(t *testing.T) {
	ctx := context.Background()
	reservationStore := privacyreservation.NewMemoryStore()
	svc := Service{
		Reservation: privacyreservation.Service{Store: reservationStore, Now: testNow},
		Now:         testNow,
	}
	input := testPayrollInput()
	input.CompanyID = " company-a "
	input.PayrollID = " payroll-a "
	input.BatchID = " batch-a "
	input.Denom = " uclair "
	input.Items[0].ItemID = " item-1 "
	input.Items[0].EmployeeID = " employee-1 "
	input.Items[0].RecipientAddress = " " + testRecipientAddress("1") + " "
	input.Items[0].Denom = " "
	input.Items[0].Amount = big.NewInt(70)
	input.Items[0].ExpectedOutputCommitment = " commitment-a "
	input.Items[0].ExpectedDisclosureDigest = " disclosure-a "
	notes := []TreasuryNote{
		testTreasuryNote("large", "uclair", 100, false, ""),
		testTreasuryNote("zero", "uclair", 0, false, ""),
	}

	plan, err := svc.CreatePlan(ctx, input, notes)
	require.NoError(t, err)
	require.Equal(t, "company-a", plan.CompanyID)
	require.Equal(t, "payroll-a", plan.PayrollID)
	require.Equal(t, "batch-a", plan.BatchID)
	require.Equal(t, "uclair", plan.Denom)
	require.Equal(t, "item-1", plan.Items[0].ItemID)
	require.Equal(t, "employee-1", plan.Items[0].EmployeeID)
	require.Equal(t, testRecipientAddress("1"), plan.Items[0].RecipientAddress)
	require.Equal(t, "uclair", plan.Items[0].Denom)
	require.Equal(t, HashRecipient(testRecipientAddress("1")), plan.Items[0].ExpectedRecipientHash)
	require.Equal(t, HashAmount("uclair", big.NewInt(70)), plan.Items[0].ExpectedAmountHash)
	require.Equal(t, "commitment-a", plan.Items[0].ExpectedOutputCommitment)
	require.Equal(t, "disclosure-a", plan.Items[0].ExpectedDisclosureDigest)
	require.Equal(t, operationID("company-a", "batch-a", "payroll-a", "item-1", 0), plan.Items[0].OperationID)

	confirmed, err := svc.ConfirmPlan(ctx, *plan)
	require.NoError(t, err)
	operation, err := reservationStore.GetOperation(ctx, confirmed.Items[0].OperationID)
	require.NoError(t, err)
	require.Equal(t, "uclair", operation.ExpectedDenom)
	require.Equal(t, HashRecipient(testRecipientAddress("1")), operation.ExpectedRecipientHash)
	require.Equal(t, HashAmount("uclair", big.NewInt(70)), operation.ExpectedAmountHash)
	require.Equal(t, "commitment-a", operation.ExpectedOutputCommitment)
	require.Equal(t, "disclosure-a", operation.ExpectedDisclosureDigest)
}

func TestServiceConfirmPlanCanonicalizesDirectPlanEvidence(t *testing.T) {
	ctx := context.Background()
	reservationStore := privacyreservation.NewMemoryStore()
	svc := Service{
		Reservation: privacyreservation.Service{Store: reservationStore, Now: testNow},
		Now:         testNow,
	}
	note := testTreasuryNote(" note-a ", " uclair ", 10, false, "")
	note.OwnerKeyID = " owner-a "
	note.NullifierLookupKey = " lookup-note-a "
	note.NullifierLookupKeyID = " lookup-v1 "
	plan := PayrollPlan{
		CompanyID: " company-a ",
		PayrollID: " payroll-a ",
		BatchID:   " batch-a ",
		Denom:     " uclair ",
		Status:    PlanStatusDraft,
		Items: []PayrollPlanItem{{
			CompanyID:             " company-a ",
			PayrollID:             " payroll-a ",
			BatchID:               " batch-a ",
			ChunkID:               " chunk-a ",
			ItemID:                " item-1 ",
			OperationID:           " op-1 ",
			RecipientAddress:      " " + testRecipientAddress("1") + " ",
			ExpectedRecipientHash: "stale-recipient-hash",
			ExpectedAmountHash:    "stale-amount-hash",
			Amount:                big.NewInt(10),
			Denom:                 " ",
			InputNotes: []TreasuryNote{
				note,
				testTreasuryNote(" zero-note ", " uclair ", 0, false, ""),
			},
		}},
	}

	confirmed, err := svc.ConfirmPlan(ctx, plan)
	require.NoError(t, err)
	require.Equal(t, "uclair", confirmed.Items[0].Denom)
	require.Equal(t, HashRecipient(testRecipientAddress("1")), confirmed.Items[0].ExpectedRecipientHash)
	require.Equal(t, HashAmount("uclair", big.NewInt(10)), confirmed.Items[0].ExpectedAmountHash)
	require.Equal(t, "note-a", confirmed.Items[0].InputNotes[0].NoteID)
	require.Equal(t, "zero-note", confirmed.Items[0].InputNotes[1].NoteID)
	require.NotEmpty(t, confirmed.Items[0].InputNotes[0].ReservationID)

	operation, err := reservationStore.GetOperation(ctx, "op-1")
	require.NoError(t, err)
	require.Equal(t, "uclair", operation.ExpectedDenom)
	require.Equal(t, HashRecipient(testRecipientAddress("1")), operation.ExpectedRecipientHash)
	require.Equal(t, HashAmount("uclair", big.NewInt(10)), operation.ExpectedAmountHash)
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
				InputNotes: []TreasuryNote{
					testTreasuryNote("shared", "uclair", 10, false, ""),
					testTreasuryNote("zero-1", "uclair", 0, false, ""),
				},
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
				InputNotes: []TreasuryNote{
					testTreasuryNote("shared-again", "uclair", 10, false, ""),
					testTreasuryNote("zero-2", "uclair", 0, false, ""),
				},
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

func TestServiceConfirmPlanRejectsItemWithoutInputNotes(t *testing.T) {
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
		Items: []PayrollPlanItem{{
			CompanyID:        "company-a",
			PayrollID:        "payroll-a",
			BatchID:          "batch-a",
			ChunkID:          "chunk-a",
			ItemID:           "item-1",
			OperationID:      "op-1",
			RecipientAddress: testRecipientAddress("1"),
			Amount:           big.NewInt(10),
			Denom:            "uclair",
		}},
	}

	_, err := svc.ConfirmPlan(ctx, plan)
	require.ErrorIs(t, err, ErrInvalidPayrollInput)
	require.ErrorContains(t, err, "exactly 2 input notes")

	reservations, listErr := reservationStore.ListReservations(ctx, privacyreservation.ReservationFilter{})
	require.NoError(t, listErr)
	require.Empty(t, reservations)
}

func TestServiceConfirmPlanRejectsMalformedRecipientBeforeReserve(t *testing.T) {
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
		Items: []PayrollPlanItem{{
			CompanyID:        "company-a",
			PayrollID:        "payroll-a",
			BatchID:          "batch-a",
			ChunkID:          "chunk-a",
			ItemID:           "item-1",
			OperationID:      "op-1",
			RecipientAddress: "clairs1notshielded",
			Amount:           big.NewInt(10),
			Denom:            "uclair",
			InputNotes:       []TreasuryNote{testTreasuryNote("note-a", "uclair", 10, false, "")},
		}},
	}

	_, err := svc.ConfirmPlan(ctx, plan)
	require.ErrorIs(t, err, ErrInvalidPayrollInput)

	reservations, listErr := reservationStore.ListReservations(ctx, privacyreservation.ReservationFilter{})
	require.NoError(t, listErr)
	require.Empty(t, reservations)
}

func TestServiceConfirmPlanRejectsInvalidAmountBeforeReserve(t *testing.T) {
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
		Items: []PayrollPlanItem{{
			CompanyID:        "company-a",
			PayrollID:        "payroll-a",
			BatchID:          "batch-a",
			ChunkID:          "chunk-a",
			ItemID:           "item-1",
			OperationID:      "op-1",
			RecipientAddress: testRecipientAddress("1"),
			Amount:           big.NewInt(0),
			Denom:            "uclair",
			InputNotes:       []TreasuryNote{testTreasuryNote("note-a", "uclair", 10, false, "")},
		}},
	}

	_, err := svc.ConfirmPlan(ctx, plan)
	require.ErrorIs(t, err, ErrInvalidPayrollInput)

	reservations, listErr := reservationStore.ListReservations(ctx, privacyreservation.ReservationFilter{})
	require.NoError(t, listErr)
	require.Empty(t, reservations)
}

func TestServiceConfirmPlanRejectsInvalidInputNotesBeforeReserve(t *testing.T) {
	tests := []struct {
		name  string
		notes []TreasuryNote
	}{
		{
			name: "insufficient amount",
			notes: []TreasuryNote{
				testTreasuryNote("note-a", "uclair", 9, false, ""),
				testTreasuryNote("zero-a", "uclair", 0, false, ""),
			},
		},
		{
			name: "wrong denom",
			notes: []TreasuryNote{
				testTreasuryNote("note-a", "uatom", 10, false, ""),
				testTreasuryNote("zero-a", "uclair", 0, false, ""),
			},
		},
		{
			name: "spent note",
			notes: []TreasuryNote{
				testTreasuryNote("note-a", "uclair", 10, true, ""),
				testTreasuryNote("zero-a", "uclair", 0, false, ""),
			},
		},
		{
			name: "reserved note",
			notes: []TreasuryNote{
				testTreasuryNote("note-a", "uclair", 10, false, "reservation-a"),
				testTreasuryNote("zero-a", "uclair", 0, false, ""),
			},
		},
		{
			name: "wrong input count",
			notes: []TreasuryNote{
				testTreasuryNote("note-a", "uclair", 10, false, ""),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
				Items: []PayrollPlanItem{{
					CompanyID:        "company-a",
					PayrollID:        "payroll-a",
					BatchID:          "batch-a",
					ChunkID:          "chunk-a",
					ItemID:           "item-1",
					OperationID:      "op-1",
					RecipientAddress: testRecipientAddress("1"),
					Amount:           big.NewInt(10),
					Denom:            "uclair",
					InputNotes:       tt.notes,
				}},
			}

			_, err := svc.ConfirmPlan(ctx, plan)
			require.ErrorIs(t, err, ErrInvalidPayrollInput)

			reservations, listErr := reservationStore.ListReservations(ctx, privacyreservation.ReservationFilter{})
			require.NoError(t, listErr)
			require.Empty(t, reservations)
		})
	}
}

func TestServiceReplanItemsUsesNewAttemptIDs(t *testing.T) {
	ctx := context.Background()
	reservationStore := privacyreservation.NewMemoryStore()
	svc := Service{
		Reservation: privacyreservation.Service{Store: reservationStore, Now: testNow},
		Now:         testNow,
	}
	input := testPayrollInput()
	input.Items[0].Amount = big.NewInt(70)

	firstPlan, err := svc.CreatePlan(ctx, input, []TreasuryNote{
		testTreasuryNote("large-a", "uclair", 100, false, ""),
		testTreasuryNote("zero-a", "uclair", 0, false, ""),
	})
	require.NoError(t, err)
	firstConfirmed, err := svc.ConfirmPlan(ctx, *firstPlan)
	require.NoError(t, err)

	replan, err := svc.ReplanItems(input, []TreasuryNote{
		testTreasuryNote("large-b", "uclair", 100, false, ""),
		testTreasuryNote("zero-b", "uclair", 0, false, ""),
	}, map[string]struct{}{input.Items[0].ItemID: {}})
	require.NoError(t, err)
	require.Equal(t, 1, replan.Attempt)
	require.NotEqual(t, firstConfirmed.Items[0].OperationID, replan.Items[0].OperationID)

	secondConfirmed, err := svc.ConfirmPlan(ctx, *replan)
	require.NoError(t, err)
	require.NotEqual(t, firstConfirmed.Items[0].InputNotes[0].ReservationID, secondConfirmed.Items[0].InputNotes[0].ReservationID)
	_, err = reservationStore.GetOperation(ctx, replan.Items[0].OperationID)
	require.NoError(t, err)
}

func TestServiceConfirmPlanDoesNotCollideAcrossTenants(t *testing.T) {
	ctx := context.Background()
	reservationStore := privacyreservation.NewMemoryStore()
	svc := Service{
		Reservation: privacyreservation.Service{Store: reservationStore, Now: testNow},
		Now:         testNow,
	}
	firstInput := testPayrollInput()
	firstInput.CompanyID = "company-a"
	firstInput.BatchID = "batch-a"
	firstInput.Items[0].Amount = big.NewInt(70)
	secondInput := testPayrollInput()
	secondInput.CompanyID = "company-b"
	secondInput.BatchID = "batch-a"
	secondInput.Items[0].Amount = big.NewInt(70)

	firstPlan, err := svc.CreatePlan(ctx, firstInput, []TreasuryNote{
		testTreasuryNote("company-a-large", "uclair", 100, false, ""),
		testTreasuryNote("company-a-zero", "uclair", 0, false, ""),
	})
	require.NoError(t, err)
	secondPlan, err := svc.CreatePlan(ctx, secondInput, []TreasuryNote{
		testTreasuryNote("company-b-large", "uclair", 100, false, ""),
		testTreasuryNote("company-b-zero", "uclair", 0, false, ""),
	})
	require.NoError(t, err)
	require.NotEqual(t, firstPlan.Items[0].OperationID, secondPlan.Items[0].OperationID)
	require.NotEqual(t, firstPlan.Items[0].ChunkID, secondPlan.Items[0].ChunkID)

	_, err = svc.ConfirmPlan(ctx, *firstPlan)
	require.NoError(t, err)
	_, err = svc.ConfirmPlan(ctx, *secondPlan)
	require.NoError(t, err)
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
	switch suffix {
	case "2":
		return "clairs1uwnerzqjukcmg56pqwe509jmfvsvdnd8j4k657d8c839f5rthu0q6d9yk3vda5wyhmvggjgkj94axzegkchypz0h3nx577vw3th7lpq7mwjed"
	default:
		return "clairs19x5u4mf4l4zqcpvr7d809fh4tjy5j50p2mwgky0nj38jpqpj7svndu3hqshu5e3s8w6pea5p30xek5p9flxjf7f44xh7cnfrlsd84pc7upgh3"
	}
}

func testTreasuryNote(noteID string, denom string, amount int64, spent bool, reservationID string) TreasuryNote {
	return testTreasuryNoteBig(noteID, denom, big.NewInt(amount), spent, reservationID)
}

func testTreasuryNoteBig(noteID string, denom string, amount *big.Int, spent bool, reservationID string) TreasuryNote {
	return TreasuryNote{
		NoteID:               noteID,
		OwnerKeyID:           "owner-a",
		NullifierLookupKey:   "lookup-" + noteID,
		NullifierLookupKeyID: "lookup-v1",
		Denom:                denom,
		Amount:               cloneBigInt(amount),
		IsSpent:              spent,
		ReservationID:        reservationID,
	}
}

func testNow() time.Time {
	return time.Date(2026, 7, 3, 9, 0, 0, 0, time.UTC)
}
