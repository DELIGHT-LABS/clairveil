package reservation

import (
	"context"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

var _ BatchOperationStore = (*MemoryStore)(nil)

func (s *MemoryStore) CreateBatchOperation(_ context.Context, reservations []NoteReservation, graph BatchOperationGraph) (*BatchOperationGraph, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureMapsLocked()

	if err := s.validateBatchGraphCreateLocked(reservations, graph); err != nil {
		return nil, err
	}
	for _, reservation := range reservations {
		s.storeReservationLocked(reservation)
	}
	operationID := graph.Operation.OperationID
	s.batchOperations[operationID] = cloneBatchOperation(graph.Operation)
	s.batchInputs[operationID] = cloneBatchInputs(graph.Inputs)
	s.batchItems[operationID] = cloneBatchItems(graph.Items)
	s.batchEvidence[operationID] = cloneBatchEvidence(graph.Evidence)
	created := s.batchGraphLocked(operationID)
	return &created, nil
}

func (s *MemoryStore) validateBatchGraphCreateLocked(reservations []NoteReservation, graph BatchOperationGraph) error {
	op := graph.Operation
	if op.SchemaVersion != BatchOperationSchemaVersionV1 {
		return fmt.Errorf("%w: batch operation schema version must be %q", ErrInvalidReservation, BatchOperationSchemaVersionV1)
	}
	if strings.TrimSpace(op.OperationID) == "" {
		return fmt.Errorf("%w: batch operation_id is required", ErrInvalidReservation)
	}
	if _, exists := s.batchOperations[op.OperationID]; exists {
		return fmt.Errorf("%w: batch operation already exists", ErrInvalidReservation)
	}
	if _, exists := s.operations[op.OperationID]; exists {
		return fmt.Errorf("%w: operation_id already exists in ordinary payroll operations", ErrInvalidReservation)
	}
	if op.Status != OperationStatusPlanned {
		return fmt.Errorf("%w: new batch operation status must be Planned", ErrInvalidReservation)
	}
	if len(op.PreparedPayloadCiphertext) == 0 || strings.TrimSpace(op.PreparedPayloadHash) == "" {
		return fmt.Errorf("%w: prepared batch payload must be durably attached before reservation", ErrInvalidReservation)
	}
	if len(reservations) < 1 || len(reservations) > 16 || op.InputCount != len(reservations) {
		return fmt.Errorf("%w: batch input reservations must be 1..16 and equal input_count", ErrInvalidReservation)
	}
	if len(graph.Items) < 1 || len(graph.Items) > 32 || op.OutputCount != len(graph.Items) {
		return fmt.Errorf("%w: batch item outputs must be 1..32 and equal output_count", ErrInvalidReservation)
	}
	if len(graph.Inputs) != len(reservations) || len(graph.Evidence) != len(graph.Items) {
		return fmt.Errorf("%w: batch graph relations must be complete", ErrInvalidReservation)
	}

	pendingReservations := make(map[string]NoteReservation, len(reservations))
	pendingActiveKeys := make(map[string]string, len(reservations))
	reservationIDs := make(map[string]struct{}, len(reservations))
	for _, reservation := range reservations {
		if reservation.Status != StatusReserved || reservation.OperationID != op.OperationID {
			return fmt.Errorf("%w: batch input reservation must be Reserved and linked to operation", ErrInvalidReservation)
		}
		if err := s.validateReservationCreateLocked(reservation, pendingReservations, pendingActiveKeys); err != nil {
			return err
		}
		pendingReservations[reservation.ReservationID] = reservation
		reservationIDs[reservation.ReservationID] = struct{}{}
	}

	seenInputIndex := make(map[int]struct{}, len(graph.Inputs))
	seenInputReservation := make(map[string]struct{}, len(graph.Inputs))
	for _, input := range graph.Inputs {
		if input.SchemaVersion != BatchOperationSchemaVersionV1 || input.OperationID != op.OperationID || input.InputIndex < 0 || input.InputIndex >= op.InputCount || !isCanonicalBatchFieldHex(input.Commitment) {
			return fmt.Errorf("%w: invalid batch operation input relation", ErrInvalidReservation)
		}
		if _, ok := reservationIDs[input.ReservationID]; !ok {
			return fmt.Errorf("%w: batch operation input references an unknown reservation", ErrInvalidReservation)
		}
		if _, exists := seenInputIndex[input.InputIndex]; exists {
			return fmt.Errorf("%w: duplicate batch input index", ErrInvalidReservation)
		}
		if _, exists := seenInputReservation[input.ReservationID]; exists {
			return fmt.Errorf("%w: duplicate batch input reservation", ErrInvalidReservation)
		}
		seenInputIndex[input.InputIndex] = struct{}{}
		seenInputReservation[input.ReservationID] = struct{}{}
	}

	evidenceByIndex := make(map[int]ExpectedOutputEvidence, len(graph.Evidence))
	for _, evidence := range graph.Evidence {
		if evidence.SchemaVersion != BatchOperationSchemaVersionV1 || evidence.OperationID != op.OperationID || evidence.OutputIndex < 0 || evidence.OutputIndex >= op.OutputCount {
			return fmt.Errorf("%w: invalid expected output evidence relation", ErrInvalidReservation)
		}
		if strings.TrimSpace(evidence.Commitment) == "" || strings.TrimSpace(evidence.FullDisclosureDigest) == "" || strings.TrimSpace(evidence.AssetID) == "" || strings.TrimSpace(evidence.Denom) == "" {
			return fmt.Errorf("%w: expected output evidence is incomplete", ErrInvalidReservation)
		}
		if err := validateBatchUserEvidence(evidence); err != nil {
			return err
		}
		if _, exists := evidenceByIndex[evidence.OutputIndex]; exists {
			return fmt.Errorf("%w: duplicate expected output evidence index", ErrInvalidReservation)
		}
		evidenceByIndex[evidence.OutputIndex] = evidence
	}

	seenItemIndex := make(map[int]struct{}, len(graph.Items))
	for _, item := range graph.Items {
		if item.SchemaVersion != BatchOperationSchemaVersionV1 || item.OperationID != op.OperationID || item.OutputIndex < 0 || item.OutputIndex >= op.OutputCount {
			return fmt.Errorf("%w: invalid payroll item output relation", ErrInvalidReservation)
		}
		if item.EvidenceStatus != BatchItemEvidencePending {
			return fmt.Errorf("%w: new payroll item evidence status must be Pending", ErrInvalidReservation)
		}
		if !validBatchOutputRole(item.Role) {
			return fmt.Errorf("%w: unsupported batch output role %q", ErrInvalidReservation, item.Role)
		}
		if item.Role == BatchOutputRolePayment && strings.TrimSpace(item.ItemID) == "" {
			return fmt.Errorf("%w: payment output item_id is required", ErrInvalidReservation)
		}
		if item.Role != BatchOutputRolePayment && strings.TrimSpace(item.ItemID) != "" {
			return fmt.Errorf("%w: change/padding output must not claim a payroll item", ErrInvalidReservation)
		}
		if _, exists := seenItemIndex[item.OutputIndex]; exists {
			return fmt.Errorf("%w: duplicate payroll output index", ErrInvalidReservation)
		}
		seenItemIndex[item.OutputIndex] = struct{}{}
		evidence, ok := evidenceByIndex[item.OutputIndex]
		if !ok || evidence.Role != item.Role {
			return fmt.Errorf("%w: payroll output and expected evidence role mismatch", ErrInvalidReservation)
		}
	}
	return nil
}

func (s *MemoryStore) GetBatchOperation(_ context.Context, operationID string) (*BatchOperationGraph, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureMapsLocked()
	if _, ok := s.batchOperations[operationID]; !ok {
		return nil, ErrOperationNotFound
	}
	graph := s.batchGraphLocked(operationID)
	return &graph, nil
}

func (s *MemoryStore) AcquireBatchOperationLease(_ context.Context, operationID, owner, token string, leaseUntil, now time.Time) (*BatchOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureMapsLocked()
	op, ok := s.batchOperations[operationID]
	if !ok {
		return nil, ErrOperationNotFound
	}
	if strings.TrimSpace(owner) == "" || strings.TrimSpace(token) == "" || !leaseUntil.After(now) {
		return nil, ErrLeaseUnavailable
	}
	if op.LeaseToken != "" && op.LeaseUntil.After(now) {
		return nil, ErrLeaseUnavailable
	}
	if IsTerminalOperationStatus(op.Status) {
		return nil, ErrLeaseUnavailable
	}
	inputReservations, err := s.batchInputReservationsLocked(operationID)
	if err != nil {
		return nil, err
	}
	for _, reservation := range inputReservations {
		if !IsActiveReservationStatus(reservation.Status) || (reservation.LeaseToken != "" && reservation.LeaseUntil.After(now)) {
			return nil, ErrLeaseUnavailable
		}
	}
	op.LeaseOwner = owner
	op.LeaseToken = token
	op.LeaseUntil = leaseUntil
	op.LastHeartbeatAt = now
	op.UpdatedAt = now
	s.batchOperations[operationID] = cloneBatchOperation(op)
	for _, reservation := range inputReservations {
		reservation.LeaseOwner = owner
		reservation.LeaseToken = token
		reservation.LeaseUntil = leaseUntil
		reservation.LastHeartbeatAt = now
		reservation.UpdatedAt = now
		s.storeReservationLocked(reservation)
	}
	cloned := cloneBatchOperation(op)
	return &cloned, nil
}

func (s *MemoryStore) HeartbeatBatchOperationLease(_ context.Context, operationID, token string, leaseUntil, now time.Time) (*BatchOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	op, err := s.requireBatchLeaseLocked(operationID, token, now)
	if err != nil {
		return nil, err
	}
	if !leaseUntil.After(now) {
		return nil, ErrLeaseUnavailable
	}
	op.LeaseUntil = leaseUntil
	op.LastHeartbeatAt = now
	op.UpdatedAt = now
	s.batchOperations[operationID] = cloneBatchOperation(op)
	for _, input := range s.batchInputs[operationID] {
		reservation := s.reservations[input.ReservationID]
		if err := requireLeaseToken(reservation, token, now); err != nil {
			return nil, err
		}
		reservation.LeaseUntil = leaseUntil
		reservation.LastHeartbeatAt = now
		reservation.UpdatedAt = now
		s.storeReservationLocked(reservation)
	}
	cloned := cloneBatchOperation(op)
	return &cloned, nil
}

func (s *MemoryStore) ReleaseBatchOperationLease(_ context.Context, operationID, token string, now time.Time) (*BatchOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	op, err := s.requireBatchLeaseLocked(operationID, token, now)
	if err != nil {
		return nil, err
	}
	clearBatchOperationLease(&op)
	op.UpdatedAt = now
	s.clearBatchInputLeasesLocked(operationID, now)
	s.batchOperations[operationID] = cloneBatchOperation(op)
	cloned := cloneBatchOperation(op)
	return &cloned, nil
}

func (s *MemoryStore) CompareAndSetBatchOperationStatus(_ context.Context, operationID, leaseToken string, from, to OperationStatus, now time.Time) (*BatchOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	op, err := s.requireBatchLeaseLocked(operationID, leaseToken, now)
	if err != nil {
		return nil, err
	}
	if op.Status != from {
		return nil, ErrCompareAndSetFailed
	}
	if to == OperationStatusProofReady || to == OperationStatusSubmitted || to == OperationStatusUnknown {
		return nil, fmt.Errorf("%w: use the durable proof/broadcast transition", ErrInvalidTransition)
	}
	if !canTransitionBatchOperation(from, to) {
		return nil, ErrInvalidTransition
	}
	if err := s.transitionBatchInputReservationsLocked(operationID, from, to, leaseToken, now); err != nil {
		return nil, err
	}
	op.Status = to
	op.UpdatedAt = now
	if IsTerminalOperationStatus(to) || to == OperationStatusPlanned || to == OperationStatusReplanRequired || to == OperationStatusManualReview {
		clearBatchOperationLease(&op)
		s.clearBatchInputLeasesLocked(operationID, now)
	}
	s.batchOperations[operationID] = cloneBatchOperation(op)
	cloned := cloneBatchOperation(op)
	return &cloned, nil
}

func (s *MemoryStore) clearBatchInputLeasesLocked(operationID string, now time.Time) {
	for _, input := range s.batchInputs[operationID] {
		reservation, ok := s.reservations[input.ReservationID]
		if !ok {
			continue
		}
		clearReservationLeaseFields(&reservation)
		reservation.UpdatedAt = now
		s.storeReservationLocked(reservation)
	}
}

func (s *MemoryStore) SaveBatchProofArtifacts(_ context.Context, operationID, leaseToken string, update BatchProofArtifactUpdate, now time.Time) (*BatchOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	op, err := s.requireBatchLeaseLocked(operationID, leaseToken, now)
	if err != nil {
		return nil, err
	}
	if op.Status != OperationStatusProving {
		return nil, ErrCompareAndSetFailed
	}
	if len(update.ProofCiphertext) == 0 || strings.TrimSpace(update.ProofHash) == "" {
		return nil, fmt.Errorf("%w: proof artifact is required", ErrInvalidReservation)
	}
	if len(update.PreparedPayloadCiphertext) != 0 || strings.TrimSpace(update.PreparedPayloadHash) != "" {
		if len(update.PreparedPayloadCiphertext) == 0 || !equalNormalized(op.PreparedPayloadHash, update.PreparedPayloadHash) {
			return nil, fmt.Errorf("%w: prepared payload artifact does not match the reserved operation", ErrInvalidReservation)
		}
		op.PreparedPayloadCiphertext = append([]byte(nil), update.PreparedPayloadCiphertext...)
	}
	op.ProofCiphertext = append([]byte(nil), update.ProofCiphertext...)
	op.ProofHash = update.ProofHash
	op.Status = OperationStatusProofReady
	op.UpdatedAt = now
	if err := s.transitionBatchInputReservationsExactLocked(operationID, StatusProving, StatusProofReady, leaseToken, now); err != nil {
		return nil, err
	}
	clearBatchOperationLease(&op)
	s.clearBatchInputLeasesLocked(operationID, now)
	s.batchOperations[operationID] = cloneBatchOperation(op)
	cloned := cloneBatchOperation(op)
	return &cloned, nil
}

func (s *MemoryStore) SaveBatchSignedTx(_ context.Context, operationID, leaseToken string, update BatchSignedTxUpdate, now time.Time) (*BatchOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	op, err := s.requireBatchLeaseLocked(operationID, leaseToken, now)
	if err != nil {
		return nil, err
	}
	if op.Status != OperationStatusProofReady && op.Status != OperationStatusSubmitted && op.Status != OperationStatusUnknown {
		return nil, ErrCompareAndSetFailed
	}
	if len(update.SignedTxBytesCiphertext) == 0 || strings.TrimSpace(update.TxBytesHash) == "" || strings.TrimSpace(update.TxHash) == "" {
		return nil, fmt.Errorf("%w: signed tx bytes, bytes hash, and tx hash are required", ErrInvalidReservation)
	}
	if op.TxBytesHash != "" && !equalNormalized(op.TxBytesHash, update.TxBytesHash) && op.Status != OperationStatusUnknown {
		return nil, fmt.Errorf("%w: new signed bytes require an Unknown operation after explicit chain reconciliation", ErrInvalidReservation)
	}
	op.SignedTxBytesCiphertext = append([]byte(nil), update.SignedTxBytesCiphertext...)
	op.TxBytesHash = update.TxBytesHash
	op.SignDocHash = update.SignDocHash
	op.TxHash = update.TxHash
	op.AccountSequence = update.AccountSequence
	op.UpdatedAt = now
	s.batchOperations[operationID] = cloneBatchOperation(op)
	cloned := cloneBatchOperation(op)
	return &cloned, nil
}

func (s *MemoryStore) RecordBatchBroadcast(_ context.Context, operationID, leaseToken string, update BatchBroadcastUpdate, now time.Time) (*BatchOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	op, err := s.requireBatchLeaseLocked(operationID, leaseToken, now)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(update.TxBytesHash) == "" || len(op.SignedTxBytesCiphertext) == 0 || !equalNormalized(op.TxBytesHash, update.TxBytesHash) || !equalNormalized(op.TxHash, update.TxHash) {
		return nil, fmt.Errorf("%w: broadcast must reference the durably staged signed tx", ErrInvalidReservation)
	}
	previousStatus := op.Status
	if op.TxBytesHash != "" {
		sameSignedBytes := equalNormalized(op.TxBytesHash, update.TxBytesHash)
		if !sameSignedBytes && op.Status != OperationStatusUnknown {
			return nil, fmt.Errorf("%w: new signed bytes require an Unknown operation after explicit chain reconciliation", ErrInvalidReservation)
		}
		if !sameSignedBytes {
			op.SignedTxBytesCiphertext = append([]byte(nil), update.SignedTxBytesCiphertext...)
			op.TxBytesHash = update.TxBytesHash
			op.SignDocHash = update.SignDocHash
			op.TxHash = update.TxHash
			op.AccountSequence = update.AccountSequence
		} else if op.TxHash == "" {
			op.TxHash = update.TxHash
		}
		if update.Unknown || previousStatus == OperationStatusUnknown {
			op.Status = OperationStatusUnknown
		} else {
			op.Status = OperationStatusSubmitted
		}
	} else {
		if op.Status != OperationStatusProofReady {
			return nil, ErrCompareAndSetFailed
		}
		op.SignedTxBytesCiphertext = append([]byte(nil), update.SignedTxBytesCiphertext...)
		op.TxBytesHash = update.TxBytesHash
		op.SignDocHash = update.SignDocHash
		op.TxHash = update.TxHash
		op.AccountSequence = update.AccountSequence
		if update.Unknown {
			op.Status = OperationStatusUnknown
		} else {
			op.Status = OperationStatusSubmitted
		}
		reservationTarget := StatusSubmitted
		if update.Unknown {
			reservationTarget = StatusUnknown
		}
		if err := s.transitionBatchInputReservationsExactLocked(operationID, StatusProofReady, reservationTarget, leaseToken, now); err != nil {
			return nil, err
		}
	}
	op.BroadcastAttemptCount++
	op.LastBroadcastAt = now
	op.LastBroadcastError = update.LastBroadcastError
	op.BroadcastHistory = append(op.BroadcastHistory, BatchBroadcastAttempt{
		SignedTxBytesCiphertext: append([]byte(nil), op.SignedTxBytesCiphertext...), TxBytesHash: update.TxBytesHash,
		SignDocHash: update.SignDocHash, TxHash: update.TxHash, AccountSequence: update.AccountSequence,
		BroadcastAt: now, BroadcastError: update.LastBroadcastError, Unknown: update.Unknown,
	})
	if previousStatus == OperationStatusProofReady && op.Status == OperationStatusSubmitted {
		if err := s.transitionBatchInputReservationsExactLocked(operationID, StatusProofReady, StatusSubmitted, leaseToken, now); err != nil {
			return nil, err
		}
	} else if previousStatus == OperationStatusProofReady && op.Status == OperationStatusUnknown {
		if err := s.transitionBatchInputReservationsExactLocked(operationID, StatusProofReady, StatusUnknown, leaseToken, now); err != nil {
			return nil, err
		}
	} else if previousStatus == OperationStatusSubmitted && op.Status == OperationStatusUnknown {
		if err := s.transitionBatchInputReservationsExactLocked(operationID, StatusSubmitted, StatusUnknown, leaseToken, now); err != nil {
			return nil, err
		}
	}
	clearBatchOperationLease(&op)
	s.clearBatchInputLeasesLocked(operationID, now)
	op.UpdatedAt = now
	s.batchOperations[operationID] = cloneBatchOperation(op)
	cloned := cloneBatchOperation(op)
	return &cloned, nil
}

func (s *MemoryStore) ReconcileBatchOperation(_ context.Context, operationID string, update BatchReconcileUpdate, now time.Time) (*BatchOperationGraph, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureMapsLocked()
	op, ok := s.batchOperations[operationID]
	if !ok {
		return nil, ErrOperationNotFound
	}
	if op.TxHash != "" && update.TxHash != "" && !equalNormalized(op.TxHash, update.TxHash) {
		return nil, fmt.Errorf("%w: reconcile tx hash does not match stored broadcast", ErrInvalidReservation)
	}
	if update.TxSucceeded && update.TxFailed {
		return nil, fmt.Errorf("%w: tx cannot be both succeeded and failed", ErrInvalidReservation)
	}

	inputs := s.batchInputs[operationID]
	inputIDs := make(map[string]struct{}, len(inputs))
	for _, input := range inputs {
		inputIDs[input.ReservationID] = struct{}{}
	}
	spentIDs := make(map[string]struct{}, len(update.SpentReservationIDs))
	for _, reservationID := range update.SpentReservationIDs {
		if _, ok := inputIDs[reservationID]; !ok {
			return nil, fmt.Errorf("%w: reconcile references a reservation outside the batch", ErrInvalidReservation)
		}
		if _, duplicate := spentIDs[reservationID]; duplicate {
			return nil, fmt.Errorf("%w: duplicate spent reservation evidence", ErrInvalidReservation)
		}
		spentIDs[reservationID] = struct{}{}
	}

	observedByIndex := make(map[int]ObservedOutputEvidence, len(update.ObservedOutputs))
	for _, observed := range update.ObservedOutputs {
		if observed.OutputIndex < 0 || observed.OutputIndex >= op.OutputCount {
			return nil, fmt.Errorf("%w: observed output index is outside the batch", ErrInvalidReservation)
		}
		if _, duplicate := observedByIndex[observed.OutputIndex]; duplicate {
			return nil, fmt.Errorf("%w: duplicate observed output evidence", ErrInvalidReservation)
		}
		observedByIndex[observed.OutputIndex] = observed
	}

	allInputsSpent := len(spentIDs) == len(inputs)
	for _, input := range inputs {
		reservation := s.reservations[input.ReservationID]
		if _, spent := spentIDs[input.ReservationID]; spent {
			reservation.Status = StatusConfirmedSpent
			clearLeaseForStatusTransition(&reservation, StatusConfirmedSpent)
			reservation.UpdatedAt = now
			s.storeReservationLocked(reservation)
		} else if update.TxFailed && IsActiveReservationStatus(reservation.Status) {
			reservation.Status = StatusReplanRequired
			clearLeaseForStatusTransition(&reservation, StatusReplanRequired)
			reservation.UpdatedAt = now
			s.storeReservationLocked(reservation)
		}
	}

	evidence := cloneBatchEvidence(s.batchEvidence[operationID])
	evidenceByIndex := make(map[int]int, len(evidence))
	for i := range evidence {
		evidenceByIndex[evidence[i].OutputIndex] = i
	}
	items := cloneBatchItems(s.batchItems[operationID])
	for i := range items {
		item := &items[i]
		expectedIndex := evidenceByIndex[item.OutputIndex]
		expected := &evidence[expectedIndex]
		observed, found := observedByIndex[item.OutputIndex]
		previousReviewReason := item.ManualReviewReason
		item.ManualReviewReason = ""
		switch {
		case !update.TxSucceeded:
			if update.TxFailed {
				item.EvidenceStatus = BatchItemEvidenceFailed
			} else if len(spentIDs) > 0 {
				item.EvidenceStatus = BatchItemEvidenceManualReview
				item.ManualReviewReason = "input nullifier spent without confirmed batch tx evidence"
			}
		case !found && (item.EvidenceStatus == BatchItemEvidenceSucceeded || item.EvidenceStatus == BatchItemEvidenceManualReview):
			// A repeated chain-only reconcile must be idempotent. Previously
			// verified output evidence remains authoritative until new,
			// contradictory output evidence is supplied.
			item.ManualReviewReason = previousReviewReason
		case !found:
			item.EvidenceStatus = BatchItemEvidenceManualReview
			item.ManualReviewReason = "expected output evidence is missing"
		case !observedOutputMatches(*expected, observed):
			item.EvidenceStatus = BatchItemEvidenceManualReview
			item.ManualReviewReason = "observed output evidence does not match the expected commitment/disclosure"
		case observed.AuditDeliveryFailed || observed.SelfViewDeliveryFailed:
			item.EvidenceStatus = BatchItemEvidenceManualReview
			item.ManualReviewReason = "disclosure delivery requires manual review"
		default:
			item.EvidenceStatus = BatchItemEvidenceSucceeded
		}
		item.UpdatedAt = now
		if found {
			expected.ObservedCommitment = observed.Commitment
			expected.ObservedUserDigest = observed.UserDisclosureDigest
			expected.ObservedFullDigest = observed.FullDisclosureDigest
			expected.ObservedRecipientHash = observed.RecipientHash
			expected.AuditDeliveryFailed = observed.AuditDeliveryFailed
			expected.SelfViewDeliveryFailed = observed.SelfViewDeliveryFailed
			expected.UpdatedAt = now
		}
	}

	if update.TxHash != "" {
		op.TxHash = update.TxHash
	}
	switch {
	case update.TxSucceeded && allInputsSpent:
		op.Status = OperationStatusSucceeded
	case update.TxFailed && len(spentIDs) == 0:
		op.Status = OperationStatusFailed
	default:
		op.Status = OperationStatusManualReview
	}
	op.LastBroadcastError = update.FailureReason
	op.UpdatedAt = now
	clearBatchOperationLease(&op)
	s.batchOperations[operationID] = cloneBatchOperation(op)
	s.batchItems[operationID] = cloneBatchItems(items)
	s.batchEvidence[operationID] = cloneBatchEvidence(evidence)
	graph := s.batchGraphLocked(operationID)
	return &graph, nil
}

func (s *MemoryStore) requireBatchLeaseLocked(operationID, token string, now time.Time) (BatchOperation, error) {
	s.ensureMapsLocked()
	op, ok := s.batchOperations[operationID]
	if !ok {
		return BatchOperation{}, ErrOperationNotFound
	}
	if strings.TrimSpace(token) == "" || op.LeaseToken != token || !op.LeaseUntil.After(now) {
		return BatchOperation{}, ErrLeaseMismatch
	}
	for _, input := range s.batchInputs[operationID] {
		reservation, ok := s.reservations[input.ReservationID]
		if !ok {
			return BatchOperation{}, ErrReservationNotFound
		}
		if err := requireLeaseToken(reservation, token, now); err != nil {
			return BatchOperation{}, err
		}
	}
	return op, nil
}

func (s *MemoryStore) batchInputReservationsLocked(operationID string) ([]NoteReservation, error) {
	inputs := s.batchInputs[operationID]
	out := make([]NoteReservation, 0, len(inputs))
	for _, input := range inputs {
		reservation, ok := s.reservations[input.ReservationID]
		if !ok {
			return nil, ErrReservationNotFound
		}
		out = append(out, reservation)
	}
	return out, nil
}

func (s *MemoryStore) transitionBatchInputReservationsLocked(operationID string, from, to OperationStatus, leaseToken string, now time.Time) error {
	var reservationFrom, reservationTo ReservationStatus
	switch {
	case from == OperationStatusPlanned && to == OperationStatusProving:
		reservationFrom, reservationTo = StatusReserved, StatusProving
	case from == OperationStatusProving && to == OperationStatusPlanned:
		reservationFrom, reservationTo = StatusProving, StatusReserved
	case from == OperationStatusProving && to == OperationStatusProofReady:
		reservationFrom, reservationTo = StatusProving, StatusProofReady
	case from == OperationStatusProofReady && to == OperationStatusSubmitted:
		reservationFrom, reservationTo = StatusProofReady, StatusSubmitted
	case from == OperationStatusProofReady && to == OperationStatusUnknown:
		reservationFrom, reservationTo = StatusProofReady, StatusUnknown
	default:
		return nil
	}
	return s.transitionBatchInputReservationsExactLocked(operationID, reservationFrom, reservationTo, leaseToken, now)
}

func (s *MemoryStore) transitionBatchInputReservationsExactLocked(operationID string, from, to ReservationStatus, leaseToken string, now time.Time) error {
	reservations, err := s.batchInputReservationsLocked(operationID)
	if err != nil {
		return err
	}
	for _, reservation := range reservations {
		if reservation.Status != from {
			return ErrCompareAndSetFailed
		}
		if err := requireLeaseToken(reservation, leaseToken, now); err != nil {
			return err
		}
		if !CanTransitionReservation(from, to) {
			return ErrInvalidTransition
		}
	}
	for _, reservation := range reservations {
		reservation.Status = to
		// Batch transitions keep the complete shared lease through
		// ProofReady/Submitted so the operation and every input can be
		// heartbeated/retried together.
		if IsTerminalReservationStatus(to) {
			clearReservationLeaseFields(&reservation)
		}
		reservation.UpdatedAt = now
		s.storeReservationLocked(reservation)
	}
	return nil
}

func (s *MemoryStore) batchGraphLocked(operationID string) BatchOperationGraph {
	return BatchOperationGraph{
		Operation: cloneBatchOperation(s.batchOperations[operationID]),
		Inputs:    cloneBatchInputs(s.batchInputs[operationID]),
		Items:     cloneBatchItems(s.batchItems[operationID]),
		Evidence:  cloneBatchEvidence(s.batchEvidence[operationID]),
	}
}

func (s *MemoryStore) validatePersistedBatchGraphsLocked() error {
	for operationID, op := range s.batchOperations {
		if op.SchemaVersion != BatchOperationSchemaVersionV1 || op.OperationID != operationID || op.InputCount < 1 || op.InputCount > 16 || op.OutputCount < 1 || op.OutputCount > 32 || len(op.PreparedPayloadCiphertext) == 0 || strings.TrimSpace(op.PreparedPayloadHash) == "" {
			return fmt.Errorf("%w: invalid persisted batch operation %s", ErrInvalidReservation, operationID)
		}
		inputs := s.batchInputs[operationID]
		items := s.batchItems[operationID]
		evidence := s.batchEvidence[operationID]
		if len(inputs) != op.InputCount || len(items) != op.OutputCount || len(evidence) != op.OutputCount {
			return fmt.Errorf("%w: persisted batch operation %s has incomplete relations", ErrInvalidReservation, operationID)
		}
		seenInputs := make(map[int]struct{}, len(inputs))
		for _, input := range inputs {
			reservation, ok := s.reservations[input.ReservationID]
			if !ok || reservation.OperationID != operationID || input.SchemaVersion != BatchOperationSchemaVersionV1 || input.OperationID != operationID || input.InputIndex < 0 || input.InputIndex >= op.InputCount || !isCanonicalBatchFieldHex(input.Commitment) {
				return fmt.Errorf("%w: invalid persisted input relation for %s", ErrInvalidReservation, operationID)
			}
			if _, duplicate := seenInputs[input.InputIndex]; duplicate {
				return fmt.Errorf("%w: duplicate persisted input index for %s", ErrInvalidReservation, operationID)
			}
			seenInputs[input.InputIndex] = struct{}{}
		}
		seenItems := make(map[int]BatchOutputRole, len(items))
		for _, item := range items {
			if item.SchemaVersion != BatchOperationSchemaVersionV1 || item.OperationID != operationID || item.OutputIndex < 0 || item.OutputIndex >= op.OutputCount || !validBatchOutputRole(item.Role) {
				return fmt.Errorf("%w: invalid persisted item relation for %s", ErrInvalidReservation, operationID)
			}
			if _, duplicate := seenItems[item.OutputIndex]; duplicate {
				return fmt.Errorf("%w: duplicate persisted output index for %s", ErrInvalidReservation, operationID)
			}
			seenItems[item.OutputIndex] = item.Role
		}
		seenEvidence := make(map[int]struct{}, len(evidence))
		for _, expected := range evidence {
			role, itemExists := seenItems[expected.OutputIndex]
			if !itemExists || expected.SchemaVersion != BatchOperationSchemaVersionV1 || expected.OperationID != operationID || expected.Role != role || strings.TrimSpace(expected.Commitment) == "" || strings.TrimSpace(expected.FullDisclosureDigest) == "" {
				return fmt.Errorf("%w: invalid persisted evidence relation for %s", ErrInvalidReservation, operationID)
			}
			if err := validateBatchUserEvidence(expected); err != nil {
				return err
			}
			if _, duplicate := seenEvidence[expected.OutputIndex]; duplicate {
				return fmt.Errorf("%w: duplicate persisted evidence index for %s", ErrInvalidReservation, operationID)
			}
			seenEvidence[expected.OutputIndex] = struct{}{}
		}
	}
	for operationID := range s.batchInputs {
		if _, ok := s.batchOperations[operationID]; !ok {
			return fmt.Errorf("%w: orphan persisted batch input relation", ErrInvalidReservation)
		}
	}
	for operationID := range s.batchItems {
		if _, ok := s.batchOperations[operationID]; !ok {
			return fmt.Errorf("%w: orphan persisted batch item relation", ErrInvalidReservation)
		}
	}
	for operationID := range s.batchEvidence {
		if _, ok := s.batchOperations[operationID]; !ok {
			return fmt.Errorf("%w: orphan persisted batch evidence relation", ErrInvalidReservation)
		}
	}
	return nil
}

func canTransitionBatchOperation(from, to OperationStatus) bool {
	switch from {
	case OperationStatusPlanned:
		return to == OperationStatusProving || to == OperationStatusReplanRequired || to == OperationStatusManualReview
	case OperationStatusProving:
		return to == OperationStatusPlanned || to == OperationStatusProofReady || to == OperationStatusReplanRequired || to == OperationStatusManualReview
	case OperationStatusProofReady:
		return to == OperationStatusSubmitted || to == OperationStatusUnknown || to == OperationStatusManualReview
	case OperationStatusSubmitted:
		return to == OperationStatusSucceeded || to == OperationStatusFailed || to == OperationStatusUnknown || to == OperationStatusManualReview
	case OperationStatusUnknown:
		return to == OperationStatusSubmitted || to == OperationStatusSucceeded || to == OperationStatusFailed || to == OperationStatusManualReview
	case OperationStatusManualReview:
		return to == OperationStatusSucceeded || to == OperationStatusFailed || to == OperationStatusReplanRequired
	default:
		return false
	}
}

func validBatchOutputRole(role BatchOutputRole) bool {
	return role == BatchOutputRolePayment || role == BatchOutputRoleChange || role == BatchOutputRolePadding
}

func validateBatchUserEvidence(evidence ExpectedOutputEvidence) error {
	if evidence.UserPrivacyPolicy > privacytypes.TransferPrivacyPolicyDiscloseAmountToFrom {
		return fmt.Errorf("%w: unsupported expected user privacy policy", ErrInvalidReservation)
	}
	if evidence.UserPrivacyPolicy == 0 {
		if evidence.UserDisclosureMode != privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_NONE || strings.TrimSpace(evidence.UserDisclosureDigest) != "" {
			return fmt.Errorf("%w: all-private output evidence must use NONE and an empty user digest", ErrInvalidReservation)
		}
		return nil
	}
	if (evidence.UserDisclosureMode != privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_PUBLIC && evidence.UserDisclosureMode != privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_RECIPIENT_ENCRYPTED) || strings.TrimSpace(evidence.UserDisclosureDigest) == "" {
		return fmt.Errorf("%w: disclosed output evidence requires a mode and user digest", ErrInvalidReservation)
	}
	return nil
}

func isCanonicalBatchFieldHex(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && privacyfield.ValidateCanonicalBytes32(decoded) == nil
}

func observedOutputMatches(expected ExpectedOutputEvidence, observed ObservedOutputEvidence) bool {
	if !equalNormalized(expected.Commitment, observed.Commitment) || !equalNormalized(expected.FullDisclosureDigest, observed.FullDisclosureDigest) {
		return false
	}
	if !equalNormalized(expected.UserDisclosureDigest, observed.UserDisclosureDigest) {
		return false
	}
	return expected.RecipientHash == "" || equalNormalized(expected.RecipientHash, observed.RecipientHash)
}

func equalNormalized(left, right string) bool {
	return strings.EqualFold(strings.TrimSpace(strings.TrimPrefix(left, "0x")), strings.TrimSpace(strings.TrimPrefix(right, "0x")))
}

func clearBatchOperationLease(op *BatchOperation) {
	op.LeaseOwner = ""
	op.LeaseToken = ""
	op.LeaseUntil = time.Time{}
}

func cloneBatchOperation(op BatchOperation) BatchOperation {
	op.PreparedPayloadCiphertext = append([]byte(nil), op.PreparedPayloadCiphertext...)
	op.ProofCiphertext = append([]byte(nil), op.ProofCiphertext...)
	op.SignedTxBytesCiphertext = append([]byte(nil), op.SignedTxBytesCiphertext...)
	op.BroadcastHistory = append([]BatchBroadcastAttempt(nil), op.BroadcastHistory...)
	for i := range op.BroadcastHistory {
		op.BroadcastHistory[i].SignedTxBytesCiphertext = append([]byte(nil), op.BroadcastHistory[i].SignedTxBytesCiphertext...)
	}
	return op
}

func cloneBatchInputs(values []OperationInputReservation) []OperationInputReservation {
	out := append([]OperationInputReservation(nil), values...)
	for i := range out {
		out[i].EncryptedAmount = append([]byte(nil), out[i].EncryptedAmount...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].InputIndex < out[j].InputIndex })
	return out
}

func cloneBatchItems(values []PayrollItemOutput) []PayrollItemOutput {
	out := append([]PayrollItemOutput(nil), values...)
	sort.Slice(out, func(i, j int) bool { return out[i].OutputIndex < out[j].OutputIndex })
	return out
}

func cloneBatchEvidence(values []ExpectedOutputEvidence) []ExpectedOutputEvidence {
	out := make([]ExpectedOutputEvidence, len(values))
	for i := range values {
		out[i] = values[i]
		out[i].EncryptedRecipient = append([]byte(nil), values[i].EncryptedRecipient...)
		out[i].EncryptedAmount = append([]byte(nil), values[i].EncryptedAmount...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].OutputIndex < out[j].OutputIndex })
	return out
}
