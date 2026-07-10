package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInitGenesisCommitmentsRejectsCapacityOverflow(t *testing.T) {
	k, ctx := setupTreeKeeper()
	k.SetLeafCount(ctx, MaxMerkleLeaves)

	err := k.InitGenesisCommitments(ctx, [][]byte{fixedFieldBytes(80)})
	require.Error(t, err)
	require.Contains(t, err.Error(), "genesis commitments exceed merkle tree capacity")
	require.Equal(t, MaxMerkleLeaves, k.GetLeafCount(ctx))
}

func TestInitGenesisCommitmentsRejectsDuplicateWithoutChangingPrefix(t *testing.T) {
	k, ctx := setupTreeKeeper()
	commitment := fixedFieldBytes(81)

	err := k.InitGenesisCommitments(ctx, [][]byte{commitment, commitment})
	require.ErrorContains(t, err, "duplicates index 0")
	require.Equal(t, uint64(0), k.GetLeafCount(ctx))
	require.Empty(t, k.GetLeaf(ctx, 0))
}

func TestInitGenesisHistoricalRootsRejectsForgedRoot(t *testing.T) {
	k, ctx := setupTreeKeeper()
	require.NoError(t, k.InitGenesisCommitments(ctx, [][]byte{fixedFieldBytes(82)}))

	err := k.InitGenesisHistoricalRoots(ctx, [][]byte{fixedFieldBytes(999)})
	require.ErrorContains(t, err, "does not match any commitment prefix root")
	require.False(t, k.CheckHistoricalRoot(ctx, fixedFieldBytes(999)))
}

func TestInitGenesisHistoricalRootsAcceptsRecomputedPrefixRoot(t *testing.T) {
	k, ctx := setupTreeKeeper()
	require.NoError(t, k.InitGenesisCommitments(ctx, [][]byte{fixedFieldBytes(83)}))
	root := append([]byte(nil), k.GetMerkleNode(ctx, uint8(MerkleDepth), 0)...)

	require.NoError(t, k.InitGenesisHistoricalRoots(ctx, [][]byte{root}))
}
