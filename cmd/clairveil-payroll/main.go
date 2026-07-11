package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"

	privacypayroll "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/payroll"
	privacyreservation "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/reservation"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

type prepareNotesFile struct {
	CompanyID               string               `json:"company_id"`
	PayrollID               string               `json:"payroll_id"`
	BatchID                 string               `json:"batch_id"`
	Denom                   string               `json:"denom"`
	MaxMessagesPerTx        int                  `json:"max_messages_per_tx,omitempty"`
	DefaultDisclosurePolicy disclosurePolicyFile `json:"default_disclosure_policy,omitempty"`
	Items                   []payrollItemFile    `json:"items"`
	TreasuryNotes           []treasuryNoteFile   `json:"treasury_notes"`
}

type payrollItemFile struct {
	ItemID                   string                `json:"item_id"`
	EmployeeID               string                `json:"employee_id,omitempty"`
	RecipientAddress         string                `json:"recipient_address"`
	Amount                   string                `json:"amount"`
	Denom                    string                `json:"denom,omitempty"`
	DisclosurePolicy         *disclosurePolicyFile `json:"disclosure_policy,omitempty"`
	ExpectedOutputCommitment string                `json:"expected_output_commitment,omitempty"`
	ExpectedDisclosureDigest string                `json:"expected_disclosure_digest,omitempty"`
}

type treasuryNoteFile struct {
	NoteID               string `json:"note_id"`
	OwnerKeyID           string `json:"owner_key_id"`
	NullifierLookupKey   string `json:"nullifier_lookup_key"`
	NullifierLookupKeyID string `json:"nullifier_lookup_key_id,omitempty"`
	Denom                string `json:"denom"`
	Amount               string `json:"amount"`
	IsSpent              bool   `json:"is_spent,omitempty"`
	ReservationID        string `json:"reservation_id,omitempty"`
}

type disclosurePolicyFile struct {
	UserPrivacyPolicy                string `json:"user_privacy_policy,omitempty"`
	UserDisclosureMode               string `json:"user_disclosure_mode,omitempty"`
	UserDisclosureTargetPubKeyHex    string `json:"user_disclosure_target_pubkey_hex,omitempty"`
	UserDisclosureTargetKeyID        string `json:"user_disclosure_target_key_id,omitempty"`
	ExpectedUserDisclosureDigest     string `json:"expected_user_disclosure_digest,omitempty"`
	ExpectedAuditDisclosureDigest    string `json:"expected_audit_disclosure_digest,omitempty"`
	ExpectedSelfViewDisclosureDigest string `json:"expected_self_view_disclosure_digest,omitempty"`
}

type validationReport struct {
	Valid           bool                                  `json:"valid"`
	Errors          []string                              `json:"errors,omitempty"`
	Warnings        []string                              `json:"warnings,omitempty"`
	NotePreparation *privacypayroll.NotePreparationReport `json:"note_preparation,omitempty"`
}

type payrollExportReport struct {
	GeneratedAt time.Time                 `json:"generated_at"`
	PayrollID   string                    `json:"payroll_id"`
	BatchID     string                    `json:"batch_id"`
	Status      privacypayroll.PlanStatus `json:"status"`
	Summary     privacypayroll.PlanReport `json:"summary"`
	Items       []payrollExportItem       `json:"items"`
}

type payrollExportItem struct {
	ItemID        string                    `json:"item_id"`
	EmployeeID    string                    `json:"employee_id,omitempty"`
	OperationID   string                    `json:"operation_id"`
	ChunkID       string                    `json:"chunk_id"`
	Status        privacypayroll.ItemStatus `json:"status"`
	Amount        string                    `json:"amount"`
	Denom         string                    `json:"denom"`
	FailureReason string                    `json:"failure_reason,omitempty"`
	RetryCount    int                       `json:"retry_count,omitempty"`
}

type reconcileEvidenceFile struct {
	Evidence []reconcileEvidenceItemFile `json:"evidence"`
}

type reconcileEvidenceItemFile struct {
	ReservationID            string `json:"reservation_id"`
	TxHash                   string `json:"tx_hash,omitempty"`
	SignDocHash              string `json:"sign_doc_hash,omitempty"`
	TxBytesHash              string `json:"tx_bytes_hash,omitempty"`
	OutputCommitment         string `json:"output_commitment,omitempty"`
	DisclosureDigest         string `json:"disclosure_digest,omitempty"`
	UserDisclosureDigest     string `json:"user_disclosure_digest,omitempty"`
	AuditDisclosureDigest    string `json:"audit_disclosure_digest,omitempty"`
	SelfViewDisclosureDigest string `json:"self_view_disclosure_digest,omitempty"`
	RecipientHash            string `json:"recipient_hash,omitempty"`
	AmountHash               string `json:"amount_hash,omitempty"`
	Denom                    string `json:"denom,omitempty"`
	BatchItemIndex           int    `json:"batch_item_index,omitempty"`
	BatchItemIndexKnown      bool   `json:"batch_item_index_known,omitempty"`
	NullifierSpent           bool   `json:"nullifier_spent,omitempty"`
	TxSucceeded              bool   `json:"tx_succeeded,omitempty"`
	TxFailed                 bool   `json:"tx_failed,omitempty"`
	TxKnown                  bool   `json:"tx_known,omitempty"`
}

type reconcileReport struct {
	Total          int                   `json:"total"`
	RequiresReview int                   `json:"requires_review"`
	Results        []reconcileItemReport `json:"results"`
}

type scanEvidenceReport struct {
	TxHash              string                      `json:"tx_hash,omitempty"`
	TxKnown             bool                        `json:"tx_known"`
	TxSucceeded         bool                        `json:"tx_succeeded"`
	TxFailed            bool                        `json:"tx_failed"`
	ObservedEvents      int                         `json:"observed_events"`
	ScannedReservations int                         `json:"scanned_reservations"`
	Warnings            []string                    `json:"warnings,omitempty"`
	Evidence            []reconcileEvidenceItemFile `json:"evidence"`
	Reconcile           *reconcileReport            `json:"reconcile,omitempty"`
}

type reconcileItemReport struct {
	ReservationID     string                               `json:"reservation_id"`
	ReservationStatus privacyreservation.ReservationStatus `json:"reservation_status"`
	OperationStatus   privacyreservation.OperationStatus   `json:"operation_status"`
	RequiresReview    bool                                 `json:"requires_review"`
	Reason            string                               `json:"reason"`
}

type stateStatusReport struct {
	ReservationTotal     int            `json:"reservation_total"`
	OperationTotal       int            `json:"operation_total"`
	ReservationsByStatus map[string]int `json:"reservations_by_status"`
	OperationsByStatus   map[string]int `json:"operations_by_status"`
}

type listNotesFile struct {
	Notes []listNotesFileNote `json:"notes"`
}

type listNotesFileNote struct {
	Index     int    `json:"index"`
	Status    string `json:"status"`
	Amount    string `json:"amount"`
	Nullifier string `json:"nullifier,omitempty"`
	TxHash    string `json:"tx_hash,omitempty"`
	Height    int64  `json:"height,omitempty"`
}

type transferBatchResultFile struct {
	TxHash       string                        `json:"txhash"`
	Height       int64                         `json:"height"`
	Code         uint32                        `json:"code"`
	RawLog       string                        `json:"raw_log,omitempty"`
	MessageCount int                           `json:"message_count"`
	Amounts      []string                      `json:"amounts"`
	Items        []transferBatchResultItemFile `json:"items,omitempty"`
}

type transferBatchResultItemFile struct {
	Amount                   string   `json:"amount,omitempty"`
	Nullifiers               []string `json:"nullifiers,omitempty"`
	OutputCommitment         string   `json:"output_commitment,omitempty"`
	UserDisclosureDigest     string   `json:"user_disclosure_digest,omitempty"`
	AuditDisclosureDigest    string   `json:"audit_disclosure_digest,omitempty"`
	SelfViewDisclosureDigest string   `json:"self_view_disclosure_digest,omitempty"`
}

type settleTransferBatchReport struct {
	TxHash                  string                `json:"tx_hash"`
	MessageCount            int                   `json:"message_count"`
	VerifiedRecipientDeltas map[string]int        `json:"verified_recipient_deltas,omitempty"`
	TotalReservations       int                   `json:"total_reservations"`
	RequiresReview          int                   `json:"requires_review"`
	Results                 []reconcileItemReport `json:"results"`
}

func main() {
	if len(os.Args) < 2 {
		fatalf("usage: clairveil-payroll <validate|build-input-from-notes|prepare-notes|plan|run|status|scan-evidence|reconcile|settle-transfer-batch|seed-localnet-notes|export-report> [flags]")
	}
	switch os.Args[1] {
	case "validate":
		if err := runValidate(os.Args[2:]); err != nil {
			fatalf("%v", err)
		}
	case "build-input-from-notes":
		if err := runBuildInputFromNotes(os.Args[2:]); err != nil {
			fatalf("%v", err)
		}
	case "prepare-notes":
		if err := runPrepareNotes(os.Args[2:]); err != nil {
			fatalf("%v", err)
		}
	case "plan":
		if err := runPlan(os.Args[2:]); err != nil {
			fatalf("%v", err)
		}
	case "run":
		if err := runRun(os.Args[2:]); err != nil {
			fatalf("%v", err)
		}
	case "status":
		if err := runStatus(os.Args[2:]); err != nil {
			fatalf("%v", err)
		}
	case "reconcile":
		if err := runReconcile(os.Args[2:]); err != nil {
			fatalf("%v", err)
		}
	case "scan-evidence":
		if err := runScanEvidence(os.Args[2:]); err != nil {
			fatalf("%v", err)
		}
	case "settle-transfer-batch":
		if err := runSettleTransferBatch(os.Args[2:]); err != nil {
			fatalf("%v", err)
		}
	case "seed-localnet-notes":
		if err := runSeedLocalnetNotes(os.Args[2:]); err != nil {
			fatalf("%v", err)
		}
	case "export-report":
		if err := runExportReport(os.Args[2:]); err != nil {
			fatalf("%v", err)
		}
	default:
		fatalf("unknown command %q", os.Args[1])
	}
}

func runValidate(args []string) error {
	flags := flag.NewFlagSet("validate", flag.ContinueOnError)
	var inputPath string
	var outPath string
	flags.StringVar(&inputPath, "input", "", "payroll input JSON path")
	flags.StringVar(&outPath, "out", "", "optional output JSON path; stdout when empty")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(inputPath) == "" {
		return fmt.Errorf("-input is required")
	}

	input, err := readPrepareNotesInput(inputPath)
	if err != nil {
		return err
	}
	report := validationReport{Valid: true}
	if err := privacypayroll.ValidateInput(input.PayrollInput); err != nil {
		report.Valid = false
		report.Errors = append(report.Errors, err.Error())
	} else {
		preparation, err := privacypayroll.AnalyzeNotePreparation(input)
		if err != nil {
			report.Valid = false
			report.Errors = append(report.Errors, err.Error())
		} else {
			report.NotePreparation = preparation
			if preparation.BlockedItems > 0 {
				report.Warnings = append(report.Warnings, fmt.Sprintf("%d payroll items need note preparation before planning", preparation.BlockedItems))
			}
		}
	}
	if err := writeJSONOutput(outPath, report); err != nil {
		return err
	}
	if !report.Valid {
		return fmt.Errorf("payroll input validation failed")
	}
	return nil
}

func runBuildInputFromNotes(args []string) error {
	flags := flag.NewFlagSet("build-input-from-notes", flag.ContinueOnError)
	var templatePath string
	var notesPath string
	var ownerKeyID string
	var lookupKeyID string
	var outPath string
	flags.StringVar(&templatePath, "template", "", "payroll input template JSON path without treasury_notes")
	flags.StringVar(&notesPath, "notes", "", "clairveild tx privacy list-notes --json output path")
	flags.StringVar(&ownerKeyID, "owner-key-id", "treasury-key", "owner key id to attach to imported notes")
	flags.StringVar(&lookupKeyID, "lookup-key-id", "localnet-scan", "nullifier lookup key id to attach to imported notes")
	flags.StringVar(&outPath, "out", "", "optional output JSON path; stdout when empty")
	if err := flags.Parse(args); err != nil {
		return err
	}
	template, err := readPrepareNotesFile(templatePath)
	if err != nil {
		return err
	}
	notes, err := readListNotesFile(notesPath)
	if err != nil {
		return err
	}
	template.TreasuryNotes = treasuryNotesFromListNotes(notes, template.Denom, ownerKeyID, lookupKeyID)
	if len(template.TreasuryNotes) == 0 {
		return fmt.Errorf("no spendable notes found in %s", notesPath)
	}
	return writeJSONOutput(outPath, template)
}

func runPrepareNotes(args []string) error {
	flags := flag.NewFlagSet("prepare-notes", flag.ContinueOnError)
	var inputPath string
	var outPath string
	var storeDir string
	var artifactID string
	flags.StringVar(&inputPath, "input", "", "payroll preparation input JSON path")
	flags.StringVar(&outPath, "out", "", "optional output JSON path; stdout when empty")
	flags.StringVar(&storeDir, "store-dir", "", "optional file artifact store directory")
	flags.StringVar(&artifactID, "artifact-id", "", "optional artifact id for -store-dir")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(inputPath) == "" {
		return fmt.Errorf("-input is required")
	}

	input, err := readPrepareNotesInput(inputPath)
	if err != nil {
		return err
	}
	report, err := privacypayroll.AnalyzeNotePreparation(input)
	if err != nil {
		return err
	}
	if strings.TrimSpace(storeDir) != "" {
		if strings.TrimSpace(artifactID) == "" {
			artifactID = defaultPayrollArtifactID(input.PayrollInput)
		}
		if _, err := (privacypayroll.FileArtifactStore{Dir: storeDir}).WriteNotePreparationReport(context.Background(), artifactID, *report); err != nil {
			return err
		}
	}
	return writeJSONOutput(outPath, report)
}

func runPlan(args []string) error {
	flags := flag.NewFlagSet("plan", flag.ContinueOnError)
	var inputPath string
	var outPath string
	var storeDir string
	var artifactID string
	flags.StringVar(&inputPath, "input", "", "payroll input JSON path")
	flags.StringVar(&outPath, "out", "", "optional output plan JSON path; stdout when empty")
	flags.StringVar(&storeDir, "store-dir", "", "optional file artifact store directory")
	flags.StringVar(&artifactID, "artifact-id", "", "optional artifact id for -store-dir")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(inputPath) == "" {
		return fmt.Errorf("-input is required")
	}

	input, err := readPrepareNotesInput(inputPath)
	if err != nil {
		return err
	}
	plan, err := (privacypayroll.Service{}).CreatePlan(context.Background(), input.PayrollInput, input.TreasuryNotes)
	if err != nil {
		return err
	}
	if strings.TrimSpace(storeDir) != "" {
		if strings.TrimSpace(artifactID) == "" {
			artifactID = defaultPayrollArtifactID(input.PayrollInput)
		}
		if _, err := (privacypayroll.FileArtifactStore{Dir: storeDir}).WritePayrollPlan(context.Background(), artifactID, *plan); err != nil {
			return err
		}
	}
	return writeJSONOutput(outPath, plan)
}

func runRun(args []string) error {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	var planPath string
	var statePath string
	var outPath string
	flags.StringVar(&planPath, "plan", "", "payroll plan JSON path")
	flags.StringVar(&statePath, "state", "", "durable reservation state JSON path")
	flags.StringVar(&outPath, "out", "", "optional confirmed plan JSON path; stdout when empty")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(statePath) == "" {
		return fmt.Errorf("-state is required")
	}
	plan, err := readPlanInput(planPath)
	if err != nil {
		return err
	}
	store, err := privacyreservation.OpenDurableFileStore(statePath)
	if err != nil {
		return err
	}
	ctx := context.Background()
	if loaded, ok, err := loadConfirmedPlanFromState(ctx, store, *plan); err != nil {
		return err
	} else if ok {
		return writeJSONOutput(outPath, loaded)
	}

	confirmed, err := (privacypayroll.Service{
		Reservation: privacyreservation.Service{Store: store},
	}).ConfirmPlan(ctx, *plan)
	if err != nil {
		return err
	}
	return writeJSONOutput(outPath, confirmed)
}

func runStatus(args []string) error {
	flags := flag.NewFlagSet("status", flag.ContinueOnError)
	var planPath string
	var statePath string
	var outPath string
	flags.StringVar(&planPath, "plan", "", "payroll plan JSON path")
	flags.StringVar(&statePath, "state", "", "durable reservation state JSON path")
	flags.StringVar(&outPath, "out", "", "optional output JSON path; stdout when empty")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(statePath) != "" {
		store, err := privacyreservation.OpenDurableFileStore(statePath)
		if err != nil {
			return err
		}
		report, err := buildStateStatusReport(context.Background(), store)
		if err != nil {
			return err
		}
		return writeJSONOutput(outPath, report)
	}
	plan, err := readPlanInput(planPath)
	if err != nil {
		return err
	}
	report := privacypayroll.BuildPlanReport(*plan)
	return writeJSONOutput(outPath, report)
}

func runReconcile(args []string) error {
	flags := flag.NewFlagSet("reconcile", flag.ContinueOnError)
	var statePath string
	var evidencePath string
	var outPath string
	flags.StringVar(&statePath, "state", "", "durable reservation state JSON path")
	flags.StringVar(&evidencePath, "evidence", "", "reconcile evidence JSON path")
	flags.StringVar(&outPath, "out", "", "optional reconcile report JSON path; stdout when empty")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(statePath) == "" {
		return fmt.Errorf("-state is required")
	}
	evidence, err := readReconcileEvidence(evidencePath)
	if err != nil {
		return err
	}
	store, err := privacyreservation.OpenDurableFileStore(statePath)
	if err != nil {
		return err
	}
	report, err := reconcileEvidenceItems(context.Background(), store, evidence)
	if err != nil {
		return err
	}
	return writeJSONOutput(outPath, report)
}

func runScanEvidence(args []string) error {
	flags := flag.NewFlagSet("scan-evidence", flag.ContinueOnError)
	var planPath string
	var statePath string
	var txPath string
	var nullifiersPath string
	var outPath string
	var apply bool
	flags.StringVar(&planPath, "plan", "", "payroll plan JSON path")
	flags.StringVar(&statePath, "state", "", "durable reservation state JSON path")
	flags.StringVar(&txPath, "tx-query", "", "clairveild query tx JSON path or TxObservation JSON path")
	flags.StringVar(&nullifiersPath, "nullifiers", "", "optional nullifier status JSON path")
	flags.StringVar(&outPath, "out", "", "optional scan evidence report JSON path; stdout when empty")
	flags.BoolVar(&apply, "apply", false, "also reconcile scanned evidence into the durable state")
	if err := flags.Parse(args); err != nil {
		return err
	}
	plan, err := readPlanInput(planPath)
	if err != nil {
		return err
	}
	if strings.TrimSpace(statePath) == "" {
		return fmt.Errorf("-state is required")
	}
	tx, err := readTxObservation(txPath)
	if err != nil {
		return err
	}
	nullifiers, err := readNullifierStatuses(nullifiersPath)
	if err != nil {
		return err
	}
	store, err := privacyreservation.OpenDurableFileStore(statePath)
	if err != nil {
		return err
	}
	scanned, err := (privacypayroll.EvidenceScanner{Store: store}).ScanTransferBatch(context.Background(), *plan, tx, nullifiers)
	if err != nil {
		return err
	}
	report := scanEvidenceReport{
		TxHash:              scanned.TxHash,
		TxKnown:             scanned.TxKnown,
		TxSucceeded:         scanned.TxSucceeded,
		TxFailed:            scanned.TxFailed,
		ObservedEvents:      scanned.ObservedEvents,
		ScannedReservations: scanned.ScannedReservations,
		Warnings:            scanned.Warnings,
		Evidence:            make([]reconcileEvidenceItemFile, 0, len(scanned.Evidence)),
	}
	for _, item := range scanned.Evidence {
		report.Evidence = append(report.Evidence, reconcileEvidenceItemFromSDK(item.ReservationID, item.Evidence))
	}
	if apply {
		reconciled, err := reconcileEvidenceItems(context.Background(), store, report.Evidence)
		if err != nil {
			return err
		}
		report.Reconcile = reconciled
	}
	return writeJSONOutput(outPath, report)
}

func runSettleTransferBatch(args []string) error {
	flags := flag.NewFlagSet("settle-transfer-batch", flag.ContinueOnError)
	var planPath string
	var statePath string
	var txPath string
	var recipientBeforePath string
	var recipientAfterPath string
	var outPath string
	var leaseOwner string
	var leaseTTL time.Duration
	var itemStart int
	var itemLimit int
	flags.StringVar(&planPath, "plan", "", "payroll plan JSON path")
	flags.StringVar(&statePath, "state", "", "durable reservation state JSON path")
	flags.StringVar(&txPath, "tx", "", "transfer-batch command JSON output path")
	flags.StringVar(&recipientBeforePath, "recipient-before", "", "recipient list-notes JSON before transfer-batch")
	flags.StringVar(&recipientAfterPath, "recipient-after", "", "recipient list-notes JSON after transfer-batch")
	flags.StringVar(&outPath, "out", "", "optional settle report JSON path; stdout when empty")
	flags.StringVar(&leaseOwner, "lease-owner", "clairveil-payroll-live-settle", "reservation lease owner used while settling")
	flags.DurationVar(&leaseTTL, "lease-ttl", time.Minute, "reservation lease ttl used while settling")
	flags.IntVar(&itemStart, "item-start", 0, "zero-based payroll plan item offset for this transfer-batch tx")
	flags.IntVar(&itemLimit, "item-limit", 0, "maximum number of payroll plan items to settle; 0 means all remaining items")
	if err := flags.Parse(args); err != nil {
		return err
	}
	plan, err := readPlanInput(planPath)
	if err != nil {
		return err
	}
	if strings.TrimSpace(statePath) == "" {
		return fmt.Errorf("-state is required")
	}
	txResult, err := readTransferBatchResult(txPath)
	if err != nil {
		return err
	}
	items, err := selectPlanItemsForSettlement(*plan, itemStart, itemLimit)
	if err != nil {
		return err
	}
	if err := validateSettlementRecipientScope(items); err != nil {
		return err
	}
	if err := validateTransferBatchResult(items, txResult); err != nil {
		return err
	}
	verifiedDeltas, err := verifyRecipientNoteDeltas(items, txResult.TxHash, recipientBeforePath, recipientAfterPath)
	if err != nil {
		return err
	}
	store, err := privacyreservation.OpenDurableFileStore(statePath)
	if err != nil {
		return err
	}
	service := privacyreservation.Service{Store: store}
	report := settleTransferBatchReport{
		TxHash:                  txResult.TxHash,
		MessageCount:            txResult.MessageCount,
		VerifiedRecipientDeltas: verifiedDeltas,
		Results:                 make([]reconcileItemReport, 0),
	}
	for relativeIndex, item := range items {
		itemIndex := itemStart + relativeIndex
		results, err := settleTransferBatchItem(context.Background(), service, item, relativeIndex, itemIndex, txResult, leaseOwner, leaseTTL)
		if err != nil {
			return err
		}
		for _, result := range results {
			report.TotalReservations++
			if result.RequiresReview {
				report.RequiresReview++
			}
			report.Results = append(report.Results, result)
		}
	}
	return writeJSONOutput(outPath, report)
}

func runExportReport(args []string) error {
	flags := flag.NewFlagSet("export-report", flag.ContinueOnError)
	var planPath string
	var statePath string
	var outPath string
	flags.StringVar(&planPath, "plan", "", "payroll plan JSON path")
	flags.StringVar(&statePath, "state", "", "optional durable reservation state JSON path")
	flags.StringVar(&outPath, "out", "", "optional output JSON path; stdout when empty")
	if err := flags.Parse(args); err != nil {
		return err
	}
	plan, err := readPlanInput(planPath)
	if err != nil {
		return err
	}
	if strings.TrimSpace(statePath) != "" {
		store, err := privacyreservation.OpenDurableFileStore(statePath)
		if err != nil {
			return err
		}
		if statePlan, ok, err := loadConfirmedPlanFromState(context.Background(), store, *plan); err != nil {
			return err
		} else if ok {
			plan = statePlan
		}
	}
	return writeJSONOutput(outPath, buildPayrollExportReport(*plan, time.Now().UTC()))
}

func readPrepareNotesInput(path string) (privacypayroll.NotePreparationInput, error) {
	payload, err := readPrepareNotesFile(path)
	if err != nil {
		return privacypayroll.NotePreparationInput{}, err
	}
	return payload.toSDK()
}

func readPrepareNotesFile(path string) (prepareNotesFile, error) {
	if strings.TrimSpace(path) == "" {
		return prepareNotesFile{}, fmt.Errorf("-input is required")
	}
	bz, err := os.ReadFile(path)
	if err != nil {
		return prepareNotesFile{}, err
	}
	var payload prepareNotesFile
	if err := json.Unmarshal(bz, &payload); err != nil {
		return prepareNotesFile{}, err
	}
	return payload, nil
}

func readListNotesFile(path string) (listNotesFile, error) {
	if strings.TrimSpace(path) == "" {
		return listNotesFile{}, fmt.Errorf("-notes is required")
	}
	bz, err := os.ReadFile(path)
	if err != nil {
		return listNotesFile{}, err
	}
	var payload listNotesFile
	if err := json.Unmarshal(bz, &payload); err != nil {
		return listNotesFile{}, err
	}
	return payload, nil
}

func readTransferBatchResult(path string) (transferBatchResultFile, error) {
	if strings.TrimSpace(path) == "" {
		return transferBatchResultFile{}, fmt.Errorf("-tx is required")
	}
	bz, err := os.ReadFile(path)
	if err != nil {
		return transferBatchResultFile{}, err
	}
	var payload transferBatchResultFile
	if err := json.Unmarshal(bz, &payload); err != nil {
		return transferBatchResultFile{}, err
	}
	payload.TxHash = strings.TrimSpace(payload.TxHash)
	return payload, nil
}

func readPlanInput(path string) (*privacypayroll.PayrollPlan, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("-plan is required")
	}
	bz, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var plan privacypayroll.PayrollPlan
	if err := json.Unmarshal(bz, &plan); err != nil {
		return nil, err
	}
	return &plan, nil
}

func readReconcileEvidence(path string) ([]reconcileEvidenceItemFile, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("-evidence is required")
	}
	bz, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var payload reconcileEvidenceFile
	if err := json.Unmarshal(bz, &payload); err == nil && len(payload.Evidence) > 0 {
		return payload.Evidence, nil
	}
	var items []reconcileEvidenceItemFile
	if err := json.Unmarshal(bz, &items); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("reconcile evidence is empty")
	}
	return items, nil
}

func readTxObservation(path string) (privacypayroll.TxObservation, error) {
	if strings.TrimSpace(path) == "" {
		return privacypayroll.TxObservation{}, fmt.Errorf("-tx-query is required")
	}
	bz, err := os.ReadFile(path)
	if err != nil {
		return privacypayroll.TxObservation{}, err
	}
	return privacypayroll.ParseTxObservationJSON(bz)
}

func readNullifierStatuses(path string) ([]privacypayroll.NullifierStatus, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	bz, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var wrapped struct {
		Statuses []privacypayroll.NullifierStatus `json:"statuses"`
	}
	if err := json.Unmarshal(bz, &wrapped); err == nil && len(wrapped.Statuses) > 0 {
		return wrapped.Statuses, nil
	}
	var statuses []privacypayroll.NullifierStatus
	if err := json.Unmarshal(bz, &statuses); err == nil && len(statuses) > 0 {
		return statuses, nil
	}
	return nil, fmt.Errorf("nullifier status file is empty or invalid")
}

func (e reconcileEvidenceItemFile) toSDK() privacyreservation.OperationEvidence {
	return privacyreservation.OperationEvidence{
		TxHash:                   e.TxHash,
		SignDocHash:              e.SignDocHash,
		TxBytesHash:              e.TxBytesHash,
		OutputCommitment:         e.OutputCommitment,
		DisclosureDigest:         e.DisclosureDigest,
		UserDisclosureDigest:     e.UserDisclosureDigest,
		AuditDisclosureDigest:    e.AuditDisclosureDigest,
		SelfViewDisclosureDigest: e.SelfViewDisclosureDigest,
		RecipientHash:            e.RecipientHash,
		AmountHash:               e.AmountHash,
		Denom:                    e.Denom,
		BatchItemIndex:           e.BatchItemIndex,
		BatchItemIndexKnown:      e.BatchItemIndexKnown,
		NullifierSpent:           e.NullifierSpent,
		TxSucceeded:              e.TxSucceeded,
		TxFailed:                 e.TxFailed,
		TxKnown:                  e.TxKnown,
	}
}

func reconcileEvidenceItemFromSDK(reservationID string, evidence privacyreservation.OperationEvidence) reconcileEvidenceItemFile {
	return reconcileEvidenceItemFile{
		ReservationID:            reservationID,
		TxHash:                   evidence.TxHash,
		SignDocHash:              evidence.SignDocHash,
		TxBytesHash:              evidence.TxBytesHash,
		OutputCommitment:         evidence.OutputCommitment,
		DisclosureDigest:         evidence.DisclosureDigest,
		UserDisclosureDigest:     evidence.UserDisclosureDigest,
		AuditDisclosureDigest:    evidence.AuditDisclosureDigest,
		SelfViewDisclosureDigest: evidence.SelfViewDisclosureDigest,
		RecipientHash:            evidence.RecipientHash,
		AmountHash:               evidence.AmountHash,
		Denom:                    evidence.Denom,
		BatchItemIndex:           evidence.BatchItemIndex,
		BatchItemIndexKnown:      evidence.BatchItemIndexKnown,
		NullifierSpent:           evidence.NullifierSpent,
		TxSucceeded:              evidence.TxSucceeded,
		TxFailed:                 evidence.TxFailed,
		TxKnown:                  evidence.TxKnown,
	}
}

func reconcileEvidenceItems(ctx context.Context, store privacyreservation.Store, evidence []reconcileEvidenceItemFile) (*reconcileReport, error) {
	report := &reconcileReport{
		Total:   len(evidence),
		Results: make([]reconcileItemReport, 0, len(evidence)),
	}
	worker := privacypayroll.ReconcileWorker{
		Reservation: privacyreservation.Service{Store: store},
	}
	for _, item := range evidence {
		result, err := worker.ReconcileReservation(ctx, item.ReservationID, item.toSDK())
		if err != nil {
			return nil, err
		}
		if result.RequiresReview {
			report.RequiresReview++
		}
		report.Results = append(report.Results, reconcileItemReport{
			ReservationID:     item.ReservationID,
			ReservationStatus: result.ReservationStatus,
			OperationStatus:   result.OperationStatus,
			RequiresReview:    result.RequiresReview,
			Reason:            result.Reason,
		})
	}
	return report, nil
}

func writeJSONOutput(path string, value any) error {
	bz, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	bz = append(bz, '\n')
	if strings.TrimSpace(path) == "" {
		_, err = os.Stdout.Write(bz)
		return err
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	return os.WriteFile(path, bz, 0o600)
}

func loadConfirmedPlanFromState(ctx context.Context, store privacyreservation.Store, plan privacypayroll.PayrollPlan) (*privacypayroll.PayrollPlan, bool, error) {
	if len(plan.Items) == 0 {
		return nil, false, nil
	}
	confirmed := plan
	confirmed.Status = privacypayroll.PlanStatusConfirmed
	for itemIndex := range confirmed.Items {
		item := &confirmed.Items[itemIndex]
		if len(item.InputNotes) == 0 {
			return nil, false, nil
		}
		itemStatus := privacypayroll.ItemStatusReserved
		for noteIndex := range item.InputNotes {
			note := &item.InputNotes[noteIndex]
			reservationID := note.ReservationID
			if reservationID == "" {
				reservationID = privacypayroll.ReservationIDForInputNote(item.OperationID, note.NoteID)
			}
			reservation, err := store.GetReservation(ctx, reservationID)
			if err != nil {
				if errors.Is(err, privacyreservation.ErrReservationNotFound) {
					return nil, false, nil
				}
				return nil, false, err
			}
			if reservation.OperationID != item.OperationID {
				return nil, false, fmt.Errorf("reservation %s belongs to operation %s, not %s", reservationID, reservation.OperationID, item.OperationID)
			}
			note.ReservationID = reservation.ReservationID
			itemStatus = mergeItemStatus(itemStatus, itemStatusFromReservation(reservation.Status))
		}
		item.Status = itemStatus
	}
	return &confirmed, true, nil
}

func mergeItemStatus(left privacypayroll.ItemStatus, right privacypayroll.ItemStatus) privacypayroll.ItemStatus {
	if itemStatusRank(right) > itemStatusRank(left) {
		return right
	}
	return left
}

func itemStatusRank(status privacypayroll.ItemStatus) int {
	switch status {
	case privacypayroll.ItemStatusManualReview:
		return 90
	case privacypayroll.ItemStatusFailed, privacypayroll.ItemStatusReplanRequired:
		return 80
	case privacypayroll.ItemStatusConfirmed:
		return 70
	case privacypayroll.ItemStatusSubmitted:
		return 60
	case privacypayroll.ItemStatusProofReady:
		return 50
	case privacypayroll.ItemStatusProving:
		return 40
	case privacypayroll.ItemStatusReserved:
		return 30
	default:
		return 10
	}
}

func itemStatusFromReservation(status privacyreservation.ReservationStatus) privacypayroll.ItemStatus {
	switch status {
	case privacyreservation.StatusReserved:
		return privacypayroll.ItemStatusReserved
	case privacyreservation.StatusProving:
		return privacypayroll.ItemStatusProving
	case privacyreservation.StatusProofReady:
		return privacypayroll.ItemStatusProofReady
	case privacyreservation.StatusSubmitted, privacyreservation.StatusUnknown:
		return privacypayroll.ItemStatusSubmitted
	case privacyreservation.StatusConfirmedSpent:
		return privacypayroll.ItemStatusConfirmed
	case privacyreservation.StatusFailed:
		return privacypayroll.ItemStatusFailed
	case privacyreservation.StatusReplanRequired:
		return privacypayroll.ItemStatusReplanRequired
	case privacyreservation.StatusManualReview:
		return privacypayroll.ItemStatusManualReview
	default:
		return privacypayroll.ItemStatusPlanned
	}
}

func buildStateStatusReport(ctx context.Context, store *privacyreservation.DurableFileStore) (*stateStatusReport, error) {
	snapshot, err := store.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	report := &stateStatusReport{
		ReservationTotal:     len(snapshot.Reservations),
		OperationTotal:       len(snapshot.Operations),
		ReservationsByStatus: make(map[string]int),
		OperationsByStatus:   make(map[string]int),
	}
	for _, reservation := range snapshot.Reservations {
		report.ReservationsByStatus[string(reservation.Status)]++
	}
	for _, operation := range snapshot.Operations {
		report.OperationsByStatus[string(operation.Status)]++
	}
	return report, nil
}

func treasuryNotesFromListNotes(notes listNotesFile, denom string, ownerKeyID string, lookupKeyID string) []treasuryNoteFile {
	out := make([]treasuryNoteFile, 0, len(notes.Notes))
	for _, note := range notes.Notes {
		if strings.TrimSpace(note.Status) != "spendable" {
			continue
		}
		amount := strings.TrimSpace(note.Amount)
		if amount == "" {
			continue
		}
		lookupKey := strings.TrimSpace(note.Nullifier)
		if lookupKey == "" {
			lookupKey = fmt.Sprintf("tx:%s:index:%d:amount:%s", strings.TrimSpace(note.TxHash), note.Index, amount)
		}
		noteID := fmt.Sprintf("scan-%03d", len(out)+1)
		if note.Index > 0 {
			noteID = fmt.Sprintf("scan-%03d", note.Index)
		}
		out = append(out, treasuryNoteFile{
			NoteID:               noteID,
			OwnerKeyID:           strings.TrimSpace(ownerKeyID),
			NullifierLookupKey:   lookupKey,
			NullifierLookupKeyID: strings.TrimSpace(lookupKeyID),
			Denom:                strings.TrimSpace(denom),
			Amount:               amount,
		})
	}
	return out
}

func selectPlanItemsForSettlement(plan privacypayroll.PayrollPlan, itemStart int, itemLimit int) ([]privacypayroll.PayrollPlanItem, error) {
	if itemStart < 0 {
		return nil, fmt.Errorf("-item-start must be non-negative")
	}
	if itemLimit < 0 {
		return nil, fmt.Errorf("-item-limit must be non-negative")
	}
	if itemStart > len(plan.Items) {
		return nil, fmt.Errorf("-item-start %d exceeds payroll item count %d", itemStart, len(plan.Items))
	}
	itemEnd := len(plan.Items)
	if itemLimit > 0 && itemStart+itemLimit < itemEnd {
		itemEnd = itemStart + itemLimit
	}
	if itemStart == itemEnd {
		return nil, fmt.Errorf("selected payroll item range is empty")
	}
	return plan.Items[itemStart:itemEnd], nil
}

func validateTransferBatchResult(items []privacypayroll.PayrollPlanItem, tx transferBatchResultFile) error {
	if tx.TxHash == "" {
		return fmt.Errorf("transfer-batch result has no txhash")
	}
	if tx.Code != 0 {
		return fmt.Errorf("transfer-batch tx %s failed with code %d: %s", tx.TxHash, tx.Code, tx.RawLog)
	}
	if tx.MessageCount != len(items) {
		return fmt.Errorf("transfer-batch message_count %d does not match selected payroll item count %d", tx.MessageCount, len(items))
	}
	if len(tx.Amounts) != len(items) {
		return fmt.Errorf("transfer-batch amount count %d does not match selected payroll item count %d", len(tx.Amounts), len(items))
	}
	if len(tx.Items) != len(items) {
		return fmt.Errorf("transfer-batch item evidence count %d does not match selected payroll item count %d", len(tx.Items), len(items))
	}
	for i, item := range items {
		expected := payrollItemCoinString(item)
		if tx.Amounts[i] != expected {
			return fmt.Errorf("transfer-batch amount %d is %s, expected %s", i, tx.Amounts[i], expected)
		}
		if err := validateTransferBatchItemEvidence(item, tx.Items[i], i); err != nil {
			return err
		}
	}
	return nil
}

func validateSettlementRecipientScope(items []privacypayroll.PayrollPlanItem) error {
	if len(items) == 0 {
		return nil
	}
	recipient := strings.TrimSpace(items[0].RecipientAddress)
	for i, item := range items {
		if strings.TrimSpace(item.RecipientAddress) != recipient {
			return fmt.Errorf("settle-transfer-batch selected item %d recipient differs from the first selected item; split mixed-recipient batches before settling", i)
		}
	}
	return nil
}

func verifyRecipientNoteDeltas(items []privacypayroll.PayrollPlanItem, txHash string, beforePath string, afterPath string) (map[string]int, error) {
	if strings.TrimSpace(beforePath) == "" || strings.TrimSpace(afterPath) == "" {
		return nil, fmt.Errorf("both -recipient-before and -recipient-after are required for settle-transfer-batch recipient verification")
	}
	txHash = strings.TrimSpace(txHash)
	if txHash == "" {
		return nil, fmt.Errorf("recipient verification requires transfer-batch txhash")
	}
	before, err := readListNotesFileAtFlag(beforePath, "-recipient-before")
	if err != nil {
		return nil, err
	}
	after, err := readListNotesFileAtFlag(afterPath, "-recipient-after")
	if err != nil {
		return nil, err
	}
	required := make(map[string]int)
	for _, item := range items {
		required[payrollItemAmountString(item)]++
	}
	beforeCounts := spendableNoteCountsByAmountAndTxHash(before, txHash)
	afterCounts := spendableNoteCountsByAmountAndTxHash(after, txHash)
	verified := make(map[string]int, len(required))
	for amount, count := range required {
		delta := afterCounts[amount] - beforeCounts[amount]
		if delta < count {
			return nil, fmt.Errorf("recipient note delta for amount %s from tx %s is %d, expected at least %d", amount, txHash, delta, count)
		}
		verified[amount] = delta
	}
	return verified, nil
}

func readListNotesFileAtFlag(path string, flagName string) (listNotesFile, error) {
	if strings.TrimSpace(path) == "" {
		return listNotesFile{}, fmt.Errorf("%s is required", flagName)
	}
	bz, err := os.ReadFile(path)
	if err != nil {
		return listNotesFile{}, err
	}
	var payload listNotesFile
	if err := json.Unmarshal(bz, &payload); err != nil {
		return listNotesFile{}, err
	}
	return payload, nil
}

func spendableNoteCountsByAmount(notes listNotesFile) map[string]int {
	counts := make(map[string]int)
	for _, note := range notes.Notes {
		if strings.TrimSpace(note.Status) != "spendable" {
			continue
		}
		amount := strings.TrimSpace(note.Amount)
		if amount == "" {
			continue
		}
		counts[amount]++
	}
	return counts
}

func spendableNoteCountsByAmountAndTxHash(notes listNotesFile, txHash string) map[string]int {
	counts := make(map[string]int)
	txHash = strings.TrimSpace(txHash)
	for _, note := range notes.Notes {
		if strings.TrimSpace(note.Status) != "spendable" {
			continue
		}
		// Cosmos RPC commonly renders tx hashes in uppercase while the typed
		// privacy scanner stores the same hex bytes in lowercase.
		if !strings.EqualFold(strings.TrimSpace(note.TxHash), txHash) {
			continue
		}
		amount := strings.TrimSpace(note.Amount)
		if amount == "" {
			continue
		}
		counts[amount]++
	}
	return counts
}

func settleTransferBatchItem(ctx context.Context, service privacyreservation.Service, item privacypayroll.PayrollPlanItem, txItemIndex int, itemIndex int, tx transferBatchResultFile, leaseOwner string, leaseTTL time.Duration) (results []reconcileItemReport, runErr error) {
	if len(item.InputNotes) == 0 {
		return nil, fmt.Errorf("payroll item %s has no input notes", item.ItemID)
	}
	if leaseTTL <= 0 {
		leaseTTL = time.Minute
	}
	evidence, err := settlementEvidenceForItem(item, txItemIndex, itemIndex, tx)
	if err != nil {
		return nil, err
	}
	refs := make([]privacyreservation.SubmittedReservationRef, 0, len(item.InputNotes))
	provingRefs := make([]privacyreservation.SubmittedReservationRef, 0, len(item.InputNotes))
	submissionRefs := make([]privacyreservation.SubmittedReservationRef, 0, len(item.InputNotes))
	rollbackRequired := true
	defer func() {
		if !rollbackRequired || len(provingRefs) == 0 {
			return
		}
		if rollbackErr := rollbackProvingRefs(ctx, service, provingRefs); rollbackErr != nil {
			runErr = errors.Join(runErr, rollbackErr)
		}
	}()
	for _, note := range item.InputNotes {
		reservationID := note.ReservationID
		if reservationID == "" {
			reservationID = privacypayroll.ReservationIDForInputNote(item.OperationID, note.NoteID)
		}
		reservation, err := service.Store.GetReservation(ctx, reservationID)
		if err != nil {
			return nil, err
		}
		switch reservation.Status {
		case privacyreservation.StatusConfirmedSpent:
			refs = append(refs, privacyreservation.SubmittedReservationRef{ReservationID: reservationID})
		case privacyreservation.StatusReserved:
			lease, err := service.AcquireLeaseForStatus(ctx, reservationID, leaseOwner, privacyreservation.StatusReserved, leaseTTL)
			if err != nil {
				return nil, err
			}
			if _, err := service.TransitionWithLease(ctx, reservationID, lease.Token, privacyreservation.StatusReserved, privacyreservation.StatusProving); err != nil {
				return nil, err
			}
			ref := privacyreservation.SubmittedReservationRef{ReservationID: reservationID, LeaseToken: lease.Token}
			refs = append(refs, ref)
			provingRefs = append(provingRefs, ref)
		case privacyreservation.StatusProving:
			lease, err := service.AcquireLeaseForStatus(ctx, reservationID, leaseOwner, privacyreservation.StatusProving, leaseTTL)
			if err != nil {
				return nil, err
			}
			ref := privacyreservation.SubmittedReservationRef{ReservationID: reservationID, LeaseToken: lease.Token}
			refs = append(refs, ref)
			provingRefs = append(provingRefs, ref)
		case privacyreservation.StatusProofReady:
			lease, err := service.AcquireLeaseForStatus(ctx, reservationID, leaseOwner, privacyreservation.StatusProofReady, leaseTTL)
			if err != nil {
				return nil, err
			}
			ref := privacyreservation.SubmittedReservationRef{ReservationID: reservationID, LeaseToken: lease.Token}
			refs = append(refs, ref)
			submissionRefs = append(submissionRefs, ref)
		case privacyreservation.StatusSubmitted, privacyreservation.StatusUnknown:
			refs = append(refs, privacyreservation.SubmittedReservationRef{ReservationID: reservationID})
		default:
			return nil, fmt.Errorf("reservation %s has status %s; settle-transfer-batch can only resume Reserved, Proving, ProofReady, Submitted, Unknown, or ConfirmedSpent", reservationID, reservation.Status)
		}
	}
	if len(refs) == 0 {
		return nil, nil
	}
	if len(provingRefs) > 0 {
		if _, _, err := service.MarkProofReadyBatch(ctx, provingRefs, privacyreservation.ProofReadyOperationUpdate{
			OperationID:                      item.OperationID,
			ExpectedOutputCommitment:         evidence.OutputCommitment,
			ExpectedDisclosureDigest:         evidence.AuditDisclosureDigest,
			ExpectedUserDisclosureDigest:     evidence.UserDisclosureDigest,
			ExpectedAuditDisclosureDigest:    evidence.AuditDisclosureDigest,
			ExpectedSelfViewDisclosureDigest: evidence.SelfViewDisclosureDigest,
		}); err != nil {
			return nil, err
		}
		submissionRefs = append(submissionRefs, provingRefs...)
		rollbackRequired = false
	}
	if len(submissionRefs) > 0 {
		if _, _, err := service.MarkSubmittedBatch(ctx, submissionRefs, []string{item.OperationID}, privacyreservation.SubmittedReservationUpdate{
			TxHash: tx.TxHash,
		}); err != nil {
			return nil, errors.Join(err, clearSettlementLeases(ctx, service, submissionRefs))
		}
	}
	return reconcileSettlementRefs(ctx, service, refs, evidence)
}

func reconcileSettlementRefs(ctx context.Context, service privacyreservation.Service, refs []privacyreservation.SubmittedReservationRef, evidence privacyreservation.OperationEvidence) ([]reconcileItemReport, error) {
	results := make([]reconcileItemReport, 0, len(refs))
	worker := privacypayroll.ReconcileWorker{Reservation: service}
	for _, ref := range refs {
		result, err := worker.ReconcileReservation(ctx, ref.ReservationID, evidence)
		if err != nil {
			return nil, err
		}
		results = append(results, reconcileItemReport{
			ReservationID:     ref.ReservationID,
			ReservationStatus: result.ReservationStatus,
			OperationStatus:   result.OperationStatus,
			RequiresReview:    result.RequiresReview,
			Reason:            result.Reason,
		})
	}
	return results, nil
}

func settlementEvidenceForItem(item privacypayroll.PayrollPlanItem, txItemIndex int, itemIndex int, tx transferBatchResultFile) (privacyreservation.OperationEvidence, error) {
	if txItemIndex < 0 || txItemIndex >= len(tx.Items) {
		return privacyreservation.OperationEvidence{}, fmt.Errorf("transfer-batch item evidence %d is missing", txItemIndex)
	}
	txItem := tx.Items[txItemIndex]
	if err := validateTransferBatchItemEvidence(item, txItem, txItemIndex); err != nil {
		return privacyreservation.OperationEvidence{}, err
	}
	auditDigest := strings.TrimSpace(txItem.AuditDisclosureDigest)
	return privacyreservation.OperationEvidence{
		TxHash:                   tx.TxHash,
		OutputCommitment:         strings.TrimSpace(txItem.OutputCommitment),
		DisclosureDigest:         auditDigest,
		UserDisclosureDigest:     strings.TrimSpace(txItem.UserDisclosureDigest),
		AuditDisclosureDigest:    auditDigest,
		SelfViewDisclosureDigest: strings.TrimSpace(txItem.SelfViewDisclosureDigest),
		RecipientHash:            item.ExpectedRecipientHash,
		AmountHash:               item.ExpectedAmountHash,
		Denom:                    item.Denom,
		BatchItemIndex:           itemIndex,
		BatchItemIndexKnown:      true,
		NullifierSpent:           true,
		TxSucceeded:              true,
		TxKnown:                  true,
	}, nil
}

func validateTransferBatchItemEvidence(item privacypayroll.PayrollPlanItem, txItem transferBatchResultItemFile, txItemIndex int) error {
	if amount := strings.TrimSpace(txItem.Amount); amount != "" && amount != payrollItemCoinString(item) {
		return fmt.Errorf("transfer-batch item %d amount is %s, expected %s", txItemIndex, amount, payrollItemCoinString(item))
	}
	if strings.TrimSpace(txItem.OutputCommitment) == "" {
		return fmt.Errorf("transfer-batch item %d is missing output_commitment evidence", txItemIndex)
	}
	if strings.TrimSpace(txItem.AuditDisclosureDigest) == "" {
		return fmt.Errorf("transfer-batch item %d is missing audit_disclosure_digest evidence", txItemIndex)
	}
	nullifiers := settlementNullifierSet(txItem.Nullifiers)
	if len(nullifiers) == 0 {
		return fmt.Errorf("transfer-batch item %d is missing nullifier evidence", txItemIndex)
	}
	for _, note := range item.InputNotes {
		lookup := normalizeSettlementEvidenceHex(note.NullifierLookupKey)
		if lookup == "" {
			return fmt.Errorf("payroll item %s input note %s has no nullifier lookup key", item.ItemID, note.NoteID)
		}
		if _, ok := nullifiers[lookup]; !ok {
			return fmt.Errorf("transfer-batch item %d missing nullifier evidence for reservation %s", txItemIndex, note.ReservationID)
		}
	}
	return nil
}

func settlementNullifierSet(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		normalized := normalizeSettlementEvidenceHex(value)
		if normalized == "" {
			continue
		}
		out[normalized] = struct{}{}
	}
	return out
}

func normalizeSettlementEvidenceHex(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.TrimPrefix(value, "0x")
	return value
}

func rollbackProvingRefs(ctx context.Context, service privacyreservation.Service, refs []privacyreservation.SubmittedReservationRef) error {
	var rollbackErr error
	for _, ref := range refs {
		if ref.ReservationID == "" || ref.LeaseToken == "" {
			continue
		}
		_, err := service.TransitionWithLease(ctx, ref.ReservationID, ref.LeaseToken, privacyreservation.StatusProving, privacyreservation.StatusReserved)
		if err != nil && !errors.Is(err, privacyreservation.ErrCompareAndSetFailed) && !errors.Is(err, privacyreservation.ErrLeaseMismatch) {
			rollbackErr = errors.Join(rollbackErr, err)
		}
	}
	return rollbackErr
}

func clearSettlementLeases(ctx context.Context, service privacyreservation.Service, refs []privacyreservation.SubmittedReservationRef) error {
	var clearErr error
	for _, ref := range refs {
		if ref.ReservationID == "" || ref.LeaseToken == "" {
			continue
		}
		if _, err := service.ClearLease(ctx, ref.ReservationID, ref.LeaseToken); err != nil && !errors.Is(err, privacyreservation.ErrLeaseUnavailable) && !errors.Is(err, privacyreservation.ErrLeaseMismatch) {
			clearErr = errors.Join(clearErr, err)
		}
	}
	return clearErr
}

func payrollItemCoinString(item privacypayroll.PayrollPlanItem) string {
	return payrollItemAmountString(item) + item.Denom
}

func payrollItemAmountString(item privacypayroll.PayrollPlanItem) string {
	if item.Amount == nil {
		return ""
	}
	return item.Amount.String()
}

func buildPayrollExportReport(plan privacypayroll.PayrollPlan, now time.Time) payrollExportReport {
	items := make([]payrollExportItem, 0, len(plan.Items))
	for _, item := range plan.Items {
		amount := ""
		if item.Amount != nil {
			amount = item.Amount.String()
		}
		items = append(items, payrollExportItem{
			ItemID:        item.ItemID,
			EmployeeID:    item.EmployeeID,
			OperationID:   item.OperationID,
			ChunkID:       item.ChunkID,
			Status:        item.Status,
			Amount:        amount,
			Denom:         item.Denom,
			FailureReason: item.FailureReason,
			RetryCount:    item.RetryCount,
		})
	}
	return payrollExportReport{
		GeneratedAt: now,
		PayrollID:   plan.PayrollID,
		BatchID:     plan.BatchID,
		Status:      plan.Status,
		Summary:     privacypayroll.BuildPlanReport(plan),
		Items:       items,
	}
}

func defaultPayrollArtifactID(input privacypayroll.PayrollInput) string {
	parts := make([]string, 0, 3)
	for _, value := range []string{input.CompanyID, input.PayrollID, input.BatchID} {
		if part := artifactIDComponent(value); part != "" {
			parts = append(parts, part)
		}
	}
	if len(parts) == 0 {
		return "payroll"
	}
	return strings.Join(parts, ".")
}

func artifactIDComponent(value string) string {
	value = strings.TrimSpace(value)
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "._-")
}

func (p prepareNotesFile) toSDK() (privacypayroll.NotePreparationInput, error) {
	defaultDisclosure, err := p.DefaultDisclosurePolicy.toSDK()
	if err != nil {
		return privacypayroll.NotePreparationInput{}, err
	}
	items := make([]privacypayroll.PayrollItemInput, 0, len(p.Items))
	for _, item := range p.Items {
		converted, err := item.toSDK()
		if err != nil {
			return privacypayroll.NotePreparationInput{}, err
		}
		items = append(items, converted)
	}
	notes := make([]privacypayroll.TreasuryNote, 0, len(p.TreasuryNotes))
	for _, note := range p.TreasuryNotes {
		converted, err := note.toSDK()
		if err != nil {
			return privacypayroll.NotePreparationInput{}, err
		}
		notes = append(notes, converted)
	}
	return privacypayroll.NotePreparationInput{
		PayrollInput: privacypayroll.PayrollInput{
			CompanyID:               p.CompanyID,
			PayrollID:               p.PayrollID,
			BatchID:                 p.BatchID,
			Denom:                   p.Denom,
			DefaultDisclosurePolicy: defaultDisclosure,
			Items:                   items,
		},
		TreasuryNotes: notes,
		Policy: privacypayroll.NotePreparationPolicy{
			MaxMessagesPerTx: p.MaxMessagesPerTx,
		},
	}, nil
}

func (p payrollItemFile) toSDK() (privacypayroll.PayrollItemInput, error) {
	amount, err := parseAmount(p.Amount, "payroll item amount")
	if err != nil {
		return privacypayroll.PayrollItemInput{}, err
	}
	var disclosure privacypayroll.PayrollDisclosurePolicy
	disclosureSet := p.DisclosurePolicy != nil
	if p.DisclosurePolicy != nil {
		disclosure, err = p.DisclosurePolicy.toSDK()
		if err != nil {
			return privacypayroll.PayrollItemInput{}, err
		}
	}
	return privacypayroll.PayrollItemInput{
		ItemID:                   p.ItemID,
		EmployeeID:               p.EmployeeID,
		RecipientAddress:         p.RecipientAddress,
		Amount:                   amount,
		Denom:                    p.Denom,
		DisclosurePolicy:         disclosure,
		DisclosurePolicySet:      disclosureSet,
		ExpectedOutputCommitment: p.ExpectedOutputCommitment,
		ExpectedDisclosureDigest: p.ExpectedDisclosureDigest,
	}, nil
}

func (n treasuryNoteFile) toSDK() (privacypayroll.TreasuryNote, error) {
	amount, err := parseAmount(n.Amount, "treasury note amount")
	if err != nil {
		return privacypayroll.TreasuryNote{}, err
	}
	return privacypayroll.TreasuryNote{
		NoteID:               n.NoteID,
		OwnerKeyID:           n.OwnerKeyID,
		NullifierLookupKey:   n.NullifierLookupKey,
		NullifierLookupKeyID: n.NullifierLookupKeyID,
		Denom:                n.Denom,
		Amount:               amount,
		IsSpent:              n.IsSpent,
		ReservationID:        n.ReservationID,
	}, nil
}

func (p disclosurePolicyFile) toSDK() (privacypayroll.PayrollDisclosurePolicy, error) {
	policy, err := parsePrivacyPolicy(p.UserPrivacyPolicy)
	if err != nil {
		return privacypayroll.PayrollDisclosurePolicy{}, err
	}
	mode, err := parseDisclosureMode(p.UserDisclosureMode)
	if err != nil {
		return privacypayroll.PayrollDisclosurePolicy{}, err
	}
	return privacypayroll.PayrollDisclosurePolicy{
		UserPrivacyPolicy:                policy,
		UserDisclosureMode:               mode,
		UserDisclosureTargetPubKeyHex:    p.UserDisclosureTargetPubKeyHex,
		UserDisclosureTargetKeyID:        p.UserDisclosureTargetKeyID,
		ExpectedUserDisclosureDigest:     p.ExpectedUserDisclosureDigest,
		ExpectedAuditDisclosureDigest:    p.ExpectedAuditDisclosureDigest,
		ExpectedSelfViewDisclosureDigest: p.ExpectedSelfViewDisclosureDigest,
	}, nil
}

func parsePrivacyPolicy(value string) (uint32, error) {
	switch strings.TrimSpace(value) {
	case "", "all-private":
		return privacytypes.TransferPrivacyPolicyAllPrivate, nil
	case "amount":
		return privacytypes.TransferPrivacyPolicyDiscloseAmount, nil
	case "to":
		return privacytypes.TransferPrivacyPolicyDiscloseTo, nil
	case "amount-to":
		return privacytypes.TransferPrivacyPolicyDiscloseAmountTo, nil
	case "from":
		return privacytypes.TransferPrivacyPolicyDiscloseFrom, nil
	case "amount-from":
		return privacytypes.TransferPrivacyPolicyDiscloseAmountFrom, nil
	case "from-to", "to-from":
		return privacytypes.TransferPrivacyPolicyDiscloseToFrom, nil
	case "amount-from-to", "amount-to-from":
		return privacytypes.TransferPrivacyPolicyDiscloseAmountToFrom, nil
	default:
		return 0, fmt.Errorf("unsupported user_privacy_policy %q", value)
	}
}

func parseDisclosureMode(value string) (privacytypes.UserDisclosureMode, error) {
	switch strings.TrimSpace(value) {
	case "", "none":
		return privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_NONE, nil
	case "public":
		return privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_PUBLIC, nil
	case "recipient-encrypted":
		return privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_RECIPIENT_ENCRYPTED, nil
	default:
		return privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_NONE, fmt.Errorf("unsupported user_disclosure_mode %q", value)
	}
}

func parseAmount(value string, name string) (*big.Int, error) {
	amount, ok := new(big.Int).SetString(strings.TrimSpace(value), 10)
	if !ok {
		return nil, fmt.Errorf("%s must be a base-10 integer", name)
	}
	return amount, nil
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
