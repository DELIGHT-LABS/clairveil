package provider

import (
	"testing"

	sdkmath "cosmossdk.io/math"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"

	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
)

func TestCosmosTxBroadcasterPrepareFactoryRequiresFlags(t *testing.T) {
	broadcaster := CosmosTxBroadcaster{}

	_, err := broadcaster.PrepareFactory(testProviderMsg())
	require.ErrorContains(t, err, "tx flags are required to prepare a tx factory")
}

func TestCosmosTxBroadcasterGenerateOrBroadcastRequiresMessages(t *testing.T) {
	broadcaster := CosmosTxBroadcaster{
		Flags: pflag.NewFlagSet("test", pflag.ContinueOnError),
	}

	err := broadcaster.GenerateOrBroadcast()
	require.ErrorContains(t, err, "at least one sdk message is required to generate or broadcast a tx")
}

func TestCosmosTxBroadcasterGenerateOrBroadcastRequiresFlags(t *testing.T) {
	broadcaster := CosmosTxBroadcaster{}

	err := broadcaster.GenerateOrBroadcast(testProviderMsg())
	require.ErrorContains(t, err, "tx flags are required to generate or broadcast a tx")
}

func TestCosmosTxBroadcasterPrepareFactoryRequiresTxConfig(t *testing.T) {
	broadcaster := CosmosTxBroadcaster{
		Flags: pflag.NewFlagSet("test", pflag.ContinueOnError),
	}

	_, err := broadcaster.PrepareFactory(testProviderMsg())
	require.ErrorContains(t, err, "tx config is required to prepare a tx factory")
}

func TestCosmosTxBroadcasterPrepareFactoryRequiresAccountRetriever(t *testing.T) {
	encodingConfig := moduletestutil.MakeTestEncodingConfig()
	broadcaster := CosmosTxBroadcaster{
		ClientContext: client.Context{}.WithTxConfig(encodingConfig.TxConfig),
		Flags:         pflag.NewFlagSet("test", pflag.ContinueOnError),
	}

	_, err := broadcaster.PrepareFactory(testProviderMsg())
	require.ErrorContains(t, err, "account retriever is required to prepare a tx factory")
}

func TestCosmosTxBroadcasterPrepareFactoryRequiresFromAddress(t *testing.T) {
	encodingConfig := moduletestutil.MakeTestEncodingConfig()
	broadcaster := CosmosTxBroadcaster{
		ClientContext: client.Context{}.
			WithTxConfig(encodingConfig.TxConfig).
			WithAccountRetriever(client.MockAccountRetriever{}),
		Flags: pflag.NewFlagSet("test", pflag.ContinueOnError),
	}

	_, err := broadcaster.PrepareFactory(testProviderMsg())
	require.ErrorContains(t, err, "from address is required to prepare a tx factory")
}

func TestCosmosTxBroadcasterPrepareFactoryRequiresFromName(t *testing.T) {
	encodingConfig := moduletestutil.MakeTestEncodingConfig()
	broadcaster := CosmosTxBroadcaster{
		ClientContext: client.Context{}.
			WithTxConfig(encodingConfig.TxConfig).
			WithAccountRetriever(client.MockAccountRetriever{}).
			WithFromAddress(sdk.AccAddress(make([]byte, 20))),
		Flags: pflag.NewFlagSet("test", pflag.ContinueOnError),
	}

	_, err := broadcaster.PrepareFactory(testProviderMsg())
	require.ErrorContains(t, err, "from name is required to sign the tx")
}

func testProviderMsg() sdk.Msg {
	from := sdk.AccAddress(make([]byte, 20))
	to := sdk.AccAddress(make([]byte, 20))
	return banktypes.NewMsgSend(from, to, sdk.NewCoins(sdk.NewCoin("uclair", sdkmath.OneInt())))
}
