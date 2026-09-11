package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	tmcfg "github.com/cometbft/cometbft/config"
	tmcli "github.com/cometbft/cometbft/libs/cli"
	dbm "github.com/cosmos/cosmos-db"

	"cosmossdk.io/log/v2"
	confixcmd "cosmossdk.io/tools/confix/cmd"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/config"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/keys"
	"github.com/cosmos/cosmos-sdk/client/pruning"
	"github.com/cosmos/cosmos-sdk/client/rpc"
	"github.com/cosmos/cosmos-sdk/client/snapshot"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/server"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	authcmd "github.com/cosmos/cosmos-sdk/x/auth/client/cli"
	"github.com/cosmos/cosmos-sdk/x/auth/types"
	vestingcli "github.com/cosmos/cosmos-sdk/x/auth/vesting/client/cli"
	bankcli "github.com/cosmos/cosmos-sdk/x/bank/client/cli"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/genutil"
	genutilcli "github.com/cosmos/cosmos-sdk/x/genutil/client/cli"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	"github.com/cosmos/cosmos-sdk/x/gov"
	govclient "github.com/cosmos/cosmos-sdk/x/gov/client"
	paramsclient "github.com/cosmos/cosmos-sdk/x/params/client"
	stakingcli "github.com/cosmos/cosmos-sdk/x/staking/client/cli"
	upgrademodule "github.com/cosmos/cosmos-sdk/x/upgrade"

	"github.com/DELIGHT-LABS/clairveil/app"
	clairveiltypes "github.com/DELIGHT-LABS/clairveil/types"
	privacycli "github.com/DELIGHT-LABS/clairveil/x/privacy/client/cli"
	privacykeeper "github.com/DELIGHT-LABS/clairveil/x/privacy/keeper"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	privacyv2 "github.com/DELIGHT-LABS/clairveil/x/privacy/types/v2"
	privacyzk "github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
)

const (
	auditConfigFlag    = "audit-config"
	auditArtifactsFlag = "audit-artifacts"
)

// NewRootCmd creates the root command for the Clairveil reference daemon.
func NewRootCmd() *cobra.Command {
	initAppOptions := viper.New()
	tempDir := tempDir()
	initAppOptions.Set(flags.FlagHome, tempDir)

	tempCodec := app.NewClientCodec()
	// This object supplies only offline CLI encoding/basic-module metadata.
	// Server startup and export always construct the configured audit runtime.
	privacyv2.RegisterInterfaces(tempCodec.InterfaceRegistry)

	initClientCtx := client.Context{}.
		WithCodec(tempCodec.AppCodec).
		WithInterfaceRegistry(tempCodec.InterfaceRegistry).
		WithLegacyAmino(tempCodec.LegacyAmino).
		WithTxConfig(tempCodec.TxConfig).
		WithInput(os.Stdin).
		WithAccountRetriever(types.AccountRetriever{}).
		WithHomeDir(app.DefaultNodeHome).
		WithViper("CLAIRVEIL")

	rootCmd := &cobra.Command{
		Use:   "clairveild",
		Short: "Clairveil reference daemon",
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SetOut(cmd.OutOrStdout())
			cmd.SetErr(cmd.ErrOrStderr())

			initClientCtx = initClientCtx.WithCmdContext(cmd.Context())
			clientCtx, err := client.ReadPersistentCommandFlags(initClientCtx, cmd.Flags())
			if err != nil {
				return err
			}

			clientCtx, err = config.ReadFromClientConfig(clientCtx)
			if err != nil {
				return err
			}
			if err = client.SetCmdClientContextHandler(clientCtx, cmd); err != nil {
				return err
			}

			return server.InterceptConfigsPreRunHandler(
				cmd,
				serverconfig.DefaultConfigTemplate,
				serverconfig.DefaultConfig(),
				tmcfg.DefaultConfig(),
			)
		},
	}

	initRootCmd(rootCmd, tempCodec.BasicModuleManager, tempCodec.TxConfig, tempCodec.AppCodec)
	return rootCmd
}

func initRootCmd(rootCmd *cobra.Command, basicManager module.BasicManager, txConfig client.TxConfig, cdc codec.Codec) {
	sdk.GetConfig().Seal()
	rootCmd.PersistentFlags().String(auditConfigFlag, "", "작은 감사 V2 구성 파일(시작·export에 필수)")
	rootCmd.PersistentFlags().String(auditArtifactsFlag, "", "고정 검증키 디렉터리(시작·export에 필수)")

	ac := appCreator{}
	appCreatorWrapper := func(l log.Logger, d dbm.DB, ao servertypes.AppOptions) servertypes.Application {
		return ac.newApp(l, d, ao)
	}

	validatorAddressCodec := txConfig.SigningContext().ValidatorAddressCodec()
	addressCodec := txConfig.SigningContext().AddressCodec()

	rootCmd.AddCommand(
		initCmd(basicManager, cdc),
		genutilcli.CollectGenTxsCmd(banktypes.GenesisBalancesIterator{}, app.DefaultNodeHome, genutiltypes.DefaultMessageValidator, validatorAddressCodec),
		genutilcli.GenTxCmd(basicManager, txConfig, banktypes.GenesisBalancesIterator{}, app.DefaultNodeHome, validatorAddressCodec),
		validateGenesisCmd(basicManager),
		genutilcli.AddGenesisAccountCmd(app.DefaultNodeHome, addressCodec),
		tmcli.NewCompletionCmd(rootCmd, true),
		confixcmd.ConfigCommand(),
		pruning.Cmd(appCreatorWrapper, app.DefaultNodeHome),
		snapshot.Cmd(appCreatorWrapper),
		server.StatusCommand(),
		queryCommand(basicManager),
		txCommand(txConfig),
		keys.Commands(),
	)

	server.AddCommands(rootCmd, app.DefaultNodeHome, ac.newApp, ac.appExport, addModuleInitFlags)
}

func initCmd(basicManager module.BasicManager, cdc codec.Codec) *cobra.Command {
	// Build public Cosmos defaults, then install the small V4 privacy genesis.
	// No runtime archive, source bundle, or replay input participates.
	bootstrapManager := make(module.BasicManager, len(basicManager)-1)
	for name, basic := range basicManager {
		if name != privacytypes.ModuleName {
			bootstrapManager[name] = basic
		}
	}
	cmd := genutilcli.InitCmd(bootstrapManager, app.DefaultNodeHome)
	if flag := cmd.Flags().Lookup(genutilcli.FlagDefaultBondDenom); flag != nil {
		flag.DefValue = clairveiltypes.DefaultDenom
		_ = cmd.Flags().Set(genutilcli.FlagDefaultBondDenom, clairveiltypes.DefaultDenom)
	}
	runE := cmd.RunE
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		config, err := auditNodeConfigFromCmd(cmd)
		if err != nil {
			return err
		}
		if err := alignInitWithAuditConfig(cmd, config); err != nil {
			return err
		}
		if err := runE(cmd, args); err != nil {
			return err
		}
		return rewriteGenesisDefaults(cmd, cdc, config)
	}

	return cmd
}

func validateGenesisCmd(basicManager module.BasicManager) *cobra.Command {
	cmd := genutilcli.ValidateGenesisCmd(basicManager)
	runE := cmd.RunE
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := runE(cmd, args); err != nil {
			return err
		}
		return validateAuditGenesis(cmd, args)
	}
	return cmd
}

func validateAuditGenesis(cmd *cobra.Command, args []string) error {
	serverCtx := server.GetServerContextFromCmd(cmd)
	genesisPath := serverCtx.Config.GenesisFile()
	if len(args) == 1 {
		genesisPath = args[0]
	}
	genesis, err := genutiltypes.AppGenesisFromFile(genesisPath)
	if err != nil {
		return err
	}
	var state app.GenesisState
	if err := json.Unmarshal(genesis.AppState, &state); err != nil {
		return err
	}
	_, err = privacytypes.ParseFreshGenesisV4(state[privacytypes.ModuleName])
	return err
}

type auditNodeConfig struct {
	ChainID            string                           `json:"chain_id"`
	NetworkNonce       []byte                           `json:"network_nonce32"`
	InitialHeight      uint64                           `json:"initial_height"`
	InitialAuditKey    privacytypes.InitialAuditKeyV4   `json:"initial_audit_key"`
	CircuitSetIdentity *privacytypes.CircuitSetIdentity `json:"circuit_set_identity"`
}

func (c auditNodeConfig) validate() error {
	g := privacytypes.GenesisStateV4{FormatVersion: 4, Mode: "FRESH", NetworkNonce: c.NetworkNonce, InitialHeight: c.InitialHeight, InitialAuditKey: c.InitialAuditKey, CircuitSetIdentity: c.CircuitSetIdentity, AssetRegistry: defaultAuditGenesisAssets(), TreeCapacity: 1 << 32}
	if c.ChainID == "" {
		return fmt.Errorf("audit config chain_id is required")
	}
	return g.Validate()
}

func auditNodeConfigFromCmd(cmd *cobra.Command) (auditNodeConfig, error) {
	configPath, err := cmd.Flags().GetString(auditConfigFlag)
	if err != nil {
		return auditNodeConfig{}, err
	}
	if configPath == "" {
		return auditNodeConfig{}, fmt.Errorf("--%s is required", auditConfigFlag)
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return auditNodeConfig{}, fmt.Errorf("read audit config: %w", err)
	}
	var config auditNodeConfig
	if err := json.Unmarshal(raw, &config); err != nil {
		return auditNodeConfig{}, err
	}
	return config, config.validate()
}

func alignInitWithAuditConfig(cmd *cobra.Command, config auditNodeConfig) error {
	chainID, err := cmd.Flags().GetString(flags.FlagChainID)
	if err != nil {
		return err
	}
	if chainID != "" && chainID != config.ChainID {
		return fmt.Errorf("--%s must match audit runtime chain ID", flags.FlagChainID)
	}
	if err := cmd.Flags().Set(flags.FlagChainID, config.ChainID); err != nil {
		return err
	}
	initialHeight, err := cmd.Flags().GetInt64(flags.FlagInitHeight)
	if err != nil {
		return err
	}
	if cmd.Flags().Changed(flags.FlagInitHeight) && uint64(initialHeight) != config.InitialHeight {
		return fmt.Errorf("--%s must match audit runtime initial height", flags.FlagInitHeight)
	}
	return cmd.Flags().Set(flags.FlagInitHeight, fmt.Sprintf("%d", config.InitialHeight))
}

func rewriteGenesisDefaults(cmd *cobra.Command, cdc codec.Codec, config auditNodeConfig) error {
	clientCtx := client.GetClientContextFromCmd(cmd)
	serverCtx := server.GetServerContextFromCmd(cmd)
	serverCtx.Config.SetRoot(clientCtx.HomeDir)

	genFile := serverCtx.Config.GenesisFile()
	appGenesis, err := genutiltypes.AppGenesisFromFile(genFile)
	if err != nil {
		return err
	}

	if appGenesis.ChainID != config.ChainID || uint64(appGenesis.InitialHeight) != config.InitialHeight {
		return fmt.Errorf("generated genesis chain/height differs from audit config")
	}
	privacyGenesis := privacytypes.GenesisStateV4{FormatVersion: 4, Mode: "FRESH", NetworkNonce: config.NetworkNonce, InitialHeight: config.InitialHeight, InitialAuditKey: config.InitialAuditKey, CircuitSetIdentity: config.CircuitSetIdentity, AssetRegistry: defaultAuditGenesisAssets(), TreeCapacity: 1 << 32}
	appStateJSON, err := json.MarshalIndent(privacyGenesis, "", " ")
	if err != nil {
		return err
	}
	var state map[string]json.RawMessage
	if err := json.Unmarshal(appGenesis.AppState, &state); err != nil {
		return err
	}
	state[privacytypes.ModuleName] = appStateJSON
	appGenesis.AppState, err = json.MarshalIndent(state, "", " ")
	if err != nil {
		return err
	}

	return genutil.ExportGenesisFile(appGenesis, genFile)
}

func defaultAuditGenesisAssets() []*privacytypes.AssetRegistryEntryV1 {
	assetID := privacytypes.ComputeAssetIDV1(clairveiltypes.DefaultDenom).FillBytes(make([]byte, 32))
	return []*privacytypes.AssetRegistryEntryV1{{CanonicalDenom: clairveiltypes.DefaultDenom, AssetId: assetID}}
}

func addModuleInitFlags(startCmd *cobra.Command) {
	startCmd.Flags().Set(server.FlagMinGasPrices, "0"+clairveiltypes.DefaultDenom)
}

func queryCommand(basicManager module.BasicManager) *cobra.Command {
	cmd := &cobra.Command{
		Use:                        "query",
		Aliases:                    []string{"q"},
		Short:                      "Querying subcommands",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(
		bankQueryCommand(),
		rpc.ValidatorCommand(),
		server.QueryBlocksCmd(),
		server.QueryBlockCmd(),
		server.QueryBlockResultsCmd(),
		authcmd.QueryTxsByEventsCmd(),
		authcmd.QueryTxCmd(),
	)
	basicManager.AddQueryCommands(cmd)

	cmd.PersistentFlags().String(flags.FlagChainID, "", "The network chain ID")
	return cmd
}

func bankQueryCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        "bank",
		Short:                      "Bank query subcommands",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}
	cmd.AddCommand(bankBalancesCommand())
	return cmd
}

func bankBalancesCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "balances [address]",
		Short: "Query all balances for an account",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx := client.GetClientContextFromCmd(cmd)
			if _, err := sdk.AccAddressFromBech32(args[0]); err != nil {
				return err
			}

			queryClient := banktypes.NewQueryClient(clientCtx)
			res, err := queryClient.AllBalances(cmd.Context(), &banktypes.QueryAllBalancesRequest{
				Address: args[0],
			})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

func txCommand(txConfig client.TxConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:                        "tx",
		Short:                      "Transactions subcommands",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(
		authcmd.GetSignCommand(),
		authcmd.GetSignBatchCommand(),
		authcmd.GetMultiSignCommand(),
		authcmd.GetMultiSignBatchCmd(),
		authcmd.GetValidateSignaturesCommand(),
		flags.LineBreak,
		authcmd.GetBroadcastCommand(),
		authcmd.GetEncodeCommand(),
		authcmd.GetDecodeCommand(),
		bankcli.NewTxCmd(txConfig.SigningContext().AddressCodec()),
		stakingcli.NewTxCmd(txConfig.SigningContext().ValidatorAddressCodec(), txConfig.SigningContext().AddressCodec()),
		gov.NewAppModuleBasic([]govclient.ProposalHandler{paramsclient.ProposalHandler}).GetTxCmd(),
		upgrademodule.NewAppModule(nil, txConfig.SigningContext().AddressCodec()).GetTxCmd(),
		vestingcli.GetTxCmd(txConfig.SigningContext().AddressCodec()),
		privacycli.GetTxCmd(),
	)

	cmd.PersistentFlags().String(flags.FlagChainID, "", "The network chain ID")
	return cmd
}

type appCreator struct{}

func (a appCreator) newApp(logger log.Logger, db dbm.DB, appOpts servertypes.AppOptions) servertypes.Application {
	application, err := newPrivacyApp(logger, db, appOpts, -1)
	if err != nil {
		panic(err)
	}
	return application
}

func (a appCreator) appExport(
	logger log.Logger,
	db dbm.DB,
	height int64,
	forZeroHeight bool,
	jailAllowedAddrs []string,
	appOpts servertypes.AppOptions,
	modulesToExport []string,
) (servertypes.ExportedApp, error) {
	homePath, ok := appOpts.Get(flags.FlagHome).(string)
	if !ok || homePath == "" {
		return servertypes.ExportedApp{}, errors.New("application home is not set")
	}

	viperAppOpts, ok := appOpts.(*viper.Viper)
	if !ok {
		return servertypes.ExportedApp{}, errors.New("app options are not viper-backed")
	}
	viperAppOpts.Set(server.FlagInvCheckPeriod, 1)
	application, err := newPrivacyApp(logger, db, viperAppOpts, height)
	if err != nil {
		return servertypes.ExportedApp{}, err
	}
	defer application.Close()
	_ = homePath
	return application.ExportAppStateAndValidators(forZeroHeight, jailAllowedAddrs, modulesToExport)
}

func newPrivacyApp(logger log.Logger, db dbm.DB, appOpts servertypes.AppOptions, height int64) (*app.ClairveilApp, error) {
	configPath, _ := appOpts.Get(auditConfigFlag).(string)
	artifactDir, _ := appOpts.Get(auditArtifactsFlag).(string)
	if configPath == "" || artifactDir == "" {
		return nil, fmt.Errorf("--%s and --%s are required", auditConfigFlag, auditArtifactsFlag)
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}
	var config auditNodeConfig
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, err
	}
	if err := config.validate(); err != nil {
		return nil, err
	}
	registry, err := privacyzk.NewArtifactRegistry(privacyzk.ArtifactRegistryConfig{ArtifactDir: artifactDir, CircuitSetID: privacyzk.AuditFieldCircuitSetID, RuntimeEnvironment: privacyzk.ZKRuntimeEnvironmentDevelopment})
	if err != nil {
		return nil, err
	}
	identity, err := registry.LocalCircuitSetIdentity()
	if err != nil {
		return nil, err
	}
	configured, _ := config.CircuitSetIdentity.Marshal()
	local, _ := identity.Marshal()
	if string(configured) != string(local) {
		return nil, fmt.Errorf("local verifier identity differs from audit config")
	}
	var nonce [32]byte
	copy(nonce[:], config.NetworkNonce)
	application := app.NewAuditFieldApp(logger, db, nil, appOpts, privacykeeper.AuditRuntimeConfig{ArtifactDir: artifactDir, ExpectedIdentity: config.CircuitSetIdentity, NetworkNonce: nonce, InitialHeight: config.InitialHeight, Authority: types.NewModuleAddress("gov").String()}, baseapp.SetChainID(config.ChainID))
	if height >= 0 {
		if err := application.LoadHeight(height); err != nil {
			return nil, err
		}
	} else if err := application.LoadLatestVersion(); err != nil {
		return nil, err
	}
	return application, nil
}

var tempDir = func() string {
	dir, err := os.MkdirTemp("", ".clairveil")
	if err != nil {
		return app.DefaultNodeHome
	}
	defer os.RemoveAll(dir)

	return dir
}
