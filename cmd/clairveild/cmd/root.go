package cmd

import (
	"encoding/json"
	"errors"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	tmcfg "github.com/cometbft/cometbft/config"
	tmcli "github.com/cometbft/cometbft/libs/cli"
	dbm "github.com/cosmos/cosmos-db"

	"cosmossdk.io/log/v2"
	confixcmd "cosmossdk.io/tools/confix/cmd"

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
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/genutil"
	genutilcli "github.com/cosmos/cosmos-sdk/x/genutil/client/cli"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"

	"github.com/DELIGHT-LABS/clairveil/app"
	clairveiltypes "github.com/DELIGHT-LABS/clairveil/types"
	privacyzk "github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
)

// NewRootCmd creates the root command for the Clairveil reference daemon.
func NewRootCmd() *cobra.Command {
	initAppOptions := viper.New()
	tempDir := tempDir()
	initAppOptions.Set(flags.FlagHome, tempDir)

	tempApplication := app.NewClairveilApp(
		log.NewNopLogger(),
		dbm.NewMemDB(),
		nil,
		false,
		initAppOptions,
	)
	defer func() {
		if err := tempApplication.Close(); err != nil {
			panic(err)
		}
	}()

	initClientCtx := client.Context{}.
		WithCodec(tempApplication.AppCodec()).
		WithInterfaceRegistry(tempApplication.InterfaceRegistry()).
		WithLegacyAmino(tempApplication.LegacyAmino()).
		WithTxConfig(tempApplication.TxConfig()).
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

	initRootCmd(rootCmd, tempApplication.BasicModuleManager, tempApplication.TxConfig(), tempApplication.AppCodec())
	return rootCmd
}

func initRootCmd(rootCmd *cobra.Command, basicManager module.BasicManager, txConfig client.TxConfig, cdc codec.Codec) {
	sdk.GetConfig().Seal()

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
		genutilcli.ValidateGenesisCmd(basicManager),
		genutilcli.AddGenesisAccountCmd(app.DefaultNodeHome, addressCodec),
		tmcli.NewCompletionCmd(rootCmd, true),
		confixcmd.ConfigCommand(),
		pruning.Cmd(appCreatorWrapper, app.DefaultNodeHome),
		snapshot.Cmd(appCreatorWrapper),
		server.StatusCommand(),
		queryCommand(basicManager),
		txCommand(basicManager),
		keys.Commands(),
	)

	server.AddCommands(rootCmd, app.DefaultNodeHome, ac.newApp, ac.appExport, addModuleInitFlags)
}

func initCmd(basicManager module.BasicManager, cdc codec.Codec) *cobra.Command {
	cmd := genutilcli.InitCmd(basicManager, app.DefaultNodeHome)
	if flag := cmd.Flags().Lookup(genutilcli.FlagDefaultBondDenom); flag != nil {
		flag.DefValue = clairveiltypes.DefaultDenom
		_ = cmd.Flags().Set(genutilcli.FlagDefaultBondDenom, clairveiltypes.DefaultDenom)
	}

	runE := cmd.RunE
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := runE(cmd, args); err != nil {
			return err
		}
		return rewriteGenesisDefaults(cmd, cdc)
	}

	return cmd
}

func rewriteGenesisDefaults(cmd *cobra.Command, cdc codec.Codec) error {
	clientCtx := client.GetClientContextFromCmd(cmd)
	serverCtx := server.GetServerContextFromCmd(cmd)
	serverCtx.Config.SetRoot(clientCtx.HomeDir)

	genFile := serverCtx.Config.GenesisFile()
	appGenesis, err := genutiltypes.AppGenesisFromFile(genFile)
	if err != nil {
		return err
	}

	var appState app.GenesisState
	if err := json.Unmarshal(appGenesis.AppState, &appState); err != nil {
		return err
	}
	app.ApplyClairveilGenesisDefaults(cdc, appState)

	appStateJSON, err := json.MarshalIndent(appState, "", " ")
	if err != nil {
		return err
	}
	appGenesis.AppState = appStateJSON

	return genutil.ExportGenesisFile(appGenesis, genFile)
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

func txCommand(basicManager module.BasicManager) *cobra.Command {
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
	)
	basicManager.AddTxCommands(cmd)

	cmd.PersistentFlags().String(flags.FlagChainID, "", "The network chain ID")
	return cmd
}

type appCreator struct{}

func (a appCreator) newApp(logger log.Logger, db dbm.DB, appOpts servertypes.AppOptions) servertypes.Application {
	baseappOptions := server.DefaultBaseappOptions(appOpts)

	if err := privacyzk.RunPreflight(logger.With(log.ModuleKey, "privacy")); err != nil {
		panic(err)
	}

	return app.NewClairveilApp(logger, db, nil, true, appOpts, baseappOptions...)
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

	loadLatest := height == -1
	clairveilApp := app.NewClairveilApp(logger, db, nil, loadLatest, viperAppOpts)
	if height != -1 {
		if err := clairveilApp.LoadHeight(height); err != nil {
			return servertypes.ExportedApp{}, err
		}
	}

	_ = homePath
	return clairveilApp.ExportAppStateAndValidators(forZeroHeight, jailAllowedAddrs, modulesToExport)
}

var tempDir = func() string {
	dir, err := os.MkdirTemp("", ".clairveil")
	if err != nil {
		return app.DefaultNodeHome
	}
	defer os.RemoveAll(dir)

	return dir
}
