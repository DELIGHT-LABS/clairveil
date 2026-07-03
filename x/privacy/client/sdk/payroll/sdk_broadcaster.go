package payroll

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	privacyprovider "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/provider"
)

type SDKTxBroadcaster interface {
	BroadcastSDKMessages(ctx context.Context, msgs ...sdk.Msg) (*sdk.TxResponse, error)
}

type SDKTxMetadataBroadcaster interface {
	BroadcastSDKMessagesWithMetadata(ctx context.Context, msgs ...sdk.Msg) (*privacyprovider.CosmosTxBroadcastResult, error)
}

type SDKMessageBroadcasterAdapter struct {
	Broadcaster SDKTxBroadcaster
}

func (a SDKMessageBroadcasterAdapter) BroadcastMessages(ctx context.Context, msgs ...sdk.Msg) (*BroadcastResult, error) {
	if a.Broadcaster == nil {
		return nil, fmt.Errorf("an sdk tx broadcaster is required")
	}
	if metadataBroadcaster, ok := a.Broadcaster.(SDKTxMetadataBroadcaster); ok {
		result, err := metadataBroadcaster.BroadcastSDKMessagesWithMetadata(ctx, msgs...)
		if err != nil {
			if result == nil {
				return nil, err
			}
			return broadcastResultFromMetadata(result), err
		}
		if result == nil || result.Response == nil {
			return nil, fmt.Errorf("sdk tx broadcaster returned nil response")
		}
		return broadcastResultFromMetadata(result), nil
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

func broadcastResultFromMetadata(result *privacyprovider.CosmosTxBroadcastResult) *BroadcastResult {
	out := &BroadcastResult{
		TxBytesHash:     result.TxBytesHash,
		SignDocHash:     result.SignDocHash,
		AccountSequence: result.AccountSequence,
	}
	if result.Response != nil {
		out.TxHash = result.Response.TxHash
		out.Height = result.Response.Height
		out.Code = result.Response.Code
		out.RawLog = result.Response.RawLog
	}
	return out
}
