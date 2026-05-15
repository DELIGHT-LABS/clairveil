package scan

import (
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestSummarizeSpendableNotes(t *testing.T) {
	notes := []FoundNote{
		{Note: privacytypes.Note{Amount: big.NewInt(5)}, IsSpent: false},
		{Note: privacytypes.Note{Amount: big.NewInt(7)}, IsSpent: true},
		{Note: privacytypes.Note{Amount: big.NewInt(11)}, IsSpent: false},
	}

	spendable, total := SummarizeSpendableNotes(notes)

	require.Len(t, spendable, 2)
	require.Equal(t, int64(5), spendable[0].Note.Amount.Int64())
	require.Equal(t, int64(11), spendable[1].Note.Amount.Int64())
	require.Equal(t, int64(16), total.Int64())
}

func TestNormalizeFoundNotesDeduplicatesAndSorts(t *testing.T) {
	duplicate := FoundNote{
		Note:      privacytypes.Note{Amount: big.NewInt(7), AssetID: privacycrypto.HashString("uclair")},
		Nullifier: "bb",
		Height:    7,
		TxHash:    "B2",
	}
	notes := []FoundNote{
		{
			Note:      privacytypes.Note{Amount: big.NewInt(11), AssetID: privacycrypto.HashString("uclair")},
			Nullifier: "cc",
			Height:    11,
			TxHash:    "C3",
		},
		duplicate,
		duplicate,
		{
			Note:      privacytypes.Note{Amount: big.NewInt(5), AssetID: privacycrypto.HashString("uclair")},
			Nullifier: "aa",
			Height:    3,
			TxHash:    "A1",
		},
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

	original := &LocalWalletData{
		LastHeight: 23,
		Notes: []FoundNote{
			{
				Note:      privacytypes.Note{Amount: big.NewInt(9), AssetID: privacycrypto.HashString("uclair")},
				Nullifier: "aa",
				TxHash:    "ABCD",
				Height:    23,
			},
		},
	}

	require.NoError(t, SaveLocalWalletFile(dbPath, original))

	result, err := LoadLocalWalletFile(tempDir, userAddress)
	require.NoError(t, err)
	require.Equal(t, int64(23), result.Wallet.LastHeight)
	require.Len(t, result.Wallet.Notes, 1)
	require.Equal(t, "aa", result.Wallet.Notes[0].Nullifier)
	require.Equal(t, "9", result.Wallet.Notes[0].Note.Amount.String())
}
