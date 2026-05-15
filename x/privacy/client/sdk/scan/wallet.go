package scan

import (
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
)

type LocalWalletData struct {
	LastHeight int64       `json:"last_height"`
	Notes      []FoundNote `json:"notes"`
}

type LoadLocalWalletFileResult struct {
	Wallet                 *LocalWalletData
	Path                   string
	CorruptBackupPath      string
	CorruptBackupRenameErr error
}

func SummarizeSpendableNotes(notes []FoundNote) ([]FoundNote, *big.Int) {
	spendable := make([]FoundNote, 0, len(notes))
	total := new(big.Int)

	for _, fn := range notes {
		if fn.IsSpent {
			continue
		}

		spendable = append(spendable, fn)
		total.Add(total, fn.Note.Amount)
	}

	return spendable, total
}

func NormalizeFoundNotes(notes []FoundNote) ([]FoundNote, bool) {
	if len(notes) == 0 {
		return []FoundNote{}, false
	}

	seen := make(map[string]FoundNote, len(notes))
	order := make([]string, 0, len(notes))
	for _, note := range notes {
		key := foundNoteIdentityKey(note)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = note
		order = append(order, key)
	}

	normalized := make([]FoundNote, 0, len(order))
	for _, key := range order {
		normalized = append(normalized, seen[key])
	}

	sort.Slice(normalized, func(i, j int) bool {
		return foundNoteDisplayLess(normalized[i], normalized[j])
	})

	if len(normalized) != len(notes) {
		return normalized, true
	}
	for i := range normalized {
		if foundNoteIdentityKey(normalized[i]) != foundNoteIdentityKey(notes[i]) {
			return normalized, true
		}
	}

	return normalized, false
}

func WalletFilePath(homeDir string, userAddress string) string {
	filename := fmt.Sprintf("privacy_wallet_%s.json", userAddress)
	return filepath.Join(homeDir, filename)
}

func LoadLocalWalletFile(homeDir string, userAddress string) (*LoadLocalWalletFileResult, error) {
	dbPath := WalletFilePath(homeDir, userAddress)
	result := &LoadLocalWalletFileResult{
		Wallet: &LocalWalletData{
			LastHeight: 0,
			Notes:      []FoundNote{},
		},
		Path: dbPath,
	}

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return result, nil
	} else if err != nil {
		return nil, err
	}

	fileBytes, err := os.ReadFile(dbPath)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(fileBytes, result.Wallet); err != nil {
		backupPath := fmt.Sprintf("%s.corrupt-%d.bak", dbPath, time.Now().Unix())
		if renameErr := os.Rename(dbPath, backupPath); renameErr != nil {
			result.CorruptBackupRenameErr = renameErr
		} else {
			result.CorruptBackupPath = backupPath
		}
		result.Wallet = &LocalWalletData{
			LastHeight: 0,
			Notes:      []FoundNote{},
		}
	}

	return result, nil
}

func SaveLocalWalletFile(dbPath string, data *LocalWalletData) error {
	fileBytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(dbPath, fileBytes, 0600)
}

func foundNoteIdentityKey(note FoundNote) string {
	if trimmed := strings.ToLower(strings.TrimSpace(note.Nullifier)); trimmed != "" {
		return "nullifier:" + trimmed
	}

	commitment := note.Note.ComputeCommitment()
	if commitmentHex, err := privacyfield.CanonicalHexFromBigInt(commitment); err == nil {
		return "commitment:" + commitmentHex
	}

	return fmt.Sprintf(
		"fallback:%d:%s:%s",
		note.Height,
		strings.ToLower(strings.TrimSpace(note.TxHash)),
		note.Note.Amount.String(),
	)
}

func foundNoteDisplayLess(left, right FoundNote) bool {
	if left.Height != right.Height {
		return left.Height < right.Height
	}

	leftTxHash := strings.ToLower(strings.TrimSpace(left.TxHash))
	rightTxHash := strings.ToLower(strings.TrimSpace(right.TxHash))
	if leftTxHash != rightTxHash {
		return leftTxHash < rightTxHash
	}

	leftNullifier := strings.ToLower(strings.TrimSpace(left.Nullifier))
	rightNullifier := strings.ToLower(strings.TrimSpace(right.Nullifier))
	if leftNullifier != rightNullifier {
		return leftNullifier < rightNullifier
	}

	return left.Note.Amount.Cmp(right.Note.Amount) < 0
}
