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
	if !evidence.TxKnown && !evidence.NullifierSpent {
		updated, err := s.Transition(ctx, reservationID, reservation.Status, StatusUnknown)
		if err != nil {
			return nil, err
		}
		return &ReconcileResult{ReservationStatus: updated.Status, OperationStatus: OperationStatusUnknown, Reason: "tx and nullifier state are unknown"}, nil
	}

	var operation *PayrollOperation
	if reservation.OperationID != "" {
		operation, _ = s.Store.GetOperation(ctx, reservation.OperationID)
	}

	if evidence.TxFailed && !evidence.NullifierSpent {
		updated, err := s.Transition(ctx, reservationID, reservation.Status, StatusFailed)
		if err != nil {
			return nil, err
		}
		if operation != nil {
			operation.Status = OperationStatusFailed
			operation.UpdatedAt = s.now()
			_, _ = s.Store.UpdateOperation(ctx, *operation)
		}
		return &ReconcileResult{ReservationStatus: updated.Status, OperationStatus: OperationStatusFailed, Reason: "tx failed and nullifier is unspent"}, nil
	}

	if evidence.TxSucceeded || evidence.NullifierSpent {
		if !operationMatchesEvidence(operation, evidence) {
			reservation.Status = StatusConfirmedSpent
			reservation.UpdatedAt = s.now()
			_, updateErr := s.Store.UpdateReservation(ctx, *reservation)
			if operation != nil {
				operation.Status = OperationStatusConflictSpent
				operation.UpdatedAt = s.now()
				_, _ = s.Store.UpdateOperation(ctx, *operation)
			}
			if updateErr != nil {
				return nil, updateErr
			}
			return &ReconcileResult{
				ReservationStatus: StatusConfirmedSpent,
				OperationStatus:   OperationStatusConflictSpent,
				RequiresReview:    true,
				Reason:            "nullifier spent but evidence does not match operation",
			}, nil
		}

		reservation.Status = StatusConfirmedSpent
		reservation.UpdatedAt = s.now()
		_, updateErr := s.Store.UpdateReservation(ctx, *reservation)
		if operation != nil {
			operation.Status = OperationStatusSucceeded
			operation.UpdatedAt = s.now()
			_, _ = s.Store.UpdateOperation(ctx, *operation)
		}
		if updateErr != nil {
			return nil, updateErr
		}
		return &ReconcileResult{ReservationStatus: StatusConfirmedSpent, OperationStatus: OperationStatusSucceeded, Reason: "operation evidence matched"}, nil
	}

	return &ReconcileResult{ReservationStatus: reservation.Status, OperationStatus: OperationStatusUnknown, RequiresReview: true, Reason: "unhandled reconcile evidence"}, ErrManualReviewRequired
}

func operationMatchesEvidence(operation *PayrollOperation, evidence OperationEvidence) bool {
	if operation == nil {
		return false
	}
	if !matchOptional(operation.TxHash, evidence.TxHash) {
		return false
	}
	if !matchOptional(operation.SignDocHash, evidence.SignDocHash) {
		return false
	}
	if !matchOptional(operation.TxBytesHash, evidence.TxBytesHash) {
		return false
	}
	if !matchOptional(operation.ExpectedOutputCommitment, evidence.OutputCommitment) {
		return false
	}
	if !matchOptional(operation.ExpectedDisclosureDigest, evidence.DisclosureDigest) {
		return false
	}
	if !matchOptional(operation.ExpectedRecipientHash, evidence.RecipientHash) {
		return false
	}
	if !matchOptional(operation.ExpectedAmountHash, evidence.AmountHash) {
		return false
	}
	if !matchOptional(operation.ExpectedDenom, evidence.Denom) {
		return false
	}
	if operation.BatchItemIndex >= 0 {
		if !evidence.BatchItemIndexKnown {
			return false
		}
		if operation.BatchItemIndex != evidence.BatchItemIndex {
			return false
		}
	}
	return true
}

func matchOptional(expected string, actual string) bool {
	expected = strings.TrimSpace(expected)
	actual = strings.TrimSpace(actual)
	if expected == "" {
		return true
	}
	return expected == actual
}
