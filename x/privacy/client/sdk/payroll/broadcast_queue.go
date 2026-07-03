package payroll

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	privacyreservation "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/reservation"
)

type BroadcastResult struct {
	TxHash          string
	TxBytesHash     string
	SignDocHash     string
	AccountSequence uint64
	Height          int64
	Code            uint32
	RawLog          string
}

type MessageBroadcaster interface {
	BroadcastMessages(ctx context.Context, msgs ...sdk.Msg) (*BroadcastResult, error)
}

type BroadcastWorker struct {
	Reservation privacyreservation.Service
	Broadcaster MessageBroadcaster
}

func (w BroadcastWorker) SubmitProofResult(ctx context.Context, result ProofResult) (*BroadcastResult, error) {
	if w.Reservation.Store == nil {
		return nil, fmt.Errorf("reservation service is required")
	}
	if w.Broadcaster == nil {
		return nil, fmt.Errorf("a message broadcaster is required")
	}
	if result.Message == nil {
		return nil, fmt.Errorf("proof result has no transfer message")
	}

	broadcast, err := w.Broadcaster.BroadcastMessages(ctx, result.Message)
	if err != nil {
		return nil, err
	}
	if broadcast == nil {
		return nil, fmt.Errorf("message broadcaster returned nil result")
	}
	if broadcast.Code != 0 {
		return broadcast, fmt.Errorf("tx failed with code %d: %s", broadcast.Code, broadcast.RawLog)
	}

	if err := markProofResultSubmitted(ctx, w.Reservation, result, broadcast); err != nil {
		return nil, err
	}

	return broadcast, nil
}
