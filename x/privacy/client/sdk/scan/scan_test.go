package scan

import (
	"math/big"
	"testing"

	cmttypes "github.com/cometbft/cometbft/rpc/core/types"
	"github.com/stretchr/testify/require"

	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
	privacyidentity "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/identity"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestParseNoteBytesAndBuildFoundNote(t *testing.T) {
	rootSeed := []byte("scan-root-seed")

	_, spendPubKey, _ := privacyidentity.DeriveSpendKeys(rootSeed)
	_, viewPubKey, _ := privacyidentity.DeriveViewKeys(rootSeed)

	note, err := privacytypes.NewNote(
		pointBigInt(&spendPubKey.X),
		pointBigInt(&spendPubKey.Y),
		pointBigInt(&viewPubKey.X),
		pointBigInt(&viewPubKey.Y),
		big.NewInt(7),
		"uclair",
		"sdk-scan-test",
	)
	require.NoError(t, err)

	parsed, err := ParseNoteBytes(note.Bytes())
	require.NoError(t, err)
	require.Equal(t, note.Amount.String(), parsed.Amount.String())

	txRes := &cmttypes.ResultTx{
		Hash:   []byte{0xAA, 0xBB, 0xCC},
		Height: 42,
	}
	found := BuildFoundNote(parsed, txRes)

	expectedNullifier, err := privacyfield.CanonicalHexFromBigInt(note.ComputeNullifier())
	require.NoError(t, err)

	require.Equal(t, expectedNullifier, found.Nullifier)
	require.Equal(t, "AABBCC", found.TxHash)
	require.Equal(t, int64(42), found.Height)
	require.False(t, found.IsSpent)
}

func pointBigInt(value interface{ BigInt(*big.Int) *big.Int }) *big.Int {
	v := new(big.Int)
	value.BigInt(v)
	return v
}
