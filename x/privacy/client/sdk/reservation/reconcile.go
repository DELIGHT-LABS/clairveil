package reservation

import (
	"context"
	"fmt"
	"strings"
)

type OperationEvidence struct {
	TxHash              string
	SignDocHash         string
	TxBytesHash         string
	OutputCommitment    string
	DisclosureDigest    string
	RecipientHash       string
	AmountHash          string
	Denom               string
	BatchItemIndex      int
	BatchItemIndexKnown bool
	NullifierSpent      bool
	TxSucceeded         bool
	TxFailed            bool
	TxKnown             bool
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
	if reservation.OperationID != "" {
		operation, err = s.Store.GetOperation(ctx, reservation.OperationID)
		if err != nil {
			return nil, err
		}
	}
	if IsTerminalReservationStatus(reservation.Status) {
		operationStatus := currentOperationStatus(operation)
		requiresReview, reason := terminalReservationReview(reservation.Status, operationStatus)
		return &ReconcileResult{
			ReservationStatus: reservation.Status,
			OperationStatus:   operationStatus,
			RequiresReview:    requiresReview,
			Reason:            reason,
		}, nil
	}
	txKnown := evidence.TxKnown || evidence.TxSucceeded || evidence.TxFailed
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
		updated, operationStatus, err := s.reconcileReservationOperation(ctx, reservation, operation, StatusUnknown, OperationStatusUnknown)
		if err != nil {
			return nil, err
		}
		return reconciledResult(updated.Status, operationStatus, OperationStatusUnknown, "tx and nullifier state are unknown"), nil
	}

	if evidence.TxFailed && !evidence.NullifierSpent {
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
			updated, operationStatus, updateErr := s.reconcileReservationOperation(ctx, reservation, operation, StatusConfirmedSpent, OperationStatusConflictSpent)
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

		updated, operationStatus, updateErr := s.reconcileReservationOperation(ctx, reservation, operation, StatusConfirmedSpent, OperationStatusSucceeded)
		if updateErr != nil {
			return nil, updateErr
		}
		return reconciledResult(updated.Status, operationStatus, OperationStatusSucceeded, "operation evidence matched"), nil
	}

	return &ReconcileResult{ReservationStatus: reservation.Status, OperationStatus: OperationStatusUnknown, RequiresReview: true, Reason: "unhandled reconcile evidence"}, ErrManualReviewRequired
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

func terminalReservationReview(reservationStatus ReservationStatus, operationStatus OperationStatus) (bool, string) {
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
	updatedReservation, updatedOperation, err := s.Store.CompareAndSetReservationStatusWithOperation(ctx, reservation.ReservationID, reservation.Status, reservationStatus, operationUpdate, now)
	if err != nil {
		return nil, "", err
	}
	if updatedOperation != nil {
		return updatedReservation, updatedOperation.Status, nil
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
	if !matchRequired(operation.ExpectedDisclosureDigest, evidence.DisclosureDigest) {
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
	matched := false
	if txMatched, ok := txIdentityMatches(
		[]string{operation.TxHash, operation.TxBytesHash},
		[]string{evidence.TxHash, evidence.TxBytesHash},
	); !ok {
		return false
	} else if txMatched {
		matched = true
	}
	if expected := normalizedTxIdentity(operation.SignDocHash); expected != "" {
		actual := normalizedTxIdentity(evidence.SignDocHash)
		if actual == expected {
			matched = true
		} else if actual != "" {
			return false
		}
	}
	return matched
}

func txIdentityMatches(expectedValues []string, actualValues []string) (bool, bool) {
	expected := make(map[string]struct{}, len(expectedValues))
	for _, value := range expectedValues {
		if normalized := normalizedTxIdentity(value); normalized != "" {
			expected[normalized] = struct{}{}
		}
	}
	if len(expected) == 0 {
		return false, true
	}

	matched := false
	for _, value := range actualValues {
		normalized := normalizedTxIdentity(value)
		if normalized == "" {
			continue
		}
		if _, ok := expected[normalized]; !ok {
			return false, false
		}
		matched = true
	}
	return matched, true
}

func normalizedTxIdentity(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func matchRequired(expected string, actual string) bool {
	expected = strings.TrimSpace(expected)
	actual = strings.TrimSpace(actual)
	return expected != "" && actual != "" && expected == actual
}
