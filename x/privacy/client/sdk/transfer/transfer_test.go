package transfer

import (
	"math/big"
	"testing"

	"github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
	"github.com/stretchr/testify/require"

	privacyscan "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/scan"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestResolveRecipientRejectsTransparentAddress(t *testing.T) {
	_, _, err := ResolveRecipient("clair1notshielded")
	require.Error(t, err)
	require.Contains(t, err.Error(), "shielded address")
}

func TestResolveRecipientDecodesShieldedBundle(t *testing.T) {
	curve := twistededwards.GetEdwardsCurve()

	var spendPubKey twistededwards.PointAffine
	var viewPubKey twistededwards.PointAffine
	spendPubKey.ScalarMultiplication(&curve.Base, big.NewInt(3))
	viewPubKey.ScalarMultiplication(&curve.Base, big.NewInt(7))

	addr, err := privacytypes.EncodeShieldedAddressWithView(&spendPubKey, &viewPubKey)
	require.NoError(t, err)

	resolvedSpend, resolvedView, err := ResolveRecipient(addr)
	require.NoError(t, err)
	require.Equal(t, spendPubKey.Bytes(), resolvedSpend.Bytes())
	require.Equal(t, viewPubKey.Bytes(), resolvedView.Bytes())
}

func TestSummarizeSpendableNotesByDenom(t *testing.T) {
	notes := []privacyscan.FoundNote{
		{Note: privacytypes.Note{Amount: big.NewInt(5), AssetID: privacycrypto.HashString("uclair")}, IsSpent: false},
		{Note: privacytypes.Note{Amount: big.NewInt(7), AssetID: privacycrypto.HashString("uatom")}, IsSpent: false},
		{Note: privacytypes.Note{Amount: big.NewInt(11), AssetID: privacycrypto.HashString("uclair")}, IsSpent: true},
		{Note: privacytypes.Note{Amount: big.NewInt(13), AssetID: privacycrypto.HashString("uclair")}, IsSpent: false},
	}

	spendable, total := SummarizeSpendableNotesByDenom(notes, "uclair")

	require.Len(t, spendable, 2)
	require.Equal(t, int64(5), spendable[0].Note.Amount.Int64())
	require.Equal(t, int64(13), spendable[1].Note.Amount.Int64())
	require.Equal(t, int64(18), total.Int64())
}

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

func TestPlannerStateFingerprintUsesSortedSameDenomSpendableNotes(t *testing.T) {
	left := []privacyscan.FoundNote{
		{Note: privacytypes.Note{Amount: big.NewInt(10), AssetID: privacycrypto.HashString("uclair")}, Nullifier: "bb", Height: 9},
		{Note: privacytypes.Note{Amount: big.NewInt(3), AssetID: privacycrypto.HashString("uclair")}, Nullifier: "aa", Height: 5},
		{Note: privacytypes.Note{Amount: big.NewInt(9), AssetID: privacycrypto.HashString("uatom")}, Nullifier: "xx", Height: 4},
	}
	right := []privacyscan.FoundNote{
		{Note: privacytypes.Note{Amount: big.NewInt(3), AssetID: privacycrypto.HashString("uclair")}, Nullifier: "aa", Height: 5},
		{Note: privacytypes.Note{Amount: big.NewInt(10), AssetID: privacycrypto.HashString("uclair")}, Nullifier: "bb", Height: 9},
	}

	require.Equal(t, PlannerStateFingerprint(left, "uclair", big.NewInt(7)), PlannerStateFingerprint(right, "uclair", big.NewInt(7)))
}

func TestSelectInputsFiltersDifferentDenomZeroNote(t *testing.T) {
	notes := []privacyscan.FoundNote{
		{Note: privacytypes.Note{Amount: big.NewInt(10), AssetID: privacycrypto.HashString("uclair")}, IsSpent: false},
		{Note: privacytypes.Note{Amount: big.NewInt(0), AssetID: privacycrypto.HashString("uatom")}, IsSpent: false},
	}

	selection := SelectInputs(notes, "uclair", big.NewInt(10))
	require.Equal(t, int64(0), selection.Total.Int64())
	require.False(t, selection.IsFinal)
	require.True(t, selection.NeedsZeroDummy)
}

func TestSelectInputsUsesSameDenomPairOnly(t *testing.T) {
	notes := []privacyscan.FoundNote{
		{Note: privacytypes.Note{Amount: big.NewInt(7), AssetID: privacycrypto.HashString("uclair")}, IsSpent: false},
		{Note: privacytypes.Note{Amount: big.NewInt(5), AssetID: privacycrypto.HashString("uatom")}, IsSpent: false},
		{Note: privacytypes.Note{Amount: big.NewInt(4), AssetID: privacycrypto.HashString("uclair")}, IsSpent: false},
	}

	selection := SelectInputs(notes, "uclair", big.NewInt(10))
	require.False(t, selection.NeedsZeroDummy)
	require.True(t, selection.IsFinal)
	require.Equal(t, int64(11), selection.Total.Int64())
	require.Equal(t, 0, selection.Inputs[0].Note.AssetID.Cmp(privacycrypto.HashString("uclair")))
	require.Equal(t, 0, selection.Inputs[1].Note.AssetID.Cmp(privacycrypto.HashString("uclair")))
}

func TestSelectInputsFallsBackToPositivePairWhenSingleNoteNeedsZero(t *testing.T) {
	notes := []privacyscan.FoundNote{
		{Note: privacytypes.Note{Amount: big.NewInt(11), AssetID: privacycrypto.HashString("uclair")}, IsSpent: false},
		{Note: privacytypes.Note{Amount: big.NewInt(10), AssetID: privacycrypto.HashString("uclair")}, IsSpent: false},
	}

	selection := SelectInputs(notes, "uclair", big.NewInt(8))
	require.False(t, selection.NeedsZeroDummy)
	require.True(t, selection.IsFinal)
	require.Equal(t, int64(21), selection.Total.Int64())
	require.Equal(t, int64(10), selection.Inputs[0].Note.Amount.Int64())
	require.Equal(t, int64(11), selection.Inputs[1].Note.Amount.Int64())
}

func TestSelectInputsChoosesSmallestSufficientPairDeterministically(t *testing.T) {
	notes := []privacyscan.FoundNote{
		{Note: privacytypes.Note{Amount: big.NewInt(11), AssetID: privacycrypto.HashString("uclair")}, Nullifier: "c", Height: 3, IsSpent: false},
		{Note: privacytypes.Note{Amount: big.NewInt(5), AssetID: privacycrypto.HashString("uclair")}, Nullifier: "a", Height: 1, IsSpent: false},
		{Note: privacytypes.Note{Amount: big.NewInt(7), AssetID: privacycrypto.HashString("uclair")}, Nullifier: "b", Height: 2, IsSpent: false},
	}

	selection := SelectInputs(notes, "uclair", big.NewInt(12))
	require.False(t, selection.NeedsZeroDummy)
	require.True(t, selection.IsFinal)
	require.Equal(t, int64(12), selection.Total.Int64())
	require.Equal(t, int64(5), selection.Inputs[0].Note.Amount.Int64())
	require.Equal(t, int64(7), selection.Inputs[1].Note.Amount.Int64())
}

func TestSelectInputsRequiresDummyWhenPairWouldOverflowOutputAmounts(t *testing.T) {
	maxAmount := privacytypes.MaxShieldedAmount()
	notes := []privacyscan.FoundNote{
		{Note: privacytypes.Note{Amount: new(big.Int).Set(maxAmount), AssetID: privacycrypto.HashString("uclair")}, Nullifier: "a", Height: 1, IsSpent: false},
		{Note: privacytypes.Note{Amount: new(big.Int).Set(maxAmount), AssetID: privacycrypto.HashString("uclair")}, Nullifier: "b", Height: 2, IsSpent: false},
	}

	selection := SelectInputs(notes, "uclair", big.NewInt(1))
	require.True(t, selection.NeedsZeroDummy)
	require.False(t, selection.IsFinal)
	require.Equal(t, 0, selection.Total.Sign())
}

func TestSelectInputsChoosesLargestMergePairWhenNoFinalPairExists(t *testing.T) {
	notes := []privacyscan.FoundNote{
		{Note: privacytypes.Note{Amount: big.NewInt(2), AssetID: privacycrypto.HashString("uclair")}, Nullifier: "a", Height: 1, IsSpent: false},
		{Note: privacytypes.Note{Amount: big.NewInt(3), AssetID: privacycrypto.HashString("uclair")}, Nullifier: "b", Height: 2, IsSpent: false},
		{Note: privacytypes.Note{Amount: big.NewInt(9), AssetID: privacycrypto.HashString("uclair")}, Nullifier: "c", Height: 3, IsSpent: false},
	}

	selection := SelectInputs(notes, "uclair", big.NewInt(20))
	require.False(t, selection.NeedsZeroDummy)
	require.False(t, selection.IsFinal)
	require.Equal(t, int64(12), selection.Total.Int64())
	require.Equal(t, int64(3), selection.Inputs[0].Note.Amount.Int64())
	require.Equal(t, int64(9), selection.Inputs[1].Note.Amount.Int64())
}
