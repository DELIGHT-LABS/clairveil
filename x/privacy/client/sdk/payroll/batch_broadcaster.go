package payroll

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"

	privacyreservation "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/reservation"
)

type BatchBroadcastWorker struct {
	Reservation      privacyreservation.Service
	Broadcaster      MessageBroadcaster
	NullifierChecker BroadcastNullifierChecker
	Assembler        TransferMessageAssembler
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
	refs, operationIDs, err := preflightSubmissionState(ctx, w.Reservation, chunk.Results, w.LeaseOwner, w.LeaseTTL)
	if err != nil {
		return nil, err
	}
	messages, err := messagesForSubmission(chunk, w.Assembler)
	if err != nil {
		return nil, err
	}

	return broadcastWithSubmissionLeaseHeartbeat(ctx, w.Reservation, refs, w.LeaseTTL, func(broadcastCtx context.Context, commit submissionLeaseCommit, refresh submissionLeaseRefresh) (*BroadcastResult, error) {
		if err := ensureNullifiersUnspent(broadcastCtx, resolveBroadcastNullifierChecker(w.NullifierChecker, w.Broadcaster), chunk.Results); err != nil {
			return nil, err
		}
		if err := refresh(); err != nil {
			return nil, err
		}
		preparedBroadcast, err := prepareMessageBroadcast(broadcastCtx, w.Broadcaster, messages...)
		if err != nil {
			return nil, err
		}
		attemptReservations, attemptOperations, err := w.Reservation.MarkBroadcastAttempting(
			broadcastCtx,
			refs,
			operationIDs,
			broadcastAttemptStart(preparedBroadcast.Identity),
		)
		if err != nil {
			return nil, err
		}
		broadcast, err := preparedBroadcast.Submit(broadcastCtx)
		if broadcast == nil && err == nil {
			cause := fmt.Errorf("message broadcaster returned nil result")
			err := &ManualReviewBroadcastError{Cause: cause}
			return nil, errors.Join(err, commit(func(commitCtx context.Context, heartbeatErr error) error {
				return markProofResultsBroadcastAmbiguous(commitCtx, w.Reservation, refs, operationIDs, errors.Join(cause, heartbeatErr))
			}))
		}
		broadcast, identityErr := mergeBroadcastResultWithStoredIdentity(broadcast, attemptReservations, attemptOperations)
		if identityErr != nil {
			ambiguityErr := &ManualReviewBroadcastError{Cause: identityErr}
			return broadcast, errors.Join(err, ambiguityErr, commit(func(commitCtx context.Context, heartbeatErr error) error {
				return markProofResultsBroadcastAmbiguous(commitCtx, w.Reservation, refs, operationIDs, errors.Join(identityErr, heartbeatErr))
			}))
		}
		if err != nil {
			if broadcast != nil {
				err = errors.Join(err, commit(func(commitCtx context.Context, heartbeatErr error) error {
					return markProofResultsBroadcastUnknown(commitCtx, w.Reservation, refs, operationIDs, broadcast, errors.Join(err, heartbeatErr))
				}))
			} else {
				manualReviewErr := &ManualReviewBroadcastError{Cause: err}
				err = errors.Join(manualReviewErr, commit(func(commitCtx context.Context, heartbeatErr error) error {
					return markProofResultsBroadcastAmbiguous(commitCtx, w.Reservation, refs, operationIDs, errors.Join(err, heartbeatErr))
				}))
			}
			return broadcast, err
		}
		if broadcast == nil {
			cause := fmt.Errorf("message broadcaster returned nil result")
			err := &ManualReviewBroadcastError{Cause: cause}
			return nil, errors.Join(err, commit(func(commitCtx context.Context, heartbeatErr error) error {
				return markProofResultsBroadcastAmbiguous(commitCtx, w.Reservation, refs, operationIDs, errors.Join(cause, heartbeatErr))
			}))
		}
		if broadcast.Code != 0 {
			txErr := broadcastCodeError(broadcast)
			return broadcast, errors.Join(txErr, commit(func(commitCtx context.Context, heartbeatErr error) error {
				return markProofResultsBroadcastUnknown(commitCtx, w.Reservation, refs, operationIDs, broadcast, errors.Join(txErr, heartbeatErr))
			}))
		}
		if err := commit(func(commitCtx context.Context, heartbeatErr error) error {
			return markProofResultsSubmitted(commitCtx, w.Reservation, refs, operationIDs, broadcast, heartbeatErr)
		}); err != nil {
			return broadcast, err
		}
		return broadcast, nil
	})
}

func messagesForSubmission(chunk MessageChunk, assembler TransferMessageAssembler) ([]sdk.Msg, error) {
	if len(chunk.Results) == 0 {
		return nil, fmt.Errorf("message chunk %s has no proof results", chunk.ChunkID)
	}
	messages := make([]sdk.Msg, 0, len(chunk.Results))
	seenNullifiers := make(map[string]string)
	for i, result := range chunk.Results {
		if err := validateProofResultArtifact(result, assembler); err != nil {
			return nil, fmt.Errorf("message chunk %s proof result %d is invalid: %w", chunk.ChunkID, i, err)
		}
		if err := checkDuplicateNullifiers(result.Message, result.Item.OperationID, seenNullifiers); err != nil {
			return nil, err
		}
		messages = append(messages, result.Message)
	}
	return messages, nil
}

func preflightSubmissionState(ctx context.Context, reservation privacyreservation.Service, results []ProofResult, leaseOwner string, ttl time.Duration) ([]privacyreservation.SubmittedReservationRef, []string, error) {
	refs, operationIDs, err := collectSubmissionRefs(results)
	if err != nil {
		return nil, nil, err
	}
	if err := validateSubmissionLinks(ctx, reservation.Store, refs, operationIDs); err != nil {
		return nil, nil, err
	}
	ttl = submissionLeaseTTL(ttl)
	for i, ref := range refs {
		if refs[i].LeaseOwner == "" {
			stored, getErr := reservation.Store.GetReservation(ctx, ref.ReservationID)
			if getErr != nil {
				return nil, nil, getErr
			}
			// Legacy ProofResult records did not persist LeaseOwner. Recover it
			// only when the persisted token proves this result belongs to the
			// current lease; never pair an old token with the broadcaster owner.
			if stored.LeaseOwner != "" && stored.LeaseToken != "" && stored.LeaseToken == ref.LeaseToken {
				refs[i].LeaseOwner = stored.LeaseOwner
				ref.LeaseOwner = stored.LeaseOwner
			} else {
				refs[i].LeaseOwner = leaseOwner
				ref.LeaseOwner = leaseOwner
			}
		}
		if _, err := reservation.HeartbeatLeaseForStatus(ctx, ref.ReservationID, ref.LeaseOwner, ref.LeaseToken, privacyreservation.StatusProofReady, ttl); err != nil {
			return nil, nil, fmt.Errorf("proof-ready submission lease is unavailable; reconcile before retry: %w", err)
		}
	}
	return submittedReservationRefs(refs), operationIDs, nil
}

type submissionLeaseCommit func(func(context.Context, error) error) error
type submissionLeaseRefresh func() error

const (
	submissionFinalHeartbeatTimeout = 2 * time.Second
	submissionTerminalCommitTimeout = 10 * time.Second
	submissionTerminalLeaseMargin   = time.Second
)

func broadcastWithSubmissionLeaseHeartbeat(ctx context.Context, reservation privacyreservation.Service, refs []privacyreservation.SubmittedReservationRef, ttl time.Duration, broadcast func(context.Context, submissionLeaseCommit, submissionLeaseRefresh) (*BroadcastResult, error)) (*BroadcastResult, error) {
	if len(refs) == 0 {
		return broadcast(ctx, func(apply func(context.Context, error) error) error { return apply(ctx, nil) }, func() error { return nil })
	}
	ttl = submissionLeaseTTL(ttl)
	heartbeatCtx, cancelHeartbeats := context.WithCancel(ctx)
	defer cancelHeartbeats()

	var stateMu sync.Mutex
	var renewalMu sync.Mutex
	var heartbeatErr error
	terminal := false
	stopping := false
	stopped := make(chan struct{})

	refresh := func() error {
		renewalMu.Lock()
		defer renewalMu.Unlock()

		stateMu.Lock()
		if terminal || stopping {
			stateMu.Unlock()
			return nil
		}
		if heartbeatErr != nil {
			err := heartbeatErr
			stateMu.Unlock()
			return err
		}
		stateMu.Unlock()

		err := heartbeatSubmissionLeases(heartbeatCtx, reservation, refs, ttl)
		if err == nil {
			return nil
		}
		stateMu.Lock()
		defer stateMu.Unlock()
		if terminal || stopping {
			return nil
		}
		if heartbeatErr == nil {
			heartbeatErr = err
			cancelHeartbeats()
		}
		return heartbeatErr
	}

	go func() {
		defer close(stopped)
		ticker := time.NewTicker(submissionHeartbeatInterval(ttl))
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := refresh(); err != nil {
					return
				}
			case <-heartbeatCtx.Done():
				return
			}
		}
	}()

	commit := func(apply func(context.Context, error) error) error {
		stateMu.Lock()
		stopping = true
		schedulerHeartbeatErr := heartbeatErr
		stateMu.Unlock()

		// Cancel an in-flight scheduler heartbeat before waiting on renewalMu.
		// Otherwise a context-aware store call can hold renewalMu forever while
		// commit waits for the lock that it needs in order to cancel that call.
		cancelHeartbeats()
		renewalMu.Lock()
		heartbeatCommitCtx, cancelHeartbeatCommit := context.WithTimeout(
			context.WithoutCancel(ctx),
			submissionFinalHeartbeatTimeout,
		)
		finalHeartbeatErr := heartbeatSubmissionLeases(
			heartbeatCommitCtx,
			reservation,
			refs,
			submissionTerminalLeaseTTL(ttl),
		)
		cancelHeartbeatCommit()
		if finalHeartbeatErr != nil {
			stateMu.Lock()
			if heartbeatErr == nil {
				heartbeatErr = finalHeartbeatErr
			}
			stateMu.Unlock()
		}
		renewalMu.Unlock()

		// No heartbeat may race the terminal state write. The write itself must
		// survive a caller cancellation because the external effect already ran.
		<-stopped

		// The final heartbeat gets its own deadline. Terminal persistence starts
		// with a fresh detached budget after the external broadcast boundary.
		terminalCtx, cancelTerminal := context.WithTimeout(
			context.WithoutCancel(ctx),
			submissionTerminalCommitTimeout,
		)
		defer cancelTerminal()
		if err := apply(terminalCtx, errors.Join(schedulerHeartbeatErr, finalHeartbeatErr)); err != nil {
			return errors.Join(err, schedulerHeartbeatErr, finalHeartbeatErr)
		}
		stateMu.Lock()
		terminal = true
		stateMu.Unlock()
		return nil
	}
	result, err := broadcast(heartbeatCtx, commit, refresh)
	stateMu.Lock()
	stopping = true
	stateMu.Unlock()
	cancelHeartbeats()
	<-stopped
	stateMu.Lock()
	if !terminal {
		err = errors.Join(err, heartbeatErr)
	}
	stateMu.Unlock()
	return result, err
}

func heartbeatSubmissionLeases(ctx context.Context, reservation privacyreservation.Service, refs []privacyreservation.SubmittedReservationRef, ttl time.Duration) error {
	var heartbeatErr error
	for _, ref := range refs {
		if _, err := reservation.HeartbeatLeaseForStatus(ctx, ref.ReservationID, ref.LeaseOwner, ref.LeaseToken, privacyreservation.StatusProofReady, ttl); err != nil {
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

func submissionTerminalLeaseTTL(ttl time.Duration) time.Duration {
	terminalBudget := submissionFinalHeartbeatTimeout + submissionTerminalCommitTimeout + submissionTerminalLeaseMargin
	if ttl < terminalBudget {
		return terminalBudget
	}
	return ttl
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
			return SpentNullifierError{}
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
	operationPayloadHashes := make(map[string]string, len(operationIDs))
	linkedReservationsByOperation := make(map[string]map[string]struct{}, len(operationIDs))
	for _, operationID := range uniqueOperationIDs(operationIDs) {
		operation, err := store.GetOperation(ctx, operationID)
		if err != nil {
			return err
		}
		operationPayloadHashes[operationID] = operation.PayloadHash
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
		if ref.PayloadHash == "" || reservation.PayloadHash != ref.PayloadHash || operationPayloadHashes[ref.OperationID] != ref.PayloadHash {
			return fmt.Errorf("%w: proof result payload hash does not match reservation %s and operation %s", privacyreservation.ErrInvalidReservation, ref.ReservationID, ref.OperationID)
		}
		if reservation.BroadcastInFlight || reservation.BroadcastAttemptCount != 0 {
			return fmt.Errorf("%w: reservation %s has a prior broadcast attempt; reconcile before retry", privacyreservation.ErrInvalidReservation, ref.ReservationID)
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

func markProofResultsSubmitted(ctx context.Context, reservation privacyreservation.Service, refs []privacyreservation.SubmittedReservationRef, operationIDs []string, broadcast *BroadcastResult, heartbeatErr error) error {
	if !broadcastHasSubmissionIdentity(broadcast) {
		ambiguityErr := errors.Join(
			heartbeatErr,
			fmt.Errorf("successful broadcast result is missing tx_hash and tx_bytes_hash"),
		)
		return errors.Join(ambiguityErr, markProofResultsBroadcastAmbiguous(ctx, reservation, refs, operationIDs, ambiguityErr))
	}
	lastError := ""
	if heartbeatErr != nil {
		lastError = heartbeatErr.Error()
	}
	_, _, err := reservation.MarkSubmittedBatch(ctx, refs, operationIDs, privacyreservation.SubmittedReservationUpdate{
		TxHash:             broadcast.TxHash,
		TxBytesHash:        broadcast.TxBytesHash,
		SignDocHash:        broadcast.SignDocHash,
		AccountSequence:    broadcast.AccountSequence,
		LastBroadcastError: lastError,
	})
	if err == nil {
		return nil
	}
	if submittedStateMatches(ctx, reservation.Store, refs, broadcast) {
		return nil
	}
	fallbackErr := markProofResultsBroadcastUnknown(
		ctx,
		reservation,
		refs,
		operationIDs,
		broadcast,
		errors.Join(fmt.Errorf("submitted bookkeeping failed: %w", err), heartbeatErr),
	)
	return errors.Join(err, fallbackErr)
}

func submittedStateMatches(ctx context.Context, store privacyreservation.Store, refs []privacyreservation.SubmittedReservationRef, broadcast *BroadcastResult) bool {
	if store == nil || !broadcastHasSubmissionIdentity(broadcast) {
		return false
	}
	for _, ref := range refs {
		stored, err := store.GetReservation(ctx, ref.ReservationID)
		if err != nil || stored.Status != privacyreservation.StatusSubmitted {
			return false
		}
		if strings.TrimSpace(broadcast.TxHash) != "" && normalizeBroadcastIdentity(stored.TxHash) != normalizeBroadcastIdentity(broadcast.TxHash) {
			return false
		}
		if strings.TrimSpace(broadcast.TxBytesHash) != "" && normalizeBroadcastIdentity(stored.TxBytesHash) != normalizeBroadcastIdentity(broadcast.TxBytesHash) {
			return false
		}
	}
	return true
}

func normalizeBroadcastIdentity(value string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value)), "0x")
}

func prepareMessageBroadcast(ctx context.Context, broadcaster MessageBroadcaster, messages ...sdk.Msg) (*PreparedMessageBroadcast, error) {
	preparedBroadcaster, ok := broadcaster.(PreparedMessageBroadcaster)
	if !ok {
		return nil, fmt.Errorf("%w: message broadcaster", ErrPreparedBroadcastUnsupported)
	}
	prepared, err := preparedBroadcaster.PrepareBroadcastMessages(ctx, messages...)
	if err != nil {
		return nil, err
	}
	if prepared == nil || prepared.Submit == nil {
		return nil, fmt.Errorf("prepared broadcaster returned no submit callback")
	}
	if !broadcastHasAttemptIdentity(&prepared.Identity) {
		return nil, fmt.Errorf("prepared broadcaster returned no durable tx identity")
	}
	return prepared, nil
}

func broadcastAttemptStart(identity BroadcastResult) privacyreservation.BroadcastAttemptStart {
	return privacyreservation.BroadcastAttemptStart{
		Reason:          "payroll broadcast boundary crossed",
		TxHash:          identity.TxHash,
		TxBytesHash:     identity.TxBytesHash,
		SignDocHash:     identity.SignDocHash,
		AccountSequence: identity.AccountSequence,
	}
}

func mergeBroadcastResultWithStoredIdentity(broadcast *BroadcastResult, reservations []privacyreservation.NoteReservation, operations []privacyreservation.PayrollOperation) (*BroadcastResult, error) {
	var merged BroadcastResult
	hasIdentity := false
	if broadcast != nil {
		merged = *broadcast
		hasIdentity = broadcastHasAttemptIdentity(broadcast) || strings.TrimSpace(broadcast.SignDocHash) != ""
	}
	merge := func(name string, target *string, incoming string) error {
		if strings.TrimSpace(incoming) == "" {
			return nil
		}
		hasIdentity = true
		if strings.TrimSpace(*target) == "" {
			*target = incoming
			return nil
		}
		if normalizeBroadcastIdentity(*target) != normalizeBroadcastIdentity(incoming) {
			return fmt.Errorf("stored %s conflicts with broadcast result", name)
		}
		return nil
	}
	for _, reservation := range reservations {
		if err := merge("tx_hash", &merged.TxHash, reservation.TxHash); err != nil {
			return broadcast, err
		}
		if err := merge("tx_bytes_hash", &merged.TxBytesHash, reservation.TxBytesHash); err != nil {
			return broadcast, err
		}
		if err := merge("sign_doc_hash", &merged.SignDocHash, reservation.SignDocHash); err != nil {
			return broadcast, err
		}
		if merged.AccountSequence == 0 && reservation.AccountSequence != 0 {
			merged.AccountSequence = reservation.AccountSequence
		}
	}
	for _, operation := range operations {
		if err := merge("tx_hash", &merged.TxHash, operation.TxHash); err != nil {
			return broadcast, err
		}
		if err := merge("tx_bytes_hash", &merged.TxBytesHash, operation.TxBytesHash); err != nil {
			return broadcast, err
		}
		if err := merge("sign_doc_hash", &merged.SignDocHash, operation.SignDocHash); err != nil {
			return broadcast, err
		}
	}
	if broadcast == nil && !hasIdentity {
		return nil, nil
	}
	return &merged, nil
}

func broadcastCodeError(broadcast *BroadcastResult) error {
	return fmt.Errorf("tx failed with code %d: %s", broadcast.Code, broadcast.RawLog)
}

func markProofResultsBroadcastUnknown(ctx context.Context, reservation privacyreservation.Service, refs []privacyreservation.SubmittedReservationRef, operationIDs []string, broadcast *BroadcastResult, broadcastErr error) error {
	if !broadcastHasAttemptIdentity(broadcast) {
		ambiguityErr := errors.Join(
			broadcastErr,
			fmt.Errorf("broadcast result is missing durable tx identity"),
		)
		return errors.Join(ambiguityErr, markProofResultsBroadcastAmbiguous(ctx, reservation, refs, operationIDs, ambiguityErr))
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
	if err == nil {
		return nil
	}
	ambiguityErr := errors.Join(
		broadcastErr,
		fmt.Errorf("broadcast identity could not be persisted as Unknown: %w", err),
		fmt.Errorf(
			"conflicting attempt identity tx_hash=%q tx_bytes_hash=%q sign_doc_hash=%q",
			broadcast.TxHash,
			broadcast.TxBytesHash,
			broadcast.SignDocHash,
		),
	)
	return errors.Join(
		err,
		markProofResultsBroadcastAmbiguous(ctx, reservation, refs, operationIDs, ambiguityErr),
	)
}

func broadcastHasAttemptIdentity(broadcast *BroadcastResult) bool {
	if broadcast == nil {
		return false
	}
	return strings.TrimSpace(broadcast.TxHash) != "" ||
		strings.TrimSpace(broadcast.TxBytesHash) != ""
}

func broadcastHasSubmissionIdentity(broadcast *BroadcastResult) bool {
	if broadcast == nil {
		return false
	}
	return strings.TrimSpace(broadcast.TxHash) != "" || strings.TrimSpace(broadcast.TxBytesHash) != ""
}

func markProofResultsBroadcastAmbiguous(ctx context.Context, reservation privacyreservation.Service, refs []privacyreservation.SubmittedReservationRef, operationIDs []string, broadcastErr error) error {
	lastError := ""
	if broadcastErr != nil {
		lastError = broadcastErr.Error()
	}
	_, _, err := reservation.MarkBroadcastAmbiguousBatch(ctx, refs, operationIDs, privacyreservation.BroadcastAmbiguityUpdate{
		LastBroadcastError: lastError,
	})
	return err
}

type submissionRef struct {
	privacyreservation.SubmittedReservationRef
	OperationID string
	PayloadHash string
}

func collectSubmissionRefs(results []ProofResult) ([]submissionRef, []string, error) {
	refs := make([]submissionRef, 0)
	operationIDs := make([]string, 0, len(results))
	for _, result := range results {
		if result.Item.OperationID == "" {
			return nil, nil, fmt.Errorf("%w: proof result has no operation_id", privacyreservation.ErrInvalidReservation)
		}
		payloadHash := strings.TrimSpace(result.Payload.PayloadHash)
		if payloadHash == "" || result.Proof.PayloadHash != payloadHash {
			return nil, nil, fmt.Errorf("%w: proof result payload hash mismatch for operation %s", privacyreservation.ErrInvalidReservation, result.Item.OperationID)
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
			owner := result.ReservationLeaseOwners[note.ReservationID]
			refs = append(refs, submissionRef{
				SubmittedReservationRef: privacyreservation.SubmittedReservationRef{
					ReservationID: note.ReservationID,
					LeaseOwner:    owner,
					LeaseToken:    token,
				},
				OperationID: result.Item.OperationID,
				PayloadHash: payloadHash,
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
