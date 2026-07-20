package main

import (
	"context"
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"strings"
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

func TestWriteJSONOutputReplacesPermissiveMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "payroll-private.json")
	require.NoError(t, os.WriteFile(path, []byte("old"), 0o644))
	require.NoError(t, os.Chmod(path, 0o644))

	require.NoError(t, writeJSONOutput(path, map[string]string{"employee_id": "private"}))

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
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

func TestBuildInputFromNotesCommandImportsSpendableNotes(t *testing.T) {
	dir := t.TempDir()
	templatePath := filepath.Join(dir, "template.json")
	notesPath := filepath.Join(dir, "notes.json")
	outPath := filepath.Join(dir, "payroll.json")
	template := validPrepareNotesPayload()
	template.TreasuryNotes = nil
	writePayrollInput(t, templatePath, template)
	notes := listNotesFile{Notes: []listNotesFileNote{
		{Index: 1, Status: "spendable", Amount: "70", Nullifier: "lookup-70"},
		{Index: 2, Status: "spent", Amount: "10", Nullifier: "lookup-spent"},
		{Index: 3, Status: "spendable", Amount: "0", Nullifier: "lookup-zero"},
	}}
	bz, err := json.Marshal(notes)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(notesPath, bz, 0o600))

	require.NoError(t, runBuildInputFromNotes([]string{"-template", templatePath, "-notes", notesPath, "-owner-key-id", "owner-a", "-lookup-key-id", "lookup-v1", "-out", outPath}))
	outBytes, err := os.ReadFile(outPath)
	require.NoError(t, err)
	var payload prepareNotesFile
	require.NoError(t, json.Unmarshal(outBytes, &payload))
	require.Len(t, payload.TreasuryNotes, 2)
	require.Equal(t, "scan-001", payload.TreasuryNotes[0].NoteID)
	require.Equal(t, "lookup-70", payload.TreasuryNotes[0].NullifierLookupKey)
	require.Equal(t, "owner-a", payload.TreasuryNotes[0].OwnerKeyID)
	require.Equal(t, "lookup-v1", payload.TreasuryNotes[0].NullifierLookupKeyID)
}

func TestReadListNotesFileUsesCLITransactionHashField(t *testing.T) {
	dir := t.TempDir()
	notesPath := filepath.Join(dir, "notes.json")
	require.NoError(t, os.WriteFile(notesPath, []byte(`{
  "notes": [
    {
      "index": 1,
      "status": "spendable",
      "amount": "70",
      "nullifier": "recipient-note",
      "tx_hash": "LIVE_TX_HASH",
      "height": 9
    }
  ]
}`), 0o600))

	notes, err := readListNotesFileAtFlag(notesPath, "-notes")
	require.NoError(t, err)
	require.Len(t, notes.Notes, 1)
	require.Equal(t, "LIVE_TX_HASH", notes.Notes[0].TxHash)
	require.Equal(t, map[string]int{"70": 1}, spendableNoteCountsByAmountAndTxHash(notes, "LIVE_TX_HASH"))
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
	userDigest := strings.Repeat("0", 63) + "1"
	auditDigest := strings.Repeat("0", 63) + "2"
	selfViewDigest := strings.Repeat("0", 63) + "3"
	payload.Items[0].DisclosurePolicy = &disclosurePolicyFile{
		UserPrivacyPolicy:                "amount",
		UserDisclosureMode:               "public",
		ExpectedUserDisclosureDigest:     userDigest,
		ExpectedAuditDisclosureDigest:    auditDigest,
		ExpectedSelfViewDisclosureDigest: selfViewDigest,
	}
	payload.Items[0].ExpectedOutputCommitment = "commitment-a"
	payload.Items[0].ExpectedDisclosureDigest = auditDigest
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
	evidenceItems := make([]reconcileEvidenceItemFile, 0, len(confirmed.Items[0].InputNotes))
	for _, note := range confirmed.Items[0].InputNotes {
		evidenceItems = append(evidenceItems, reconcileEvidenceItemFile{
			ReservationID:            note.ReservationID,
			OperationID:              confirmed.Items[0].OperationID,
			TxHash:                   "txhash",
			OutputCommitment:         "commitment-a",
			DisclosureDigest:         auditDigest,
			UserDisclosureDigest:     userDigest,
			AuditDisclosureDigest:    auditDigest,
			SelfViewDisclosureDigest: selfViewDigest,
			RecipientHash:            confirmed.Items[0].ExpectedRecipientHash,
			AmountHash:               confirmed.Items[0].ExpectedAmountHash,
			Denom:                    "uclair",
			NullifierSpent:           true,
			BatchItemIndex:           0,
			BatchItemIndexKnown:      true,
			TxSucceeded:              true,
		})
	}
	evidence := reconcileEvidenceFile{Evidence: evidenceItems}
	bz, err := json.Marshal(evidence)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(evidencePath, bz, 0o600))

	require.NoError(t, runReconcile([]string{"-state", statePath, "-evidence", evidencePath, "-out", reconcilePath}))
	reconcileBytes, err := os.ReadFile(reconcilePath)
	require.NoError(t, err)
	var report reconcileReport
	require.NoError(t, json.Unmarshal(reconcileBytes, &report))
	require.Equal(t, 2, report.Total)
	require.Equal(t, 0, report.RequiresReview)
	require.Len(t, report.Results, 2)
	for _, item := range report.Results {
		require.Equal(t, privacyreservation.StatusConfirmedSpent, item.ReservationStatus)
		require.Equal(t, privacyreservation.OperationStatusSucceeded, item.OperationStatus)
	}

	require.NoError(t, runExportReport([]string{"-plan", planPath, "-state", statePath, "-out", exportPath}))
	exportBytes, err := os.ReadFile(exportPath)
	require.NoError(t, err)
	var exported payrollExportReport
	require.NoError(t, json.Unmarshal(exportBytes, &exported))
	require.Equal(t, privacypayroll.ItemStatusConfirmed, exported.Items[0].Status)
}

func TestScanEvidenceCommandAppliesTxObservation(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "payroll.json")
	planPath := filepath.Join(dir, "plan.json")
	confirmedPath := filepath.Join(dir, "confirmed-plan.json")
	statePath := filepath.Join(dir, "reservation-state.json")
	txPath := filepath.Join(dir, "tx-query.json")
	scanPath := filepath.Join(dir, "scan.json")
	payload := validPrepareNotesPayload()
	userDigest := strings.Repeat("0", 63) + "1"
	auditDigest := strings.Repeat("0", 63) + "2"
	selfViewDigest := strings.Repeat("0", 63) + "3"
	payload.Items[0].DisclosurePolicy = &disclosurePolicyFile{
		UserPrivacyPolicy:                "amount",
		UserDisclosureMode:               "public",
		ExpectedUserDisclosureDigest:     userDigest,
		ExpectedAuditDisclosureDigest:    auditDigest,
		ExpectedSelfViewDisclosureDigest: selfViewDigest,
	}
	payload.Items[0].ExpectedOutputCommitment = "commitment-a"
	payload.Items[0].ExpectedDisclosureDigest = auditDigest
	writePayrollInput(t, inputPath, payload)
	require.NoError(t, runPlan([]string{"-input", inputPath, "-out", planPath}))
	require.NoError(t, runRun([]string{"-plan", planPath, "-state", statePath, "-out", confirmedPath}))

	confirmedBytes, err := os.ReadFile(confirmedPath)
	require.NoError(t, err)
	var confirmed privacypayroll.PayrollPlan
	require.NoError(t, json.Unmarshal(confirmedBytes, &confirmed))
	markConfirmedPlanSubmitted(t, statePath, confirmed)
	writeJSONForTest(t, txPath, map[string]any{
		"tx_response": map[string]any{
			"txhash": "txhash",
			"height": "10",
			"code":   0,
			"events": []map[string]any{{
				"type": "shielded_transfer",
				"attributes": []map[string]string{
					{"key": "nullifier_1", "value": "lookup-large"},
					{"key": "nullifier_2", "value": "lookup-zero"},
					{"key": "commitment_1", "value": "commitment-a"},
					{"key": "user_disclosure_digest", "value": userDigest},
					{"key": "audit_disclosure_digest", "value": auditDigest},
					{"key": "self_view_disclosure_digest", "value": selfViewDigest},
				},
			}},
		},
	})

	require.NoError(t, runScanEvidence([]string{"-plan", planPath, "-state", statePath, "-tx-query", txPath, "-apply", "-out", scanPath}))
	scanBytes, err := os.ReadFile(scanPath)
	require.NoError(t, err)
	var scan scanEvidenceReport
	require.NoError(t, json.Unmarshal(scanBytes, &scan))
	require.True(t, scan.TxSucceeded)
	require.Len(t, scan.Evidence, 2)
	require.Equal(t, userDigest, scan.Evidence[0].UserDisclosureDigest)
	require.Equal(t, auditDigest, scan.Evidence[0].AuditDisclosureDigest)
	require.Equal(t, selfViewDigest, scan.Evidence[0].SelfViewDisclosureDigest)
	require.NotNil(t, scan.Reconcile)
	require.Equal(t, 0, scan.Reconcile.RequiresReview)

	store, err := privacyreservation.OpenDurableFileStore(statePath)
	require.NoError(t, err)
	operation, err := store.GetOperation(context.Background(), confirmed.Items[0].OperationID)
	require.NoError(t, err)
	require.Equal(t, privacyreservation.OperationStatusSucceeded, operation.Status)
}

func TestReconcileEvidenceFilePreservesConfirmedUnspentNullifier(t *testing.T) {
	encoded, err := json.Marshal(reconcileEvidenceFile{Evidence: []reconcileEvidenceItemFile{{
		ReservationID:             "reservation-1",
		OperationID:               "operation-1",
		TxFailed:                  true,
		TxKnown:                   true,
		NullifierUnspentConfirmed: true,
	}}})
	require.NoError(t, err)

	var decoded reconcileEvidenceFile
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	require.Len(t, decoded.Evidence, 1)
	require.True(t, decoded.Evidence[0].toSDK().NullifierUnspentConfirmed)
}

func TestSelectPlanItemsForSettlementSupportsChunkRanges(t *testing.T) {
	plan := privacypayroll.PayrollPlan{
		Items: []privacypayroll.PayrollPlanItem{
			{ItemID: "item-1", Amount: big.NewInt(1), Denom: "uclair"},
			{ItemID: "item-2", Amount: big.NewInt(2), Denom: "uclair"},
			{ItemID: "item-3", Amount: big.NewInt(3), Denom: "uclair"},
		},
	}

	items, err := selectPlanItemsForSettlement(plan, 1, 1)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, "item-2", items[0].ItemID)

	err = validateTransferBatchResult(items, transferBatchResultFile{
		TxHash:       "txhash",
		MessageCount: 1,
		Amounts:      []string{"2uclair"},
		Items: []transferBatchResultItemFile{{
			Amount:                "2uclair",
			Nullifiers:            []string{"lookup-2"},
			OutputCommitment:      "commitment-2",
			AuditDisclosureDigest: "audit-2",
		}},
	})
	require.NoError(t, err)
}

func TestValidateSettlementRecipientScopeRejectsMixedRecipients(t *testing.T) {
	items := []privacypayroll.PayrollPlanItem{
		{RecipientAddress: testPayrollRecipientAddress()},
		{RecipientAddress: "clairs1uwnerzqjukcmg56pqwe509jmfvsvdnd8j4k657d8c839f5rthu0q6d9yk3vda5wyhmvggjgkj94axzegkchypz0h3nx577vw3th7lpq7mwjed"},
	}

	require.Error(t, validateSettlementRecipientScope(items))
}

func TestSettleTransferBatchRequiresRecipientDeltaEvidence(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "payroll.json")
	planPath := filepath.Join(dir, "plan.json")
	confirmedPath := filepath.Join(dir, "confirmed-plan.json")
	statePath := filepath.Join(dir, "reservation-state.json")
	txPath := filepath.Join(dir, "transfer-batch.json")
	payload := validPrepareNotesPayload()
	payload.TreasuryNotes[0].Amount = "70"
	writePayrollInput(t, inputPath, payload)
	require.NoError(t, runPlan([]string{"-input", inputPath, "-out", planPath}))
	require.NoError(t, runRun([]string{"-plan", planPath, "-state", statePath, "-out", confirmedPath}))
	confirmedBytes, err := os.ReadFile(confirmedPath)
	require.NoError(t, err)
	var confirmed privacypayroll.PayrollPlan
	require.NoError(t, json.Unmarshal(confirmedBytes, &confirmed))
	writeJSONForTest(t, txPath, settlementTxResultForItem("LIVE_TX_HASH", confirmed.Items[0], "settle-commitment", "settle-audit"))

	err = runSettleTransferBatch([]string{
		"-plan", planPath,
		"-state", statePath,
		"-tx", txPath,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "-recipient-before")
}

func TestSettleTransferBatchCommandConfirmsDurableState(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "payroll.json")
	planPath := filepath.Join(dir, "plan.json")
	confirmedPath := filepath.Join(dir, "confirmed-plan.json")
	statePath := filepath.Join(dir, "reservation-state.json")
	txPath := filepath.Join(dir, "transfer-batch.json")
	beforePath := filepath.Join(dir, "recipient-before.json")
	afterPath := filepath.Join(dir, "recipient-after.json")
	settlePath := filepath.Join(dir, "settle.json")
	exportPath := filepath.Join(dir, "export.json")
	payload := validPrepareNotesPayload()
	payload.TreasuryNotes[0].Amount = "70"
	writePayrollInput(t, inputPath, payload)
	require.NoError(t, runPlan([]string{"-input", inputPath, "-out", planPath}))
	require.NoError(t, runRun([]string{"-plan", planPath, "-state", statePath, "-out", confirmedPath}))
	confirmedBytes, err := os.ReadFile(confirmedPath)
	require.NoError(t, err)
	var confirmed privacypayroll.PayrollPlan
	require.NoError(t, json.Unmarshal(confirmedBytes, &confirmed))

	writeJSONForTest(t, txPath, settlementTxResultForItem("LIVE_TX_HASH", confirmed.Items[0], "settle-commitment", "settle-audit"))
	writeJSONForTest(t, beforePath, listNotesFile{Notes: []listNotesFileNote{}})
	writeJSONForTest(t, afterPath, listNotesFile{Notes: []listNotesFileNote{{Index: 1, Status: "spendable", Amount: "70", Nullifier: "recipient-note", TxHash: "live_tx_hash"}}})

	require.NoError(t, runSettleTransferBatch([]string{
		"-plan", planPath,
		"-state", statePath,
		"-tx", txPath,
		"-recipient-before", beforePath,
		"-recipient-after", afterPath,
		"-out", settlePath,
	}))
	settleBytes, err := os.ReadFile(settlePath)
	require.NoError(t, err)
	var settle settleTransferBatchReport
	require.NoError(t, json.Unmarshal(settleBytes, &settle))
	require.Equal(t, "LIVE_TX_HASH", settle.TxHash)
	require.Equal(t, 2, settle.TotalReservations)
	require.Equal(t, 0, settle.RequiresReview)

	require.NoError(t, runExportReport([]string{"-plan", planPath, "-state", statePath, "-out", exportPath}))
	exportBytes, err := os.ReadFile(exportPath)
	require.NoError(t, err)
	var exported payrollExportReport
	require.NoError(t, json.Unmarshal(exportBytes, &exported))
	require.Equal(t, privacypayroll.PlanStatusConfirmed, exported.Status)
	require.Equal(t, privacypayroll.ItemStatusConfirmed, exported.Items[0].Status)

	require.NoError(t, runSettleTransferBatch([]string{
		"-plan", planPath,
		"-state", statePath,
		"-tx", txPath,
		"-recipient-before", beforePath,
		"-recipient-after", afterPath,
		"-out", settlePath,
	}))
	settleBytes, err = os.ReadFile(settlePath)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(settleBytes, &settle))
	require.Equal(t, 2, settle.TotalReservations)
	require.Equal(t, 0, settle.RequiresReview)
}

func TestSettleTransferBatchRejectsRecipientDeltaFromDifferentTx(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "payroll.json")
	planPath := filepath.Join(dir, "plan.json")
	confirmedPath := filepath.Join(dir, "confirmed-plan.json")
	statePath := filepath.Join(dir, "reservation-state.json")
	txPath := filepath.Join(dir, "transfer-batch.json")
	beforePath := filepath.Join(dir, "recipient-before.json")
	afterPath := filepath.Join(dir, "recipient-after.json")
	payload := validPrepareNotesPayload()
	payload.TreasuryNotes[0].Amount = "70"
	writePayrollInput(t, inputPath, payload)
	require.NoError(t, runPlan([]string{"-input", inputPath, "-out", planPath}))
	require.NoError(t, runRun([]string{"-plan", planPath, "-state", statePath, "-out", confirmedPath}))
	confirmedBytes, err := os.ReadFile(confirmedPath)
	require.NoError(t, err)
	var confirmed privacypayroll.PayrollPlan
	require.NoError(t, json.Unmarshal(confirmedBytes, &confirmed))

	writeJSONForTest(t, txPath, settlementTxResultForItem("LIVE_TX_HASH", confirmed.Items[0], "settle-commitment", "settle-audit"))
	writeJSONForTest(t, beforePath, listNotesFile{Notes: []listNotesFileNote{}})
	writeJSONForTest(t, afterPath, listNotesFile{Notes: []listNotesFileNote{{Index: 1, Status: "spendable", Amount: "70", Nullifier: "recipient-note", TxHash: "OTHER_TX_HASH"}}})

	err = runSettleTransferBatch([]string{
		"-plan", planPath,
		"-state", statePath,
		"-tx", txPath,
		"-recipient-before", beforePath,
		"-recipient-after", afterPath,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "LIVE_TX_HASH")
}

func TestSettleTransferBatchRejectsMismatchedNullifierEvidence(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "payroll.json")
	planPath := filepath.Join(dir, "plan.json")
	confirmedPath := filepath.Join(dir, "confirmed-plan.json")
	statePath := filepath.Join(dir, "reservation-state.json")
	txPath := filepath.Join(dir, "transfer-batch.json")
	payload := validPrepareNotesPayload()
	payload.TreasuryNotes[0].Amount = "70"
	writePayrollInput(t, inputPath, payload)
	require.NoError(t, runPlan([]string{"-input", inputPath, "-out", planPath}))
	require.NoError(t, runRun([]string{"-plan", planPath, "-state", statePath, "-out", confirmedPath}))
	confirmedBytes, err := os.ReadFile(confirmedPath)
	require.NoError(t, err)
	var confirmed privacypayroll.PayrollPlan
	require.NoError(t, json.Unmarshal(confirmedBytes, &confirmed))
	tx := settlementTxResultForItem("LIVE_TX_HASH", confirmed.Items[0], "settle-commitment", "settle-audit")
	tx.Items[0].Nullifiers = []string{"different-nullifier"}
	writeJSONForTest(t, txPath, tx)

	err = runSettleTransferBatch([]string{
		"-plan", planPath,
		"-state", statePath,
		"-tx", txPath,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing nullifier evidence")

	store, err := privacyreservation.OpenDurableFileStore(statePath)
	require.NoError(t, err)
	for _, note := range confirmed.Items[0].InputNotes {
		reservation, err := store.GetReservation(context.Background(), note.ReservationID)
		require.NoError(t, err)
		require.Equal(t, privacyreservation.StatusReserved, reservation.Status)
	}
}

func TestSettleTransferBatchResumesProofReadyAndSubmittedState(t *testing.T) {
	for _, tc := range []struct {
		name   string
		submit bool
	}{
		{name: "proof ready", submit: false},
		{name: "submitted", submit: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			inputPath := filepath.Join(dir, "payroll.json")
			planPath := filepath.Join(dir, "plan.json")
			confirmedPath := filepath.Join(dir, "confirmed-plan.json")
			statePath := filepath.Join(dir, "reservation-state.json")
			txPath := filepath.Join(dir, "transfer-batch.json")
			beforePath := filepath.Join(dir, "recipient-before.json")
			afterPath := filepath.Join(dir, "recipient-after.json")
			settlePath := filepath.Join(dir, "settle.json")
			payload := validPrepareNotesPayload()
			payload.TreasuryNotes[0].Amount = "70"
			writePayrollInput(t, inputPath, payload)
			require.NoError(t, runPlan([]string{"-input", inputPath, "-out", planPath}))
			require.NoError(t, runRun([]string{"-plan", planPath, "-state", statePath, "-out", confirmedPath}))
			confirmedBytes, err := os.ReadFile(confirmedPath)
			require.NoError(t, err)
			var confirmed privacypayroll.PayrollPlan
			require.NoError(t, json.Unmarshal(confirmedBytes, &confirmed))
			tx := settlementTxResultForItem("LIVE_TX_HASH", confirmed.Items[0], "settle-commitment", "settle-audit")
			writeJSONForTest(t, txPath, tx)
			markConfirmedPlanProofReadyForSettlement(t, statePath, confirmed, tx, tc.submit)
			writeJSONForTest(t, beforePath, listNotesFile{Notes: []listNotesFileNote{}})
			writeJSONForTest(t, afterPath, listNotesFile{Notes: []listNotesFileNote{{Index: 1, Status: "spendable", Amount: "70", Nullifier: "recipient-note", TxHash: "LIVE_TX_HASH"}}})

			require.NoError(t, runSettleTransferBatch([]string{
				"-plan", planPath,
				"-state", statePath,
				"-tx", txPath,
				"-recipient-before", beforePath,
				"-recipient-after", afterPath,
				"-out", settlePath,
			}))
			settleBytes, err := os.ReadFile(settlePath)
			require.NoError(t, err)
			var settle settleTransferBatchReport
			require.NoError(t, json.Unmarshal(settleBytes, &settle))
			require.Equal(t, 2, settle.TotalReservations)
			require.Equal(t, 0, settle.RequiresReview)
		})
	}
}

func TestSettleTransferBatchRollsBackProvingOnProofReadyFailure(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "payroll.json")
	planPath := filepath.Join(dir, "plan.json")
	confirmedPath := filepath.Join(dir, "confirmed-plan.json")
	statePath := filepath.Join(dir, "reservation-state.json")
	txPath := filepath.Join(dir, "transfer-batch.json")
	beforePath := filepath.Join(dir, "recipient-before.json")
	afterPath := filepath.Join(dir, "recipient-after.json")
	payload := validPrepareNotesPayload()
	payload.TreasuryNotes[0].Amount = "70"
	payload.Items[0].ExpectedOutputCommitment = "expected-commitment"
	writePayrollInput(t, inputPath, payload)
	require.NoError(t, runPlan([]string{"-input", inputPath, "-out", planPath}))
	require.NoError(t, runRun([]string{"-plan", planPath, "-state", statePath, "-out", confirmedPath}))

	confirmedBytes, err := os.ReadFile(confirmedPath)
	require.NoError(t, err)
	var confirmed privacypayroll.PayrollPlan
	require.NoError(t, json.Unmarshal(confirmedBytes, &confirmed))
	writeJSONForTest(t, txPath, settlementTxResultForItem("LIVE_TX_HASH", confirmed.Items[0], "different-commitment", "settle-audit"))
	writeJSONForTest(t, beforePath, listNotesFile{Notes: []listNotesFileNote{}})
	writeJSONForTest(t, afterPath, listNotesFile{Notes: []listNotesFileNote{{Index: 1, Status: "spendable", Amount: "70", Nullifier: "recipient-note", TxHash: "LIVE_TX_HASH"}}})

	err = runSettleTransferBatch([]string{
		"-plan", planPath,
		"-state", statePath,
		"-tx", txPath,
		"-recipient-before", beforePath,
		"-recipient-after", afterPath,
	})
	require.Error(t, err)

	store, err := privacyreservation.OpenDurableFileStore(statePath)
	require.NoError(t, err)
	for _, note := range confirmed.Items[0].InputNotes {
		reservation, err := store.GetReservation(context.Background(), note.ReservationID)
		require.NoError(t, err)
		require.Equal(t, privacyreservation.StatusReserved, reservation.Status)
	}
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
	writeJSONForTest(t, path, payload)
}

func writeJSONForTest(t *testing.T, path string, payload any) {
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

func settlementTxResultForItem(txHash string, item privacypayroll.PayrollPlanItem, outputCommitment string, auditDigest string) transferBatchResultFile {
	nullifiers := make([]string, 0, len(item.InputNotes))
	for _, note := range item.InputNotes {
		nullifiers = append(nullifiers, note.NullifierLookupKey)
	}
	amount := payrollItemCoinString(item)
	return transferBatchResultFile{
		TxHash:       txHash,
		Code:         0,
		MessageCount: 1,
		Amounts:      []string{amount},
		Items: []transferBatchResultItemFile{{
			Amount:                   amount,
			Nullifiers:               nullifiers,
			OutputCommitment:         outputCommitment,
			AuditDisclosureDigest:    auditDigest,
			SelfViewDisclosureDigest: item.DisclosurePolicy.ExpectedSelfViewDisclosureDigest,
		}},
	}
}

func markConfirmedPlanProofReadyForSettlement(t *testing.T, statePath string, plan privacypayroll.PayrollPlan, tx transferBatchResultFile, submit bool) {
	t.Helper()
	ctx := context.Background()
	store, err := privacyreservation.OpenDurableFileStore(statePath)
	require.NoError(t, err)
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	svc := privacyreservation.Service{Store: store, Now: func() time.Time { return now }}
	item := plan.Items[0]
	txItem := tx.Items[0]
	refs := make([]privacyreservation.SubmittedReservationRef, 0, len(item.InputNotes))
	for _, note := range item.InputNotes {
		lease, err := svc.AcquireLeaseForStatus(ctx, note.ReservationID, "test-proof-worker", privacyreservation.StatusReserved, time.Minute)
		require.NoError(t, err)
		_, err = svc.TransitionWithLease(ctx, note.ReservationID, lease.Token, privacyreservation.StatusReserved, privacyreservation.StatusProving)
		require.NoError(t, err)
		refs = append(refs, privacyreservation.SubmittedReservationRef{
			ReservationID: note.ReservationID,
			LeaseToken:    lease.Token,
		})
	}
	_, _, err = svc.MarkProofReadyBatch(ctx, refs, privacyreservation.ProofReadyOperationUpdate{
		OperationID:                      item.OperationID,
		ExpectedOutputCommitment:         txItem.OutputCommitment,
		ExpectedDisclosureDigest:         txItem.AuditDisclosureDigest,
		ExpectedAuditDisclosureDigest:    txItem.AuditDisclosureDigest,
		ExpectedSelfViewDisclosureDigest: txItem.SelfViewDisclosureDigest,
	})
	require.NoError(t, err)
	if submit {
		_, _, err = svc.MarkSubmittedBatch(ctx, refs, []string{item.OperationID}, privacyreservation.SubmittedReservationUpdate{TxHash: tx.TxHash})
		require.NoError(t, err)
	}
}
