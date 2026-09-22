package transfer

import (
	"math/big"
	"testing"

	privacyamount "github.com/DELIGHT-LABS/clairveil/x/privacy/amount"
	"github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
	"github.com/stretchr/testify/require"

	privacyscan "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/scan"
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
		{Note: privacytypes.Note{Amount: big.NewInt(5), AssetID: privacytypes.ComputeAssetIDV1("uclair")}, IsSpent: false},
		{Note: privacytypes.Note{Amount: big.NewInt(7), AssetID: privacytypes.ComputeAssetIDV1("uatom")}, IsSpent: false},
		{Note: privacytypes.Note{Amount: big.NewInt(11), AssetID: privacytypes.ComputeAssetIDV1("uclair")}, IsSpent: true},
		{Note: privacytypes.Note{Amount: big.NewInt(13), AssetID: privacytypes.ComputeAssetIDV1("uclair")}, IsSpent: false},
	}

	spendable, total := SummarizeSpendableNotesByDenom(testSecretFoundNotes(t, notes), "uclair")

	require.Len(t, spendable, 2)
	require.Equal(t, privacyamount.FromUint64(5), spendable[0].Note.Amount)
	require.Equal(t, privacyamount.FromUint64(13), spendable[1].Note.Amount)
	require.Equal(t, int64(18), total.Int64())
}

func TestFindExactMatchSpendableNoteByDenomIgnoresDifferentDenom(t *testing.T) {
	notes := []privacyscan.FoundNote{
		{Note: privacytypes.Note{Amount: big.NewInt(10), AssetID: privacytypes.ComputeAssetIDV1("uatom")}, IsSpent: false},
		{Note: privacytypes.Note{Amount: big.NewInt(10), AssetID: privacytypes.ComputeAssetIDV1("uclair")}, IsSpent: true},
		{Note: privacytypes.Note{Amount: big.NewInt(10), AssetID: privacytypes.ComputeAssetIDV1("uclair")}, IsSpent: false},
	}

	selected := FindExactMatchSpendableNoteByDenom(testSecretFoundNotes(t, notes), "uclair", big.NewInt(10))
	require.NotNil(t, selected)
	require.Equal(t, privacytypes.ComputeSecretAssetIDV1("uclair").Bytes(), selected.Note.AssetID.Bytes())
	require.Equal(t, privacyamount.FromUint64(10), selected.Note.Amount)
	require.False(t, selected.IsSpent)
}

func TestFindExactMatchSpendableNoteByDenomUsesDeterministicOrder(t *testing.T) {
	notes := []privacyscan.FoundNote{
		{
			Note:      privacytypes.Note{Amount: big.NewInt(10), AssetID: privacytypes.ComputeAssetIDV1("uclair")},
			Nullifier: "bb",
			Height:    9,
			IsSpent:   false,
		},
		{
			Note:      privacytypes.Note{Amount: big.NewInt(10), AssetID: privacytypes.ComputeAssetIDV1("uclair")},
			Nullifier: "aa",
			Height:    5,
			IsSpent:   false,
		},
	}

	selected := FindExactMatchSpendableNoteByDenom(testSecretFoundNotes(t, notes), "uclair", big.NewInt(10))
	require.NotNil(t, selected)
	require.Equal(t, "aa", selected.Nullifier)
}

func TestPlannerStateFingerprintUsesSortedSameDenomSpendableNotes(t *testing.T) {
	left := []privacyscan.FoundNote{
		{Note: privacytypes.Note{Amount: big.NewInt(10), AssetID: privacytypes.ComputeAssetIDV1("uclair")}, Nullifier: "bb", Height: 9},
		{Note: privacytypes.Note{Amount: big.NewInt(3), AssetID: privacytypes.ComputeAssetIDV1("uclair")}, Nullifier: "aa", Height: 5},
		{Note: privacytypes.Note{Amount: big.NewInt(9), AssetID: privacytypes.ComputeAssetIDV1("uatom")}, Nullifier: "xx", Height: 4},
	}
	right := []privacyscan.FoundNote{
		{Note: privacytypes.Note{Amount: big.NewInt(3), AssetID: privacytypes.ComputeAssetIDV1("uclair")}, Nullifier: "aa", Height: 5},
		{Note: privacytypes.Note{Amount: big.NewInt(10), AssetID: privacytypes.ComputeAssetIDV1("uclair")}, Nullifier: "bb", Height: 9},
	}

	require.Equal(t, PlannerStateFingerprint(testSecretFoundNotes(t, left), "uclair", big.NewInt(7)), PlannerStateFingerprint(testSecretFoundNotes(t, right), "uclair", big.NewInt(7)))
}

func TestSelectInputsFiltersDifferentDenomZeroNote(t *testing.T) {
	notes := []privacyscan.FoundNote{
		{Note: privacytypes.Note{Amount: big.NewInt(10), AssetID: privacytypes.ComputeAssetIDV1("uclair")}, IsSpent: false},
		{Note: privacytypes.Note{Amount: big.NewInt(0), AssetID: privacytypes.ComputeAssetIDV1("uatom")}, IsSpent: false},
	}

	selection := SelectInputs(testSecretFoundNotes(t, notes), "uclair", big.NewInt(10))
	require.Equal(t, int64(0), selection.Total.Int64())
	require.False(t, selection.IsFinal)
	require.True(t, selection.NeedsZeroDummy)
}

func TestSelectInputsUsesSameDenomPairOnly(t *testing.T) {
	notes := []privacyscan.FoundNote{
		{Note: privacytypes.Note{Amount: big.NewInt(7), AssetID: privacytypes.ComputeAssetIDV1("uclair")}, IsSpent: false},
		{Note: privacytypes.Note{Amount: big.NewInt(5), AssetID: privacytypes.ComputeAssetIDV1("uatom")}, IsSpent: false},
		{Note: privacytypes.Note{Amount: big.NewInt(4), AssetID: privacytypes.ComputeAssetIDV1("uclair")}, IsSpent: false},
	}

	selection := SelectInputs(testSecretFoundNotes(t, notes), "uclair", big.NewInt(10))
	require.False(t, selection.NeedsZeroDummy)
	require.True(t, selection.IsFinal)
	require.Equal(t, int64(11), selection.Total.Int64())
	require.Equal(t, privacytypes.ComputeSecretAssetIDV1("uclair").Bytes(), selection.Inputs[0].Note.AssetID.Bytes())
	require.Equal(t, privacytypes.ComputeSecretAssetIDV1("uclair").Bytes(), selection.Inputs[1].Note.AssetID.Bytes())
}

func TestSelectInputsFallsBackToPositivePairWhenSingleNoteNeedsZero(t *testing.T) {
	notes := []privacyscan.FoundNote{
		{Note: privacytypes.Note{Amount: big.NewInt(11), AssetID: privacytypes.ComputeAssetIDV1("uclair")}, IsSpent: false},
		{Note: privacytypes.Note{Amount: big.NewInt(10), AssetID: privacytypes.ComputeAssetIDV1("uclair")}, IsSpent: false},
	}

	selection := SelectInputs(testSecretFoundNotes(t, notes), "uclair", big.NewInt(8))
	require.False(t, selection.NeedsZeroDummy)
	require.True(t, selection.IsFinal)
	require.Equal(t, int64(21), selection.Total.Int64())
	require.Equal(t, privacyamount.FromUint64(10), selection.Inputs[0].Note.Amount)
	require.Equal(t, privacyamount.FromUint64(11), selection.Inputs[1].Note.Amount)
}

func TestSelectInputsChoosesSmallestSufficientPairDeterministically(t *testing.T) {
	notes := []privacyscan.FoundNote{
		{Note: privacytypes.Note{Amount: big.NewInt(11), AssetID: privacytypes.ComputeAssetIDV1("uclair")}, Nullifier: "c", Height: 3, IsSpent: false},
		{Note: privacytypes.Note{Amount: big.NewInt(5), AssetID: privacytypes.ComputeAssetIDV1("uclair")}, Nullifier: "a", Height: 1, IsSpent: false},
		{Note: privacytypes.Note{Amount: big.NewInt(7), AssetID: privacytypes.ComputeAssetIDV1("uclair")}, Nullifier: "b", Height: 2, IsSpent: false},
	}

	selection := SelectInputs(testSecretFoundNotes(t, notes), "uclair", big.NewInt(12))
	require.False(t, selection.NeedsZeroDummy)
	require.True(t, selection.IsFinal)
	require.Equal(t, int64(12), selection.Total.Int64())
	require.Equal(t, privacyamount.FromUint64(5), selection.Inputs[0].Note.Amount)
	require.Equal(t, privacyamount.FromUint64(7), selection.Inputs[1].Note.Amount)
}

func TestSelectInputsRequiresDummyWhenPairWouldOverflowOutputAmounts(t *testing.T) {
	maxAmount := privacytypes.MaxShieldedAmount()
	notes := []privacyscan.FoundNote{
		{Note: privacytypes.Note{Amount: new(big.Int).Set(maxAmount), AssetID: privacytypes.ComputeAssetIDV1("uclair")}, Nullifier: "a", Height: 1, IsSpent: false},
		{Note: privacytypes.Note{Amount: new(big.Int).Set(maxAmount), AssetID: privacytypes.ComputeAssetIDV1("uclair")}, Nullifier: "b", Height: 2, IsSpent: false},
	}

	selection := SelectInputs(testSecretFoundNotes(t, notes), "uclair", big.NewInt(1))
	require.True(t, selection.NeedsZeroDummy)
	require.False(t, selection.IsFinal)
	require.Equal(t, 0, selection.Total.Sign())
}

func TestSelectInputsChoosesLargestMergePairWhenNoFinalPairExists(t *testing.T) {
	notes := []privacyscan.FoundNote{
		{Note: privacytypes.Note{Amount: big.NewInt(2), AssetID: privacytypes.ComputeAssetIDV1("uclair")}, Nullifier: "a", Height: 1, IsSpent: false},
		{Note: privacytypes.Note{Amount: big.NewInt(3), AssetID: privacytypes.ComputeAssetIDV1("uclair")}, Nullifier: "b", Height: 2, IsSpent: false},
		{Note: privacytypes.Note{Amount: big.NewInt(9), AssetID: privacytypes.ComputeAssetIDV1("uclair")}, Nullifier: "c", Height: 3, IsSpent: false},
	}

	selection := SelectInputs(testSecretFoundNotes(t, notes), "uclair", big.NewInt(20))
	require.False(t, selection.NeedsZeroDummy)
	require.False(t, selection.IsFinal)
	require.Equal(t, int64(12), selection.Total.Int64())
	require.Equal(t, privacyamount.FromUint64(3), selection.Inputs[0].Note.Amount)
	require.Equal(t, privacyamount.FromUint64(9), selection.Inputs[1].Note.Amount)
}

func TestSelectInputBatchBacktracksAcrossOriginalOrder(t *testing.T) {
	notes := []privacyscan.FoundNote{
		{Note: privacytypes.Note{Amount: big.NewInt(0), AssetID: privacytypes.ComputeAssetIDV1("uclair")}, Nullifier: "zero", Height: 1, IsSpent: false},
		{Note: privacytypes.Note{Amount: big.NewInt(2), AssetID: privacytypes.ComputeAssetIDV1("uclair")}, Nullifier: "two", Height: 2, IsSpent: false},
		{Note: privacytypes.Note{Amount: big.NewInt(3), AssetID: privacytypes.ComputeAssetIDV1("uclair")}, Nullifier: "three", Height: 3, IsSpent: false},
		{Note: privacytypes.Note{Amount: big.NewInt(100), AssetID: privacytypes.ComputeAssetIDV1("uclair")}, Nullifier: "hundred", Height: 4, IsSpent: false},
	}

	selections, err := SelectInputBatch(testSecretFoundNotes(t, notes), "uclair", []*big.Int{big.NewInt(5), big.NewInt(100)})
	require.NoError(t, err)
	require.Len(t, selections, 2)
	require.True(t, selections[0].IsFinal)
	require.True(t, selections[1].IsFinal)
	require.Equal(t, int64(5), selections[0].Total.Int64())
	require.Equal(t, []privacyamount.Amount128{privacyamount.FromUint64(2), privacyamount.FromUint64(3)}, []privacyamount.Amount128{selections[0].Inputs[0].Note.Amount, selections[0].Inputs[1].Note.Amount})
	require.Equal(t, int64(100), selections[1].Total.Int64())
	require.Equal(t, []privacyamount.Amount128{privacyamount.FromUint64(100), privacyamount.FromUint64(0)}, []privacyamount.Amount128{selections[1].Inputs[0].Note.Amount, selections[1].Inputs[1].Note.Amount})
}

func TestSelectInputsSkipsOperationOverflowAndContinues(t *testing.T) {
	max := privacytypes.MaxShieldedAmount()
	notes := []privacyscan.FoundNote{
		{Note: privacytypes.Note{Amount: max, AssetID: privacytypes.ComputeAssetIDV1("uclair")}, Nullifier: "maximum"},
		{Note: privacytypes.Note{Amount: new(big.Int).Sub(max, big.NewInt(1)), AssetID: privacytypes.ComputeAssetIDV1("uclair")}, Nullifier: "almost"},
		{Note: privacytypes.Note{Amount: big.NewInt(1), AssetID: privacytypes.ComputeAssetIDV1("uclair")}, Nullifier: "one"},
	}
	selection := SelectInputs(testSecretFoundNotes(t, notes), "uclair", max)
	require.True(t, selection.IsFinal)
	require.Zero(t, selection.Total.Cmp(max))
	for _, input := range selection.Inputs {
		require.NotEqual(t, "maximum", input.Nullifier)
	}
	// Sufficient balance can still require manually splitting a note.
	half := new(big.Int).Lsh(big.NewInt(1), 127)
	notes = []privacyscan.FoundNote{
		{Note: privacytypes.Note{Amount: half, AssetID: privacytypes.ComputeAssetIDV1("uclair")}, Nullifier: "left"},
		{Note: privacytypes.Note{Amount: half, AssetID: privacytypes.ComputeAssetIDV1("uclair")}, Nullifier: "right"},
	}
	_, err := NewRecursivePlannerRuntime().DecideNextStep(RecursivePlannerInput{FoundNotes: testSecretFoundNotes(t, notes), TargetDenom: "uclair", TargetAmount: max, Step: 1})
	require.ErrorContains(t, err, "note preparation required")
	require.NotContains(t, err.Error(), "insufficient")
}
