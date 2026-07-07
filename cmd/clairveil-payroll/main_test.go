package main

import (
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
	payload := prepareNotesFile{
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
	bz, err := json.Marshal(payload)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(inputPath, bz, 0o600))

	err = runPrepareNotes([]string{"-input", inputPath, "-out", outPath})
	require.NoError(t, err)

	reportBytes, err := os.ReadFile(outPath)
	require.NoError(t, err)
	var report privacypayroll.NotePreparationReport
	require.NoError(t, json.Unmarshal(reportBytes, &report))
	require.Equal(t, 1, report.ReadyItems)
	require.Equal(t, 0, report.BlockedItems)
	require.Equal(t, 1, report.EstimatedMessageChunks)
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
