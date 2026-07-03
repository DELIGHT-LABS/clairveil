package payroll

import (
	"context"
	"fmt"
	"time"

	privacyreservation "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/reservation"
)

type BatchBroadcastWorker struct {
	Reservation privacyreservation.Service
	Broadcaster MessageBroadcaster
	LeaseTTL    time.Duration
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

	refs, operationIDs, err := preflightSubmissionState(ctx, w.Reservation, chunk.Results, w.LeaseTTL)
	if err != nil {
		return nil, err
	}

	broadcast, err := w.Broadcaster.BroadcastMessages(ctx, chunk.Messages...)
	if err != nil {
		return broadcast, err
	}
	if broadcast == nil {
		return nil, fmt.Errorf("message broadcaster returned nil result")
	}
	if broadcast.Code != 0 {
		return broadcast, fmt.Errorf("tx failed with code %d: %s", broadcast.Code, broadcast.RawLog)
	}

	if err := markProofResultsSubmitted(ctx, w.Reservation, refs, operationIDs, broadcast); err != nil {
		return broadcast, err
	}
	return broadcast, nil
}

func preflightSubmissionState(ctx context.Context, reservation privacyreservation.Service, results []ProofResult, ttl time.Duration) ([]privacyreservation.SubmittedReservationRef, []string, error) {
	refs, operationIDs, err := collectSubmissionRefs(results)
	if err != nil {
		return nil, nil, err
	}
	for _, operationID := range uniqueOperationIDs(operationIDs) {
		if _, err := reservation.Store.GetOperation(ctx, operationID); err != nil {
			return nil, nil, err
		}
	}
	if ttl <= 0 {
		ttl = time.Minute
	}
	for _, ref := range refs {
		if _, err := reservation.HeartbeatLeaseForStatus(ctx, ref.ReservationID, ref.LeaseToken, privacyreservation.StatusProofReady, ttl); err != nil {
			return nil, nil, err
		}
	}
	return refs, operationIDs, nil
}

func markProofResultsSubmitted(ctx context.Context, reservation privacyreservation.Service, refs []privacyreservation.SubmittedReservationRef, operationIDs []string, broadcast *BroadcastResult) error {
	_, _, err := reservation.MarkSubmittedBatch(ctx, refs, operationIDs, privacyreservation.SubmittedReservationUpdate{
		TxHash:          broadcast.TxHash,
		TxBytesHash:     broadcast.TxBytesHash,
		SignDocHash:     broadcast.SignDocHash,
		AccountSequence: broadcast.AccountSequence,
	})
	return err
}

func collectSubmissionRefs(results []ProofResult) ([]privacyreservation.SubmittedReservationRef, []string, error) {
	refs := make([]privacyreservation.SubmittedReservationRef, 0)
	operationIDs := make([]string, 0, len(results))
	for _, result := range results {
		if result.Item.OperationID != "" {
			operationIDs = append(operationIDs, result.Item.OperationID)
		}
		if len(result.Item.InputNotes) == 0 {
			return nil, nil, fmt.Errorf("proof result for operation %s has no input notes", result.Item.OperationID)
		}
		for _, note := range result.Item.InputNotes {
			token := result.ReservationLeases[note.ReservationID]
			if token == "" {
				return nil, nil, fmt.Errorf("proof result for operation %s has no lease token for reservation %s", result.Item.OperationID, note.ReservationID)
			}
			refs = append(refs, privacyreservation.SubmittedReservationRef{
				ReservationID: note.ReservationID,
				LeaseToken:    token,
			})
		}
	}
	return refs, operationIDs, nil
}

func uniqueOperationIDs(operationIDs []string) []string {
	out := make([]string, 0, len(operationIDs))
	seen := make(map[string]struct{}, len(operationIDs))
	for _, operationID := range operationIDs {
		if operationID == "" {
			continue
		}
		if _, ok := seen[operationID]; ok {
			continue
		}
		seen[operationID] = struct{}{}
		out = append(out, operationID)
	}
	return out
}
