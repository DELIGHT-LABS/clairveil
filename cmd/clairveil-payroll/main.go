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
	ItemID                   string               `json:"item_id"`
	EmployeeID               string               `json:"employee_id,omitempty"`
	RecipientAddress         string               `json:"recipient_address"`
	Amount                   string               `json:"amount"`
	Denom                    string               `json:"denom,omitempty"`
	DisclosurePolicy         disclosurePolicyFile `json:"disclosure_policy,omitempty"`
	ExpectedOutputCommitment string               `json:"expected_output_commitment,omitempty"`
	ExpectedDisclosureDigest string               `json:"expected_disclosure_digest,omitempty"`
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
	ReservationID       string `json:"reservation_id"`
	TxHash              string `json:"tx_hash,omitempty"`
	SignDocHash         string `json:"sign_doc_hash,omitempty"`
	TxBytesHash         string `json:"tx_bytes_hash,omitempty"`
	OutputCommitment    string `json:"output_commitment,omitempty"`
	DisclosureDigest    string `json:"disclosure_digest,omitempty"`
	RecipientHash       string `json:"recipient_hash,omitempty"`
	AmountHash          string `json:"amount_hash,omitempty"`
	Denom               string `json:"denom,omitempty"`
	BatchItemIndex      int    `json:"batch_item_index,omitempty"`
	BatchItemIndexKnown bool   `json:"batch_item_index_known,omitempty"`
	NullifierSpent      bool   `json:"nullifier_spent,omitempty"`
	TxSucceeded         bool   `json:"tx_succeeded,omitempty"`
	TxFailed            bool   `json:"tx_failed,omitempty"`
	TxKnown             bool   `json:"tx_known,omitempty"`
}

type reconcileReport struct {
	Total          int                   `json:"total"`
	RequiresReview int                   `json:"requires_review"`
	Results        []reconcileItemReport `json:"results"`
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

func main() {
	if len(os.Args) < 2 {
		fatalf("usage: clairveil-payroll <validate|prepare-notes|plan|run|status|reconcile|export-report> [flags]")
	}
	switch os.Args[1] {
	case "validate":
		if err := runValidate(os.Args[2:]); err != nil {
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
	report := reconcileReport{
		Total:   len(evidence),
		Results: make([]reconcileItemReport, 0, len(evidence)),
	}
	worker := privacypayroll.ReconcileWorker{
		Reservation: privacyreservation.Service{Store: store},
	}
	for _, item := range evidence {
		result, err := worker.ReconcileReservation(context.Background(), item.ReservationID, item.toSDK())
		if err != nil {
			return err
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
	return writeJSONOutput(outPath, report)
}

func runExportReport(args []string) error {
	flags := flag.NewFlagSet("export-report", flag.ContinueOnError)
	var planPath string
	var outPath string
	flags.StringVar(&planPath, "plan", "", "payroll plan JSON path")
	flags.StringVar(&outPath, "out", "", "optional output JSON path; stdout when empty")
	if err := flags.Parse(args); err != nil {
		return err
	}
	plan, err := readPlanInput(planPath)
	if err != nil {
		return err
	}
	return writeJSONOutput(outPath, buildPayrollExportReport(*plan, time.Now().UTC()))
}

func readPrepareNotesInput(path string) (privacypayroll.NotePreparationInput, error) {
	bz, err := os.ReadFile(path)
	if err != nil {
		return privacypayroll.NotePreparationInput{}, err
	}
	var payload prepareNotesFile
	if err := json.Unmarshal(bz, &payload); err != nil {
		return privacypayroll.NotePreparationInput{}, err
	}
	return payload.toSDK()
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

func (e reconcileEvidenceItemFile) toSDK() privacyreservation.OperationEvidence {
	return privacyreservation.OperationEvidence{
		TxHash:              e.TxHash,
		SignDocHash:         e.SignDocHash,
		TxBytesHash:         e.TxBytesHash,
		OutputCommitment:    e.OutputCommitment,
		DisclosureDigest:    e.DisclosureDigest,
		RecipientHash:       e.RecipientHash,
		AmountHash:          e.AmountHash,
		Denom:               e.Denom,
		BatchItemIndex:      e.BatchItemIndex,
		BatchItemIndexKnown: e.BatchItemIndexKnown,
		NullifierSpent:      e.NullifierSpent,
		TxSucceeded:         e.TxSucceeded,
		TxFailed:            e.TxFailed,
		TxKnown:             e.TxKnown,
	}
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
	disclosure, err := p.DisclosurePolicy.toSDK()
	if err != nil {
		return privacypayroll.PayrollItemInput{}, err
	}
	return privacypayroll.PayrollItemInput{
		ItemID:                   p.ItemID,
		EmployeeID:               p.EmployeeID,
		RecipientAddress:         p.RecipientAddress,
		Amount:                   amount,
		Denom:                    p.Denom,
		DisclosurePolicy:         disclosure,
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
