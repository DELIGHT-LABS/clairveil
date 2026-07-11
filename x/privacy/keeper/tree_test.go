package keeper

import (
	"encoding/hex"
	"testing"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/store/v2/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
	"github.com/stretchr/testify/require"
)

func setupTreeKeeper() (*Keeper, sdk.Context) {
	storeKey := types.NewKVStoreKey(privacytypes.StoreKey)
	tKey := types.NewTransientStoreKey("transient_test")
	ctx := testutil.DefaultContext(storeKey, tKey)

	k := NewKeeper(nil, runtime.NewKVStoreService(storeKey), paramtypes.Subspace{}, nil)
	return k, ctx
}

func deleteLeaf(k *Keeper, ctx sdk.Context, index uint64) {
	store := k.storeService.OpenKVStore(ctx)
	key := append([]byte("Leaf/"), sdk.Uint64ToBigEndian(index)...)
	store.Delete(key)
}

func deleteMerkleNode(k *Keeper, ctx sdk.Context, level uint8, index uint64) {
	store := k.storeService.OpenKVStore(ctx)
	store.Delete(privacytypes.GetMerkleNodeKey(level, index))
}

func naiveRootFromLeaves(leaves [][]byte) []byte {
	if len(leaves) == 0 {
		return emptyNodeBytes(MerkleDepth)
	}

	layer := make([][]byte, len(leaves))
	copy(layer, leaves)

	for i := 0; i < MerkleDepth; i++ {
		nextLayerLen := (len(layer) + 1) / 2
		nextLayer := make([][]byte, nextLayerLen)

		for j := 0; j < len(layer); j += 2 {
			left := layer[j]
			right := emptyNodeBytes(uint32(i))
			if j+1 < len(layer) {
				right = layer[j+1]
			}
			nextLayer[j/2] = hashNodes(uint32(i), left, right)
		}

		layer = nextLayer
	}

	return layer[0]
}

func rootFromPath(commitment []byte, path []string, helper []uint32) ([]byte, error) {
	current := commitment

	for i := 0; i < MerkleDepth; i++ {
		sibling, err := hex.DecodeString(path[i])
		if err != nil {
			return nil, err
		}

		if helper[i] == 0 {
			current = hashNodes(uint32(i), current, sibling)
		} else {
			current = hashNodes(uint32(i), sibling, current)
		}
	}

	return current, nil
}

func TestAppendCommitmentIncrementalRoot(t *testing.T) {
	k, ctx := setupTreeKeeper()

	commitments := [][]byte{fixedFieldBytes(1), fixedFieldBytes(2), fixedFieldBytes(3), fixedFieldBytes(4), fixedFieldBytes(5)}
	leaves := make([][]byte, 0, len(commitments))

	for i, commitment := range commitments {
		require.NoError(t, k.AppendCommitment(ctx, commitment))
		leaves = append(leaves, commitment)

		expectedRoot := naiveRootFromLeaves(leaves)
		storedRoot := k.GetMerkleNode(ctx, uint8(MerkleDepth), 0)

		require.Equal(t, expectedRoot, storedRoot, "append step %d root mismatch", i)
		require.True(t, k.CheckHistoricalRoot(ctx, storedRoot))
	}

	require.Equal(t, uint64(len(commitments)), k.GetLeafCount(ctx))
}

func TestEmptyTreeAndPathUseExactDepthSpecificRoots(t *testing.T) {
	k, ctx := setupTreeKeeper()

	emptyRoot, err := k.RecalculateRoot(ctx, 0)
	require.NoError(t, err)
	require.Equal(t, emptyNodeBytes(MerkleDepth), emptyRoot)
	require.NotEqual(t, zeroNodeBytes(), emptyRoot)

	commitment := fixedFieldBytes(0x77)
	require.NoError(t, k.AppendCommitment(ctx, commitment))
	path, helper, root, err := k.GetPath(ctx, commitment)
	require.NoError(t, err)
	require.Len(t, path, MerkleDepth)
	for level := 0; level < MerkleDepth; level++ {
		require.Equal(t, hex.EncodeToString(emptyNodeBytes(uint32(level))), path[level], "empty sibling mismatch at level %d", level)
		require.Zero(t, helper[level])
	}
	reconstructed, err := rootFromPath(commitment, path, helper)
	require.NoError(t, err)
	require.Equal(t, root, reconstructed)
}

func TestNoteTreeNodeV1SeparatesLevels(t *testing.T) {
	left := fixedFieldBytes(1)
	right := fixedFieldBytes(2)
	require.NotEqual(t, hashNodes(0, left, right), hashNodes(1, left, right))
}

func TestAppendCommitmentRejectsZeroAndNonCanonicalEncoding(t *testing.T) {
	k, ctx := setupTreeKeeper()

	require.ErrorIs(t, k.AppendCommitment(ctx, zeroNodeBytes()), errMerkleZeroCommitment)
	require.ErrorContains(t, k.AppendCommitment(ctx, []byte{1}), "exactly 32 bytes")
	require.Zero(t, k.GetLeafCount(ctx))
}

func TestAppendCommitmentAllowsFinalLeaf(t *testing.T) {
	k, ctx := setupTreeKeeper()

	k.SetLeafCount(ctx, MaxMerkleLeaves-1)
	k.SetMerkleNode(ctx, uint8(MerkleDepth), 0, fixedFieldBytes(101))
	currentIndex := MaxMerkleLeaves - 1
	for level := 0; level < MerkleDepth; level++ {
		if currentIndex%2 == 1 {
			k.SetMerkleNode(ctx, uint8(level), currentIndex-1, fixedFieldBytes(uint64(200+level)))
		}
		currentIndex /= 2
	}

	commitment := fixedFieldBytes(102)
	require.NoError(t, k.AppendCommitment(ctx, commitment))
	require.Equal(t, MaxMerkleLeaves, k.GetLeafCount(ctx))

	index, found := k.GetCommitmentIndex(ctx, commitment)
	require.True(t, found)
	require.Equal(t, MaxMerkleLeaves-1, index)
}

func TestAppendCommitmentRejectsFullTree(t *testing.T) {
	k, ctx := setupTreeKeeper()

	k.SetLeafCount(ctx, MaxMerkleLeaves)

	err := k.AppendCommitment(ctx, fixedFieldBytes(103))
	require.Error(t, err)
	require.Contains(t, err.Error(), "merkle tree capacity exceeded")
	require.Equal(t, MaxMerkleLeaves, k.GetLeafCount(ctx))
}

func TestGetPathFromIncrementalNodes(t *testing.T) {
	k, ctx := setupTreeKeeper()

	commitments := [][]byte{fixedFieldBytes(0x10), fixedFieldBytes(0x20), fixedFieldBytes(0x30), fixedFieldBytes(0x40)}
	for _, commitment := range commitments {
		require.NoError(t, k.AppendCommitment(ctx, commitment))
	}

	fullRoot := k.GetMerkleNode(ctx, uint8(MerkleDepth), 0)

	for idx, commitment := range commitments {
		path, helper, root, err := k.GetPath(ctx, commitment)
		require.NoError(t, err)
		require.Len(t, path, MerkleDepth)
		require.Len(t, helper, MerkleDepth)
		require.Equal(t, fullRoot, root)

		computedRoot, err := rootFromPath(commitment, path, helper)
		require.NoError(t, err)
		require.Equal(t, root, computedRoot)

		currentIndex := uint64(idx)
		for i := 0; i < MerkleDepth; i++ {
			require.Equal(t, uint32(currentIndex%2), helper[i])
			currentIndex /= 2
		}
	}
}

func TestGetPathBootstrapsLegacyLeafState(t *testing.T) {
	k, ctx := setupTreeKeeper()

	leaves := [][]byte{fixedFieldBytes(0xaa), fixedFieldBytes(0xbb), fixedFieldBytes(0xcc)}
	for i, leaf := range leaves {
		k.SetLeaf(ctx, uint64(i), leaf)
	}
	k.SetLeafCount(ctx, uint64(len(leaves)))

	require.False(t, k.HasMerkleNode(ctx, uint8(MerkleDepth), 0))

	path, helper, root, err := k.GetPath(ctx, leaves[1])
	require.NoError(t, err)
	require.Len(t, path, MerkleDepth)
	require.Len(t, helper, MerkleDepth)

	expectedRoot := naiveRootFromLeaves(leaves)
	require.Equal(t, expectedRoot, root)
	require.True(t, k.HasMerkleNode(ctx, uint8(MerkleDepth), 0))

	idx, found := k.GetCommitmentIndex(ctx, leaves[1])
	require.True(t, found)
	require.Equal(t, uint64(1), idx)
}

func TestAppendCommitmentRejectsDuplicateWithoutChangingState(t *testing.T) {
	k, ctx := setupTreeKeeper()

	dup := fixedFieldBytes(0x42)
	require.NoError(t, k.AppendCommitment(ctx, dup))
	require.NoError(t, k.AppendCommitment(ctx, fixedFieldBytes(0x99)))
	rootBefore := append([]byte(nil), k.GetMerkleNode(ctx, uint8(MerkleDepth), 0)...)
	countBefore := k.GetLeafCount(ctx)

	err := k.AppendCommitment(ctx, dup)
	require.ErrorIs(t, err, errMerkleDuplicateCommitment)
	require.Equal(t, countBefore, k.GetLeafCount(ctx))
	require.Equal(t, rootBefore, k.GetMerkleNode(ctx, uint8(MerkleDepth), 0))
	require.Empty(t, k.GetLeaf(ctx, countBefore))

	idx, found := k.GetCommitmentIndex(ctx, dup)
	require.True(t, found)
	require.Equal(t, uint64(0), idx)
	require.True(t, k.HasCommitment(ctx, dup))
}

func TestGetPathNotFound(t *testing.T) {
	k, ctx := setupTreeKeeper()

	require.NoError(t, k.AppendCommitment(ctx, fixedFieldBytes(0x01)))
	_, _, _, err := k.GetPath(ctx, fixedFieldBytes(0xff))
	require.Error(t, err)
}

func TestGetPathRejectsOverflowTreeState(t *testing.T) {
	k, ctx := setupTreeKeeper()

	k.SetLeafCount(ctx, MaxMerkleLeaves+1)

	_, _, _, err := k.GetPath(ctx, fixedFieldBytes(104))
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds max capacity")
}

func TestGetPathRejectsLargeLegacyTreeWithoutCachedRoot(t *testing.T) {
	k, ctx := setupTreeKeeper()

	k.SetLeafCount(ctx, MaxMerkleRebuildLeaves+1)

	_, _, _, err := k.GetPath(ctx, fixedFieldBytes(105))
	require.Error(t, err)
	require.Contains(t, err.Error(), "cached root is required")
}

func TestGetPathRejectsMissingRequiredSiblingNode(t *testing.T) {
	k, ctx := setupTreeKeeper()

	first := fixedFieldBytes(106)
	second := fixedFieldBytes(107)
	require.NoError(t, k.AppendCommitment(ctx, first))
	require.NoError(t, k.AppendCommitment(ctx, second))

	deleteMerkleNode(k, ctx, 0, 1)

	_, _, _, err := k.GetPath(ctx, first)
	require.Error(t, err)
	require.Contains(t, err.Error(), "merkle tree node is missing")
	require.Contains(t, err.Error(), "level=0")
	require.Contains(t, err.Error(), "index=1")
}

func TestAppendCommitmentRejectsMissingRequiredLeftSibling(t *testing.T) {
	k, ctx := setupTreeKeeper()

	k.SetLeafCount(ctx, 1)
	k.SetMerkleNode(ctx, uint8(MerkleDepth), 0, fixedFieldBytes(108))

	newCommitment := fixedFieldBytes(109)
	err := k.AppendCommitment(ctx, newCommitment)
	require.Error(t, err)
	require.Contains(t, err.Error(), "merkle tree node is missing")
	require.Contains(t, err.Error(), "level=0")
	require.Contains(t, err.Error(), "index=0")
	require.Empty(t, k.GetLeaf(ctx, 1))

	_, found := k.GetCommitmentIndex(ctx, newCommitment)
	require.False(t, found)
}

func TestRecalculateRootRejectsMissingLeaf(t *testing.T) {
	k, ctx := setupTreeKeeper()

	k.SetLeaf(ctx, 0, fixedFieldBytes(110))
	k.SetLeafCount(ctx, 2)

	_, err := k.RecalculateRoot(ctx, 2)
	require.Error(t, err)
	require.Contains(t, err.Error(), "merkle tree leaf is missing")
	require.Contains(t, err.Error(), "index=1")
}

func TestGetPathRejectsMissingLeafDuringSmallRebuild(t *testing.T) {
	k, ctx := setupTreeKeeper()

	first := fixedFieldBytes(111)
	k.SetLeaf(ctx, 0, first)
	k.SetLeaf(ctx, 1, fixedFieldBytes(112))
	k.SetLeafCount(ctx, 2)
	deleteLeaf(k, ctx, 1)

	_, _, _, err := k.GetPath(ctx, first)
	require.Error(t, err)
	require.Contains(t, err.Error(), "merkle tree leaf is missing")
	require.Contains(t, err.Error(), "index=1")
}

func TestRecalculateRootRejectsOverflowTreeState(t *testing.T) {
	k, ctx := setupTreeKeeper()

	_, err := k.RecalculateRoot(ctx, MaxMerkleLeaves+1)
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds max capacity")
}

func TestRecalculateRootRejectsLargeRebuildWithoutCachedRoot(t *testing.T) {
	k, ctx := setupTreeKeeper()

	_, err := k.RecalculateRoot(ctx, MaxMerkleRebuildLeaves+1)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cached root is required")
}

func TestNullifierCanonicalization(t *testing.T) {
	k, ctx := setupTreeKeeper()

	k.SetNullifier(ctx, []byte{0x01})
	require.True(t, k.HasNullifier(ctx, []byte{0x01}))
	require.True(t, k.HasNullifier(ctx, []byte{0x00, 0x01}))
}

func TestHistoricalRootCanonicalization(t *testing.T) {
	k, ctx := setupTreeKeeper()

	k.SetHistoricalRoot(ctx, []byte{0x02})
	require.True(t, k.CheckHistoricalRoot(ctx, []byte{0x02}))
	require.True(t, k.CheckHistoricalRoot(ctx, []byte{0x00, 0x02}))
}
