package privacy

import (
	"encoding/binary"
	"testing"

	"github.com/cosmos/cosmos-sdk/runtime"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
	"github.com/stretchr/testify/require"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/keeper"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func setupPrivacyGenesisKeeper() (*keeper.Keeper, sdk.Context) {
	storeKey := storetypes.NewKVStoreKey(privacytypes.StoreKey)
	tKey := storetypes.NewTransientStoreKey("transient_test")
	ctx := testutil.DefaultContext(storeKey, tKey)

	k := keeper.NewKeeper(nil, runtime.NewKVStoreService(storeKey), paramtypes.Subspace{}, nil)
	return k, ctx
}

func fixedFieldBytesFromUint64(v uint64) []byte {
	bz := make([]byte, 32)
	binary.BigEndian.PutUint64(bz[24:], v)
	return bz
}

func TestGenesisRoundTrip(t *testing.T) {
	k, ctx := setupPrivacyGenesisKeeper()

	commitments := [][]byte{
		fixedFieldBytesFromUint64(1),
		fixedFieldBytesFromUint64(2),
		fixedFieldBytesFromUint64(3),
	}

	for _, commitment := range commitments {
		require.NoError(t, k.AppendCommitment(ctx, commitment))
	}

	nullifiers := [][]byte{
		fixedFieldBytesFromUint64(11),
		fixedFieldBytesFromUint64(12),
	}
	for _, nullifier := range nullifiers {
		k.SetNullifier(ctx, nullifier)
	}

	extraHistoricalRoot := fixedFieldBytesFromUint64(99)
	k.SetHistoricalRoot(ctx, extraHistoricalRoot)

	exported := ExportGenesis(ctx, *k)
	require.NotNil(t, exported)
	require.NoError(t, exported.Validate())
	require.Equal(t, commitments, exported.Commitments)
	require.ElementsMatch(t, nullifiers, exported.Nullifiers)
	require.NotEmpty(t, exported.HistoricalRoots)

	restoredKeeper, restoredCtx := setupPrivacyGenesisKeeper()
	InitGenesis(restoredCtx, *restoredKeeper, *exported)

	restoredExport := ExportGenesis(restoredCtx, *restoredKeeper)
	require.Equal(t, exported, restoredExport)
	require.Equal(t, uint64(len(exported.Commitments)), restoredKeeper.GetLeafCount(restoredCtx))

	for _, commitment := range exported.Commitments {
		_, found := restoredKeeper.GetCommitmentIndex(restoredCtx, commitment)
		require.True(t, found)
	}

	for _, nullifier := range exported.Nullifiers {
		require.True(t, restoredKeeper.HasNullifier(restoredCtx, nullifier))
	}

	for _, root := range exported.HistoricalRoots {
		require.True(t, restoredKeeper.CheckHistoricalRoot(restoredCtx, root))
	}
}

func TestInitGenesisPanicsWithInvalidState(t *testing.T) {
	k, ctx := setupPrivacyGenesisKeeper()

	state := privacytypes.GenesisState{
		Commitments: [][]byte{{0x01}},
	}

	require.Panics(t, func() {
		InitGenesis(ctx, *k, state)
	})
}
