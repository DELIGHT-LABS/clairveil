package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"

	privacypayroll "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/payroll"
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

func main() {
	if len(os.Args) < 2 {
		fatalf("usage: clairveil-payroll <validate|prepare-notes|plan|status|export-report> [flags]")
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
	case "status":
		if err := runStatus(os.Args[2:]); err != nil {
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

func runStatus(args []string) error {
	flags := flag.NewFlagSet("status", flag.ContinueOnError)
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
	report := privacypayroll.BuildPlanReport(*plan)
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
