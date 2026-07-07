package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"

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

func main() {
	if len(os.Args) < 2 {
		fatalf("usage: clairveil-payroll prepare-notes -input payroll.json [-out report.json]")
	}
	switch os.Args[1] {
	case "prepare-notes":
		if err := runPrepareNotes(os.Args[2:]); err != nil {
			fatalf("%v", err)
		}
	default:
		fatalf("unknown command %q", os.Args[1])
	}
}

func runPrepareNotes(args []string) error {
	flags := flag.NewFlagSet("prepare-notes", flag.ContinueOnError)
	var inputPath string
	var outPath string
	flags.StringVar(&inputPath, "input", "", "payroll preparation input JSON path")
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
	report, err := privacypayroll.AnalyzeNotePreparation(input)
	if err != nil {
		return err
	}

	bz, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	bz = append(bz, '\n')
	if strings.TrimSpace(outPath) == "" {
		_, err = os.Stdout.Write(bz)
		return err
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil && filepath.Dir(outPath) != "." {
		return err
	}
	return os.WriteFile(outPath, bz, 0o644)
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
