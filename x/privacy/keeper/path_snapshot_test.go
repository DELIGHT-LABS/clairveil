package keeper

import (
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
