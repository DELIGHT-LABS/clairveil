package provider

import (
	"context"
	"strings"
	"testing"

	sdkmath "cosmossdk.io/math"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
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

func TestCosmosTxBroadcasterRejectsMutatedPreparedBytes(t *testing.T) {
	original := []byte("original tx bytes")
	prepared := &PreparedCosmosTxBroadcast{
		TxBytes: append([]byte(nil), original...),
		Result: CosmosTxBroadcastResult{
			TxBytesHash: sha256Hex(original),
		},
	}
	prepared.TxBytes[0] ^= 0xff

	result, err := (CosmosTxBroadcaster{}).BroadcastPreparedSDKMessages(context.Background(), prepared)
	require.ErrorContains(t, err, "prepared tx bytes hash mismatch")
	require.Equal(t, prepared.Result.TxBytesHash, result.TxBytesHash)
}

func TestCosmosTxBroadcasterPreparedBroadcastUsesCallerContext(t *testing.T) {
	txBytes := []byte("signed tx bytes")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	broadcaster := CosmosTxBroadcaster{ClientContext: client.Context{
		Client:        contextAwareCometRPC{},
		BroadcastMode: flags.BroadcastSync,
	}}
	prepared := &PreparedCosmosTxBroadcast{
		TxBytes: txBytes,
		Result: CosmosTxBroadcastResult{
			TxBytesHash: sha256Hex(txBytes),
			TxHash:      strings.ToUpper(sha256Hex(txBytes)),
		},
	}

	result, err := broadcaster.BroadcastPreparedSDKMessages(ctx, prepared)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, strings.ToUpper(sha256Hex(txBytes)), result.TxHash)
}

func TestCosmosTxBroadcasterBuildSignedUsesCallerContextForAccountLookup(t *testing.T) {
	encodingConfig := moduletestutil.MakeTestEncodingConfig()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	broadcaster := CosmosTxBroadcaster{
		ClientContext: client.Context{}.
			WithTxConfig(encodingConfig.TxConfig).
			WithAccountRetriever(contextAwareAccountRetriever{}).
			WithFromAddress(sdk.AccAddress(make([]byte, 20))).
			WithFromName("test-account"),
		Flags: pflag.NewFlagSet("test", pflag.ContinueOnError),
	}

	_, err := broadcaster.BuildSignedSDKMessages(ctx, testProviderMsg())
	require.ErrorIs(t, err, context.Canceled)
}

type contextAwareCometRPC struct {
	client.CometRPC
}

func (contextAwareCometRPC) BroadcastTxSync(ctx context.Context, _ cmttypes.Tx) (*coretypes.ResultBroadcastTx, error) {
	return nil, ctx.Err()
}

type contextAwareAccountRetriever struct {
	client.MockAccountRetriever
}

func (contextAwareAccountRetriever) EnsureExists(clientContext client.Context, _ sdk.AccAddress) error {
	return clientContext.GetCmdContextWithFallback().Err()
}

func testProviderMsg() sdk.Msg {
	from := sdk.AccAddress(make([]byte, 20))
	to := sdk.AccAddress(make([]byte, 20))
	return banktypes.NewMsgSend(from, to, sdk.NewCoins(sdk.NewCoin("uclair", sdkmath.OneInt())))
}
