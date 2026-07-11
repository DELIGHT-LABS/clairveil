package keeper

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sdk "github.com/cosmos/cosmos-sdk/types"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestCommitmentPathsAtRootReturnsOneHistoricalSnapshot(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()
	first := fixedFieldBytes(201)
	second := fixedFieldBytes(202)
	third := fixedFieldBytes(203)

	ctx = ctx.WithBlockHeight(10)
	require.NoError(t, k.AppendCommitment(ctx, first))
	ctx = ctx.WithBlockHeight(11)
	require.NoError(t, k.AppendCommitment(ctx, second))
	historicalRoot := append([]byte(nil), k.GetMerkleNode(ctx, uint8(MerkleDepth), 0)...)
	ctx = ctx.WithBlockHeight(12)
	require.NoError(t, k.AppendCommitment(ctx, third))
	snapshotKey := privacytypes.GetMerkleRootSnapshotKey(historicalRoot)
	indexedBefore, err := k.storeService.OpenKVStore(ctx).Has(snapshotKey)
	require.NoError(t, err)
	require.True(t, indexedBefore, "append must persist every prefix snapshot")
	storedBefore, err := k.storeService.OpenKVStore(ctx).Get(snapshotKey)
	require.NoError(t, err)

	response, err := k.CommitmentPathsAtRoot(sdk.WrapSDKContext(ctx), &privacytypes.QueryCommitmentPathsAtRootRequest{
		CommitmentHexes: []string{hex.EncodeToString(first), hex.EncodeToString(second)},
		RootHex:         hex.EncodeToString(historicalRoot),
		SnapshotHeight:  11,
	})
	require.NoError(t, err)
	require.Equal(t, hex.EncodeToString(historicalRoot), response.RootHex)
	require.Equal(t, int64(11), response.SnapshotHeight)
	require.Equal(t, uint64(2), response.LeafCount)
	indexedAfter, err := k.storeService.OpenKVStore(ctx).Has(snapshotKey)
	require.NoError(t, err)
	require.True(t, indexedAfter)
	storedAfter, err := k.storeService.OpenKVStore(ctx).Get(snapshotKey)
	require.NoError(t, err)
	require.Equal(t, storedBefore, storedAfter, "query must not mutate the root snapshot index")
	require.Len(t, response.Paths, 2)
	for i, path := range response.Paths {
		require.Equal(t, uint64(i), path.LeafIndex)
		require.Len(t, path.Path, MerkleDepth)
		require.Len(t, path.PathHelper, MerkleDepth)
		commitment, decodeErr := hex.DecodeString(path.CommitmentHex)
		require.NoError(t, decodeErr)
		reconstructed, rebuildErr := rootFromPath(commitment, path.Path, path.PathHelper)
		require.NoError(t, rebuildErr)
		require.Equal(t, historicalRoot, reconstructed)
	}
}

func TestCommitmentPathsAtRootRejectsMixedSnapshotAndBounds(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()
	first := fixedFieldBytes(211)
	second := fixedFieldBytes(212)
	ctx = ctx.WithBlockHeight(20)
	require.NoError(t, k.AppendCommitment(ctx, first))
	firstRoot := append([]byte(nil), k.GetMerkleNode(ctx, uint8(MerkleDepth), 0)...)
	ctx = ctx.WithBlockHeight(21)
	require.NoError(t, k.AppendCommitment(ctx, second))

	_, err := k.CommitmentPathsAtRoot(sdk.WrapSDKContext(ctx), &privacytypes.QueryCommitmentPathsAtRootRequest{
		CommitmentHexes: []string{hex.EncodeToString(second)},
		RootHex:         hex.EncodeToString(firstRoot),
	})
	require.Equal(t, codes.NotFound, status.Code(err))

	_, err = k.CommitmentPathsAtRoot(sdk.WrapSDKContext(ctx), &privacytypes.QueryCommitmentPathsAtRootRequest{
		CommitmentHexes: []string{hex.EncodeToString(first)},
		RootHex:         hex.EncodeToString(firstRoot),
		SnapshotHeight:  999,
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	tooMany := make([]string, MaxCommitmentPathSnapshotQuery+1)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("%064x", i+1)
	}
	_, err = k.CommitmentPathsAtRoot(sdk.WrapSDKContext(ctx), &privacytypes.QueryCommitmentPathsAtRootRequest{
		CommitmentHexes: tooMany,
		RootHex:         hex.EncodeToString(firstRoot),
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestCommitmentPathsAtRootRejectsOversizedHistoricalRebuildBeforeLeafRead(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()
	root := fixedFieldBytes(241)
	snapshot := &privacytypes.MerkleRootSnapshotV1{
		Root:      root,
		LeafCount: MaxHistoricalPathQueryRebuildLeaves + 1,
		Height:    41,
	}
	k.SetLeafCount(ctx, snapshot.LeafCount+1)
	k.SetMerkleNode(ctx, uint8(MerkleDepth), 0, fixedFieldBytes(245))
	k.SetHistoricalRoot(ctx.WithBlockHeight(snapshot.Height), root)
	require.NoError(t, k.SetMerkleRootSnapshotV1(ctx, snapshot))

	response, err := k.CommitmentPathsAtRoot(sdk.WrapSDKContext(ctx), &privacytypes.QueryCommitmentPathsAtRootRequest{
		CommitmentHexes: []string{hex.EncodeToString(fixedFieldBytes(242))},
		RootHex:         hex.EncodeToString(root),
		SnapshotHeight:  snapshot.Height,
	})
	require.Nil(t, response)
	require.Equal(t, codes.ResourceExhausted, status.Code(err))
	require.Contains(t, err.Error(), fmt.Sprintf("max_query_rebuild_leaves=%d", MaxHistoricalPathQueryRebuildLeaves))
}

func TestCommitmentPathsAtRootHonorsCanceledContextBeforeHistoricalLeafRead(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()
	first := fixedFieldBytes(243)
	second := fixedFieldBytes(244)
	require.NoError(t, k.AppendCommitment(ctx.WithBlockHeight(50), first))
	historicalRoot := append([]byte(nil), k.GetMerkleNode(ctx, uint8(MerkleDepth), 0)...)
	require.NoError(t, k.AppendCommitment(ctx.WithBlockHeight(51), second))

	goCtx, cancel := context.WithCancel(sdk.WrapSDKContext(ctx))
	cancel()
	response, err := k.CommitmentPathsAtRoot(goCtx, &privacytypes.QueryCommitmentPathsAtRootRequest{
		CommitmentHexes: []string{hex.EncodeToString(first)},
		RootHex:         hex.EncodeToString(historicalRoot),
		SnapshotHeight:  50,
	})
	require.Nil(t, response)
	require.Equal(t, codes.Canceled, status.Code(err))
}

func TestCommitmentPathsAtRootRejectsWhenHistoricalRebuildAdmissionIsFull(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()
	first := fixedFieldBytes(246)
	second := fixedFieldBytes(247)
	require.NoError(t, k.AppendCommitment(ctx.WithBlockHeight(60), first))
	historicalRoot := append([]byte(nil), k.GetMerkleNode(ctx, uint8(MerkleDepth), 0)...)
	require.NoError(t, k.AppendCommitment(ctx.WithBlockHeight(61), second))
	for i := 0; i < MaxConcurrentHistoricalPathQueryRebuilds; i++ {
		k.historicalPathQueryRebuildSlots <- struct{}{}
		defer func() { <-k.historicalPathQueryRebuildSlots }()
	}

	response, err := k.CommitmentPathsAtRoot(sdk.WrapSDKContext(ctx), &privacytypes.QueryCommitmentPathsAtRootRequest{
		CommitmentHexes: []string{hex.EncodeToString(first)},
		RootHex:         hex.EncodeToString(historicalRoot),
		SnapshotHeight:  60,
	})
	require.Nil(t, response)
	require.Equal(t, codes.ResourceExhausted, status.Code(err))
	require.Contains(t, err.Error(), "admission is full")
}

func TestCommitmentPathsAtRootRequiresCachedRootWithoutOnlineRebuild(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()
	first := fixedFieldBytes(248)
	second := fixedFieldBytes(249)
	require.NoError(t, k.AppendCommitment(ctx.WithBlockHeight(70), first))
	historicalRoot := append([]byte(nil), k.GetMerkleNode(ctx, uint8(MerkleDepth), 0)...)
	require.NoError(t, k.AppendCommitment(ctx.WithBlockHeight(71), second))
	deleteMerkleNode(k, ctx, uint8(MerkleDepth), 0)

	response, err := k.CommitmentPathsAtRoot(sdk.WrapSDKContext(ctx), &privacytypes.QueryCommitmentPathsAtRootRequest{
		CommitmentHexes: []string{hex.EncodeToString(first)},
		RootHex:         hex.EncodeToString(historicalRoot),
		SnapshotHeight:  70,
	})
	require.Nil(t, response)
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	require.Contains(t, err.Error(), "offline repair")
	require.False(t, k.HasMerkleNode(ctx, uint8(MerkleDepth), 0), "query must not rebuild or mutate cached tree state")
}

func TestCommitmentPathsAtRootChecksCancellationBeforeCachedRootState(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()
	commitment := fixedFieldBytes(250)
	require.NoError(t, k.AppendCommitment(ctx.WithBlockHeight(80), commitment))
	root := append([]byte(nil), k.GetMerkleNode(ctx, uint8(MerkleDepth), 0)...)
	deleteMerkleNode(k, ctx, uint8(MerkleDepth), 0)

	goCtx, cancel := context.WithCancel(sdk.WrapSDKContext(ctx))
	cancel()
	response, err := k.CommitmentPathsAtRoot(goCtx, &privacytypes.QueryCommitmentPathsAtRootRequest{
		CommitmentHexes: []string{hex.EncodeToString(commitment)},
		RootHex:         hex.EncodeToString(root),
		SnapshotHeight:  80,
	})
	require.Nil(t, response)
	require.Equal(t, codes.Canceled, status.Code(err))
	require.False(t, k.HasMerkleNode(ctx, uint8(MerkleDepth), 0))
}

func BenchmarkHistoricalPathRebuildWorkBudget(b *testing.B) {
	base := make([][]byte, MaxHistoricalPathQueryRebuildLeaves)
	for i := range base {
		base[i] = fixedFieldBytes(uint64(i + 1))
	}
	b.ReportAllocs()
	b.ReportMetric(float64(MaxHistoricalPathQueryRebuildLeaves-1), "hashes/op")
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		layer := append([][]byte(nil), base...)
		for level := uint32(0); level < MerkleDepth && len(layer) > 1; level++ {
			next := make([][]byte, (len(layer)+1)/2)
			for i := 0; i < len(layer); i += 2 {
				right := emptyNodeBytes(level)
				if i+1 < len(layer) {
					right = layer[i+1]
				}
				next[i/2] = hashNodes(level, layer[i], right)
			}
			layer = next
		}
	}
}

func TestMerkleRootSnapshotLookupRejectsHistoricalHeightCorruption(t *testing.T) {
	k, ctx := setupTreeKeeper()
	ctx = ctx.WithBlockHeight(77)
	require.NoError(t, k.AppendCommitment(ctx, fixedFieldBytes(180)))
	root := append([]byte(nil), k.GetMerkleNode(ctx, uint8(MerkleDepth), 0)...)
	store := k.storeService.OpenKVStore(ctx)

	require.NoError(t, store.Set(privacytypes.GetHistoricalRootKey(root), []byte{1}))
	snapshot, found, err := k.GetMerkleRootSnapshotV1(ctx, root)
	require.ErrorContains(t, err, "height")
	require.False(t, found)
	require.Nil(t, snapshot)

	wrongHeight := make([]byte, 8)
	binary.BigEndian.PutUint64(wrongHeight, 78)
	require.NoError(t, store.Set(privacytypes.GetHistoricalRootKey(root), wrongHeight))
	snapshot, found, err = k.GetMerkleRootSnapshotV1(ctx, root)
	require.ErrorContains(t, err, "inconsistent")
	require.False(t, found)
	require.Nil(t, snapshot)
}

func TestMerkleRootSnapshotGenesisExportCoversEveryPrefix(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()
	for i := uint64(0); i < 4; i++ {
		ctx = ctx.WithBlockHeight(int64(30 + i))
		require.NoError(t, k.AppendCommitment(ctx, fixedFieldBytes(220+i)))
	}

	snapshots, err := k.ExportGenesisMerkleRootSnapshotsV1(ctx)
	require.NoError(t, err)
	require.Len(t, snapshots, 4)
	counts := make(map[uint64]int64)
	for _, snapshot := range snapshots {
		counts[snapshot.LeafCount] = snapshot.Height
	}
	require.Equal(t, map[uint64]int64{1: 30, 2: 31, 3: 32, 4: 33}, counts)
}
