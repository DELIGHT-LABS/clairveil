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

type SDKTxPreparedBroadcaster interface {
	PrepareSDKMessagesWithMetadata(ctx context.Context, msgs ...sdk.Msg) (*privacyprovider.PreparedCosmosTxBroadcast, error)
	BroadcastPreparedSDKMessages(ctx context.Context, prepared *privacyprovider.PreparedCosmosTxBroadcast) (*privacyprovider.CosmosTxBroadcastResult, error)
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

func (a SDKMessageBroadcasterAdapter) PrepareBroadcastMessages(ctx context.Context, msgs ...sdk.Msg) (*PreparedMessageBroadcast, error) {
	preparedBroadcaster, ok := a.Broadcaster.(SDKTxPreparedBroadcaster)
	if !ok {
		return nil, fmt.Errorf("%w: sdk tx broadcaster", ErrPreparedBroadcastUnsupported)
	}
	prepared, err := preparedBroadcaster.PrepareSDKMessagesWithMetadata(ctx, msgs...)
	if err != nil {
		return nil, err
	}
	if prepared == nil {
		return nil, fmt.Errorf("sdk tx broadcaster returned nil prepared transaction")
	}
	identity := broadcastResultFromMetadata(&prepared.Result)
	return &PreparedMessageBroadcast{
		Identity: *identity,
		Submit: func(submitCtx context.Context) (*BroadcastResult, error) {
			result, submitErr := preparedBroadcaster.BroadcastPreparedSDKMessages(submitCtx, prepared)
			if result == nil {
				if submitErr != nil {
					return nil, submitErr
				}
				return nil, fmt.Errorf("sdk tx broadcaster returned nil prepared broadcast result")
			}
			if result.Response == nil && submitErr == nil {
				// Metadata proves the exact signed transaction identity, but it is
				// not proof that the node accepted it. Return that identity with an
				// error so the caller durably records an ambiguous attempt instead of
				// treating the zero-valued response fields as a successful broadcast.
				return broadcastResultFromMetadata(result), fmt.Errorf("sdk tx broadcaster returned nil prepared broadcast response")
			}
			return broadcastResultFromMetadata(result), submitErr
		},
	}, nil
}

func broadcastResultFromMetadata(result *privacyprovider.CosmosTxBroadcastResult) *BroadcastResult {
	out := &BroadcastResult{
		TxHash:          result.TxHash,
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
