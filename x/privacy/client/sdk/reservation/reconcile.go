package reservation

import (
	"context"
	"fmt"
	"strings"
)

type OperationEvidence struct {
	TxHash                   string
	SignDocHash              string
	TxBytesHash              string
	OutputCommitment         string
	DisclosureDigest         string
	UserDisclosureDigest     string
	AuditDisclosureDigest    string
	SelfViewDisclosureDigest string
	RecipientHash            string
	AmountHash               string
	Denom                    string
	BatchItemIndex           int
	BatchItemIndexKnown      bool
	NullifierSpent           bool
	TxSucceeded              bool
	TxFailed                 bool
	TxKnown                  bool
	// NullifierUnspentConfirmed is true only after a successful lookup has
	// explicitly confirmed that the nullifier is not spent. A false value is
	// unknown and must never justify reusing a failed transaction's note.
	NullifierUnspentConfirmed bool
}

// OperationReservationEvidence binds chain evidence to one input reservation
// of a multi-input operation. A successful operation reconciliation must
// provide an entry for every linked reservation.
type OperationReservationEvidence struct {
	ReservationID string
	Evidence      OperationEvidence
}

type ReconcileResult struct {
	ReservationStatus ReservationStatus
	OperationStatus   OperationStatus
	RequiresReview    bool
	Reason            string
}

func (s Service) Reconcile(ctx context.Context, reservationID string, evidence OperationEvidence) (*ReconcileResult, error) {
	if s.Store == nil {
		return nil, fmt.Errorf("reservation store is required")
	}
	reservation, err := s.Store.GetReservation(ctx, reservationID)
	if err != nil {
		return nil, err
	}
	var operation *PayrollOperation
	multiInputOperation := false
	if reservation.OperationID != "" {
		operation, err = s.Store.GetOperation(ctx, reservation.OperationID)
		if err != nil {
			return nil, err
		}
		reservations, listErr := s.Store.ListReservations(ctx, ReservationFilter{})
		if listErr != nil {
			return nil, listErr
		}
		for _, linked := range reservations {
			if linked.OperationID == reservation.OperationID && linked.ReservationID != reservation.ReservationID {
				multiInputOperation = true
				break
			}
		}
	}
	if IsTerminalReservationStatus(reservation.Status) {
		if multiInputOperation && !IsTerminalOperationStatus(operation.Status) {
			return unresolvedReconcileResult(reservation, operation, "multi-input operation requires ReconcileOperation evidence for every reservation"), nil
		}
		operationStatus := currentOperationStatus(operation)
		if reservation.Status == StatusFailed && evidence.NullifierSpent {
			updated, updatedOperationStatus, reconcileErr := s.quarantineSpentReservation(ctx, reservation, operation, OperationStatusConflictSpent, "terminal failed reservation conflicts with spent nullifier evidence")
			if reconcileErr != nil {
				return nil, reconcileErr
			}
			return &ReconcileResult{
				ReservationStatus: updated.Status,
				OperationStatus:   updatedOperationStatus,
				RequiresReview:    true,
				Reason:            "terminal failed reservation conflicts with spent nullifier evidence",
			}, nil
		}
		requiresReview, reason := terminalReservationReview(reservation.Status, operation, evidence)
		if requiresReview && reason != "reservation is already terminal" {
			updated, auditErr := s.recordTerminalReconciliationAudit(ctx, reservation, reason)
			if auditErr != nil {
				return nil, auditErr
			}
			reservation = updated
		}
		return &ReconcileResult{
			ReservationStatus: reservation.Status,
			OperationStatus:   operationStatus,
			RequiresReview:    requiresReview,
			Reason:            reason,
		}, nil
	}
	if evidence.NullifierSpent && evidence.NullifierUnspentConfirmed {
		updated, operationStatus, updateErr := s.markConflictingNullifierEvidenceManualReview(
			ctx,
			reservation,
			operation,
			"conflicting nullifier evidence",
		)
		if updateErr != nil {
			return nil, updateErr
		}
		return &ReconcileResult{
			ReservationStatus: updated.Status,
			OperationStatus:   operationStatus,
			RequiresReview:    true,
			Reason:            "conflicting nullifier evidence",
		}, nil
	}
	if evidence.TxSucceeded && evidence.TxFailed {
		if evidence.NullifierSpent {
			updated, operationStatus, err := s.quarantineSpentReservation(ctx, reservation, operation, OperationStatusConflictSpent, "conflicting transaction execution evidence")
			if err != nil {
				return nil, err
			}
			return &ReconcileResult{
				ReservationStatus: updated.Status,
				OperationStatus:   operationStatus,
				RequiresReview:    true,
				Reason:            "conflicting transaction execution evidence",
			}, nil
		}
		updated, operationStatus, err := s.markConflictingNullifierEvidenceManualReview(
			ctx,
			reservation,
			operation,
			"conflicting transaction execution evidence",
		)
		if err != nil {
			return nil, err
		}
		return &ReconcileResult{
			ReservationStatus: updated.Status,
			OperationStatus:   operationStatus,
			RequiresReview:    true,
			Reason:            "conflicting transaction execution evidence",
		}, nil
	}
	if evidence.TxFailed && evidence.NullifierSpent {
		updated, operationStatus, err := s.quarantineSpentReservation(ctx, reservation, operation, OperationStatusConflictSpent, "failed transaction conflicts with spent nullifier evidence")
		if err != nil {
			return nil, err
		}
		return &ReconcileResult{
			ReservationStatus: updated.Status,
			OperationStatus:   operationStatus,
			RequiresReview:    true,
			Reason:            "failed transaction conflicts with spent nullifier evidence",
		}, nil
	}
	if evidence.TxSucceeded && evidence.NullifierUnspentConfirmed {
		if multiInputOperation && !IsTerminalOperationStatus(operation.Status) {
			return unresolvedReconcileResult(reservation, operation, "multi-input operation requires ReconcileOperation evidence for every reservation"), nil
		}
		if !canReconcileToManualReview(reservation.Status) {
			return unresolvedReconcileResult(reservation, operation, "successful transaction conflicts with confirmed unspent nullifier"), nil
		}
		updated, operationStatus, err := s.reconcileReservationOperation(ctx, reservation, operation, StatusManualReview, OperationStatusManualReview)
		if err != nil {
			return nil, err
		}
		return &ReconcileResult{
			ReservationStatus: updated.Status,
			OperationStatus:   operationStatus,
			RequiresReview:    true,
			Reason:            "successful transaction conflicts with confirmed unspent nullifier",
		}, nil
	}
	txKnown := evidence.TxKnown || evidence.TxSucceeded || evidence.TxFailed
	if evidence.TxKnown && !evidence.TxSucceeded && !evidence.TxFailed && !evidence.NullifierSpent {
		return s.reconcileKnownPendingTransaction(ctx, reservation, operation, evidence)
	}
	if !txKnown && !evidence.NullifierSpent {
		if reservation.Status == StatusManualReview {
			return &ReconcileResult{
				ReservationStatus: reservation.Status,
				OperationStatus:   currentOperationStatus(operation),
				RequiresReview:    true,
				Reason:            "reservation requires manual review",
			}, nil
		}
		if reservation.Status == StatusUnknown {
			return reconciledResult(StatusUnknown, currentOperationStatus(operation), OperationStatusUnknown, "tx and nullifier state are unknown"), nil
		}
		if multiInputOperation && !IsTerminalOperationStatus(operation.Status) {
			return unresolvedReconcileResult(reservation, operation, "multi-input operation requires ReconcileOperation evidence for every reservation"), nil
		}
		if !canReconcileToUnknown(reservation.Status) {
			return unresolvedReconcileResult(reservation, operation, "tx and nullifier state are unknown"), nil
		}
		updated, operationStatus, err := s.reconcileReservationOperation(ctx, reservation, operation, StatusUnknown, OperationStatusUnknown)
		if err != nil {
			return nil, err
		}
		return reconciledResult(updated.Status, operationStatus, OperationStatusUnknown, "tx and nullifier state are unknown"), nil
	}

	if evidence.TxFailed && !evidence.NullifierSpent {
		if multiInputOperation && !IsTerminalOperationStatus(operation.Status) {
			return unresolvedReconcileResult(reservation, operation, "multi-input operation requires ReconcileOperation evidence for every reservation"), nil
		}
		if !evidence.NullifierUnspentConfirmed {
			return unresolvedReconcileResult(reservation, operation, "tx failed but nullifier unspent state is unverified"), nil
		}
		if !canReconcileFailedTransaction(reservation.Status) {
			return unresolvedReconcileResult(reservation, operation, "failed tx cannot reconcile this reservation state"), nil
		}
		if !txEvidenceMatchesOperation(operation, evidence) {
			updated, operationStatus, err := s.reconcileReservationOperation(ctx, reservation, operation, StatusManualReview, OperationStatusManualReview)
			if err != nil {
				return nil, err
			}
			return &ReconcileResult{
				ReservationStatus: updated.Status,
				OperationStatus:   operationStatus,
				RequiresReview:    true,
				Reason:            "failed tx evidence does not match operation",
			}, nil
		}
		reservationStatus, operationStatusTarget := failedTxReconcileTarget(operation)
		if reservation.Status == reservationStatus {
			return reconciledResult(reservation.Status, currentOperationStatus(operation), operationStatusTarget, "tx failed and nullifier is unspent"), nil
		}
		updated, operationStatus, err := s.reconcileReservationOperation(ctx, reservation, operation, reservationStatus, operationStatusTarget)
		if err != nil {
			return nil, err
		}
		return reconciledResult(updated.Status, operationStatus, operationStatusTarget, "tx failed and nullifier is unspent"), nil
	}

	if evidence.TxSucceeded || evidence.NullifierSpent {
		if !operationMatchesEvidence(operation, evidence) {
			updated, operationStatus, updateErr := s.quarantineSpentReservation(ctx, reservation, operation, OperationStatusConflictSpent, "nullifier spent but evidence does not match operation")
			if updateErr != nil {
				return nil, updateErr
			}
			return &ReconcileResult{
				ReservationStatus: updated.Status,
				OperationStatus:   operationStatus,
				RequiresReview:    true,
				Reason:            "nullifier spent but evidence does not match operation",
			}, nil
		}
		if operation != nil && !IsTerminalOperationStatus(operation.Status) {
			reservations, listErr := s.Store.ListReservations(ctx, ReservationFilter{})
			if listErr != nil {
				return nil, listErr
			}
			for _, linked := range reservations {
				if linked.OperationID == operation.OperationID && linked.ReservationID != reservation.ReservationID {
					return unresolvedReconcileResult(reservation, operation, "multi-input operation requires ReconcileOperation evidence for every reservation"), nil
				}
			}
		}

		updated, operationStatus, updateErr := s.quarantineSpentReservation(ctx, reservation, operation, OperationStatusSucceeded, "operation evidence matched")
		if updateErr != nil {
			return nil, updateErr
		}
		return reconciledResult(updated.Status, operationStatus, OperationStatusSucceeded, "operation evidence matched"), nil
	}

	return &ReconcileResult{ReservationStatus: reservation.Status, OperationStatus: OperationStatusUnknown, RequiresReview: true, Reason: "unhandled reconcile evidence"}, ErrManualReviewRequired
}

// ReconcileOperation atomically closes a multi-input operation only after
// every linked reservation has independently supplied matching success or
// failure evidence. Callers must use this instead of Reconcile when an
// operation has more than one input note.
func (s Service) ReconcileOperation(ctx context.Context, operationID string, evidences []OperationReservationEvidence) (*ReconcileResult, error) {
	if s.Store == nil {
		return nil, fmt.Errorf("reservation store is required")
	}
	if strings.TrimSpace(operationID) == "" {
		return nil, fmt.Errorf("%w: operation_id is required", ErrInvalidReservation)
	}
	operation, err := s.Store.GetOperation(ctx, operationID)
	if err != nil {
		return nil, err
	}
	reservations, err := s.Store.ListReservations(ctx, ReservationFilter{})
	if err != nil {
		return nil, err
	}
	linked := make([]NoteReservation, 0, len(reservations))
	for _, reservation := range reservations {
		if reservation.OperationID == operationID {
			linked = append(linked, reservation)
		}
	}
	if len(linked) == 0 {
		return nil, fmt.Errorf("%w: operation %s has no linked reservations", ErrInvalidReservation, operationID)
	}
	provided := make(map[string]OperationEvidence, len(evidences))
	for _, item := range evidences {
		if item.ReservationID == "" {
			return nil, fmt.Errorf("%w: reservation_id is required", ErrInvalidReservation)
		}
		if _, exists := provided[item.ReservationID]; exists {
			return nil, fmt.Errorf("%w: duplicate reconciliation evidence for reservation %s", ErrInvalidReservation, item.ReservationID)
		}
		provided[item.ReservationID] = item.Evidence
	}
	if len(provided) != len(linked) {
		return nil, fmt.Errorf("%w: reconciliation evidence must cover every operation reservation", ErrManualReviewRequired)
	}
	if result, handled, pendingErr := s.reconcileKnownPendingOperation(ctx, operation, linked, provided); handled {
		return result, pendingErr
	}
	if IsTerminalOperationStatus(operation.Status) {
		switch operation.Status {
		case OperationStatusSucceeded:
			for _, reservation := range linked {
				evidence, ok := provided[reservation.ReservationID]
				if !ok || reservation.Status != StatusConfirmedSpent || !evidence.NullifierSpent || evidence.NullifierUnspentConfirmed || !evidence.TxSucceeded || evidence.TxFailed || !operationMatchesEvidence(operation, evidence) {
					return nil, fmt.Errorf("%w: retry evidence conflicts with succeeded operation %s", ErrManualReviewRequired, operationID)
				}
			}
			return reconciledResult(StatusConfirmedSpent, OperationStatusSucceeded, OperationStatusSucceeded, "operation reconciliation already applied"), nil
		case OperationStatusFailed:
			for _, reservation := range linked {
				evidence, ok := provided[reservation.ReservationID]
				if !ok || reservation.Status != StatusFailed || !failedOperationEvidenceMatches(operation, evidence) {
					return nil, fmt.Errorf("%w: retry evidence conflicts with failed operation %s", ErrManualReviewRequired, operationID)
				}
			}
			return reconciledResult(StatusFailed, OperationStatusFailed, OperationStatusFailed, "failed operation reconciliation already applied"), nil
		default:
			return nil, fmt.Errorf("%w: operation %s is already terminal", ErrManualReviewRequired, operationID)
		}
	}
	if result, handled, failedErr := s.reconcileFailedOperation(ctx, operation, linked, provided); handled {
		return result, failedErr
	}
	linkedIDs := make([]string, 0, len(linked))
	for _, reservation := range linked {
		evidence, ok := provided[reservation.ReservationID]
		if !ok {
			return nil, fmt.Errorf("%w: reconciliation evidence is missing for reservation %s", ErrManualReviewRequired, reservation.ReservationID)
		}
		if !canReconcileSpent(reservation.Status) {
			return nil, fmt.Errorf("%w: reservation %s cannot be reconciled as spent from %s", ErrManualReviewRequired, reservation.ReservationID, reservation.Status)
		}
		if !evidence.NullifierSpent || evidence.NullifierUnspentConfirmed || !evidence.TxSucceeded || evidence.TxFailed || !operationMatchesEvidence(operation, evidence) {
			return nil, fmt.Errorf("%w: reconciliation evidence does not prove successful consumption for reservation %s", ErrManualReviewRequired, reservation.ReservationID)
		}
		linkedIDs = append(linkedIDs, reservation.ReservationID)
	}

	now := s.now()
	updatedOperation := *operation
	updatedOperation.Status = OperationStatusSucceeded
	updatedOperation.UpdatedAt = now
	updated, reconciledOperation, err := s.Store.ApplyReconciliationTransition(ctx, ReconciliationTransition{
		ReservationID:           linked[0].ReservationID,
		From:                    linked[0].Status,
		To:                      StatusConfirmedSpent,
		Operation:               &updatedOperation,
		SiblingOperationStatus:  OperationStatusConflictSpent,
		AuditReason:             "operation evidence matched for every input reservation",
		OperationReservationIDs: linkedIDs,
		Now:                     now,
		serviceAuthorized:       true,
		quarantineMatchingSpent: true,
	})
	if err != nil {
		return nil, err
	}
	status := OperationStatusSucceeded
	if reconciledOperation != nil {
		status = reconciledOperation.Status
	}
	return reconciledResult(updated.Status, status, OperationStatusSucceeded, "operation evidence matched for every input reservation"), nil
}

func (s Service) reconcileFailedOperation(ctx context.Context, operation *PayrollOperation, linked []NoteReservation, provided map[string]OperationEvidence) (*ReconcileResult, bool, error) {
	if operation == nil || len(linked) == 0 {
		return nil, false, nil
	}
	var identity OperationEvidence
	ids := make([]string, 0, len(linked))
	fromStatuses := make(map[string]ReservationStatus, len(linked))
	for _, reservation := range linked {
		evidence, ok := provided[reservation.ReservationID]
		if !ok || !failedOperationEvidenceMatches(operation, evidence) {
			return nil, false, nil
		}
		if !canReconcileFailedTransaction(reservation.Status) {
			return unresolvedReconcileResult(&reservation, operation, "failed tx cannot reconcile this operation state"), true, nil
		}
		if err := mergeOperationEvidenceIdentity(&identity, evidence); err != nil {
			return unresolvedReconcileResult(&reservation, operation, "operation inputs report conflicting failed tx identity"), true, nil
		}
		ids = append(ids, reservation.ReservationID)
		fromStatuses[reservation.ReservationID] = reservation.Status
	}

	now := s.now()
	updatedOperation := cloneOperation(*operation)
	if err := mergeEvidenceIntoOperation(&updatedOperation, identity); err != nil {
		return unresolvedReconcileResult(&linked[0], operation, "failed tx identity conflicts with operation"), true, nil
	}
	updatedOperation.Status = OperationStatusFailed
	updatedOperation.UpdatedAt = now
	updated, reconciledOperation, err := s.Store.ApplyReconciliationTransition(ctx, ReconciliationTransition{
		ReservationID:                    linked[0].ReservationID,
		From:                             linked[0].Status,
		To:                               StatusFailed,
		Operation:                        &updatedOperation,
		TxHash:                           identity.TxHash,
		TxBytesHash:                      identity.TxBytesHash,
		SignDocHash:                      identity.SignDocHash,
		AuditReason:                      "transaction failed and every input nullifier is confirmed unspent",
		OperationReservationIDs:          ids,
		OperationReservationFromStatuses: fromStatuses,
		Now:                              now,
		serviceAuthorized:                true,
	})
	if err != nil {
		return nil, true, err
	}
	operationStatus := OperationStatusFailed
	if reconciledOperation != nil {
		operationStatus = reconciledOperation.Status
	}
	return reconciledResult(updated.Status, operationStatus, OperationStatusFailed, "transaction failed and every input nullifier is confirmed unspent"), true, nil
}

func failedOperationEvidenceMatches(operation *PayrollOperation, evidence OperationEvidence) bool {
	return evidence.TxFailed &&
		!evidence.TxSucceeded &&
		!evidence.NullifierSpent &&
		evidence.NullifierUnspentConfirmed &&
		txEvidenceMatchesOperation(operation, evidence)
}

func (s Service) reconcileKnownPendingTransaction(ctx context.Context, reservation *NoteReservation, operation *PayrollOperation, evidence OperationEvidence) (*ReconcileResult, error) {
	if !hasChainTransactionIdentity(evidence) {
		return unresolvedReconcileResult(reservation, operation, "known pending tx is missing tx hash or tx bytes hash"), nil
	}
	if reservation.Status == StatusProofReady && !reservation.BroadcastInFlight {
		return unresolvedReconcileResult(reservation, operation, "ProofReady reservation has no durable broadcast attempt"), nil
	}
	if reservation.Status != StatusProofReady && reservation.Status != StatusSubmitted && reservation.Status != StatusUnknown {
		return unresolvedReconcileResult(reservation, operation, "known pending tx cannot reconcile this reservation state"), nil
	}
	if operation != nil {
		reservations, err := s.Store.ListReservations(ctx, ReservationFilter{})
		if err != nil {
			return nil, err
		}
		for _, linked := range reservations {
			if linked.OperationID == operation.OperationID && linked.ReservationID != reservation.ReservationID {
				return unresolvedReconcileResult(reservation, operation, "multi-input pending tx requires ReconcileOperation evidence for every reservation"), nil
			}
		}
	}
	now := s.now()
	var operationUpdate *PayrollOperation
	if operation != nil {
		updated := cloneOperation(*operation)
		if err := mergeEvidenceIntoOperation(&updated, evidence); err != nil {
			return unresolvedReconcileResult(reservation, operation, "known pending tx identity conflicts with operation"), nil
		}
		updated.Status = OperationStatusUnknown
		updated.UpdatedAt = now
		operationUpdate = &updated
	}
	updated, reconciledOperation, err := s.Store.ApplyReconciliationTransition(ctx, ReconciliationTransition{
		ReservationID:     reservation.ReservationID,
		From:              reservation.Status,
		To:                StatusUnknown,
		Operation:         operationUpdate,
		TxHash:            evidence.TxHash,
		TxBytesHash:       evidence.TxBytesHash,
		SignDocHash:       evidence.SignDocHash,
		AuditReason:       "known transaction is pending confirmation",
		Now:               now,
		serviceAuthorized: true,
		requireSingleReservationOperation: operation != nil &&
			!IsTerminalOperationStatus(operation.Status),
	})
	if err != nil {
		return nil, err
	}
	operationStatus := OperationStatusUnknown
	if reconciledOperation != nil {
		operationStatus = reconciledOperation.Status
	}
	return reconciledResult(updated.Status, operationStatus, OperationStatusUnknown, "known transaction is pending confirmation"), nil
}

func (s Service) reconcileKnownPendingOperation(ctx context.Context, operation *PayrollOperation, linked []NoteReservation, provided map[string]OperationEvidence) (*ReconcileResult, bool, error) {
	if operation == nil || IsTerminalOperationStatus(operation.Status) || len(linked) == 0 {
		return nil, false, nil
	}
	allPending := true
	var identity OperationEvidence
	commonTxHash := ""
	commonTxBytesHash := ""
	identityInitialized := false
	for _, reservation := range linked {
		evidence := provided[reservation.ReservationID]
		if !evidence.TxKnown || evidence.TxSucceeded || evidence.TxFailed || evidence.NullifierSpent || !hasChainTransactionIdentity(evidence) {
			allPending = false
			break
		}
		if reservation.Status == StatusProofReady && !reservation.BroadcastInFlight {
			return unresolvedReconcileResult(&reservation, operation, "ProofReady reservation has no durable broadcast attempt"), true, nil
		}
		if reservation.Status != StatusProofReady && reservation.Status != StatusSubmitted && reservation.Status != StatusUnknown {
			return unresolvedReconcileResult(&reservation, operation, "known pending tx cannot reconcile this operation state"), true, nil
		}
		if err := mergeOperationEvidenceIdentity(&identity, evidence); err != nil {
			return unresolvedReconcileResult(&reservation, operation, "operation inputs report conflicting pending tx identity"), true, nil
		}
		txHash := normalizedTxIdentity(evidence.TxHash)
		txBytesHash := normalizedTxIdentity(evidence.TxBytesHash)
		if !identityInitialized {
			commonTxHash = txHash
			commonTxBytesHash = txBytesHash
			identityInitialized = true
		} else {
			if commonTxHash == "" || txHash == "" || commonTxHash != txHash {
				commonTxHash = ""
			}
			if commonTxBytesHash == "" || txBytesHash == "" || commonTxBytesHash != txBytesHash {
				commonTxBytesHash = ""
			}
		}
	}
	if !allPending {
		return nil, false, nil
	}
	if commonTxHash == "" && commonTxBytesHash == "" {
		return unresolvedReconcileResult(&linked[0], operation, "operation inputs do not share a common pending tx identity"), true, nil
	}
	now := s.now()
	updatedOperation := cloneOperation(*operation)
	if err := mergeEvidenceIntoOperation(&updatedOperation, identity); err != nil {
		return unresolvedReconcileResult(&linked[0], operation, "known pending tx identity conflicts with operation"), true, nil
	}
	updatedOperation.Status = OperationStatusUnknown
	updatedOperation.UpdatedAt = now
	ids := make([]string, 0, len(linked))
	fromStatuses := make(map[string]ReservationStatus, len(linked))
	for _, reservation := range linked {
		ids = append(ids, reservation.ReservationID)
		fromStatuses[reservation.ReservationID] = reservation.Status
	}
	updated, reconciledOperation, err := s.Store.ApplyReconciliationTransition(ctx, ReconciliationTransition{
		ReservationID:                    linked[0].ReservationID,
		From:                             linked[0].Status,
		To:                               StatusUnknown,
		Operation:                        &updatedOperation,
		TxHash:                           identity.TxHash,
		TxBytesHash:                      identity.TxBytesHash,
		SignDocHash:                      identity.SignDocHash,
		AuditReason:                      "known transaction is pending confirmation",
		OperationReservationIDs:          ids,
		OperationReservationFromStatuses: fromStatuses,
		Now:                              now,
		serviceAuthorized:                true,
	})
	if err != nil {
		return nil, true, err
	}
	operationStatus := OperationStatusUnknown
	if reconciledOperation != nil {
		operationStatus = reconciledOperation.Status
	}
	return reconciledResult(updated.Status, operationStatus, OperationStatusUnknown, "known transaction is pending confirmation"), true, nil
}

func hasChainTransactionIdentity(evidence OperationEvidence) bool {
	return strings.TrimSpace(evidence.TxHash) != "" || strings.TrimSpace(evidence.TxBytesHash) != ""
}

func mergeOperationEvidenceIdentity(target *OperationEvidence, evidence OperationEvidence) error {
	merge := func(name string, current *string, incoming string) error {
		incoming = normalizedTxIdentity(incoming)
		if incoming == "" {
			return nil
		}
		if normalized := normalizedTxIdentity(*current); normalized != "" && normalized != incoming {
			return fmt.Errorf("%s mismatch", name)
		}
		*current = incoming
		return nil
	}
	if err := merge("tx_hash", &target.TxHash, evidence.TxHash); err != nil {
		return err
	}
	if err := merge("tx_bytes_hash", &target.TxBytesHash, evidence.TxBytesHash); err != nil {
		return err
	}
	return merge("sign_doc_hash", &target.SignDocHash, evidence.SignDocHash)
}

func mergeEvidenceIntoOperation(operation *PayrollOperation, evidence OperationEvidence) error {
	transition := ReconciliationTransition{
		TxHash:      evidence.TxHash,
		TxBytesHash: evidence.TxBytesHash,
		SignDocHash: evidence.SignDocHash,
	}
	return mergeReconciliationIdentityIntoOperation(operation, transition)
}

func canReconcileToUnknown(status ReservationStatus) bool {
	return status == StatusSubmitted || status == StatusUnknown
}

func canReconcileFailedTransaction(status ReservationStatus) bool {
	return status == StatusSubmitted || status == StatusUnknown
}

func canReconcileToManualReview(status ReservationStatus) bool {
	return status == StatusSubmitted || status == StatusUnknown
}

func canReconcileSpent(status ReservationStatus) bool {
	return status == StatusProofReady || status == StatusSubmitted || status == StatusUnknown || status == StatusManualReview
}

func unresolvedReconcileResult(reservation *NoteReservation, operation *PayrollOperation, reason string) *ReconcileResult {
	return &ReconcileResult{
		ReservationStatus: reservation.Status,
		OperationStatus:   currentOperationStatus(operation),
		RequiresReview:    true,
		Reason:            reason,
	}
}

func reconciledResult(reservationStatus ReservationStatus, operationStatus OperationStatus, requestedOperationStatus OperationStatus, reason string) *ReconcileResult {
	result := &ReconcileResult{
		ReservationStatus: reservationStatus,
		OperationStatus:   operationStatus,
		Reason:            reason,
	}
	if operationStatus != requestedOperationStatus && IsTerminalOperationStatus(operationStatus) {
		result.RequiresReview = true
		result.Reason = "operation already terminal; preserved existing status"
	}
	return result
}

func currentOperationStatus(operation *PayrollOperation) OperationStatus {
	if operation == nil {
		return OperationStatusUnknown
	}
	return operation.Status
}

func operationStatusRequiresReview(status OperationStatus) bool {
	switch status {
	case OperationStatusConflictSpent, OperationStatusManualReview:
		return true
	default:
		return false
	}
}

func failedTxReconcileTarget(operation *PayrollOperation) (ReservationStatus, OperationStatus) {
	if operation != nil && IsTerminalOperationStatus(operation.Status) && operation.Status != OperationStatusFailed {
		return StatusManualReview, OperationStatusManualReview
	}
	return StatusFailed, OperationStatusFailed
}

func terminalReservationReview(reservationStatus ReservationStatus, operation *PayrollOperation, evidence OperationEvidence) (bool, string) {
	operationStatus := currentOperationStatus(operation)
	if reservationStatus == StatusFailed && evidence.NullifierSpent {
		return true, "terminal failed reservation conflicts with spent nullifier evidence"
	}
	if reservationStatus == StatusConfirmedSpent && evidence.TxFailed {
		switch {
		case evidence.NullifierSpent:
			return true, "terminal spent reservation conflicts with failed transaction and spent nullifier evidence"
		case evidence.NullifierUnspentConfirmed:
			return true, "terminal spent reservation conflicts with failed transaction and unspent nullifier evidence"
		default:
			return true, "terminal spent reservation conflicts with failed transaction evidence"
		}
	}
	if reservationStatus == StatusConfirmedSpent && evidence.NullifierUnspentConfirmed {
		return true, "terminal spent reservation conflicts with confirmed unspent nullifier evidence"
	}
	if reservationStatus == StatusFailed && evidence.TxSucceeded {
		return true, "terminal failed reservation conflicts with successful transaction evidence"
	}
	if txEvidenceConflictsOperation(operation, evidence) {
		return true, "terminal reservation conflicts with transaction identity evidence"
	}
	if operationStatusRequiresReview(operationStatus) {
		return true, "terminal operation requires review"
	}
	if reservationStatus == StatusConfirmedSpent && operationStatus != OperationStatusSucceeded {
		return true, "terminal spent reservation has no successful operation evidence"
	}
	if reservationStatus == StatusFailed && operationStatus == OperationStatusSucceeded {
		return true, "terminal failed reservation has successful operation evidence"
	}
	return false, "reservation is already terminal"
}

func (s Service) recordTerminalReconciliationAudit(ctx context.Context, reservation *NoteReservation, reason string) (*NoteReservation, error) {
	updated, _, err := s.Store.ApplyReconciliationTransition(ctx, ReconciliationTransition{
		ReservationID:     reservation.ReservationID,
		From:              reservation.Status,
		To:                reservation.Status,
		AuditReason:       reason,
		Now:               s.now(),
		serviceAuthorized: true,
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

func (s Service) reconcileReservationOperation(ctx context.Context, reservation *NoteReservation, operation *PayrollOperation, reservationStatus ReservationStatus, operationStatus OperationStatus) (*NoteReservation, OperationStatus, error) {
	if RequiresLeaseToken(reservation.Status, reservationStatus) {
		return nil, "", fmt.Errorf("%w: %s -> %s requires lease token", ErrLeaseMismatch, reservation.Status, reservationStatus)
	}

	now := s.now()
	var operationUpdate *PayrollOperation
	nextOperationStatus := operationStatus
	if operation != nil {
		if IsTerminalOperationStatus(operation.Status) && operation.Status != operationStatus {
			nextOperationStatus = operation.Status
		} else {
			updated := *operation
			updated.Status = operationStatus
			updated.UpdatedAt = now
			operationUpdate = &updated
		}
	}
	updatedReservation, updatedOperation, err := s.Store.ApplyReconciliationTransition(ctx, ReconciliationTransition{
		ReservationID:     reservation.ReservationID,
		From:              reservation.Status,
		To:                reservationStatus,
		Operation:         operationUpdate,
		Now:               now,
		serviceAuthorized: true,
		requireSingleReservationOperation: operation != nil &&
			!IsTerminalOperationStatus(operation.Status),
	})
	if err != nil {
		return nil, "", err
	}
	if updatedOperation != nil {
		return updatedReservation, updatedOperation.Status, nil
	}
	return updatedReservation, nextOperationStatus, nil
}

func (s Service) markConflictingNullifierEvidenceManualReview(ctx context.Context, reservation *NoteReservation, operation *PayrollOperation, reason string) (*NoteReservation, OperationStatus, error) {
	now := s.now()
	if reservation.Status == StatusManualReview && operation == nil {
		return reservation, OperationStatusManualReview, nil
	}
	if operation == nil {
		updated, _, err := s.Store.ApplyReconciliationTransition(ctx, ReconciliationTransition{
			ReservationID:     reservation.ReservationID,
			From:              reservation.Status,
			To:                StatusManualReview,
			AuditReason:       reason,
			Now:               now,
			serviceAuthorized: true,
		})
		return updated, OperationStatusManualReview, err
	}

	all, err := s.Store.ListReservations(ctx, ReservationFilter{})
	if err != nil {
		return nil, "", err
	}
	ids := make([]string, 0)
	fromStatuses := make(map[string]ReservationStatus)
	for _, linked := range all {
		if linked.OperationID != operation.OperationID {
			continue
		}
		if linked.Status != StatusManualReview && !CanTransitionReservation(linked.Status, StatusManualReview) {
			return nil, "", fmt.Errorf("%w: reservation %s cannot enter ManualReview from %s", ErrManualReviewRequired, linked.ReservationID, linked.Status)
		}
		ids = append(ids, linked.ReservationID)
		fromStatuses[linked.ReservationID] = linked.Status
	}
	if len(ids) == 0 {
		return nil, "", fmt.Errorf("%w: operation %s has no linked reservations", ErrInvalidReservation, operation.OperationID)
	}
	updatedOperation := cloneOperation(*operation)
	if !IsTerminalOperationStatus(operation.Status) {
		updatedOperation.Status = OperationStatusManualReview
		updatedOperation.UpdatedAt = now
	}
	updated, reconciledOperation, err := s.Store.ApplyReconciliationTransition(ctx, ReconciliationTransition{
		ReservationID:                    reservation.ReservationID,
		From:                             reservation.Status,
		To:                               StatusManualReview,
		Operation:                        &updatedOperation,
		AuditReason:                      reason,
		OperationReservationIDs:          ids,
		OperationReservationFromStatuses: fromStatuses,
		Now:                              now,
		serviceAuthorized:                true,
	})
	if err != nil {
		return nil, "", err
	}
	if reconciledOperation != nil {
		return updated, reconciledOperation.Status, nil
	}
	return updated, operation.Status, nil
}

// quarantineSpentReservation atomically records spent inventory for every
// reservation sharing the same owner/nullifier lookup key. This blocks a
// previously failed reservation from being replanned after the chain later
// proves the note was consumed.
func (s Service) quarantineSpentReservation(ctx context.Context, reservation *NoteReservation, operation *PayrollOperation, operationStatus OperationStatus, reason string) (*NoteReservation, OperationStatus, error) {
	now := s.now()
	nextOperationStatus := operationStatus
	var operationUpdate *PayrollOperation
	if operation != nil {
		if IsTerminalOperationStatus(operation.Status) && operation.Status != operationStatus {
			nextOperationStatus = operation.Status
		} else {
			updated := *operation
			updated.Status = operationStatus
			updated.UpdatedAt = now
			operationUpdate = &updated
		}
	}
	updatedReservation, updatedOperation, err := s.Store.ApplyReconciliationTransition(ctx, ReconciliationTransition{
		ReservationID:           reservation.ReservationID,
		From:                    reservation.Status,
		To:                      StatusConfirmedSpent,
		Operation:               operationUpdate,
		SiblingOperationStatus:  OperationStatusConflictSpent,
		AuditReason:             reason,
		Now:                     now,
		serviceAuthorized:       true,
		quarantineMatchingSpent: true,
	})
	if err != nil {
		return nil, "", err
	}
	if updatedOperation != nil {
		nextOperationStatus = updatedOperation.Status
	}
	return updatedReservation, nextOperationStatus, nil
}

func operationMatchesEvidence(operation *PayrollOperation, evidence OperationEvidence) bool {
	if operation == nil {
		return false
	}
	if !txEvidenceMatchesOperation(operation, evidence) {
		return false
	}
	if !matchRequired(operation.ExpectedOutputCommitment, evidence.OutputCommitment) {
		return false
	}
	expectedAuditDigest := firstNonEmpty(operation.ExpectedAuditDisclosureDigest, operation.ExpectedDisclosureDigest)
	actualAuditDigest := firstNonEmpty(evidence.AuditDisclosureDigest, evidence.DisclosureDigest)
	if !matchRequired(expectedAuditDigest, actualAuditDigest) {
		return false
	}
	if operation.ExpectedUserDisclosureDigest != "" && !matchRequired(operation.ExpectedUserDisclosureDigest, evidence.UserDisclosureDigest) {
		return false
	}
	if operation.ExpectedSelfViewDisclosureDigest != "" && !matchRequired(operation.ExpectedSelfViewDisclosureDigest, evidence.SelfViewDisclosureDigest) {
		return false
	}
	if !matchRequired(operation.ExpectedRecipientHash, evidence.RecipientHash) {
		return false
	}
	if !matchRequired(operation.ExpectedAmountHash, evidence.AmountHash) {
		return false
	}
	if !matchRequired(operation.ExpectedDenom, evidence.Denom) {
		return false
	}
	if operation.BatchItemIndexKnown {
		if !evidence.BatchItemIndexKnown {
			return false
		}
		if operation.BatchItemIndex != evidence.BatchItemIndex {
			return false
		}
	}
	return true
}

func txEvidenceMatchesOperation(operation *PayrollOperation, evidence OperationEvidence) bool {
	if operation == nil {
		return false
	}
	expectedTxHash := normalizedTxIdentity(operation.TxHash)
	expectedTxBytesHash := normalizedTxIdentity(operation.TxBytesHash)
	if expectedTxHash == "" && expectedTxBytesHash == "" {
		return false
	}
	matched := false
	if actual := normalizedTxIdentity(evidence.TxHash); actual != "" {
		if expectedTxHash == "" || actual != expectedTxHash {
			return false
		}
		matched = true
	}
	if actual := normalizedTxIdentity(evidence.TxBytesHash); actual != "" {
		if expectedTxBytesHash == "" || actual != expectedTxBytesHash {
			return false
		}
		matched = true
	}
	if expected := normalizedTxIdentity(operation.SignDocHash); expected != "" {
		actual := normalizedTxIdentity(evidence.SignDocHash)
		if actual != "" && actual != expected {
			return false
		}
	}
	return matched
}

func txEvidenceConflictsOperation(operation *PayrollOperation, evidence OperationEvidence) bool {
	if operation == nil {
		return normalizedTxIdentity(evidence.TxHash) != "" ||
			normalizedTxIdentity(evidence.TxBytesHash) != "" ||
			normalizedTxIdentity(evidence.SignDocHash) != ""
	}
	if actual := normalizedTxIdentity(evidence.TxHash); actual != "" && actual != normalizedTxIdentity(operation.TxHash) {
		return true
	}
	if actual := normalizedTxIdentity(evidence.TxBytesHash); actual != "" && actual != normalizedTxIdentity(operation.TxBytesHash) {
		return true
	}
	if actual := normalizedTxIdentity(evidence.SignDocHash); actual != "" {
		expected := normalizedTxIdentity(operation.SignDocHash)
		return expected != "" && actual != expected
	}
	return false
}

func normalizedTxIdentity(value string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value)), "0x")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func matchRequired(expected string, actual string) bool {
	expected = strings.TrimSpace(expected)
	actual = strings.TrimSpace(actual)
	return expected != "" && actual != "" && expected == actual
}
