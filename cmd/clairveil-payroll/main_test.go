package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	privacypayroll "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/payroll"
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
