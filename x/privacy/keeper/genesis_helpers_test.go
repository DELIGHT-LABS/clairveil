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
