package withdraw

import (
	"math/big"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	privacyscan "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/scan"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestFindExactMatchSpendableNoteByDenomIgnoresDifferentDenom(t *testing.T) {
	notes := []privacyscan.FoundNote{
		{Note: privacytypes.Note{Amount: big.NewInt(10), AssetID: privacycrypto.HashString("uatom")}, IsSpent: false},
		{Note: privacytypes.Note{Amount: big.NewInt(10), AssetID: privacycrypto.HashString("uclair")}, IsSpent: true},
		{Note: privacytypes.Note{Amount: big.NewInt(10), AssetID: privacycrypto.HashString("uclair")}, IsSpent: false},
	}

	selected := FindExactMatchSpendableNoteByDenom(notes, "uclair", big.NewInt(10))
	require.NotNil(t, selected)
	require.Equal(t, 0, selected.Note.AssetID.Cmp(privacycrypto.HashString("uclair")))
	require.Equal(t, int64(10), selected.Note.Amount.Int64())
	require.False(t, selected.IsSpent)
}

func TestFindExactMatchSpendableNoteByDenomUsesDeterministicOrder(t *testing.T) {
	notes := []privacyscan.FoundNote{
		{
			Note:      privacytypes.Note{Amount: big.NewInt(10), AssetID: privacycrypto.HashString("uclair")},
			Nullifier: "bb",
			Height:    9,
			IsSpent:   false,
		},
		{
			Note:      privacytypes.Note{Amount: big.NewInt(10), AssetID: privacycrypto.HashString("uclair")},
			Nullifier: "aa",
			Height:    5,
			IsSpent:   false,
		},
	}

	selected := FindExactMatchSpendableNoteByDenom(notes, "uclair", big.NewInt(10))
	require.NotNil(t, selected)
	require.Equal(t, "aa", selected.Nullifier)
}

func TestBuildExactMatchErrorShowsSpendableGuidance(t *testing.T) {
	targetCoin := sdk.NewInt64Coin("uclair", 10)
	notes := []privacyscan.FoundNote{
		{Note: privacytypes.Note{Amount: big.NewInt(3), AssetID: privacycrypto.HashString("uclair")}, IsSpent: false},
		{Note: privacytypes.Note{Amount: big.NewInt(7), AssetID: privacycrypto.HashString("uclair")}, IsSpent: false},
		{Note: privacytypes.Note{Amount: big.NewInt(9), AssetID: privacycrypto.HashString("uatom")}, IsSpent: false},
	}

	err := BuildExactMatchError(targetCoin, notes)
	require.Error(t, err)
	require.Contains(t, err.Error(), "exact-match note")
	require.Contains(t, err.Error(), "does not create change")
	require.Contains(t, err.Error(), "3uclair, 7uclair")
	require.Contains(t, err.Error(), "total 10uclair across 2 notes")
	require.Contains(t, err.Error(), "will not split larger notes or merge fragmented notes")
	require.Contains(t, err.Error(), "shielded self-transfer")
	require.Contains(t, err.Error(), "retry withdraw or prepare-withdraw")
}

func TestBuildExactMatchErrorHandlesMissingDenomNotes(t *testing.T) {
	targetCoin := sdk.NewInt64Coin("uclair", 10)
	notes := []privacyscan.FoundNote{
		{Note: privacytypes.Note{Amount: big.NewInt(9), AssetID: privacycrypto.HashString("uatom")}, IsSpent: false},
	}

	err := BuildExactMatchError(targetCoin, notes)
	require.Error(t, err)
	require.Contains(t, err.Error(), "exact-match note")
	require.Contains(t, err.Error(), "no spendable uclair notes were found")
	require.Contains(t, err.Error(), "Run list-notes")
}
