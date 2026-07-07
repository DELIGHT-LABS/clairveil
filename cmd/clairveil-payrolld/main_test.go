package main

import (
	"context"
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	privacypayroll "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/payroll"
	privacyreservation "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/reservation"
)

const (
	testPayrolldRecipientA = "clairs19x5u4mf4l4zqcpvr7d809fh4tjy5j50p2mwgky0nj38jpqpj7svndu3hqshu5e3s8w6pea5p30xek5p9flxjf7f44xh7cnfrlsd84pc7upgh3"
)

func TestRunOnceCompletesDurableState(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	statePath := filepath.Join(dir, "reservation-state.json")
	reportPath := filepath.Join(dir, "payrolld-report.json")

	store, err := privacyreservation.OpenDurableFileStore(statePath)
	require.NoError(t, err)
	svc := privacypayroll.Service{Reservation: privacyreservation.Service{Store: store}}
	input := privacypayroll.PayrollInput{
		CompanyID: "company-demo",
		PayrollID: "payroll-demo",
		BatchID:   "run-001",
		Denom:     "uclair",
		Items: []privacypayroll.PayrollItemInput{{
			ItemID:           "item-001",
			EmployeeID:       "employee-001",
			RecipientAddress: testPayrolldRecipientA,
			Amount:           big.NewInt(70),
		}},
	}
	plan, err := svc.CreatePlan(ctx, input, []privacypayroll.TreasuryNote{
		testPayrolldTreasuryNote("note-large", "uclair", 70),
		testPayrolldTreasuryNote("note-zero", "uclair", 0),
	})
	require.NoError(t, err)
	_, err = svc.ConfirmPlan(ctx, *plan)
	require.NoError(t, err)

	require.NoError(t, run([]string{"-state", statePath, "-once", "-out", reportPath}))
	reportBytes, err := os.ReadFile(reportPath)
	require.NoError(t, err)
	var report privacypayroll.ReferenceDaemonRunReport
	require.NoError(t, json.Unmarshal(reportBytes, &report))
	require.Equal(t, 1, report.ProofReady)
	require.Equal(t, 1, report.Submitted)
	require.Equal(t, 2, report.Reconciled)
	require.Equal(t, 0, report.RequiresReview)

	reloadedStore, err := privacyreservation.OpenDurableFileStore(statePath)
	require.NoError(t, err)
	operation, err := reloadedStore.GetOperation(ctx, plan.Items[0].OperationID)
	require.NoError(t, err)
	require.Equal(t, privacyreservation.OperationStatusSucceeded, operation.Status)
}

func testPayrolldTreasuryNote(id string, denom string, amount int64) privacypayroll.TreasuryNote {
	return privacypayroll.TreasuryNote{
		NoteID:               id,
		OwnerKeyID:           "treasury-key",
		NullifierLookupKey:   "lookup-" + id,
		NullifierLookupKeyID: "lookup-v1",
		Denom:                denom,
		Amount:               big.NewInt(amount),
	}
}
