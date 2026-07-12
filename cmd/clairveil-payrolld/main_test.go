package main

import (
	"context"
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	privacypayroll "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/payroll"
	privacyreservation "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/reservation"
)

const (
	testPayrolldRecipientA = "clairs19x5u4mf4l4zqcpvr7d809fh4tjy5j50p2mwgky0nj38jpqpj7svndu3hqshu5e3s8w6pea5p30xek5p9flxjf7f44xh7cnfrlsd84pc7upgh3"
)

func TestWriteJSONReplacesPermissiveMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "payrolld-private.json")
	require.NoError(t, os.WriteFile(path, []byte("old"), 0o644))
	require.NoError(t, os.Chmod(path, 0o644))

	require.NoError(t, writeJSON(path, map[string]string{"recipient": "private"}))

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

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

func TestRunLiveModeReconcilesSubmittedState(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	statePath := filepath.Join(dir, "reservation-state.json")
	planPath := filepath.Join(dir, "payroll-plan.json")
	txPath := filepath.Join(dir, "tx-query.json")
	reportPath := filepath.Join(dir, "payrolld-live-report.json")

	store, err := privacyreservation.OpenDurableFileStore(statePath)
	require.NoError(t, err)
	svc := privacypayroll.Service{Reservation: privacyreservation.Service{Store: store}}
	input := privacypayroll.PayrollInput{
		CompanyID: "company-live",
		PayrollID: "payroll-live",
		BatchID:   "run-001",
		Denom:     "uclair",
		Items: []privacypayroll.PayrollItemInput{{
			ItemID:                   "item-001",
			EmployeeID:               "employee-001",
			RecipientAddress:         testPayrolldRecipientA,
			Amount:                   big.NewInt(70),
			ExpectedOutputCommitment: "commitment-a",
			ExpectedDisclosureDigest: "digest-a",
		}},
	}
	plan, err := svc.CreatePlan(ctx, input, []privacypayroll.TreasuryNote{
		testPayrolldTreasuryNote("note-large", "uclair", 70),
		testPayrolldTreasuryNote("note-zero", "uclair", 0),
	})
	require.NoError(t, err)
	confirmed, err := svc.ConfirmPlan(ctx, *plan)
	require.NoError(t, err)
	writeJSONForPayrolldTest(t, planPath, confirmed)
	markPayrolldPlanSubmitted(t, ctx, store, *confirmed)
	writeJSONForPayrolldTest(t, txPath, map[string]any{
		"tx_response": map[string]any{
			"txhash": "txhash",
			"height": "9",
			"code":   0,
			"events": []map[string]any{{
				"type": "shielded_transfer",
				"attributes": []map[string]string{
					{"key": "nullifier_1", "value": "lookup-note-large"},
					{"key": "nullifier_2", "value": "lookup-note-zero"},
					{"key": "commitment_1", "value": "commitment-a"},
					{"key": "audit_disclosure_digest", "value": "digest-a"},
				},
			}},
		},
	})

	require.NoError(t, run([]string{"-mode", "live", "-state", statePath, "-plan", planPath, "-tx-query", txPath, "-once", "-out", reportPath}))
	reportBytes, err := os.ReadFile(reportPath)
	require.NoError(t, err)
	var report privacypayroll.ReferenceDaemonRunReport
	require.NoError(t, json.Unmarshal(reportBytes, &report))
	require.Equal(t, "live", report.Mode)
	require.Equal(t, 2, report.Reconciled)
	require.Equal(t, 0, report.RequiresReview)

	reloadedStore, err := privacyreservation.OpenDurableFileStore(statePath)
	require.NoError(t, err)
	operation, err := reloadedStore.GetOperation(ctx, confirmed.Items[0].OperationID)
	require.NoError(t, err)
	require.Equal(t, privacyreservation.OperationStatusSucceeded, operation.Status)
}

func TestRunRejectsMissingStatePath(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "missing-state.json")
	err := run([]string{"-state", statePath, "-once"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not exist")
}

func TestDaemonRunnerReopensStateEachTick(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	statePath := filepath.Join(dir, "reservation-state.json")
	store, err := privacyreservation.OpenDurableFileStore(statePath)
	require.NoError(t, err)
	svc := privacypayroll.Service{Reservation: privacyreservation.Service{Store: store}}
	input := privacypayroll.PayrollInput{
		CompanyID: "company-live",
		PayrollID: "payroll-live",
		BatchID:   "run-001",
		Denom:     "uclair",
		Items: []privacypayroll.PayrollItemInput{{
			ItemID:                   "item-001",
			EmployeeID:               "employee-001",
			RecipientAddress:         testPayrolldRecipientA,
			Amount:                   big.NewInt(70),
			ExpectedOutputCommitment: "commitment-a",
			ExpectedDisclosureDigest: "digest-a",
		}},
	}
	plan, err := svc.CreatePlan(ctx, input, []privacypayroll.TreasuryNote{
		testPayrolldTreasuryNote("note-large", "uclair", 70),
		testPayrolldTreasuryNote("note-zero", "uclair", 0),
	})
	require.NoError(t, err)
	confirmed, err := svc.ConfirmPlan(ctx, *plan)
	require.NoError(t, err)

	runner, err := buildDaemonRunner("simulated", statePath, "", "", "", "clairveil-payrolld-test", time.Minute, 0)
	require.NoError(t, err)
	externalStore, err := privacyreservation.OpenDurableFileStore(statePath)
	require.NoError(t, err)
	markPayrolldPlanSubmitted(t, ctx, externalStore, *confirmed)

	report, err := runner.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, report.ProofReady)
	require.Equal(t, 0, report.Submitted)
	require.Equal(t, 2, report.Reconciled)
	reloadedStore, err := privacyreservation.OpenDurableFileStore(statePath)
	require.NoError(t, err)
	operation, err := reloadedStore.GetOperation(ctx, confirmed.Items[0].OperationID)
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

func markPayrolldPlanSubmitted(t *testing.T, ctx context.Context, store privacyreservation.Store, plan privacypayroll.PayrollPlan) {
	t.Helper()
	svc := privacyreservation.Service{Store: store}
	refs := make([]privacyreservation.SubmittedReservationRef, 0, len(plan.Items[0].InputNotes))
	for _, note := range plan.Items[0].InputNotes {
		lease, err := svc.AcquireLeaseForStatus(ctx, note.ReservationID, "test-live-broadcaster", privacyreservation.StatusReserved, time.Minute)
		require.NoError(t, err)
		_, err = svc.TransitionWithLease(ctx, note.ReservationID, lease.Token, privacyreservation.StatusReserved, privacyreservation.StatusProving)
		require.NoError(t, err)
		refs = append(refs, privacyreservation.SubmittedReservationRef{ReservationID: note.ReservationID, LeaseToken: lease.Token})
	}
	_, _, err := svc.MarkProofReadyBatch(ctx, refs, privacyreservation.ProofReadyOperationUpdate{
		OperationID:                   plan.Items[0].OperationID,
		ExpectedOutputCommitment:      "commitment-a",
		ExpectedDisclosureDigest:      "digest-a",
		ExpectedAuditDisclosureDigest: "digest-a",
	})
	require.NoError(t, err)
	_, _, err = svc.MarkSubmittedBatch(ctx, refs, []string{plan.Items[0].OperationID}, privacyreservation.SubmittedReservationUpdate{
		TxHash:      "txhash",
		TxBytesHash: "tx-bytes",
		SignDocHash: "sign-doc",
	})
	require.NoError(t, err)
}

func writeJSONForPayrolldTest(t *testing.T, path string, payload any) {
	t.Helper()
	bz, err := json.Marshal(payload)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, bz, 0o600))
}
