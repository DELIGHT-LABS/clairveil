package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	privacypayroll "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/payroll"
	privacyreservation "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/reservation"
)

func TestPrepareNotesCommandWritesReport(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "payroll.json")
	outPath := filepath.Join(dir, "report.json")
	writePayrollInput(t, inputPath, validPrepareNotesPayload())

	err := runPrepareNotes([]string{"-input", inputPath, "-out", outPath})
	require.NoError(t, err)

	reportBytes, err := os.ReadFile(outPath)
	require.NoError(t, err)
	var report privacypayroll.NotePreparationReport
	require.NoError(t, json.Unmarshal(reportBytes, &report))
	require.Equal(t, 1, report.ReadyItems)
	require.Equal(t, 0, report.BlockedItems)
	require.Equal(t, 1, report.EstimatedMessageChunks)
}

func TestValidateCommandWritesReport(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "payroll.json")
	outPath := filepath.Join(dir, "validation.json")
	writePayrollInput(t, inputPath, validPrepareNotesPayload())

	err := runValidate([]string{"-input", inputPath, "-out", outPath})
	require.NoError(t, err)

	reportBytes, err := os.ReadFile(outPath)
	require.NoError(t, err)
	var report validationReport
	require.NoError(t, json.Unmarshal(reportBytes, &report))
	require.True(t, report.Valid)
	require.NotNil(t, report.NotePreparation)
	require.Equal(t, 1, report.NotePreparation.ReadyItems)
}

func TestPlanStatusAndExportReportCommands(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "payroll.json")
	planPath := filepath.Join(dir, "plan.json")
	statusPath := filepath.Join(dir, "status.json")
	exportPath := filepath.Join(dir, "export.json")
	writePayrollInput(t, inputPath, validPrepareNotesPayload())

	require.NoError(t, runPlan([]string{"-input", inputPath, "-out", planPath}))

	planBytes, err := os.ReadFile(planPath)
	require.NoError(t, err)
	var plan privacypayroll.PayrollPlan
	require.NoError(t, json.Unmarshal(planBytes, &plan))
	require.Equal(t, privacypayroll.PlanStatusDraft, plan.Status)
	require.Len(t, plan.Items, 1)
	require.Equal(t, privacypayroll.ItemStatusPlanned, plan.Items[0].Status)
	require.Len(t, plan.Items[0].InputNotes, 2)

	require.NoError(t, runStatus([]string{"-plan", planPath, "-out", statusPath}))
	statusBytes, err := os.ReadFile(statusPath)
	require.NoError(t, err)
	var status privacypayroll.PlanReport
	require.NoError(t, json.Unmarshal(statusBytes, &status))
	require.Equal(t, 1, status.TotalItems)
	require.Equal(t, 1, status.PlannedItems)

	require.NoError(t, runExportReport([]string{"-plan", planPath, "-out", exportPath}))
	exportBytes, err := os.ReadFile(exportPath)
	require.NoError(t, err)
	var exported payrollExportReport
	require.NoError(t, json.Unmarshal(exportBytes, &exported))
	require.Equal(t, "payroll-a", exported.PayrollID)
	require.Equal(t, 1, exported.Summary.TotalItems)
	require.Equal(t, "70", exported.Items[0].Amount)
}

func TestPlanCommandCanWriteFileArtifactStore(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "payroll.json")
	outPath := filepath.Join(dir, "plan.json")
	storeDir := filepath.Join(dir, "store")
	writePayrollInput(t, inputPath, validPrepareNotesPayload())

	require.NoError(t, runPlan([]string{"-input", inputPath, "-out", outPath, "-store-dir", storeDir}))

	store := privacypayroll.FileArtifactStore{Dir: storeDir}
	plan, err := store.ReadPayrollPlan(context.Background(), "company-a.payroll-a.batch-a")
	require.NoError(t, err)
	require.Equal(t, "payroll-a", plan.PayrollID)
	require.Len(t, plan.Items, 1)
}

func TestRunStatusAndReconcileCommandsUseDurableState(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "payroll.json")
	planPath := filepath.Join(dir, "plan.json")
	confirmedPath := filepath.Join(dir, "confirmed-plan.json")
	statePath := filepath.Join(dir, "reservation-state.json")
	statusPath := filepath.Join(dir, "state-status.json")
	evidencePath := filepath.Join(dir, "evidence.json")
	reconcilePath := filepath.Join(dir, "reconcile.json")
	exportPath := filepath.Join(dir, "export.json")
	payload := validPrepareNotesPayload()
	payload.Items[0].ExpectedOutputCommitment = "commitment-a"
	payload.Items[0].ExpectedDisclosureDigest = "digest-a"
	writePayrollInput(t, inputPath, payload)
	require.NoError(t, runPlan([]string{"-input", inputPath, "-out", planPath}))

	require.NoError(t, runRun([]string{"-plan", planPath, "-state", statePath, "-out", confirmedPath}))
	require.NoError(t, runRun([]string{"-plan", planPath, "-state", statePath, "-out", filepath.Join(dir, "confirmed-again.json")}))

	confirmedBytes, err := os.ReadFile(confirmedPath)
	require.NoError(t, err)
	var confirmed privacypayroll.PayrollPlan
	require.NoError(t, json.Unmarshal(confirmedBytes, &confirmed))
	require.Equal(t, privacypayroll.PlanStatusConfirmed, confirmed.Status)
	require.Equal(t, privacypayroll.ItemStatusReserved, confirmed.Items[0].Status)
	require.Len(t, confirmed.Items[0].InputNotes, 2)
	require.NotEmpty(t, confirmed.Items[0].InputNotes[0].ReservationID)

	require.NoError(t, runStatus([]string{"-state", statePath, "-out", statusPath}))
	statusBytes, err := os.ReadFile(statusPath)
	require.NoError(t, err)
	var status stateStatusReport
	require.NoError(t, json.Unmarshal(statusBytes, &status))
	require.Equal(t, 2, status.ReservationTotal)
	require.Equal(t, 1, status.OperationTotal)
	require.Equal(t, 2, status.ReservationsByStatus[string(privacyreservation.StatusReserved)])

	markConfirmedPlanSubmitted(t, statePath, confirmed)
	evidence := reconcileEvidenceFile{Evidence: []reconcileEvidenceItemFile{{
		ReservationID:       confirmed.Items[0].InputNotes[0].ReservationID,
		TxHash:              "txhash",
		OutputCommitment:    "commitment-a",
		DisclosureDigest:    "digest-a",
		RecipientHash:       confirmed.Items[0].ExpectedRecipientHash,
		AmountHash:          confirmed.Items[0].ExpectedAmountHash,
		Denom:               "uclair",
		NullifierSpent:      true,
		BatchItemIndex:      0,
		BatchItemIndexKnown: true,
		TxSucceeded:         true,
	}}}
	bz, err := json.Marshal(evidence)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(evidencePath, bz, 0o600))

	require.NoError(t, runReconcile([]string{"-state", statePath, "-evidence", evidencePath, "-out", reconcilePath}))
	reconcileBytes, err := os.ReadFile(reconcilePath)
	require.NoError(t, err)
	var report reconcileReport
	require.NoError(t, json.Unmarshal(reconcileBytes, &report))
	require.Equal(t, 1, report.Total)
	require.Equal(t, 0, report.RequiresReview)
	require.Equal(t, privacyreservation.StatusConfirmedSpent, report.Results[0].ReservationStatus)
	require.Equal(t, privacyreservation.OperationStatusSucceeded, report.Results[0].OperationStatus)

	require.NoError(t, runExportReport([]string{"-plan", planPath, "-state", statePath, "-out", exportPath}))
	exportBytes, err := os.ReadFile(exportPath)
	require.NoError(t, err)
	var exported payrollExportReport
	require.NoError(t, json.Unmarshal(exportBytes, &exported))
	require.Equal(t, privacypayroll.ItemStatusConfirmed, exported.Items[0].Status)
}

func TestParsePrivacyPolicyLabels(t *testing.T) {
	_, err := parsePrivacyPolicy("amount-from-to")
	require.NoError(t, err)
	_, err = parsePrivacyPolicy("unknown")
	require.Error(t, err)
}

func testPayrollRecipientAddress() string {
	return "clairs19x5u4mf4l4zqcpvr7d809fh4tjy5j50p2mwgky0nj38jpqpj7svndu3hqshu5e3s8w6pea5p30xek5p9flxjf7f44xh7cnfrlsd84pc7upgh3"
}

func validPrepareNotesPayload() prepareNotesFile {
	return prepareNotesFile{
		CompanyID:        "company-a",
		PayrollID:        "payroll-a",
		BatchID:          "batch-a",
		Denom:            "uclair",
		MaxMessagesPerTx: 20,
		Items: []payrollItemFile{{
			ItemID:           "item-1",
			EmployeeID:       "employee-1",
			RecipientAddress: testPayrollRecipientAddress(),
			Amount:           "70",
		}},
		TreasuryNotes: []treasuryNoteFile{
			{
				NoteID:             "large",
				OwnerKeyID:         "owner-a",
				NullifierLookupKey: "lookup-large",
				Denom:              "uclair",
				Amount:             "100",
			},
			{
				NoteID:             "zero",
				OwnerKeyID:         "owner-a",
				NullifierLookupKey: "lookup-zero",
				Denom:              "uclair",
				Amount:             "0",
			},
		},
	}
}

func writePayrollInput(t *testing.T, path string, payload prepareNotesFile) {
	t.Helper()
	bz, err := json.Marshal(payload)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, bz, 0o600))
}

func markConfirmedPlanSubmitted(t *testing.T, statePath string, plan privacypayroll.PayrollPlan) {
	t.Helper()
	ctx := context.Background()
	store, err := privacyreservation.OpenDurableFileStore(statePath)
	require.NoError(t, err)
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	svc := privacyreservation.Service{Store: store, Now: func() time.Time { return now }}

	refs := make([]privacyreservation.SubmittedReservationRef, 0, len(plan.Items[0].InputNotes))
	for _, note := range plan.Items[0].InputNotes {
		lease, err := svc.AcquireLeaseForStatus(ctx, note.ReservationID, "test-broadcaster", privacyreservation.StatusReserved, time.Minute)
		require.NoError(t, err)
		_, err = svc.TransitionWithLease(ctx, note.ReservationID, lease.Token, privacyreservation.StatusReserved, privacyreservation.StatusProving)
		require.NoError(t, err)
		_, err = svc.TransitionWithLease(ctx, note.ReservationID, lease.Token, privacyreservation.StatusProving, privacyreservation.StatusProofReady)
		require.NoError(t, err)
		refs = append(refs, privacyreservation.SubmittedReservationRef{
			ReservationID: note.ReservationID,
			LeaseToken:    lease.Token,
		})
	}
	_, _, err = svc.MarkSubmittedBatch(ctx, refs, nil, privacyreservation.SubmittedReservationUpdate{
		TxHash:          "txhash",
		TxBytesHash:     "tx-bytes",
		SignDocHash:     "sign-doc",
		AccountSequence: 7,
	})
	require.NoError(t, err)
}
