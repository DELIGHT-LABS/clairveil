package app

import (
	"encoding/json"
	"fmt"
	upgradekeeper "github.com/cosmos/cosmos-sdk/x/upgrade/keeper"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"
	"io"
	"maps"
	"os"
	"path/filepath"

	abci "github.com/cometbft/cometbft/abci/types"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/gogoproto/proto"

	"cosmossdk.io/log/v2"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/grpc/cmtservice"
	nodeservice "github.com/cosmos/cosmos-sdk/client/grpc/node"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/address"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/cosmos/cosmos-sdk/server/api"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/cosmos/cosmos-sdk/std"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
	"github.com/cosmos/cosmos-sdk/version"
	"github.com/cosmos/cosmos-sdk/x/auth"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	authcodec "github.com/cosmos/cosmos-sdk/x/auth/codec"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	"github.com/cosmos/cosmos-sdk/x/auth/posthandler"
	authsims "github.com/cosmos/cosmos-sdk/x/auth/simulation"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/auth/vesting"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	"github.com/cosmos/cosmos-sdk/x/bank"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/consensus"
	consensusparamkeeper "github.com/cosmos/cosmos-sdk/x/consensus/keeper"
	consensusparamtypes "github.com/cosmos/cosmos-sdk/x/consensus/types"
	distr "github.com/cosmos/cosmos-sdk/x/distribution"
	distrkeeper "github.com/cosmos/cosmos-sdk/x/distribution/keeper"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	"github.com/cosmos/cosmos-sdk/x/genutil"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	"github.com/cosmos/cosmos-sdk/x/gov"
	govclient "github.com/cosmos/cosmos-sdk/x/gov/client"
	govkeeper "github.com/cosmos/cosmos-sdk/x/gov/keeper"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1types "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	"github.com/cosmos/cosmos-sdk/x/mint"
	mintkeeper "github.com/cosmos/cosmos-sdk/x/mint/keeper"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	"github.com/cosmos/cosmos-sdk/x/params"
	paramsclient "github.com/cosmos/cosmos-sdk/x/params/client"
	paramskeeper "github.com/cosmos/cosmos-sdk/x/params/keeper"
	paramstypes "github.com/cosmos/cosmos-sdk/x/params/types"
	"github.com/cosmos/cosmos-sdk/x/slashing"
	slashingkeeper "github.com/cosmos/cosmos-sdk/x/slashing/keeper"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	"github.com/cosmos/cosmos-sdk/x/staking"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/cosmos/cosmos-sdk/x/tx/signing"
	upgrademodule "github.com/cosmos/cosmos-sdk/x/upgrade"

	"github.com/DELIGHT-LABS/clairveil/internal/auditbank"
	clairveiltypes "github.com/DELIGHT-LABS/clairveil/types"
	"github.com/DELIGHT-LABS/clairveil/x/privacy"
	privacykeeper "github.com/DELIGHT-LABS/clairveil/x/privacy/keeper"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	privacyzk "github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
)

const appName = "Clairveil"

var DefaultNodeHome string

var maccPerms = map[string][]string{
	authtypes.FeeCollectorName:     nil,
	distrtypes.ModuleName:          nil,
	minttypes.ModuleName:           {authtypes.Minter},
	stakingtypes.BondedPoolName:    {authtypes.Burner, authtypes.Staking},
	stakingtypes.NotBondedPoolName: {authtypes.Burner, authtypes.Staking},
	govtypes.ModuleName:            {authtypes.Burner},
	privacytypes.ModuleName:        nil,
}

var _ servertypes.Application = (*ClairveilApp)(nil)

func init() {
	userHomeDir, err := os.UserHomeDir()
	if err != nil {
		panic(err)
	}
	DefaultNodeHome = filepath.Join(userHomeDir, ".clairveil")
}

type ClairveilApp struct {
	*baseapp.BaseApp

	legacyAmino       *codec.LegacyAmino
	appCodec          codec.Codec
	txConfig          client.TxConfig
	interfaceRegistry codectypes.InterfaceRegistry

	keys  map[string]*storetypes.KVStoreKey
	tkeys map[string]*storetypes.TransientStoreKey

	AccountKeeper         authkeeper.AccountKeeper
	BankKeeper            auditbank.Facade
	StakingKeeper         *stakingkeeper.Keeper
	SlashingKeeper        slashingkeeper.Keeper
	MintKeeper            mintkeeper.Keeper
	DistrKeeper           distrkeeper.Keeper
	GovKeeper             govkeeper.Keeper
	ParamsKeeper          paramskeeper.Keeper
	ConsensusParamsKeeper consensusparamkeeper.Keeper
	PrivacyKeeper         privacykeeper.Keeper
	auditUpgradeKeeper    *upgradekeeper.Keeper
	auditValidateBank     func(sdk.Context) error
	genesisTxHandler      *scopedGenesisTxHandler

	ModuleManager      *module.Manager
	BasicModuleManager module.BasicManager
	configurator       module.Configurator
}

// NewClientCodec is an offline command-codec bootstrap only.
type ClientCodec struct {
	BasicModuleManager module.BasicManager
	AppCodec           codec.Codec
	InterfaceRegistry  codectypes.InterfaceRegistry
	LegacyAmino        *codec.LegacyAmino
	TxConfig           client.TxConfig
}

// NewClientCodec constructs only offline CLI encoding and basic-module
// metadata. It owns no database, keeper, bank facade, or server handler.
func NewClientCodec() *ClientCodec {
	interfaceRegistry, err := codectypes.NewInterfaceRegistryWithOptions(codectypes.InterfaceRegistryOptions{ProtoFiles: proto.HybridResolver, SigningOptions: signing.Options{AddressCodec: address.Bech32Codec{Bech32Prefix: sdk.GetConfig().GetBech32AccountAddrPrefix()}, ValidatorAddressCodec: address.Bech32Codec{Bech32Prefix: sdk.GetConfig().GetBech32ValidatorAddrPrefix()}}})
	if err != nil {
		panic(err)
	}
	appCodec := codec.NewProtoCodec(interfaceRegistry)
	legacyAmino := codec.NewLegacyAmino()
	txConfig := authtx.NewTxConfig(appCodec, authtx.DefaultSignModes)
	txConfig, err = authtx.NewTxConfigWithOptions(appCodec, authtx.ConfigOptions{EnabledSignModes: authtx.DefaultSignModes, ProtoDecoder: auditTxDecoder(txConfig.TxDecoder(), interfaceRegistry)})
	if err != nil {
		panic(err)
	}
	std.RegisterLegacyAminoCodec(legacyAmino)
	std.RegisterInterfaces(interfaceRegistry)
	// Command wiring keeps public Cosmos module metadata and V2 privacy types,
	// but deliberately instantiates no keeper or server handler.
	basic := module.NewBasicManager(genutil.NewAppModuleBasic(genutiltypes.DefaultMessageValidator), auth.AppModuleBasic{}, vesting.AppModuleBasic{}, bank.AppModuleBasic{}, distr.AppModuleBasic{}, gov.NewAppModuleBasic([]govclient.ProposalHandler{paramsclient.ProposalHandler}), mint.AppModuleBasic{}, slashing.AppModuleBasic{}, staking.AppModuleBasic{}, params.AppModuleBasic{}, consensus.AppModuleBasic{}, upgrademodule.NewAppModule(nil, authcodec.NewBech32Codec(clairveiltypes.Bech32PrefixAccAddr)), privacy.AppModuleBasic{AuditRuntime: true})
	basic.RegisterLegacyAminoCodec(legacyAmino)
	basic.RegisterInterfaces(interfaceRegistry)
	return &ClientCodec{BasicModuleManager: basic, AppCodec: appCodec, InterfaceRegistry: interfaceRegistry, LegacyAmino: legacyAmino, TxConfig: txConfig}
}

// NewAuditFieldApp composes the audit-field v2 runtime. The artifact manifest
// remains development-grade until an authenticated production setup exists.
func NewAuditFieldApp(logger log.Logger, db dbm.DB, traceStore io.Writer, appOpts servertypes.AppOptions, config privacykeeper.AuditRuntimeConfig, baseAppOptions ...func(*baseapp.BaseApp)) *ClairveilApp {
	return newClairveilApp(logger, db, traceStore, false, appOpts, config, baseAppOptions...)
}
func newClairveilApp(logger log.Logger, db dbm.DB, traceStore io.Writer, loadLatest bool, appOpts servertypes.AppOptions, auditConfig privacykeeper.AuditRuntimeConfig, baseAppOptions ...func(*baseapp.BaseApp)) *ClairveilApp {
	interfaceRegistry, err := codectypes.NewInterfaceRegistryWithOptions(codectypes.InterfaceRegistryOptions{
		ProtoFiles: proto.HybridResolver,
		SigningOptions: signing.Options{
			AddressCodec: address.Bech32Codec{
				Bech32Prefix: sdk.GetConfig().GetBech32AccountAddrPrefix(),
			},
			ValidatorAddressCodec: address.Bech32Codec{
				Bech32Prefix: sdk.GetConfig().GetBech32ValidatorAddrPrefix(),
			},
		},
	})
	if err != nil {
		panic(err)
	}

	appCodec := codec.NewProtoCodec(interfaceRegistry)
	legacyAmino := codec.NewLegacyAmino()
	txConfig := authtx.NewTxConfig(appCodec, authtx.DefaultSignModes)
	txConfig, err = authtx.NewTxConfigWithOptions(appCodec, authtx.ConfigOptions{EnabledSignModes: authtx.DefaultSignModes, ProtoDecoder: auditTxDecoder(txConfig.TxDecoder(), interfaceRegistry)})
	if err != nil {
		panic(err)
	}

	std.RegisterLegacyAminoCodec(legacyAmino)
	std.RegisterInterfaces(interfaceRegistry)

	if traceStore != nil {
		baseAppOptions = append(baseAppOptions, baseapp.SetTrace(true))
	}

	bApp := baseapp.NewBaseApp(appName, logger, db, txConfig.TxDecoder(), baseAppOptions...)
	bApp.SetVersion(version.Version)
	bApp.SetInterfaceRegistry(interfaceRegistry)
	bApp.SetTxEncoder(txConfig.TxEncoder())

	keys := storetypes.NewKVStoreKeys(
		authtypes.StoreKey,
		banktypes.StoreKey,
		stakingtypes.StoreKey,
		minttypes.StoreKey,
		distrtypes.StoreKey,
		slashingtypes.StoreKey,
		govtypes.StoreKey,
		paramstypes.StoreKey,
		consensusparamtypes.StoreKey,
		privacytypes.StoreKey,
	)
	keys[upgradetypes.StoreKey] = storetypes.NewKVStoreKey(upgradetypes.StoreKey)
	if err := bApp.RegisterStreamingServices(appOpts, keys); err != nil {
		panic(err)
	}

	tkeys := storetypes.NewTransientStoreKeys(paramstypes.TStoreKey)

	app := &ClairveilApp{
		BaseApp:           bApp,
		legacyAmino:       legacyAmino,
		appCodec:          appCodec,
		txConfig:          txConfig,
		interfaceRegistry: interfaceRegistry,
		keys:              keys,
		tkeys:             tkeys,
		genesisTxHandler:  newScopedGenesisTxHandler(bApp),
	}

	app.ParamsKeeper = initParamsKeeper(appCodec, legacyAmino, keys[paramstypes.StoreKey], tkeys[paramstypes.TStoreKey])
	govModuleAddress := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	app.ConsensusParamsKeeper = consensusparamkeeper.NewKeeper(
		appCodec,
		runtime.NewKVStoreService(keys[consensusparamtypes.StoreKey]),
		govModuleAddress,
		runtime.EventService{},
	)
	bApp.SetParamStore(app.ConsensusParamsKeeper.ParamsStore)

	app.AccountKeeper = authkeeper.NewAccountKeeper(
		appCodec,
		runtime.NewKVStoreService(keys[authtypes.StoreKey]),
		authtypes.ProtoBaseAccount,
		maccPerms,
		authcodec.NewBech32Codec(clairveiltypes.Bech32PrefixAccAddr),
		clairveiltypes.Bech32PrefixAccAddr,
		govModuleAddress,
	)
	rawBank := bankkeeper.NewBaseKeeper(
		appCodec,
		runtime.NewKVStoreService(keys[banktypes.StoreKey]),
		app.AccountKeeper,
		BlockedAddresses(),
		govModuleAddress,
		logger,
	)
	app.BankKeeper = auditbank.NewFacade(rawBank, true)
	app.auditUpgradeKeeper = upgradekeeper.NewKeeper(nil, runtime.NewKVStoreService(keys[upgradetypes.StoreKey]), appCodec, DefaultNodeHome, nil, govModuleAddress)
	app.auditValidateBank = func(ctx sdk.Context) error {
		var invalid error
		rawBank.IterateAllBalances(ctx, func(addr sdk.AccAddress, coin sdk.Coin) bool {
			if len(addr) == 0 || !coin.IsValid() || coin.Amount.IsNegative() || coin.Amount.BigInt().BitLen() > 256 {
				invalid = fmt.Errorf("invalid initialized bank balance")
				return true
			}
			return false
		})
		return invalid
	}
	app.StakingKeeper = stakingkeeper.NewKeeper(
		appCodec,
		runtime.NewKVStoreService(keys[stakingtypes.StoreKey]),
		app.AccountKeeper,
		app.BankKeeper,
		govModuleAddress,
		authcodec.NewBech32Codec(clairveiltypes.Bech32PrefixValAddr),
		authcodec.NewBech32Codec(clairveiltypes.Bech32PrefixConsAddr),
	)
	app.MintKeeper = mintkeeper.NewKeeper(
		appCodec,
		runtime.NewKVStoreService(keys[minttypes.StoreKey]),
		app.StakingKeeper,
		app.AccountKeeper,
		app.BankKeeper,
		authtypes.FeeCollectorName,
		govModuleAddress,
	)
	app.DistrKeeper = distrkeeper.NewKeeper(
		appCodec,
		runtime.NewKVStoreService(keys[distrtypes.StoreKey]),
		app.AccountKeeper,
		app.BankKeeper,
		app.StakingKeeper,
		authtypes.FeeCollectorName,
		govModuleAddress,
	)
	app.SlashingKeeper = slashingkeeper.NewKeeper(
		appCodec,
		legacyAmino,
		runtime.NewKVStoreService(keys[slashingtypes.StoreKey]),
		app.StakingKeeper,
		govModuleAddress,
	)
	app.StakingKeeper.SetHooks(stakingtypes.NewMultiStakingHooks(app.DistrKeeper.Hooks(), app.SlashingKeeper.Hooks()))

	govConfig := govtypes.DefaultConfig()
	govKeeper := govkeeper.NewKeeper(
		appCodec,
		runtime.NewKVStoreService(keys[govtypes.StoreKey]),
		app.AccountKeeper,
		app.BankKeeper,
		app.DistrKeeper,
		app.MsgServiceRouter(),
		govConfig,
		govModuleAddress,
		govkeeper.NewDefaultCalculateVoteResultsAndVotingPower(app.StakingKeeper),
	)
	app.GovKeeper = *govKeeper.SetHooks(govtypes.NewMultiGovHooks())

	app.PrivacyKeeper = *privacykeeper.NewKeeper(
		appCodec,
		runtime.NewKVStoreService(keys[privacytypes.StoreKey]),
		app.GetSubspace(privacytypes.ModuleName),
		app.BankKeeper,
	)

	config := auditConfig
	config.PrincipalAdapter = auditbank.NewPrincipal(rawBank)
	if err := app.PrivacyKeeper.ConfigureAuditRuntime(config); err != nil {
		panic(err)
	}

	app.ModuleManager = module.NewManager(
		genutil.NewAppModule(app.AccountKeeper, app.StakingKeeper, app.genesisTxHandler, txConfig),
		auth.NewAppModule(appCodec, app.AccountKeeper, authsims.RandomGenesisAccounts, app.GetSubspace(authtypes.ModuleName)),
		vesting.NewAppModule(app.AccountKeeper, app.BankKeeper),
		bank.NewAppModule(appCodec, rawBank, app.AccountKeeper, app.GetSubspace(banktypes.ModuleName)),
		gov.NewAppModule(appCodec, &app.GovKeeper, app.AccountKeeper, app.BankKeeper, app.GetSubspace(govtypes.ModuleName)),
		mint.NewAppModule(appCodec, app.MintKeeper, app.AccountKeeper, nil, app.GetSubspace(minttypes.ModuleName)),
		slashing.NewAppModule(appCodec, app.SlashingKeeper, app.AccountKeeper, app.BankKeeper, app.StakingKeeper, app.GetSubspace(slashingtypes.ModuleName), app.interfaceRegistry),
		distr.NewAppModule(appCodec, app.DistrKeeper, app.AccountKeeper, app.BankKeeper, app.StakingKeeper, app.GetSubspace(distrtypes.ModuleName)),
		staking.NewAppModule(appCodec, app.StakingKeeper, app.AccountKeeper, app.BankKeeper, app.GetSubspace(stakingtypes.ModuleName)),
		params.NewAppModule(app.ParamsKeeper),
		consensus.NewAppModule(appCodec, app.ConsensusParamsKeeper),
		privacy.NewAppModule(appCodec, app.PrivacyKeeper),
	)
	app.ModuleManager.Modules[upgradetypes.ModuleName] = upgrademodule.NewAppModule(app.auditUpgradeKeeper, authcodec.NewBech32Codec(clairveiltypes.Bech32PrefixAccAddr))
	app.BasicModuleManager = module.NewBasicManagerFromManager(
		app.ModuleManager,
		map[string]module.AppModuleBasic{
			genutiltypes.ModuleName: genutil.NewAppModuleBasic(genutiltypes.DefaultMessageValidator),
			govtypes.ModuleName: gov.NewAppModuleBasic([]govclient.ProposalHandler{
				paramsclient.ProposalHandler,
			}),
		},
	)
	app.BasicModuleManager.RegisterLegacyAminoCodec(legacyAmino)
	app.BasicModuleManager.RegisterInterfaces(interfaceRegistry)

	app.ModuleManager.SetOrderBeginBlockers(
		minttypes.ModuleName,
		distrtypes.ModuleName,
		slashingtypes.ModuleName,
		stakingtypes.ModuleName,
		authtypes.ModuleName,
		banktypes.ModuleName,
		govtypes.ModuleName,
		genutiltypes.ModuleName,
		paramstypes.ModuleName,
		consensusparamtypes.ModuleName,
		vestingtypes.ModuleName,
		privacytypes.ModuleName,
	)
	app.ModuleManager.SetOrderEndBlockers(
		govtypes.ModuleName,
		stakingtypes.ModuleName,
		authtypes.ModuleName,
		banktypes.ModuleName,
		distrtypes.ModuleName,
		slashingtypes.ModuleName,
		minttypes.ModuleName,
		genutiltypes.ModuleName,
		paramstypes.ModuleName,
		consensusparamtypes.ModuleName,
		vestingtypes.ModuleName,
		privacytypes.ModuleName,
	)

	genesisModuleOrder := []string{
		authtypes.ModuleName,
		banktypes.ModuleName,
		distrtypes.ModuleName,
		stakingtypes.ModuleName,
		slashingtypes.ModuleName,
		govtypes.ModuleName,
		minttypes.ModuleName,
		genutiltypes.ModuleName,
		paramstypes.ModuleName,
		vestingtypes.ModuleName,
		consensusparamtypes.ModuleName,
		privacytypes.ModuleName,
	}
	genesisModuleOrder = append([]string{privacytypes.ModuleName, upgradetypes.ModuleName}, genesisModuleOrder[:len(genesisModuleOrder)-1]...)
	app.ModuleManager.SetOrderInitGenesis(genesisModuleOrder...)
	app.ModuleManager.SetOrderExportGenesis(genesisModuleOrder...)

	app.configurator = module.NewConfigurator(appCodec, app.MsgServiceRouter(), app.GRPCQueryRouter())
	if err := app.ModuleManager.RegisterServices(app.configurator); err != nil {
		panic(err)
	}

	app.MountKVStores(keys)
	app.MountTransientStores(tkeys)

	app.SetInitChainer(app.InitChainer)
	app.SetPreBlocker(app.UpgradePreBlocker)
	app.SetBeginBlocker(app.BeginBlocker)
	app.SetEndBlocker(app.EndBlocker)
	app.setAnteHandler(txConfig)
	app.setPostHandler()

	protoFiles, err := proto.MergedRegistry()
	if err != nil {
		panic(err)
	}
	if err := msgservice.ValidateProtoAnnotations(protoFiles); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err.Error())
	}

	if loadLatest {
		if err := app.LoadLatestVersion(); err != nil {
			panic(fmt.Errorf("error loading latest version: %w", err))
		}
		if app.LastBlockHeight() > 0 {
			ctx := app.NewContextLegacy(true, tmproto.Header{Height: app.LastBlockHeight()})
			identity, found, err := app.PrivacyKeeper.GetCircuitSetIdentity(ctx)
			if err != nil {
				panic(fmt.Errorf("load consensus privacy circuit identity: %w", err))
			}
			if !found {
				panic("consensus privacy circuit identity is missing; fresh genesis or an explicit upgrade is required")
			}
			if err := privacyzk.ValidateLocalVerifierIdentity(identity); err != nil {
				panic(fmt.Errorf("privacy verifier identity mismatch: %w", err))
			}
		}
	}

	return app
}

func (app *ClairveilApp) setAnteHandler(txConfig client.TxConfig) {
	anteHandler, err := newAnteHandler(ante.HandlerOptions{
		AccountKeeper:   app.AccountKeeper,
		BankKeeper:      app.BankKeeper,
		SignModeHandler: txConfig.SignModeHandler(),
		SigGasConsumer:  ante.DefaultSigVerificationGasConsumer,
	}, false)
	if err != nil {
		panic(err)
	}
	app.SetAnteHandler(anteHandler)
}

func (app *ClairveilApp) setPostHandler() {
	postHandler, err := posthandler.NewPostHandler(posthandler.HandlerOptions{})
	if err != nil {
		panic(err)
	}
	app.SetPostHandler(postHandler)
}

func (app *ClairveilApp) Name() string { return app.BaseApp.Name() }

func (app *ClairveilApp) BeginBlocker(ctx sdk.Context) (sdk.BeginBlock, error) {
	if err := app.PrivacyKeeper.BeginAuditBlock(ctx); err != nil {
		return sdk.BeginBlock{}, err
	}
	return app.ModuleManager.BeginBlock(ctx)
}

func (app *ClairveilApp) EndBlocker(ctx sdk.Context) (sdk.EndBlock, error) {
	return app.ModuleManager.EndBlock(ctx)
}

func (app *ClairveilApp) InitChainer(ctx sdk.Context, req *abci.RequestInitChain) (*abci.ResponseInitChain, error) {
	return app.initPrivacyChain(ctx, req)
}

func (app *ClairveilApp) LoadHeight(height int64) error {
	return app.LoadVersion(height)
}

func (app *ClairveilApp) LegacyAmino() *codec.LegacyAmino {
	return app.legacyAmino
}

func (app *ClairveilApp) AppCodec() codec.Codec {
	return app.appCodec
}

func (app *ClairveilApp) InterfaceRegistry() codectypes.InterfaceRegistry {
	return app.interfaceRegistry
}

func (app *ClairveilApp) TxConfig() client.TxConfig {
	return app.txConfig
}

func (app *ClairveilApp) GetSubspace(moduleName string) paramstypes.Subspace {
	subspace, _ := app.ParamsKeeper.GetSubspace(moduleName)
	return subspace
}

func (app *ClairveilApp) RegisterAPIRoutes(apiSvr *api.Server, apiConfig serverconfig.APIConfig) {
	clientCtx := apiSvr.ClientCtx
	authtx.RegisterGRPCGatewayRoutes(clientCtx, apiSvr.GRPCGatewayRouter)
	cmtservice.RegisterGRPCGatewayRoutes(clientCtx, apiSvr.GRPCGatewayRouter)
	nodeservice.RegisterGRPCGatewayRoutes(clientCtx, apiSvr.GRPCGatewayRouter)
	app.BasicModuleManager.RegisterGRPCGatewayRoutes(clientCtx, apiSvr.GRPCGatewayRouter)
	if err := server.RegisterSwaggerAPI(apiSvr.ClientCtx, apiSvr.Router, apiConfig.Swagger); err != nil {
		panic(err)
	}
}

func (app *ClairveilApp) RegisterTxService(clientCtx client.Context) {
	authtx.RegisterTxService(app.GRPCQueryRouter(), clientCtx, app.Simulate, app.interfaceRegistry)
}

func (app *ClairveilApp) RegisterTendermintService(clientCtx client.Context) {
	cmtApp := server.NewCometABCIWrapper(app)
	cmtservice.RegisterTendermintService(clientCtx, app.GRPCQueryRouter(), app.interfaceRegistry, cmtApp.Query)
}

func (app *ClairveilApp) RegisterNodeService(clientCtx client.Context, cfg serverconfig.Config) {
	nodeservice.RegisterNodeService(clientCtx, app.GRPCQueryRouter(), cfg, func() int64 {
		return app.CommitMultiStore().EarliestVersion()
	})
}

func (app *ClairveilApp) ExportAppStateAndValidators(
	forZeroHeight bool,
	_ []string,
	modulesToExport []string,
) (servertypes.ExportedApp, error) {
	if forZeroHeight {
		return servertypes.ExportedApp{}, fmt.Errorf("zero-height export is not supported")
	}
	if len(modulesToExport) != 0 {
		return servertypes.ExportedApp{}, fmt.Errorf("partial module export is not supported")
	}
	ctx := app.NewContextLegacy(true, tmproto.Header{Height: app.LastBlockHeight()})
	genesis, err := app.ModuleManager.ExportGenesis(ctx, app.appCodec)
	if err != nil {
		return servertypes.ExportedApp{}, err
	}
	raw, err := json.Marshal(genesis)
	if err != nil {
		return servertypes.ExportedApp{}, err
	}
	validators, err := staking.WriteValidators(ctx, app.StakingKeeper)
	return servertypes.ExportedApp{
		AppState:        raw,
		Validators:      validators,
		Height:          app.LastBlockHeight() + 1,
		ConsensusParams: app.BaseApp.GetConsensusParams(ctx),
	}, err
}

func GetMaccPerms() map[string][]string {
	out := make(map[string][]string)
	maps.Copy(out, maccPerms)
	return out
}

func BlockedAddresses() map[string]bool {
	modAccAddrs := make(map[string]bool)
	for acc := range GetMaccPerms() {
		modAccAddrs[authtypes.NewModuleAddress(acc).String()] = true
	}
	delete(modAccAddrs, authtypes.NewModuleAddress(govtypes.ModuleName).String())
	return modAccAddrs
}

func initParamsKeeper(appCodec codec.BinaryCodec, legacyAmino *codec.LegacyAmino, key, tkey storetypes.StoreKey) paramskeeper.Keeper {
	paramsKeeper := paramskeeper.NewKeeper(appCodec, legacyAmino, key, tkey)
	paramsKeeper.Subspace(authtypes.ModuleName).WithKeyTable(authtypes.ParamKeyTable())
	paramsKeeper.Subspace(stakingtypes.ModuleName).WithKeyTable(stakingtypes.ParamKeyTable())
	paramsKeeper.Subspace(banktypes.ModuleName).WithKeyTable(banktypes.ParamKeyTable())
	paramsKeeper.Subspace(minttypes.ModuleName)
	paramsKeeper.Subspace(distrtypes.ModuleName).WithKeyTable(distrtypes.ParamKeyTable())
	paramsKeeper.Subspace(slashingtypes.ModuleName).WithKeyTable(slashingtypes.ParamKeyTable())
	paramsKeeper.Subspace(govtypes.ModuleName).WithKeyTable(govv1types.ParamKeyTable())
	paramsKeeper.Subspace(privacytypes.ModuleName)
	return paramsKeeper
}
