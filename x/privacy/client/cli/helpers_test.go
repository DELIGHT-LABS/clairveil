package cli

import (
	"math/big"
	"testing"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	"github.com/stretchr/testify/require"
)

func TestDecodeMerkleProof(t *testing.T) {
	path := []string{"01", "0a"}
	helpers := []uint32{1, 0}

	nodes, helperBits := decodeMerkleProof(path, helpers)

	require.Equal(t, int64(1), nodes[0].Int64())
	require.Equal(t, int64(10), nodes[1].Int64())
	require.Equal(t, int64(0), nodes[2].Int64())
	require.Equal(t, 1, helperBits[0])
	require.Equal(t, 0, helperBits[1])
	require.Equal(t, 0, helperBits[2])
}

func TestSummarizeSpendableNotesByDenom(t *testing.T) {
	notes := []FoundNote{
		{Note: types.Note{Amount: big.NewInt(5), AssetID: crypto.HashString("uclair")}, IsSpent: false},
		{Note: types.Note{Amount: big.NewInt(7), AssetID: crypto.HashString("uatom")}, IsSpent: false},
		{Note: types.Note{Amount: big.NewInt(11), AssetID: crypto.HashString("uclair")}, IsSpent: true},
		{Note: types.Note{Amount: big.NewInt(13), AssetID: crypto.HashString("uclair")}, IsSpent: false},
	}

	spendable, total := summarizeSpendableNotesByDenom(notes, "uclair")

	require.Len(t, spendable, 2)
	require.Equal(t, int64(5), spendable[0].Note.Amount.Int64())
	require.Equal(t, int64(13), spendable[1].Note.Amount.Int64())
	require.Equal(t, int64(18), total.Int64())
}

func TestBuildListNotesJSONOutput(t *testing.T) {
	notes := []FoundNote{
		{
			Note:      types.Note{Amount: big.NewInt(5), AssetID: crypto.HashString("uclair")},
			Nullifier: "aa",
			Height:    3,
			TxHash:    "A1",
			IsSpent:   false,
		},
		{
			Note:      types.Note{Amount: big.NewInt(7), AssetID: crypto.HashString("uclair")},
			Nullifier: "bb",
			Height:    7,
			TxHash:    "B2",
			IsSpent:   true,
		},
		{
			Note:      types.Note{Amount: big.NewInt(11), AssetID: crypto.HashString("uclair")},
			Nullifier: "cc",
			Height:    11,
			TxHash:    "C3",
			IsSpent:   false,
		},
	}

	diagnostics := &scanNotesDiagnostics{
		WalletPath:            "/tmp/privacy_wallet_clair1test.json",
		LoadedLastHeight:      12,
		LoadedNoteCount:       2,
		ScannedFromHeight:     13,
		ScannedToHeight:       20,
		ForcedRescan:          true,
		NormalizedCache:       true,
		NewNotesFound:         1,
		FinalNoteCount:        3,
		SavedWallet:           true,
		CorruptBackupPath:     "/tmp/privacy_wallet_clair1test.json.corrupt-1700000000.bak",
		RecoveredCorruptCache: true,
	}

	output := buildListNotesJSONOutput(notes, diagnostics)

	require.Equal(t, "16", output.Summary.TotalSpendable)
	require.Equal(t, 2, output.Summary.SpendableCount)
	require.Equal(t, 1, output.Summary.SpentCount)
	require.Equal(t, 3, output.Summary.TotalCount)
	require.NotNil(t, output.Diagnostics)
	require.Equal(t, diagnostics, output.Diagnostics)
	require.Len(t, output.Notes, 3)
	require.Equal(t, 1, output.Notes[0].Index)
	require.Equal(t, "spendable", output.Notes[0].Status)
	require.Equal(t, "5", output.Notes[0].Amount)
	require.Equal(t, "aa", output.Notes[0].Nullifier)
	require.Equal(t, "A1", output.Notes[0].TxHash)
	require.Equal(t, int64(3), output.Notes[0].Height)
	require.Equal(t, int64(5), output.Notes[0].Note.Amount.Int64())
	require.Equal(t, "spent", output.Notes[1].Status)
	require.Equal(t, "7", output.Notes[1].Amount)
}

func TestConsumeOneShotBool(t *testing.T) {
	value := true

	require.True(t, consumeOneShotBool(&value))
	require.False(t, value)
	require.False(t, consumeOneShotBool(&value))
	require.False(t, consumeOneShotBool(nil))
}

func TestPlannerStateFingerprintUsesSortedSameDenomSpendableNotes(t *testing.T) {
	left := []FoundNote{
		{Note: types.Note{Amount: big.NewInt(10), AssetID: crypto.HashString("uclair")}, Nullifier: "bb", Height: 9},
		{Note: types.Note{Amount: big.NewInt(3), AssetID: crypto.HashString("uclair")}, Nullifier: "aa", Height: 5},
		{Note: types.Note{Amount: big.NewInt(9), AssetID: crypto.HashString("uatom")}, Nullifier: "xx", Height: 4},
	}
	right := []FoundNote{
		{Note: types.Note{Amount: big.NewInt(3), AssetID: crypto.HashString("uclair")}, Nullifier: "aa", Height: 5},
		{Note: types.Note{Amount: big.NewInt(10), AssetID: crypto.HashString("uclair")}, Nullifier: "bb", Height: 9},
	}

	require.Equal(t, plannerStateFingerprint(left, "uclair", big.NewInt(7)), plannerStateFingerprint(right, "uclair", big.NewInt(7)))
}
