package auditbank

import (
	"context"
	"cosmossdk.io/log/v2"
	"github.com/DELIGHT-LABS/clairveil/internal/auditinit"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/address"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	ak "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	at "github.com/cosmos/cosmos-sdk/x/auth/types"
	bk "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	bt "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
	"reflect"
	"testing"
)

func bankFixture(t *testing.T) (sdk.Context, bk.BaseKeeper, Facade, sdk.AccAddress) {
	keys := storetypes.NewKVStoreKeys(at.StoreKey, bt.StoreKey, "privacy")
	ctx := testutil.DefaultContextWithKeys(keys, nil, nil)
	reg := codectypes.NewInterfaceRegistry()
	at.RegisterInterfaces(reg)
	cdc := codec.NewProtoCodec(reg)
	authority := at.NewModuleAddress("gov").String()
	accounts := ak.NewAccountKeeper(cdc, runtime.NewKVStoreService(keys[at.StoreKey]), at.ProtoBaseAccount, map[string][]string{"privacy": nil, "mint": {at.Minter}, "bonded_tokens_pool": {at.Staking}}, address.Bech32Codec{Bech32Prefix: sdk.GetConfig().GetBech32AccountAddrPrefix()}, sdk.GetConfig().GetBech32AccountAddrPrefix(), authority)
	bank := bk.NewBaseKeeper(cdc, runtime.NewKVStoreService(keys[bt.StoreKey]), accounts, map[string]bool{at.NewModuleAddress("privacy").String(): true}, authority, log.NewNopLogger())
	user := sdk.AccAddress(make([]byte, 20))
	user[0] = 7
	accounts.SetAccount(ctx, accounts.NewAccountWithAddress(ctx, user))
	for name, perms := range map[string][]string{"privacy": nil, "mint": {at.Minter}, "bonded_tokens_pool": {at.Staking}} {
		accounts.SetModuleAccount(ctx, accounts.NewAccount(ctx, at.NewEmptyModuleAccount(name, perms...)).(sdk.ModuleAccountI))
	}
	genesis := bt.DefaultGenesisState()
	genesis.Balances = []bt.Balance{{Address: user.String(), Coins: sdk.NewCoins(sdk.NewInt64Coin("utest", 100))}, {Address: at.NewModuleAddress("privacy").String(), Coins: sdk.NewCoins(sdk.NewInt64Coin("utest", 20))}, {Address: at.NewModuleAddress("bonded_tokens_pool").String(), Coins: sdk.NewCoins(sdk.NewInt64Coin("utest", 10))}}
	bank.InitGenesis(ctx, genesis)
	// An incoming credit must not inspect or repair malformed privacy counters.
	ctx.KVStore(keys["privacy"]).Set([]byte{8, 'u', 't', 'e', 's', 't'}, []byte("invalid"))
	return ctx, bank, NewFacade(bank, true), user
}
func TestFacadePrivacyDebitsAndAPI(t *testing.T) {
	ctx, bank, f, user := bankFixture(t)
	privacy := at.NewModuleAddress("privacy")
	coins := sdk.NewCoins(sdk.NewInt64Coin("utest", 1))
	for name, call := range map[string]func() error{
		"send":           func() error { return f.SendCoins(ctx, privacy, user, coins) },
		"account-module": func() error { return f.SendCoinsFromAccountToModule(ctx, privacy, "mint", coins) },
		"module-account": func() error { return f.SendCoinsFromModuleToAccount(ctx, "privacy", user, coins) },
		"module-module":  func() error { return f.SendCoinsFromModuleToModule(ctx, "privacy", "mint", coins) },
		"self":           func() error { return f.SendCoinsFromModuleToModule(ctx, "privacy", "privacy", coins) },
		"delegate":       func() error { return f.DelegateCoinsFromAccountToModule(ctx, privacy, "bonded_tokens_pool", coins) },
		"undelegate":     func() error { return f.UndelegateCoinsFromModuleToAccount(ctx, "privacy", user, coins) },
		"mint":           func() error { return f.MintCoins(ctx, "privacy", coins) }, "burn": func() error { return f.BurnCoins(ctx, "privacy", coins) },
	} {
		t.Run(name, func(t *testing.T) {
			require.Error(t, call())
			require.Equal(t, int64(20), bank.GetBalance(ctx, privacy, "utest").Amount.Int64())
			require.Equal(t, int64(100), bank.GetBalance(ctx, user, "utest").Amount.Int64())
		})
	}
	typ := reflect.TypeOf(f)
	for _, name := range []string{"InputOutputCoins", "SendCoinsToVirtual", "UncheckedSetBalance", "AppendSendRestriction", "ClearSendRestriction", "DelegateCoins", "UndelegateCoins"} {
		_, ok := typ.MethodByName(name)
		require.False(t, ok, name)
	}
}
func TestFacadeIncomingAndNestedRollback(t *testing.T) {
	ctx, bank, f, user := bankFixture(t)
	p := NewPrincipal(bank)
	privacy := at.NewModuleAddress("privacy")
	coins := sdk.NewCoins(sdk.NewInt64Coin("utest", 1))
	require.NoError(t, f.SendCoinsFromAccountToModule(ctx, user, "privacy", coins))
	require.NoError(t, f.MintCoins(ctx, "mint", coins))
	require.NoError(t, f.SendCoinsFromModuleToModule(ctx, "mint", "privacy", coins))
	require.NoError(t, f.UndelegateCoinsFromModuleToAccount(ctx, "bonded_tokens_pool", privacy, coins))
	require.Equal(t, int64(23), bank.GetBalance(ctx, privacy, "utest").Amount.Int64())
	_, err := bk.NewMsgServerImpl(bank).Send(ctx, &bt.MsgSend{FromAddress: user.String(), ToAddress: privacy.String(), Amount: coins})
	require.Error(t, err)
	beforeUser := bank.GetBalance(ctx, user, "utest")
	beforePrivacy := bank.GetBalance(ctx, privacy, "utest")
	bank.PrependSendRestriction(func(c context.Context, from, to sdk.AccAddress, amt sdk.Coins) (sdk.AccAddress, error) {
		_ = f.MintCoins(c, "mint", coins)
		return to, nil
	})
	cache, publish := ctx.CacheContext()
	err = p.Lock(cache, user, coins[0])
	require.ErrorContains(t, err, "scope")
	if err == nil {
		publish()
	}
	require.Equal(t, beforeUser, bank.GetBalance(ctx, user, "utest"))
	require.Equal(t, beforePrivacy, bank.GetBalance(ctx, privacy, "utest"))
	require.NoError(t, auditinit.Run(ctx, func(c sdk.Context) error {
		require.ErrorContains(t, p.Lock(c, user, coins[0]), "initialization")
		return nil
	}))
}
