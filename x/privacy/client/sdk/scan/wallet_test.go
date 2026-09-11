package scan

import (
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestSummarizeSpendableNotes(t *testing.T) {
	notes := []SecretFoundNote{
		{Note: privacytypes.SecretNoteV1{Amount: 5}, IsSpent: false},
		{Note: privacytypes.SecretNoteV1{Amount: 7}, IsSpent: true},
		{Note: privacytypes.SecretNoteV1{Amount: 11}, IsSpent: false},
	}

	spendable, total, err := SummarizeSpendableNotes(notes)
	require.NoError(t, err)

	require.Len(t, spendable, 2)
	require.Equal(t, uint64(5), spendable[0].Note.Amount)
	require.Equal(t, uint64(11), spendable[1].Note.Amount)
	require.Equal(t, uint64(16), total)
}

func TestNormalizeFoundNotesDeduplicatesAndSorts(t *testing.T) {
	duplicate := SecretFoundNote{Note: privacytypes.SecretNoteV1{Amount: 7}, Nullifier: "bb", Height: 7, TxHash: "B2"}
	notes := []SecretFoundNote{
		{Note: privacytypes.SecretNoteV1{Amount: 11}, Nullifier: "cc", Height: 11, TxHash: "C3"},
		duplicate, duplicate,
		{Note: privacytypes.SecretNoteV1{Amount: 5}, Nullifier: "aa", Height: 3, TxHash: "A1"},
	}
	normalized, changed := NormalizeFoundNotes(notes)
	require.True(t, changed)
	require.Len(t, normalized, 3)
	require.Equal(t, "aa", normalized[0].Nullifier)
	require.Equal(t, "bb", normalized[1].Nullifier)
	require.Equal(t, "cc", normalized[2].Nullifier)
}

func TestLoadLocalWalletFileMovesCorruptedCacheAside(t *testing.T) {
	tempDir := t.TempDir()
	userAddress := "clair1testwallet"
	dbPath := WalletFilePath(tempDir, userAddress)

	require.NoError(t, os.WriteFile(dbPath, []byte("{not-json"), 0600))

	result, err := LoadLocalWalletFile(tempDir, userAddress)
	require.NoError(t, err)
	require.Equal(t, dbPath, result.Path)
	require.NotNil(t, result.Wallet)
	require.Equal(t, int64(0), result.Wallet.LastHeight)
	require.Empty(t, result.Wallet.Notes)
	require.Empty(t, result.CorruptBackupRenameErr)

	backups, err := filepath.Glob(dbPath + ".corrupt-*.bak")
	require.NoError(t, err)
	require.Len(t, backups, 1)
	require.Equal(t, backups[0], result.CorruptBackupPath)
}

func TestSaveLocalWalletFileRoundTrip(t *testing.T) {
	tempDir := t.TempDir()
	userAddress := "clair1roundtrip"
	dbPath := WalletFilePath(tempDir, userAddress)

	legacyNote, txRes := newScanServiceDepositTx(t, []byte("wallet-roundtrip"), big.NewInt(9), "uclair", 23)
	found := mustSecretFoundNoteFromLegacy(t, BuildFoundNote(legacyNote, txRes))
	found.AuditKeyID, found.AuditKeyEpoch = "audit-key-id", 2
	original := &LocalWalletData{LastHeight: 23, Notes: []SecretFoundNote{found}}

	require.NoError(t, os.WriteFile(dbPath, []byte("old"), 0o644))
	require.NoError(t, os.Chmod(dbPath, 0o644))
	require.NoError(t, SaveLocalWalletFile(dbPath, original))
	info, err := os.Stat(dbPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	result, err := LoadLocalWalletFile(tempDir, userAddress)
	require.NoError(t, err)
	require.Equal(t, int64(23), result.Wallet.LastHeight)
	require.Len(t, result.Wallet.Notes, 1)
	require.Equal(t, original.Notes[0].Nullifier, result.Wallet.Notes[0].Nullifier)
	require.Equal(t, uint64(9), result.Wallet.Notes[0].Note.Amount)
	require.Equal(t, "audit-key-id", result.Wallet.Notes[0].AuditKeyID)
	require.Equal(t, uint64(2), result.Wallet.Notes[0].AuditKeyEpoch)
}

func TestLoadLegacyWalletDecodesDirectlyToFixedNote(t *testing.T) {
	tempDir := t.TempDir()
	address := "clair1legacywallet"
	legacyNote, txRes := newScanServiceDepositTx(t, []byte("legacy-wallet"), big.NewInt(21), "uclair", 31)
	legacy := struct {
		LastHeight int64       `json:"last_height"`
		Notes      []FoundNote `json:"notes"`
	}{LastHeight: 31, Notes: []FoundNote{BuildFoundNote(legacyNote, txRes)}}
	encoded, err := json.Marshal(legacy)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(WalletFilePath(tempDir, address), encoded, 0o600))

	loaded, err := LoadLocalWalletFile(tempDir, address)
	require.NoError(t, err)
	require.Empty(t, loaded.CorruptBackupPath)
	require.Len(t, loaded.Wallet.Notes, 1)
	require.Equal(t, uint64(21), loaded.Wallet.Notes[0].Note.Amount)
	require.NoError(t, loaded.Wallet.Notes[0].Note.ValidateV1())
}

func TestWalletRejectsUnknownVersionWithoutMutation(t *testing.T) {
	wallet := LocalWalletData{LastHeight: 123}
	err := json.Unmarshal([]byte(`{"version":3,"notes":[]}`), &wallet)
	require.Error(t, err)
	require.Equal(t, int64(123), wallet.LastHeight)
}

func TestLegacyWalletRejectsMalformedDecimalFields(t *testing.T) {
	for _, raw := range []json.RawMessage{json.RawMessage(`""`), json.RawMessage(`"0"`), json.RawMessage(`null`), json.RawMessage(`-1`), json.RawMessage(`1e2`), nil} {
		_, err := legacyAmount(raw)
		require.Error(t, err)
		_, err = legacyFieldDecimal(raw)
		require.Error(t, err)
	}
	amount, err := legacyAmount(json.RawMessage(`0`))
	require.NoError(t, err)
	require.Zero(t, amount)
}
