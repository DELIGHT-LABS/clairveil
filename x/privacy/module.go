package privacy

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/DELIGHT-LABS/clairveil/internal/auditinit"

	"github.com/grpc-ecosystem/grpc-gateway/runtime"

	"github.com/spf13/cobra"

	abci "github.com/cometbft/cometbft/abci/types"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	cdctypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/client/cli"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/keeper"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	v2 "github.com/DELIGHT-LABS/clairveil/x/privacy/types/v2"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
)

var (
	_ module.AppModule      = AppModule{}
	_ module.AppModuleBasic = AppModuleBasic{}
)

type AppModuleBasic struct{ AuditRuntime bool }

func (AppModuleBasic) Name() string { return types.ModuleName }

func (AppModuleBasic) RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {}

func (am AppModuleBasic) RegisterInterfaces(registry cdctypes.InterfaceRegistry) {
	if am.AuditRuntime {
		v2.RegisterInterfaces(registry)
		return
	}
	types.RegisterInterfaces(registry)
}

func (am AppModuleBasic) DefaultGenesis(cdc codec.JSONCodec) json.RawMessage {
	if am.AuditRuntime {
		panic("audit V4 privacy genesis requires BuildFreshAuditGenesis with runtime, manifest, and audit-key inputs")
	}
	identity, err := zk.LoadLocalCircuitSetIdentity()
	if err != nil {
		panic(fmt.Errorf("load default privacy circuit identity: %w", err))
	}
	return cdc.MustMarshalJSON(types.DefaultGenesis(identity))
}

func (am AppModuleBasic) ValidateGenesis(cdc codec.JSONCodec, config client.TxEncodingConfig, bz json.RawMessage) error {
	if am.AuditRuntime {
		_, err := types.ParseFreshGenesisV4(bz)
		return err
	}
	var data types.GenesisState
	if err := cdc.UnmarshalJSON(bz, &data); err != nil {
		return fmt.Errorf("failed to unmarshal %s genesis state: %w", types.ModuleName, err)
	}
	return data.Validate()
}

func (am AppModuleBasic) RegisterGRPCGatewayRoutes(clientCtx client.Context, mux *runtime.ServeMux) {
	if am.AuditRuntime {
		if err := v2.RegisterQueryHandlerClient(context.Background(), mux, v2.NewQueryClient(clientCtx)); err != nil {
			panic(err)
		}
	}
	if err := types.RegisterQueryHandlerClient(context.Background(), mux, types.NewQueryClient(clientCtx)); err != nil {
		panic(err)
	}
}

// GetTxCmd returns the root tx command for the privacy module.
func (AppModuleBasic) GetTxCmd() *cobra.Command {
	return cli.GetTxCmd()
}

// GetQueryCmd returns the root query command for the privacy module.
func (AppModuleBasic) GetQueryCmd() *cobra.Command {
	return cli.GetQueryCmd()
}

type AppModule struct {
	AppModuleBasic
	keeper keeper.Keeper
}

func NewAppModule(cdc codec.Codec, k keeper.Keeper) AppModule {
	return AppModule{
		AppModuleBasic: AppModuleBasic{AuditRuntime: k.AuditRuntimeEnabled()},
		keeper:         k,
	}
}

func (AppModule) Name() string { return types.ModuleName }

func (am AppModule) RegisterServices(cfg module.Configurator) {
	if am.AuditRuntime {
		v2.RegisterMsgServer(cfg.MsgServer(), keeper.NewAuditMsgServerImpl(am.keeper))
		v2.RegisterQueryServer(cfg.QueryServer(), keeper.NewAuditQueryServer(am.keeper))
	} else {
		types.RegisterMsgServer(cfg.MsgServer(), keeper.NewMsgServerImpl(am.keeper))
	}
	types.RegisterQueryServer(cfg.QueryServer(), am.keeper)
}

func (am AppModule) IsAppModule() {}

func (am AppModule) IsOnePerModuleType() {}

func (am AppModule) InitGenesis(ctx sdk.Context, cdc codec.JSONCodec, data json.RawMessage) []abci.ValidatorUpdate {
	if am.AuditRuntime {
		if !auditinit.Active(ctx) {
			panic("audit genesis requires trusted application initialization")
		}
		return nil
	}
	var genesisState types.GenesisState
	cdc.MustUnmarshalJSON(data, &genesisState)
	InitGenesis(ctx, am.keeper, genesisState)
	return []abci.ValidatorUpdate{}
}

func (am AppModule) ExportGenesis(ctx sdk.Context, cdc codec.JSONCodec) json.RawMessage {
	gs := ExportGenesis(ctx, am.keeper)
	if am.AuditRuntime {
		metadata, err := am.keeper.ExportAuditGenesisMetadata(ctx)
		if err != nil {
			panic(err)
		}
		metadata.State = gs
		encoded, err := json.Marshal(metadata)
		if err != nil {
			panic(err)
		}
		return encoded
	}
	return cdc.MustMarshalJSON(gs)
}

func (AppModule) ConsensusVersion() uint64 { return 2 }
