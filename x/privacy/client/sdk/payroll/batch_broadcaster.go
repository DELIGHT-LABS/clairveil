package payroll

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"

	privacyreservation "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/reservation"
)

type BatchBroadcastWorker struct {
	Reservation      privacyreservation.Service
	Broadcaster      MessageBroadcaster
	NullifierChecker BroadcastNullifierChecker
	LeaseOwner       string
	LeaseTTL         time.Duration
}

type BroadcastNullifierChecker interface {
	CheckNullifiersUsed(ctx context.Context, nullifierHexes []string) (map[string]bool, error)
}

func (w BatchBroadcastWorker) SubmitChunk(ctx context.Context, chunk MessageChunk) (*BroadcastResult, error) {
	if w.Reservation.Store == nil {
		return nil, fmt.Errorf("reservation service is required")
	}
	if w.Broadcaster == nil {
		return nil, fmt.Errorf("a message broadcaster is required")
	}
	messages, err := messagesForSubmission(chunk)
	if err != nil {
		return nil, err
	}

	refs, operationIDs, acquiredLeases, err := preflightSubmissionState(ctx, w.Reservation, chunk.Results, w.LeaseOwner, w.LeaseTTL)
	if err != nil {
		return nil, err
	}
	if err := ensureNullifiersUnspent(ctx, resolveBroadcastNullifierChecker(w.NullifierChecker, w.Broadcaster), chunk.Results); err != nil {
		return nil, errors.Join(err, clearAcquiredSubmissionLeases(ctx, w.Reservation, acquiredLeases))
	}

	broadcast, err := broadcastWithSubmissionLeaseHeartbeat(ctx, w.Reservation, refs, w.LeaseTTL, func(broadcastCtx context.Context) (*BroadcastResult, error) {
		return w.Broadcaster.BroadcastMessages(broadcastCtx, messages...)
	})
	if err != nil {
		if broadcast != nil {
			err = errors.Join(err, markProofResultsBroadcastUnknown(ctx, w.Reservation, refs, operationIDs, broadcast, err))
		}
		return broadcast, err
	}
	if broadcast == nil {
		return nil, fmt.Errorf("message broadcaster returned nil result")
	}
	if broadcast.Code != 0 {
		txErr := broadcastCodeError(broadcast)
		return broadcast, errors.Join(txErr, markProofResultsBroadcastUnknown(ctx, w.Reservation, refs, operationIDs, broadcast, txErr))
	}

	if err := markProofResultsSubmitted(ctx, w.Reservation, refs, operationIDs, broadcast); err != nil {
		return broadcast, err
	}
	return broadcast, nil
}

func messagesForSubmission(chunk MessageChunk) ([]sdk.Msg, error) {
	if len(chunk.Results) == 0 {
		return nil, fmt.Errorf("message chunk %s has no proof results", chunk.ChunkID)
	}
	messages := make([]sdk.Msg, 0, len(chunk.Results))
	seenNullifiers := make(map[string]string)
	for i, result := range chunk.Results {
		if result.Message == nil {
			return nil, fmt.Errorf("message chunk %s proof result %d has no message", chunk.ChunkID, i)
		}
		if err := checkDuplicateNullifiers(result.Message, result.Item.OperationID, seenNullifiers); err != nil {
			return nil, err
		}
		messages = append(messages, result.Message)
	}
	return messages, nil
}

func preflightSubmissionState(ctx context.Context, reservation privacyreservation.Service, results []ProofResult, leaseOwner string, ttl time.Duration) ([]privacyreservation.SubmittedReservationRef, []string, []privacyreservation.SubmittedReservationRef, error) {
	refs, operationIDs, err := collectSubmissionRefs(results)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := validateSubmissionLinks(ctx, reservation.Store, refs, operationIDs); err != nil {
		return nil, nil, nil, err
	}
	ttl = submissionLeaseTTL(ttl)
	acquiredLeases := make([]privacyreservation.SubmittedReservationRef, 0)
	for i, ref := range refs {
		if _, err := reservation.HeartbeatLeaseForStatus(ctx, ref.ReservationID, ref.LeaseToken, privacyreservation.StatusProofReady, ttl); err == nil {
			continue
		} else if (errors.Is(err, privacyreservation.ErrLeaseUnavailable) || errors.Is(err, privacyreservation.ErrLeaseMismatch)) && leaseOwner != "" {
			lease, acquireErr := reservation.AcquireLeaseForStatus(ctx, ref.ReservationID, leaseOwner, privacyreservation.StatusProofReady, ttl)
			if acquireErr != nil {
				return nil, nil, nil, errors.Join(acquireErr, clearAcquiredSubmissionLeases(ctx, reservation, acquiredLeases))
			}
			refs[i].LeaseToken = lease.Token
			acquiredLeases = append(acquiredLeases, privacyreservation.SubmittedReservationRef{
				ReservationID: ref.ReservationID,
				LeaseToken:    lease.Token,
			})
		} else {
			return nil, nil, nil, errors.Join(err, clearAcquiredSubmissionLeases(ctx, reservation, acquiredLeases))
		}
	}
	return submittedReservationRefs(refs), operationIDs, acquiredLeases, nil
}

func broadcastWithSubmissionLeaseHeartbeat(ctx context.Context, reservation privacyreservation.Service, refs []privacyreservation.SubmittedReservationRef, ttl time.Duration, broadcast func(context.Context) (*BroadcastResult, error)) (*BroadcastResult, error) {
	if len(refs) == 0 {
		return broadcast(ctx)
	}
	ttl = submissionLeaseTTL(ttl)
	heartbeatCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	heartbeatErr := make(chan error, 1)
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(submissionHeartbeatInterval(ttl))
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := heartbeatSubmissionLeases(heartbeatCtx, reservation, refs, ttl); err != nil {
					select {
					case heartbeatErr <- err:
					default:
					}
					cancel()
					return
				}
			case <-heartbeatCtx.Done():
				return
			}
		}
	}()

	result, err := broadcast(heartbeatCtx)
	cancel()
	<-stopped
	select {
	case hbErr := <-heartbeatErr:
		err = errors.Join(err, hbErr)
	default:
	}
	return result, err
}

func heartbeatSubmissionLeases(ctx context.Context, reservation privacyreservation.Service, refs []privacyreservation.SubmittedReservationRef, ttl time.Duration) error {
	var heartbeatErr error
	for _, ref := range refs {
		if _, err := reservation.HeartbeatLeaseForStatus(ctx, ref.ReservationID, ref.LeaseToken, privacyreservation.StatusProofReady, ttl); err != nil {
			heartbeatErr = errors.Join(heartbeatErr, err)
		}
	}
	return heartbeatErr
}

func submissionLeaseTTL(ttl time.Duration) time.Duration {
	if ttl <= 0 {
		return time.Minute
	}
	return ttl
}

func submissionHeartbeatInterval(ttl time.Duration) time.Duration {
	interval := ttl / 2
	if interval <= 0 {
		return ttl
	}
	return interval
}

func clearAcquiredSubmissionLeases(ctx context.Context, reservation privacyreservation.Service, refs []privacyreservation.SubmittedReservationRef) error {
	var clearErr error
	for _, ref := range refs {
		if _, err := reservation.ClearLease(ctx, ref.ReservationID, ref.LeaseToken); err != nil && !errors.Is(err, privacyreservation.ErrLeaseUnavailable) && !errors.Is(err, privacyreservation.ErrLeaseMismatch) {
			clearErr = errors.Join(clearErr, err)
		}
	}
	return clearErr
}

func ensureNullifiersUnspent(ctx context.Context, checker BroadcastNullifierChecker, results []ProofResult) error {
	if checker == nil {
		return fmt.Errorf("%w: broadcast nullifier checker is required", ErrInvalidPayrollInput)
	}
	nullifiers := make([]string, 0)
	seen := make(map[string]struct{})
	for _, result := range results {
		if result.Message == nil {
			continue
		}
		for _, nullifier := range result.Message.Nullifiers {
			hexNullifier := strings.ToLower(hex.EncodeToString(nullifier))
			if hexNullifier == "" {
				continue
			}
			if _, ok := seen[hexNullifier]; ok {
				continue
			}
			seen[hexNullifier] = struct{}{}
			nullifiers = append(nullifiers, hexNullifier)
		}
	}
	if len(nullifiers) == 0 {
		return nil
	}
	usedByNullifier, err := checker.CheckNullifiersUsed(ctx, nullifiers)
	if err != nil {
		return err
	}
	for _, nullifier := range nullifiers {
		used, ok := usedByNullifier[nullifier]
		if !ok {
			return fmt.Errorf("missing nullifier status for %s", nullifier)
		}
		if used {
			return SpentNullifierError{NullifierHex: nullifier}
		}
	}
	return nil
}

func resolveBroadcastNullifierChecker(explicit BroadcastNullifierChecker, broadcaster MessageBroadcaster) BroadcastNullifierChecker {
	if explicit != nil {
		return explicit
	}
	checker, _ := broadcaster.(BroadcastNullifierChecker)
	return checker
}

func validateSubmissionLinks(ctx context.Context, store privacyreservation.Store, refs []submissionRef, operationIDs []string) error {
	operationIDSet := make(map[string]struct{}, len(operationIDs))
	linkedReservationsByOperation := make(map[string]map[string]struct{}, len(operationIDs))
	for _, operationID := range uniqueOperationIDs(operationIDs) {
		if _, err := store.GetOperation(ctx, operationID); err != nil {
			return err
		}
		operationIDSet[operationID] = struct{}{}
	}
	seenReservations := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		if _, exists := seenReservations[ref.ReservationID]; exists {
			return fmt.Errorf("%w: duplicate reservation_id %s", privacyreservation.ErrInvalidReservation, ref.ReservationID)
		}
		seenReservations[ref.ReservationID] = struct{}{}
		if ref.OperationID == "" {
			return fmt.Errorf("%w: proof result has no operation_id", privacyreservation.ErrInvalidReservation)
		}
		reservation, err := store.GetReservation(ctx, ref.ReservationID)
		if err != nil {
			return err
		}
		if reservation.OperationID == "" {
			return fmt.Errorf("%w: reservation %s has no operation_id", privacyreservation.ErrInvalidReservation, ref.ReservationID)
		}
		if reservation.OperationID != ref.OperationID {
			return fmt.Errorf("%w: reservation %s belongs to operation %s", privacyreservation.ErrInvalidReservation, ref.ReservationID, reservation.OperationID)
		}
		if _, ok := operationIDSet[ref.OperationID]; !ok {
			return fmt.Errorf("%w: operation %s has no linked proof result", privacyreservation.ErrInvalidReservation, ref.OperationID)
		}
		if linkedReservationsByOperation[ref.OperationID] == nil {
			linkedReservationsByOperation[ref.OperationID] = make(map[string]struct{})
		}
		linkedReservationsByOperation[ref.OperationID][ref.ReservationID] = struct{}{}
	}
	for operationID := range operationIDSet {
		if _, ok := linkedReservationsByOperation[operationID]; !ok {
			return fmt.Errorf("%w: operation %s has no linked reservation", privacyreservation.ErrInvalidReservation, operationID)
		}
	}
	activeReservations, err := store.ListReservations(ctx, privacyreservation.ReservationFilter{
		Statuses: []privacyreservation.ReservationStatus{
			privacyreservation.StatusReserved,
			privacyreservation.StatusProving,
			privacyreservation.StatusProofReady,
			privacyreservation.StatusSubmitted,
			privacyreservation.StatusUnknown,
			privacyreservation.StatusManualReview,
		},
	})
	if err != nil {
		return err
	}
	for _, reservation := range activeReservations {
		if reservation.OperationID == "" {
			continue
		}
		if _, ok := operationIDSet[reservation.OperationID]; !ok {
			continue
		}
		if _, ok := linkedReservationsByOperation[reservation.OperationID][reservation.ReservationID]; !ok {
			return fmt.Errorf("%w: operation %s missing reservation %s", privacyreservation.ErrInvalidReservation, reservation.OperationID, reservation.ReservationID)
		}
	}
	return nil
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

func broadcastCodeError(broadcast *BroadcastResult) error {
	return fmt.Errorf("tx failed with code %d: %s", broadcast.Code, broadcast.RawLog)
}

func markProofResultsBroadcastUnknown(ctx context.Context, reservation privacyreservation.Service, refs []privacyreservation.SubmittedReservationRef, operationIDs []string, broadcast *BroadcastResult, broadcastErr error) error {
	if broadcast == nil {
		return nil
	}
	lastError := ""
	if broadcastErr != nil {
		lastError = broadcastErr.Error()
	}
	_, _, err := reservation.MarkBroadcastUnknownBatch(ctx, refs, operationIDs, privacyreservation.BroadcastAttemptUpdate{
		TxHash:             broadcast.TxHash,
		TxBytesHash:        broadcast.TxBytesHash,
		SignDocHash:        broadcast.SignDocHash,
		AccountSequence:    broadcast.AccountSequence,
		LastBroadcastError: lastError,
	})
	return err
}

type submissionRef struct {
	privacyreservation.SubmittedReservationRef
	OperationID string
}

func collectSubmissionRefs(results []ProofResult) ([]submissionRef, []string, error) {
	refs := make([]submissionRef, 0)
	operationIDs := make([]string, 0, len(results))
	for _, result := range results {
		if result.Item.OperationID == "" {
			return nil, nil, fmt.Errorf("%w: proof result has no operation_id", privacyreservation.ErrInvalidReservation)
		}
		operationIDs = append(operationIDs, result.Item.OperationID)
		if len(result.Item.InputNotes) == 0 {
			return nil, nil, fmt.Errorf("proof result for operation %s has no input notes", result.Item.OperationID)
		}
		for _, note := range result.Item.InputNotes {
			token := result.ReservationLeases[note.ReservationID]
			if token == "" {
				return nil, nil, fmt.Errorf("proof result for operation %s has no lease token for reservation %s", result.Item.OperationID, note.ReservationID)
			}
			refs = append(refs, submissionRef{
				SubmittedReservationRef: privacyreservation.SubmittedReservationRef{
					ReservationID: note.ReservationID,
					LeaseToken:    token,
				},
				OperationID: result.Item.OperationID,
			})
		}
	}
	return refs, operationIDs, nil
}

func submittedReservationRefs(refs []submissionRef) []privacyreservation.SubmittedReservationRef {
	out := make([]privacyreservation.SubmittedReservationRef, len(refs))
	for i, ref := range refs {
		out[i] = ref.SubmittedReservationRef
	}
	return out
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
