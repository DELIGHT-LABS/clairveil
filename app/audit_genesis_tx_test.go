package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"
	"github.com/DELIGHT-LABS/clairveil/types"
	privacymodule "github.com/DELIGHT-LABS/clairveil/x/privacy"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	abci "github.com/cometbft/cometbft/abci/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/address"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptocodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/runtime"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/cosmos/cosmos-sdk/x/auth"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/cosmos/cosmos-sdk/x/tx/signing"
	"github.com/cosmos/gogoproto/proto"
	"github.com/stretchr/testify/require"
)

type genesisTxStakingServer struct {
	stakingtypes.UnimplementedMsgServer
	storeKey *storetypes.KVStoreKey
	created  bool
}

func (s *genesisTxStakingServer) CreateValidator(ctx context.Context, msg *stakingtypes.MsgCreateValidator) (*stakingtypes.MsgCreateValidatorResponse, error) {
	sdk.UnwrapSDKContext(ctx).KVStore(s.storeKey).Set([]byte(msg.ValidatorAddress), []byte{1})
	s.created = true
	return &stakingtypes.MsgCreateValidatorResponse{}, nil
}

func TestScopedGenesisTxHandlerSharesAuthGenesisCacheWithBaseApp(t *testing.T) {
	types.SetConfig()
	registry, err := codectypes.NewInterfaceRegistryWithOptions(codectypes.InterfaceRegistryOptions{
		ProtoFiles: proto.HybridResolver,
		SigningOptions: signing.Options{
			AddressCodec:          address.Bech32Codec{Bech32Prefix: sdk.GetConfig().GetBech32AccountAddrPrefix()},
			ValidatorAddressCodec: address.Bech32Codec{Bech32Prefix: sdk.GetConfig().GetBech32ValidatorAddrPrefix()},
		},
	})
	require.NoError(t, err)
	cryptocodec.RegisterInterfaces(registry)
	authtypes.RegisterInterfaces(registry)
	stakingtypes.RegisterInterfaces(registry)
	cdc := codec.NewProtoCodec(registry)
	txConfig := authtx.NewTxConfig(cdc, authtx.DefaultSignModes)
	base := baseapp.NewBaseApp(t.Name(), log.NewNopLogger(), dbm.NewMemDB(), txConfig.TxDecoder(), baseapp.SetChainID("clairveil-genesis-tx-1"))
	base.SetInterfaceRegistry(registry)
	base.MsgServiceRouter().SetInterfaceRegistry(registry)
	base.SetTxEncoder(txConfig.TxEncoder())

	keys := storetypes.NewKVStoreKeys(authtypes.StoreKey, stakingtypes.StoreKey)
	base.MountKVStores(keys)

	addressCodec := address.Bech32Codec{Bech32Prefix: sdk.GetConfig().GetBech32AccountAddrPrefix()}
	accountKeeper := authkeeper.NewAccountKeeper(cdc, runtime.NewKVStoreService(keys[authtypes.StoreKey]), authtypes.ProtoBaseAccount, nil, addressCodec, sdk.GetConfig().GetBech32AccountAddrPrefix(), authtypes.NewModuleAddress("gov").String())
	privateKey := secp256k1.GenPrivKey()
	delegator := sdk.AccAddress(privateKey.PubKey().Address())
	manager := module.NewManager(auth.NewAppModule(cdc, accountKeeper, nil, nil))
	manager.SetOrderInitGenesis(authtypes.ModuleName)
	authState, err := cdc.MarshalJSON(authtypes.NewGenesisState(authtypes.DefaultParams(), authtypes.GenesisAccounts{authtypes.NewBaseAccountWithAddress(delegator)}))
	require.NoError(t, err)

	server := &genesisTxStakingServer{storeKey: keys[stakingtypes.StoreKey]}
	stakingtypes.RegisterMsgServer(base.MsgServiceRouter(), server)
	base.SetAnteHandler(func(ctx sdk.Context, tx sdk.Tx, _ bool) (sdk.Context, error) {
		memoTx, ok := tx.(sdk.TxWithMemo)
		require.True(t, ok)
		require.Len(t, memoTx.GetMemo(), 256)
		require.NotNil(t, accountKeeper.GetAccount(ctx, delegator), "auth genesis state must be visible to genesis ante")
		return ctx.WithGasMeter(storetypes.NewInfiniteGasMeter()), nil
	})

	gentx, err := stakingtypes.NewMsgCreateValidator(
		sdk.ValAddress(privateKey.PubKey().Address()).String(),
		privateKey.PubKey(),
		sdk.NewInt64Coin("uclair", 1),
		stakingtypes.NewDescription("genesis-validator", "", "", "", ""),
		stakingtypes.CommissionRates{},
		math.OneInt(),
	)
	require.NoError(t, err)
	builder := txConfig.NewTxBuilder()
	require.NoError(t, builder.SetMsgs(gentx))
	builder.SetMemo(strings.Repeat("m", 256))
	txBytes, err := txConfig.TxEncoder()(builder.GetTx())
	require.NoError(t, err)

	handler := newScopedGenesisTxHandler(base)
	require.ErrorContains(t, handler.ExecuteGenesisTx(txBytes), "not bound")
	base.SetInitChainer(func(ctx sdk.Context, _ *abci.RequestInitChain) (*abci.ResponseInitChain, error) {
		cache, publish := ctx.CacheContext()
		require.NoError(t, handler.bind(cache.MultiStore()))
		defer handler.unbind()
		require.ErrorContains(t, handler.bind(cache.MultiStore()), "already bound")
		manager.InitGenesis(cache, cdc, map[string]json.RawMessage{authtypes.ModuleName: authState})
		require.NoError(t, handler.ExecuteGenesisTx(txBytes))
		publish()
		return &abci.ResponseInitChain{}, nil
	})
	require.NoError(t, base.LoadLatestVersion())

	_, err = base.InitChain(&abci.RequestInitChain{ChainId: "clairveil-genesis-tx-1", InitialHeight: 1})
	require.NoError(t, err)
	require.True(t, server.created)
	require.Equal(t, []byte{1}, base.GetContextForFinalizeBlock(nil).KVStore(keys[stakingtypes.StoreKey]).Get([]byte(gentx.ValidatorAddress)))
	require.ErrorContains(t, handler.ExecuteGenesisTx(txBytes), "not bound")
}

func TestExportedPrivacyGenesisAnchorUsesOriginalInitialHeight(t *testing.T) {
	nonce := make([]byte, 32)
	nonce[0] = 7
	original, err := privacymodule.ComputeAuditGenesisAnchor("clairveil-export-1", nonce, 1)
	require.NoError(t, err)
	restarted, err := privacymodule.ComputeAuditGenesisAnchor("clairveil-export-1", nonce, 42)
	require.NoError(t, err)
	require.NotEqual(t, original, restarted)

	// An exported V4 genesis retains the first height for key provenance while
	// the CometBFT request uses its separate restart height.
	privacyGenesis := privacytypes.GenesisStateV4{InitialHeight: 1, RestartHeight: 42, NetworkNonce: nonce}
	computed, err := privacymodule.ComputeAuditGenesisAnchor("clairveil-export-1", privacyGenesis.NetworkNonce, privacyGenesis.InitialHeight)
	require.NoError(t, err)
	require.Equal(t, original, computed)
}
