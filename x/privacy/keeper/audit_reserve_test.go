package keeper

import (
	"math/big"
	"testing"

	sdkmath "cosmossdk.io/math"
	pt "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestAuditReserveCanonicalUint256AndSurplus(t *testing.T) {
	k, ctx, bank := setupMsgServerKeeper()
	store := k.storeService.OpenKVStore(ctx)
	key := pt.GetReserveDepositKey("uclair")
	max := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	overflow := new(big.Int).Add(max, big.NewInt(1))
	for _, raw := range []string{"00", "01", "+1", "-1", " 1", "1 ", "1.0", overflow.String()} {
		require.NoError(t, store.Set(key, []byte(raw)))
		_, err := k.GetReserveSnapshot(ctx, "uclair")
		require.ErrorIs(t, err, ErrReserveStateInvalid)
		_, err = k.Reserve(ctx, &pt.QueryReserveRequest{Denom: "uclair"})
		require.Equal(t, codes.FailedPrecondition, status.Code(err))
	}
	require.NoError(t, store.Set(key, []byte(max.String())))
	value, err := k.getReserveAmount(ctx, key)
	require.NoError(t, err)
	require.Equal(t, max.String(), value.String())
	require.ErrorIs(t, k.addReserveAmount(ctx, key, sdkmath.OneInt()), ErrReserveStateInvalid)
	raw, err := store.Get(key)
	require.NoError(t, err)
	require.Equal(t, max.String(), string(raw))
	require.NoError(t, store.Set(key, []byte("10")))
	require.NoError(t, store.Set(pt.GetReserveWithdrawKey("uclair"), []byte("3")))
	for _, tc := range []struct {
		balance               int64
		collateral, invariant bool
		surplus, short        string
	}{{9, true, false, "2", "0"}, {7, true, true, "0", "0"}, {6, false, false, "0", "1"}} {
		bank.moduleBalances = sdk.NewCoins(sdk.NewInt64Coin("uclair", tc.balance))
		r, err := k.Reserve(ctx, &pt.QueryReserveRequest{Denom: "uclair"})
		require.NoError(t, err)
		require.Equal(t, "7", r.Liability)
		require.Equal(t, "7", r.ExpectedModuleBalance)
		require.Equal(t, tc.collateral, r.Collateralized)
		require.Equal(t, tc.invariant, r.InvariantHolds)
		require.Equal(t, tc.surplus, r.Surplus)
		require.Equal(t, tc.short, r.Shortfall)
	}
	require.NoError(t, store.Set(pt.GetReserveDepositKey("uatom"), []byte("malformed")))
	_, err = k.GetReserveSnapshot(ctx, "uclair")
	require.NoError(t, err)
	require.NoError(t, store.Set(pt.GetReserveWithdrawKey("uclair"), []byte("11")))
	_, err = k.GetReserveSnapshot(ctx, "uclair")
	require.ErrorIs(t, err, ErrReserveStateInvalid)
}
