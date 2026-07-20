package cli

import (
	"math/big"
	"testing"

	"github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
	"github.com/stretchr/testify/require"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestResolveTransferRecipientRejectsTransparentAddress(t *testing.T) {
	_, _, err := resolveTransferRecipient(testBech32Address())
	require.Error(t, err)
	require.Contains(t, err.Error(), "shielded address")
}

func TestResolveTransferRecipientDecodesShieldedBundle(t *testing.T) {
	curve := twistededwards.GetEdwardsCurve()

	var spendPubKey twistededwards.PointAffine
	var viewPubKey twistededwards.PointAffine
	spendPubKey.ScalarMultiplication(&curve.Base, big.NewInt(3))
	viewPubKey.ScalarMultiplication(&curve.Base, big.NewInt(7))

	addr, err := privacytypes.EncodeShieldedAddressWithView(&spendPubKey, &viewPubKey)
	require.NoError(t, err)

	resolvedSpend, resolvedView, err := resolveTransferRecipient(addr)
	require.NoError(t, err)
	require.Equal(t, spendPubKey.Bytes(), resolvedSpend.Bytes())
	require.Equal(t, viewPubKey.Bytes(), resolvedView.Bytes())
}

func TestSelectInputsFiltersDifferentDenomZeroNote(t *testing.T) {
	notes := []FoundNote{
		{Note: privacytypes.Note{Amount: big.NewInt(10), AssetID: privacytypes.ComputeAssetIDV1("uclair")}, IsSpent: false, VerifiedUnspent: true},
		{Note: privacytypes.Note{Amount: big.NewInt(0), AssetID: privacytypes.ComputeAssetIDV1("uatom")}, IsSpent: false, VerifiedUnspent: true},
	}

	_, total, isFinal, needZero := selectInputs(notes, "uclair", big.NewInt(10))
	require.Equal(t, int64(0), total.Int64())
	require.False(t, isFinal)
	require.True(t, needZero)
}

func TestSelectInputsUsesSameDenomPairOnly(t *testing.T) {
	notes := []FoundNote{
		{Note: privacytypes.Note{Amount: big.NewInt(7), AssetID: privacytypes.ComputeAssetIDV1("uclair")}, IsSpent: false, VerifiedUnspent: true},
		{Note: privacytypes.Note{Amount: big.NewInt(5), AssetID: privacytypes.ComputeAssetIDV1("uatom")}, IsSpent: false, VerifiedUnspent: true},
		{Note: privacytypes.Note{Amount: big.NewInt(4), AssetID: privacytypes.ComputeAssetIDV1("uclair")}, IsSpent: false, VerifiedUnspent: true},
	}

	inputs, total, isFinal, needZero := selectInputs(notes, "uclair", big.NewInt(10))
	require.False(t, needZero)
	require.True(t, isFinal)
	require.Equal(t, int64(11), total.Int64())
	require.Equal(t, 0, inputs[0].Note.AssetID.Cmp(privacytypes.ComputeAssetIDV1("uclair")))
	require.Equal(t, 0, inputs[1].Note.AssetID.Cmp(privacytypes.ComputeAssetIDV1("uclair")))
}

func TestSelectInputsFallsBackToPositivePairWhenSingleNoteNeedsZero(t *testing.T) {
	notes := []FoundNote{
		{Note: privacytypes.Note{Amount: big.NewInt(11), AssetID: privacytypes.ComputeAssetIDV1("uclair")}, IsSpent: false, VerifiedUnspent: true},
		{Note: privacytypes.Note{Amount: big.NewInt(10), AssetID: privacytypes.ComputeAssetIDV1("uclair")}, IsSpent: false, VerifiedUnspent: true},
	}

	inputs, total, isFinal, needZero := selectInputs(notes, "uclair", big.NewInt(8))
	require.False(t, needZero)
	require.True(t, isFinal)
	require.Equal(t, int64(21), total.Int64())
	require.Equal(t, int64(10), inputs[0].Note.Amount.Int64())
	require.Equal(t, int64(11), inputs[1].Note.Amount.Int64())
}

func TestSelectInputsChoosesSmallestSufficientPairDeterministically(t *testing.T) {
	notes := []FoundNote{
		{Note: privacytypes.Note{Amount: big.NewInt(11), AssetID: privacytypes.ComputeAssetIDV1("uclair")}, Nullifier: "c", Height: 3, IsSpent: false, VerifiedUnspent: true},
		{Note: privacytypes.Note{Amount: big.NewInt(5), AssetID: privacytypes.ComputeAssetIDV1("uclair")}, Nullifier: "a", Height: 1, IsSpent: false, VerifiedUnspent: true},
		{Note: privacytypes.Note{Amount: big.NewInt(7), AssetID: privacytypes.ComputeAssetIDV1("uclair")}, Nullifier: "b", Height: 2, IsSpent: false, VerifiedUnspent: true},
	}

	inputs, total, isFinal, needZero := selectInputs(notes, "uclair", big.NewInt(12))
	require.False(t, needZero)
	require.True(t, isFinal)
	require.Equal(t, int64(12), total.Int64())
	require.Equal(t, int64(5), inputs[0].Note.Amount.Int64())
	require.Equal(t, int64(7), inputs[1].Note.Amount.Int64())
}

func TestSelectInputsChoosesLargestMergePairWhenNoFinalPairExists(t *testing.T) {
	notes := []FoundNote{
		{Note: privacytypes.Note{Amount: big.NewInt(2), AssetID: privacytypes.ComputeAssetIDV1("uclair")}, Nullifier: "a", Height: 1, IsSpent: false, VerifiedUnspent: true},
		{Note: privacytypes.Note{Amount: big.NewInt(3), AssetID: privacytypes.ComputeAssetIDV1("uclair")}, Nullifier: "b", Height: 2, IsSpent: false, VerifiedUnspent: true},
		{Note: privacytypes.Note{Amount: big.NewInt(9), AssetID: privacytypes.ComputeAssetIDV1("uclair")}, Nullifier: "c", Height: 3, IsSpent: false, VerifiedUnspent: true},
	}

	inputs, total, isFinal, needZero := selectInputs(notes, "uclair", big.NewInt(20))
	require.False(t, needZero)
	require.False(t, isFinal)
	require.Equal(t, int64(12), total.Int64())
	require.Equal(t, int64(3), inputs[0].Note.Amount.Int64())
	require.Equal(t, int64(9), inputs[1].Note.Amount.Int64())
}
