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

func TestAuditReserveCanonicalCountersAndSurplus(t *testing.T) {
	k, ctx, bank := setupMsgServerKeeper()
	store := k.storeService.OpenKVStore(ctx)
	key := pt.GetReserveDepositKey("uclair")
	max := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	overflow := new(big.Int).Add(max, big.NewInt(1))
	for _, raw := range []string{"", "00", "01", "+1", "-1", " 1", "1 ", "1.0", overflow.String()} {
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
	require.NoError(t, k.addReserveAmount(ctx, key, sdkmath.OneInt()))
	raw, err := store.Get(key)
	require.NoError(t, err)
	require.Equal(t, overflow.String(), string(raw))
	_, err = k.GetReserveSnapshot(ctx, "uclair")
	require.ErrorIs(t, err, ErrReserveStateInvalid)
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

func TestAuditReserveUnboundedCountersAndCurrentLiability(t *testing.T) {
	k, ctx, bank := setupMsgServerKeeper()
	_, err := k.RegisterCanonicalAssetV1(ctx, "uclair")
	require.NoError(t, err)
	huge := new(big.Int).Exp(big.NewInt(10), big.NewInt(78), nil)
	previous := new(big.Int).Sub(huge, big.NewInt(1))
	require.NoError(t, k.InitGenesisReserveBalancesV1(ctx, []*pt.ReserveBalanceV1{{
		CanonicalDenom: "uclair", TotalDeposited: previous.String(), TotalWithdrawn: previous.String(),
	}}))
	require.NoError(t, k.recordReserveDeposit(ctx, sdk.NewInt64Coin("uclair", 1)))
	bank.moduleBalances = sdk.NewCoins(sdk.NewInt64Coin("uclair", 1))
	snapshot, err := k.GetReserveSnapshot(ctx, "uclair")
	require.NoError(t, err)
	require.Equal(t, huge.String(), snapshot.TotalDeposited.String())
	require.Equal(t, previous.String(), snapshot.TotalWithdrawn.String())
	require.Equal(t, "1", snapshot.Liability.String())
	require.True(t, snapshot.InvariantHolds)

	require.NoError(t, k.recordReserveWithdraw(ctx, sdk.NewInt64Coin("uclair", 1)))
	bank.moduleBalances = sdk.NewCoins()
	response, err := k.Reserve(ctx, &pt.QueryReserveRequest{Denom: "uclair"})
	require.NoError(t, err)
	require.Equal(t, huge.String(), response.TotalDeposited)
	require.Equal(t, huge.String(), response.TotalWithdrawn)
	require.Equal(t, "0", response.Liability)
	require.True(t, response.InvariantHolds)
	balances, err := k.ExportGenesisReserveBalancesV1(ctx)
	require.NoError(t, err)
	require.Equal(t, huge.String(), balances[0].TotalDeposited)
	require.Equal(t, huge.String(), balances[0].TotalWithdrawn)
	restored, restoredCtx, _ := setupMsgServerKeeper()
	_, err = restored.RegisterCanonicalAssetV1(restoredCtx, "uclair")
	require.NoError(t, err)
	require.NoError(t, restored.InitGenesisReserveBalancesV1(restoredCtx, balances))
	restoredBalances, err := restored.ExportGenesisReserveBalancesV1(restoredCtx)
	require.NoError(t, err)
	require.Equal(t, balances, restoredBalances)

	maxNote := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 128), big.NewInt(1))
	for i := 0; i < 2; i++ {
		require.NoError(t, k.recordReserveDeposit(ctx, sdk.NewCoin("uclair", sdkmath.NewIntFromBigInt(maxNote))))
	}
	twiceMax := new(big.Int).Mul(maxNote, big.NewInt(2))
	bank.moduleBalances = sdk.NewCoins(sdk.NewCoin("uclair", sdkmath.NewIntFromBigInt(twiceMax)))
	snapshot, err = k.GetReserveSnapshot(ctx, "uclair")
	require.NoError(t, err)
	require.Equal(t, twiceMax.String(), snapshot.Liability.String())
	require.Equal(t, new(big.Int).Add(huge, twiceMax).String(), snapshot.TotalDeposited.String())
	require.Equal(t, huge.String(), snapshot.TotalWithdrawn.String())
	require.True(t, snapshot.InvariantHolds)
}
