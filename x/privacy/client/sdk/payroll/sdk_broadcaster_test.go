package payroll

import (
	"context"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

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

type fakeSDKTxBroadcaster struct {
	response *sdk.TxResponse
}

func (b fakeSDKTxBroadcaster) BroadcastSDKMessages(_ context.Context, _ ...sdk.Msg) (*sdk.TxResponse, error) {
	return b.response, nil
}
