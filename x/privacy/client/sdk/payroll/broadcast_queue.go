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

	for _, note := range result.Item.InputNotes {
		token := result.ReservationLeases[note.ReservationID]
		if _, err := w.Reservation.MarkSubmitted(
			ctx,
			note.ReservationID,
			token,
			broadcast.TxHash,
			broadcast.TxBytesHash,
			broadcast.SignDocHash,
			broadcast.AccountSequence,
		); err != nil {
			return nil, err
		}
	}

	if result.Item.OperationID != "" {
		operation, err := w.Reservation.Store.GetOperation(ctx, result.Item.OperationID)
		if err != nil {
			return nil, err
		}
		operation.Status = privacyreservation.OperationStatusSubmitted
		operation.TxHash = broadcast.TxHash
		operation.TxBytesHash = broadcast.TxBytesHash
		operation.SignDocHash = broadcast.SignDocHash
		operation.UpdatedAt = reservationNow(w.Reservation)
		if _, err := w.Reservation.Store.UpdateOperation(ctx, *operation); err != nil {
			return nil, err
		}
	}

	return broadcast, nil
}
