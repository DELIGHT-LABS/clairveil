package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"

	privatefile "github.com/DELIGHT-LABS/clairveil/internal/privatefile"
	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
	privacyscan "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/scan"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

type seedLocalnetNotesReport struct {
	SchemaVersion       string `json:"schema_version"`
	GenesisPath         string `json:"genesis_path"`
	WalletPath          string `json:"wallet_path"`
	NotesOutPath        string `json:"notes_out_path,omitempty"`
	OwnerAddress        string `json:"owner_address"`
	SeededItemCount     int    `json:"seeded_item_count"`
	SeededNoteCount     int    `json:"seeded_note_count"`
	AmountNoteCount     int    `json:"amount_note_count"`
	ZeroDummyCount      int    `json:"zero_dummy_count"`
	Amount              string `json:"amount"`
	Denom               string `json:"denom"`
	CommitmentsAdded    int    `json:"commitments_added"`
	ExistingCommitments int    `json:"existing_commitments"`
}

func runSeedLocalnetNotes(args []string) error {
	flags := flag.NewFlagSet("seed-localnet-notes", flag.ContinueOnError)
	var genesisPath string
	var walletHome string
	var walletPath string
	var ownerAddress string
	var shieldedAddress string
	var count int
	var amountString string
	var denom string
	var notesOutPath string
	var outPath string
	flags.StringVar(&genesisPath, "genesis", "", "localnet genesis.json path to append seeded privacy commitments")
	flags.StringVar(&walletHome, "wallet-home", "", "clairveild home directory used to derive the local wallet cache path")
	flags.StringVar(&walletPath, "wallet", "", "optional explicit local privacy wallet cache path")
	flags.StringVar(&ownerAddress, "owner-address", "", "transparent account address that owns the local wallet cache")
	flags.StringVar(&shieldedAddress, "shielded-address", "", "shielded address whose spend/view public keys receive the seeded notes")
	flags.IntVar(&count, "count", 0, "number of payroll items to seed")
	flags.StringVar(&amountString, "amount", "1", "amount for each seeded payroll amount note")
	flags.StringVar(&denom, "denom", "uclair", "denom for seeded notes")
	flags.StringVar(&notesOutPath, "notes-out", "", "optional list-notes-compatible JSON path")
	flags.StringVar(&outPath, "out", "", "optional seed report JSON path; stdout when empty")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(genesisPath) == "" {
		return fmt.Errorf("-genesis is required")
	}
	if strings.TrimSpace(ownerAddress) == "" {
		return fmt.Errorf("-owner-address is required")
	}
	if strings.TrimSpace(shieldedAddress) == "" {
		return fmt.Errorf("-shielded-address is required")
	}
	if count <= 0 {
		return fmt.Errorf("-count must be positive")
	}
	amount, ok := new(big.Int).SetString(strings.TrimSpace(amountString), 10)
	if !ok || amount.Sign() <= 0 {
		return fmt.Errorf("-amount must be a positive base-10 integer")
	}
	denom = strings.TrimSpace(denom)
	if denom == "" {
		return fmt.Errorf("-denom is required")
	}
	if strings.TrimSpace(walletPath) == "" {
		if strings.TrimSpace(walletHome) == "" {
			return fmt.Errorf("-wallet-home or -wallet is required")
		}
		walletPath = privacyscan.WalletFilePath(walletHome, ownerAddress)
	}

	notes, commitments, err := buildSeededPayrollNotes(shieldedAddress, count, amount, denom)
	if err != nil {
		return err
	}
	existingCommitments, err := appendGenesisCommitments(genesisPath, commitments)
	if err != nil {
		return err
	}
	if err := writeSeededWallet(walletPath, notes); err != nil {
		return err
	}
	if strings.TrimSpace(notesOutPath) != "" {
		if err := writeSeededListNotes(notesOutPath, notes); err != nil {
			return err
		}
	}

	report := seedLocalnetNotesReport{
		SchemaVersion:       "clairveil.payroll_localnet_seed.v1",
		GenesisPath:         genesisPath,
		WalletPath:          walletPath,
		NotesOutPath:        strings.TrimSpace(notesOutPath),
		OwnerAddress:        ownerAddress,
		SeededItemCount:     count,
		SeededNoteCount:     len(notes),
		AmountNoteCount:     count,
		ZeroDummyCount:      count,
		Amount:              amount.String(),
		Denom:               denom,
		CommitmentsAdded:    len(commitments),
		ExistingCommitments: existingCommitments,
	}
	return writeJSONOutput(outPath, report)
}

func buildSeededPayrollNotes(shieldedAddress string, count int, amount *big.Int, denom string) ([]privacyscan.FoundNote, [][]byte, error) {
	bundle, err := privacytypes.DecodeShieldedAddressBundle(strings.TrimSpace(shieldedAddress))
	if err != nil {
		return nil, nil, fmt.Errorf("invalid -shielded-address: %w", err)
	}
	spendX := new(big.Int)
	spendY := new(big.Int)
	viewX := new(big.Int)
	viewY := new(big.Int)
	bundle.SpendPubKey.X.BigInt(spendX)
	bundle.SpendPubKey.Y.BigInt(spendY)
	bundle.ViewPubKey.X.BigInt(viewX)
	bundle.ViewPubKey.Y.BigInt(viewY)

	notes := make([]privacyscan.FoundNote, 0, count*2)
	commitments := make([][]byte, 0, count*2)
	for i := 0; i < count; i++ {
		amountNote, err := privacytypes.NewNote(new(big.Int).Set(spendX), new(big.Int).Set(spendY), new(big.Int).Set(viewX), new(big.Int).Set(viewY), new(big.Int).Set(amount), denom, fmt.Sprintf("localnet payroll amount seed %d", i+1))
		if err != nil {
			return nil, nil, err
		}
		dummyNote, err := privacytypes.NewNote(new(big.Int).Set(spendX), new(big.Int).Set(spendY), new(big.Int).Set(viewX), new(big.Int).Set(viewY), big.NewInt(0), denom, fmt.Sprintf("localnet payroll dummy seed %d", i+1))
		if err != nil {
			return nil, nil, err
		}
		for _, note := range []*privacytypes.Note{amountNote, dummyNote} {
			commitment, err := privacyfield.CanonicalBytesFromBigInt(note.ComputeCommitment())
			if err != nil {
				return nil, nil, fmt.Errorf("seed note commitment is not canonical: %w", err)
			}
			found := privacyscan.BuildFoundNoteFromScanEvent(note, nil)
			found.TxHash = fmt.Sprintf("LOCALNET-SEED-%06d", len(notes)+1)
			found.Height = 0
			notes = append(notes, found)
			commitments = append(commitments, commitment)
		}
	}
	return notes, commitments, nil
}

func appendGenesisCommitments(genesisPath string, commitments [][]byte) (int, error) {
	bz, err := os.ReadFile(genesisPath)
	if err != nil {
		return 0, err
	}
	var doc map[string]any
	if err := json.Unmarshal(bz, &doc); err != nil {
		return 0, err
	}
	appState, ok := doc["app_state"].(map[string]any)
	if !ok {
		return 0, fmt.Errorf("genesis app_state is missing or invalid")
	}
	privacyState, ok := appState["privacy"].(map[string]any)
	if !ok {
		return 0, fmt.Errorf("genesis app_state.privacy is missing or invalid")
	}

	existing, _ := privacyState["commitments"].([]any)
	existingCount := len(existing)
	updated := append([]any(nil), existing...)
	for _, commitment := range commitments {
		updated = append(updated, base64.StdEncoding.EncodeToString(commitment))
	}
	privacyState["commitments"] = updated

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return 0, err
	}
	out = append(out, '\n')
	if err := privatefile.Write(genesisPath, out); err != nil {
		return 0, err
	}
	return existingCount, nil
}

func writeSeededWallet(walletPath string, notes []privacyscan.FoundNote) error {
	if dir := filepath.Dir(walletPath); dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	wallet := &privacyscan.LocalWalletData{
		LastHeight:   0,
		LastSequence: 0,
		Notes:        notes,
	}
	return privacyscan.SaveLocalWalletFile(walletPath, wallet)
}

func writeSeededListNotes(path string, notes []privacyscan.FoundNote) error {
	payload := listNotesFile{Notes: make([]listNotesFileNote, 0, len(notes))}
	for i, note := range notes {
		payload.Notes = append(payload.Notes, listNotesFileNote{
			Index:     i + 1,
			Status:    "spendable",
			Amount:    note.Note.Amount.String(),
			Nullifier: note.Nullifier,
			TxHash:    note.TxHash,
			Height:    note.Height,
		})
	}
	return writeJSONOutput(path, payload)
}
