package payroll

import (
	"context"
	"fmt"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	privacyprovider "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/provider"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestSDKMessageBroadcasterAdapterMapsTxResponse(t *testing.T) {
	adapter := SDKMessageBroadcasterAdapter{
		Broadcaster: fakeSDKTxBroadcaster{
			response: &sdk.TxResponse{
				TxHash: "TXHASH",
				Height: 42,
				Code:   7,
				RawLog: "failed",
			},
		},
	}

	result, err := adapter.BroadcastMessages(context.Background(), &privacytypes.MsgTransfer{})
	require.NoError(t, err)
	require.Equal(t, "TXHASH", result.TxHash)
	require.EqualValues(t, 42, result.Height)
	require.EqualValues(t, 7, result.Code)
	require.Equal(t, "failed", result.RawLog)
}

func TestSDKMessageBroadcasterAdapterPreservesMetadata(t *testing.T) {
	adapter := SDKMessageBroadcasterAdapter{
		Broadcaster: fakeSDKTxMetadataBroadcaster{
			result: &privacyprovider.CosmosTxBroadcastResult{
				Response: &sdk.TxResponse{
					TxHash: "TXHASH",
					Height: 42,
					Code:   0,
				},
				TxBytesHash:     "tx-bytes-hash",
				SignDocHash:     "sign-doc-hash",
				AccountSequence: 7,
			},
		},
	}

	result, err := adapter.BroadcastMessages(context.Background(), &privacytypes.MsgTransfer{})
	require.NoError(t, err)
	require.Equal(t, "tx-bytes-hash", result.TxBytesHash)
	require.Equal(t, "sign-doc-hash", result.SignDocHash)
	require.EqualValues(t, 7, result.AccountSequence)
}

func TestSDKMessageBroadcasterAdapterPreservesMetadataOnError(t *testing.T) {
	adapter := SDKMessageBroadcasterAdapter{
		Broadcaster: fakeSDKTxMetadataBroadcaster{
			result: &privacyprovider.CosmosTxBroadcastResult{
				TxBytesHash:     "tx-bytes-hash",
				SignDocHash:     "sign-doc-hash",
				AccountSequence: 7,
			},
			err: fmt.Errorf("rpc timeout"),
		},
	}

	result, err := adapter.BroadcastMessages(context.Background(), &privacytypes.MsgTransfer{})
	require.ErrorContains(t, err, "rpc timeout")
	require.Equal(t, "tx-bytes-hash", result.TxBytesHash)
	require.Equal(t, "sign-doc-hash", result.SignDocHash)
	require.EqualValues(t, 7, result.AccountSequence)
}

type fakeSDKTxBroadcaster struct {
	response *sdk.TxResponse
}

func (b fakeSDKTxBroadcaster) BroadcastSDKMessages(_ context.Context, _ ...sdk.Msg) (*sdk.TxResponse, error) {
	return b.response, nil
}

type fakeSDKTxMetadataBroadcaster struct {
	result *privacyprovider.CosmosTxBroadcastResult
	err    error
}

func (b fakeSDKTxMetadataBroadcaster) BroadcastSDKMessages(_ context.Context, _ ...sdk.Msg) (*sdk.TxResponse, error) {
	if b.result == nil {
		return nil, b.err
	}
	return b.result.Response, b.err
}

func (b fakeSDKTxMetadataBroadcaster) BroadcastSDKMessagesWithMetadata(_ context.Context, _ ...sdk.Msg) (*privacyprovider.CosmosTxBroadcastResult, error) {
	return b.result, b.err
}
