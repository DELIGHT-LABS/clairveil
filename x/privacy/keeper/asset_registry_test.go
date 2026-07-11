package keeper

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sdk "github.com/cosmos/cosmos-sdk/types"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestAssetRegistryV1RoundTripAndQueries(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()
	entry, err := k.RegisterCanonicalAssetV1(ctx, "uclair")
	require.NoError(t, err)
	require.Len(t, entry.AssetId, fieldElementByteSize)

	byDenom, found, err := k.GetAssetByDenomV1(ctx, "uclair")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, entry, byDenom)

	byID, found, err := k.GetAssetByIDV1(ctx, entry.AssetId)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, entry, byID)

	denomResponse, err := k.AssetByDenom(sdk.WrapSDKContext(ctx), &privacytypes.QueryAssetByDenomRequest{CanonicalDenom: "uclair"})
	require.NoError(t, err)
	require.Equal(t, privacytypes.AssetRegistryVersionV1, denomResponse.MappingVersion)
	require.Equal(t, entry, denomResponse.Asset)

	idResponse, err := k.AssetByID(sdk.WrapSDKContext(ctx), &privacytypes.QueryAssetByIDRequest{AssetIdHex: hex.EncodeToString(entry.AssetId)})
	require.NoError(t, err)
	require.Equal(t, privacytypes.AssetRegistryVersionV1, idResponse.MappingVersion)
	require.Equal(t, entry, idResponse.Asset)
}

func TestAssetRegistryV1RejectsReregistrationAndMismatchedID(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()
	entry, err := k.RegisterCanonicalAssetV1(ctx, "uclair")
	require.NoError(t, err)

	err = k.RegisterAssetV1(ctx, "uclair", entry.AssetId)
	require.ErrorContains(t, err, "already registered")

	wrongID, err := CanonicalAssetIDV1("uatom")
	require.NoError(t, err)
	err = k.RegisterAssetV1(ctx, "uclair", wrongID)
	require.ErrorContains(t, err, "does not match canonical denom")

	stored, found, err := k.GetAssetByDenomV1(ctx, "uclair")
	require.NoError(t, err)
	require.True(t, found)
	require.True(t, bytes.Equal(entry.AssetId, stored.AssetId))
}

func TestAssetRegistryV1CollisionAndCorruptionFailClosed(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()
	entry, err := k.RegisterCanonicalAssetV1(ctx, "uclair")
	require.NoError(t, err)

	atomID, err := CanonicalAssetIDV1("uatom")
	require.NoError(t, err)
	store := k.storeService.OpenKVStore(ctx)
	require.NoError(t, store.Set(privacytypes.GetAssetByIDKey(atomID), []byte("uclair")))
	err = k.RegisterAssetV1(ctx, "uatom", atomID)
	require.ErrorContains(t, err, "already registered")
	registered, err := store.Has(privacytypes.GetAssetByDenomKey("uatom"))
	require.NoError(t, err)
	require.False(t, registered)

	require.NoError(t, store.Set(privacytypes.GetAssetByIDKey(entry.AssetId), []byte("uatom")))
	_, found, err := k.GetAssetByDenomV1(ctx, "uclair")
	require.False(t, found)
	require.ErrorContains(t, err, "inconsistent")
}

func TestAssetRegistryV1RequireRegisteredAndQueryBounds(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()
	_, err := k.RequireRegisteredAssetV1(ctx, "uatom")
	require.ErrorContains(t, err, "not registered")

	_, err = k.AssetByDenom(sdk.WrapSDKContext(ctx), &privacytypes.QueryAssetByDenomRequest{CanonicalDenom: " uclair"})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	_, err = k.AssetByDenom(sdk.WrapSDKContext(ctx), &privacytypes.QueryAssetByDenomRequest{CanonicalDenom: "uatom"})
	require.Equal(t, codes.NotFound, status.Code(err))
	_, err = k.AssetByID(sdk.WrapSDKContext(ctx), &privacytypes.QueryAssetByIDRequest{AssetIdHex: "01"})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestAssetRegistryV1GenesisExportIsCanonicalOrder(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()
	_, err := k.RegisterCanonicalAssetV1(ctx, "uclair")
	require.NoError(t, err)
	_, err = k.RegisterCanonicalAssetV1(ctx, "uatom")
	require.NoError(t, err)

	exported, err := k.ExportGenesisAssetRegistryV1(ctx)
	require.NoError(t, err)
	require.Len(t, exported, 2)
	require.Equal(t, "uatom", exported[0].CanonicalDenom)
	require.Equal(t, "uclair", exported[1].CanonicalDenom)
}
