package payroll

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

type SDKTxBroadcaster interface {
	BroadcastSDKMessages(ctx context.Context, msgs ...sdk.Msg) (*sdk.TxResponse, error)
}

type SDKMessageBroadcasterAdapter struct {
	Broadcaster SDKTxBroadcaster
}

func (a SDKMessageBroadcasterAdapter) BroadcastMessages(ctx context.Context, msgs ...sdk.Msg) (*BroadcastResult, error) {
	if a.Broadcaster == nil {
		return nil, fmt.Errorf("an sdk tx broadcaster is required")
	}
	response, err := a.Broadcaster.BroadcastSDKMessages(ctx, msgs...)
	if err != nil {
		return nil, err
	}
	if response == nil {
		return nil, fmt.Errorf("sdk tx broadcaster returned nil response")
	}
	return &BroadcastResult{
		TxHash: response.TxHash,
		Height: response.Height,
		Code:   response.Code,
		RawLog: response.RawLog,
	}, nil
}
