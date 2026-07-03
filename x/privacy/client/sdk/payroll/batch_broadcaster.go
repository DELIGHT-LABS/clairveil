package payroll

import (
	"context"
	"fmt"

	privacyreservation "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/reservation"
)

type BatchBroadcastWorker struct {
	Reservation privacyreservation.Service
	Broadcaster MessageBroadcaster
}

func (w BatchBroadcastWorker) SubmitChunk(ctx context.Context, chunk MessageChunk) (*BroadcastResult, error) {
	if w.Reservation.Store == nil {
		return nil, fmt.Errorf("reservation service is required")
	}
	if w.Broadcaster == nil {
		return nil, fmt.Errorf("a message broadcaster is required")
	}
	if len(chunk.Messages) == 0 {
		return nil, fmt.Errorf("message chunk %s has no messages", chunk.ChunkID)
	}

	broadcast, err := w.Broadcaster.BroadcastMessages(ctx, chunk.Messages...)
	if err != nil {
		return nil, err
	}
	if broadcast == nil {
		return nil, fmt.Errorf("message broadcaster returned nil result")
	}
	if broadcast.Code != 0 {
		return broadcast, fmt.Errorf("tx failed with code %d: %s", broadcast.Code, broadcast.RawLog)
	}

	for _, result := range chunk.Results {
		if err := markProofResultSubmitted(ctx, w.Reservation, result, broadcast); err != nil {
			return nil, err
		}
	}
	return broadcast, nil
}

func markProofResultSubmitted(ctx context.Context, reservation privacyreservation.Service, result ProofResult, broadcast *BroadcastResult) error {
	for _, note := range result.Item.InputNotes {
		token := result.ReservationLeases[note.ReservationID]
		if _, err := reservation.MarkSubmitted(
			ctx,
			note.ReservationID,
			token,
			broadcast.TxHash,
			broadcast.TxBytesHash,
			broadcast.SignDocHash,
			broadcast.AccountSequence,
		); err != nil {
			return err
		}
	}

	if result.Item.OperationID == "" {
		return nil
	}
	operation, err := reservation.Store.GetOperation(ctx, result.Item.OperationID)
	if err != nil {
		return err
	}
	operation.Status = privacyreservation.OperationStatusSubmitted
	operation.TxHash = broadcast.TxHash
	operation.TxBytesHash = broadcast.TxBytesHash
	operation.SignDocHash = broadcast.SignDocHash
	operation.UpdatedAt = reservationNow(reservation)
	_, err = reservation.Store.UpdateOperation(ctx, *operation)
	return err
}
