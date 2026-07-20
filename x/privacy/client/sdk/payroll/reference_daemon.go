package payroll

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"time"

	privacyreservation "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/reservation"
)

type ReferenceDaemon struct {
	Reservation   privacyreservation.Service
	LeaseOwner    string
	LeaseTTL      time.Duration
	MaxOperations int
	Now           func() time.Time
}

type ReferenceDaemonRunReport struct {
	StartedAt      time.Time                      `json:"started_at"`
	FinishedAt     time.Time                      `json:"finished_at"`
	Mode           string                         `json:"mode"`
	ProofReady     int                            `json:"proof_ready"`
	Submitted      int                            `json:"submitted"`
	Reconciled     int                            `json:"reconciled"`
	RequiresReview int                            `json:"requires_review"`
	Skipped        int                            `json:"skipped"`
	Items          []ReferenceDaemonItemRunReport `json:"items"`
}

type ReferenceDaemonItemRunReport struct {
	OperationID       string                               `json:"operation_id,omitempty"`
	ItemID            string                               `json:"item_id,omitempty"`
	Action            string                               `json:"action"`
	ReservationIDs    []string                             `json:"reservation_ids,omitempty"`
	ReservationStatus privacyreservation.ReservationStatus `json:"reservation_status,omitempty"`
	OperationStatus   privacyreservation.OperationStatus   `json:"operation_status,omitempty"`
	RequiresReview    bool                                 `json:"requires_review,omitempty"`
	Reason            string                               `json:"reason,omitempty"`
}

type referenceReservationGroup struct {
	Operation    privacyreservation.PayrollOperation
	Reservations []privacyreservation.NoteReservation
}

func (d ReferenceDaemon) RunOnce(ctx context.Context) (*ReferenceDaemonRunReport, error) {
	if d.Reservation.Store == nil {
		return nil, fmt.Errorf("reservation service is required")
	}
	started := d.now()
	report := &ReferenceDaemonRunReport{
		StartedAt: started,
		Mode:      "simulated",
		Items:     make([]ReferenceDaemonItemRunReport, 0),
	}

	processed := 0
	for _, status := range []privacyreservation.ReservationStatus{
		privacyreservation.StatusReserved,
		privacyreservation.StatusProving,
		privacyreservation.StatusProofReady,
		privacyreservation.StatusSubmitted,
		privacyreservation.StatusUnknown,
	} {
		groups, err := d.groupsByStatus(ctx, status)
		if err != nil {
			return nil, err
		}
		for _, group := range groups {
			if d.MaxOperations > 0 && processed >= d.MaxOperations {
				report.Skipped++
				continue
			}
			if err := d.processGroup(ctx, status, group, report); err != nil {
				return nil, err
			}
			processed++
		}
	}

	report.FinishedAt = d.now()
	return report, nil
}

func (d ReferenceDaemon) groupsByStatus(ctx context.Context, status privacyreservation.ReservationStatus) ([]referenceReservationGroup, error) {
	return referenceDaemonGroupsByStatus(ctx, d.Reservation.Store, status)
}

func referenceDaemonGroupsByStatus(ctx context.Context, store privacyreservation.Store, status privacyreservation.ReservationStatus) ([]referenceReservationGroup, error) {
	reservations, err := store.ListReservations(ctx, privacyreservation.ReservationFilter{Statuses: []privacyreservation.ReservationStatus{status}})
	if err != nil {
		return nil, err
	}
	byOperation := make(map[string][]privacyreservation.NoteReservation)
	for _, reservation := range reservations {
		if reservation.OperationID == "" {
			continue
		}
		byOperation[reservation.OperationID] = append(byOperation[reservation.OperationID], reservation)
	}
	operationIDs := make([]string, 0, len(byOperation))
	for operationID := range byOperation {
		operationIDs = append(operationIDs, operationID)
	}
	sort.Strings(operationIDs)

	groups := make([]referenceReservationGroup, 0, len(operationIDs))
	for _, operationID := range operationIDs {
		operation, err := store.GetOperation(ctx, operationID)
		if err != nil {
			return nil, err
		}
		reservations := byOperation[operationID]
		sort.Slice(reservations, func(i, j int) bool {
			return reservations[i].ReservationID < reservations[j].ReservationID
		})
		groups = append(groups, referenceReservationGroup{
			Operation:    *operation,
			Reservations: reservations,
		})
	}
	return groups, nil
}

func (d ReferenceDaemon) processGroup(ctx context.Context, status privacyreservation.ReservationStatus, group referenceReservationGroup, report *ReferenceDaemonRunReport) error {
	switch status {
	case privacyreservation.StatusReserved:
		if mixed, err := referenceDaemonHasActiveReservationsOutsideStatus(ctx, d.Reservation.Store, group.Operation.OperationID, status); err != nil {
			return err
		} else if mixed {
			report.Skipped++
			report.Items = append(report.Items, referenceDaemonItem(group, "skipped", status, group.Operation.Status, false, "operation has active reservations in another status"))
			return nil
		}
		return d.simulateProofAndSubmit(ctx, group, report)
	case privacyreservation.StatusProving:
		return d.rollbackExpiredProving(ctx, group, report)
	case privacyreservation.StatusProofReady:
		if mixed, err := referenceDaemonHasActiveReservationsOutsideStatus(ctx, d.Reservation.Store, group.Operation.OperationID, status); err != nil {
			return err
		} else if mixed {
			report.Skipped++
			report.Items = append(report.Items, referenceDaemonItem(group, "skipped", status, group.Operation.Status, false, "operation has active reservations in another status"))
			return nil
		}
		return d.simulateSubmit(ctx, group, report)
	case privacyreservation.StatusSubmitted, privacyreservation.StatusUnknown:
		return d.simulateReconcile(ctx, group, report)
	default:
		report.Skipped++
		report.Items = append(report.Items, referenceDaemonItem(group, "skipped", status, group.Operation.Status, false, "unsupported status"))
		return nil
	}
}

func referenceDaemonHasActiveReservationsOutsideStatus(ctx context.Context, store privacyreservation.Store, operationID string, status privacyreservation.ReservationStatus) (bool, error) {
	if operationID == "" {
		return false, nil
	}
	reservations, err := store.ListReservations(ctx, privacyreservation.ReservationFilter{Statuses: referenceDaemonActiveReservationStatuses()})
	if err != nil {
		return false, err
	}
	for _, reservation := range reservations {
		if reservation.OperationID == operationID && reservation.Status != status {
			return true, nil
		}
	}
	return false, nil
}

func referenceDaemonActiveReservationStatuses() []privacyreservation.ReservationStatus {
	return []privacyreservation.ReservationStatus{
		privacyreservation.StatusReserved,
		privacyreservation.StatusProving,
		privacyreservation.StatusProofReady,
		privacyreservation.StatusSubmitted,
		privacyreservation.StatusUnknown,
		privacyreservation.StatusManualReview,
	}
}

func (d ReferenceDaemon) simulateProofAndSubmit(ctx context.Context, group referenceReservationGroup, report *ReferenceDaemonRunReport) (runErr error) {
	refs := make([]privacyreservation.SubmittedReservationRef, 0, len(group.Reservations))
	rollbackRequired := true
	defer func() {
		if !rollbackRequired || len(refs) == 0 {
			return
		}
		if rollbackErr := rollbackProvingReservations(ctx, d.Reservation, refs); rollbackErr != nil {
			runErr = errors.Join(runErr, rollbackErr)
		}
	}()
	for _, reservation := range group.Reservations {
		lease, err := d.Reservation.AcquireLeaseForStatus(ctx, reservation.ReservationID, d.leaseOwner(), privacyreservation.StatusReserved, d.leaseTTL())
		if err != nil {
			return err
		}
		if _, err := d.Reservation.TransitionWithLease(ctx, reservation.ReservationID, lease.Owner, lease.Token, privacyreservation.StatusReserved, privacyreservation.StatusProving); err != nil {
			return err
		}
		refs = append(refs, privacyreservation.SubmittedReservationRef{ReservationID: reservation.ReservationID, LeaseOwner: lease.Owner, LeaseToken: lease.Token})
	}
	proofReadyUpdate := privacyreservation.ProofReadyOperationUpdate{
		OperationID:                      group.Operation.OperationID,
		ExpectedOutputCommitment:         expectedOrSimulated(group.Operation.ExpectedOutputCommitment, "commitment", group.Operation.OperationID),
		ExpectedDisclosureDigest:         expectedOrSimulated(group.Operation.ExpectedDisclosureDigest, "audit-disclosure", group.Operation.OperationID),
		ExpectedAuditDisclosureDigest:    expectedOrSimulated(firstNonEmpty(group.Operation.ExpectedAuditDisclosureDigest, group.Operation.ExpectedDisclosureDigest), "audit-disclosure", group.Operation.OperationID),
		ExpectedUserDisclosureDigest:     group.Operation.ExpectedUserDisclosureDigest,
		ExpectedSelfViewDisclosureDigest: group.Operation.ExpectedSelfViewDisclosureDigest,
	}
	updatedReservations, updatedOperation, err := d.Reservation.MarkProofReadyBatch(ctx, refs, proofReadyUpdate)
	if err != nil {
		return err
	}
	rollbackRequired = false
	report.ProofReady++
	operation := group.Operation
	if updatedOperation != nil {
		operation = *updatedOperation
	}
	report.Items = append(report.Items, referenceDaemonItem(referenceReservationGroup{Operation: operation, Reservations: updatedReservations}, "proof-ready", privacyreservation.StatusProofReady, operation.Status, false, "simulated proof artifact stored"))
	return d.submitWithRefs(ctx, operation, updatedReservations, refs, report)
}

func (d ReferenceDaemon) rollbackExpiredProving(ctx context.Context, group referenceReservationGroup, report *ReferenceDaemonRunReport) error {
	refs := make([]privacyreservation.SubmittedReservationRef, 0, len(group.Reservations))
	for _, reservation := range group.Reservations {
		lease, err := d.Reservation.AcquireLeaseForStatus(ctx, reservation.ReservationID, d.leaseOwner(), privacyreservation.StatusProving, d.leaseTTL())
		if err != nil {
			if clearErr := clearAcquiredSubmissionLeases(ctx, d.Reservation, refs); clearErr != nil {
				err = errors.Join(err, clearErr)
			}
			if errors.Is(err, privacyreservation.ErrLeaseUnavailable) || errors.Is(err, privacyreservation.ErrLeaseMismatch) || errors.Is(err, privacyreservation.ErrCompareAndSetFailed) {
				report.Skipped++
				report.Items = append(report.Items, referenceDaemonItem(group, "skipped", privacyreservation.StatusProving, group.Operation.Status, false, "proving lease is owned by another worker"))
				return nil
			}
			return err
		}
		refs = append(refs, privacyreservation.SubmittedReservationRef{ReservationID: reservation.ReservationID, LeaseOwner: lease.Owner, LeaseToken: lease.Token})
	}
	for _, ref := range refs {
		if _, err := d.Reservation.TransitionWithLease(ctx, ref.ReservationID, ref.LeaseOwner, ref.LeaseToken, privacyreservation.StatusProving, privacyreservation.StatusReserved); err != nil {
			return err
		}
	}
	report.Skipped++
	report.Items = append(report.Items, referenceDaemonItem(group, "rolled-back", privacyreservation.StatusReserved, group.Operation.Status, false, "expired proving reservations returned to reserved"))
	return nil
}

func (d ReferenceDaemon) simulateSubmit(ctx context.Context, group referenceReservationGroup, report *ReferenceDaemonRunReport) error {
	refs := make([]privacyreservation.SubmittedReservationRef, 0, len(group.Reservations))
	for _, reservation := range group.Reservations {
		lease, err := d.Reservation.AcquireLeaseForStatus(ctx, reservation.ReservationID, d.leaseOwner(), privacyreservation.StatusProofReady, d.leaseTTL())
		if err != nil {
			return err
		}
		refs = append(refs, privacyreservation.SubmittedReservationRef{ReservationID: reservation.ReservationID, LeaseOwner: lease.Owner, LeaseToken: lease.Token})
	}
	return d.submitWithRefs(ctx, group.Operation, group.Reservations, refs, report)
}

func (d ReferenceDaemon) submitWithRefs(ctx context.Context, operation privacyreservation.PayrollOperation, reservations []privacyreservation.NoteReservation, refs []privacyreservation.SubmittedReservationRef, report *ReferenceDaemonRunReport) error {
	update := privacyreservation.SubmittedReservationUpdate{
		TxHash:          simulatedHex("tx", operation.OperationID),
		TxBytesHash:     simulatedHex("tx-bytes", operation.OperationID),
		SignDocHash:     simulatedHex("sign-doc", operation.OperationID),
		AccountSequence: simulatedSequence(operation.OperationID),
	}
	updatedReservations, updatedOperations, err := d.Reservation.MarkSubmittedBatch(ctx, refs, []string{operation.OperationID}, update)
	if err != nil {
		return err
	}
	if len(updatedOperations) > 0 {
		operation = updatedOperations[0]
	}
	report.Submitted++
	report.Items = append(report.Items, referenceDaemonItem(referenceReservationGroup{Operation: operation, Reservations: updatedReservations}, "submitted", privacyreservation.StatusSubmitted, operation.Status, false, "simulated tx broadcast"))
	return d.simulateReconcile(ctx, referenceReservationGroup{Operation: operation, Reservations: updatedReservations}, report)
}

func (d ReferenceDaemon) simulateReconcile(ctx context.Context, group referenceReservationGroup, report *ReferenceDaemonRunReport) error {
	evidences := make([]privacyreservation.OperationReservationEvidence, 0, len(group.Reservations))
	for _, reservation := range group.Reservations {
		evidence := privacyreservation.OperationEvidence{
			TxHash:                   firstNonEmpty(group.Operation.TxHash, simulatedHex("tx", group.Operation.OperationID)),
			SignDocHash:              firstNonEmpty(group.Operation.SignDocHash, simulatedHex("sign-doc", group.Operation.OperationID)),
			TxBytesHash:              firstNonEmpty(group.Operation.TxBytesHash, simulatedHex("tx-bytes", group.Operation.OperationID)),
			OutputCommitment:         expectedOrSimulated(group.Operation.ExpectedOutputCommitment, "commitment", group.Operation.OperationID),
			DisclosureDigest:         expectedOrSimulated(firstNonEmpty(group.Operation.ExpectedAuditDisclosureDigest, group.Operation.ExpectedDisclosureDigest), "audit-disclosure", group.Operation.OperationID),
			UserDisclosureDigest:     group.Operation.ExpectedUserDisclosureDigest,
			AuditDisclosureDigest:    expectedOrSimulated(firstNonEmpty(group.Operation.ExpectedAuditDisclosureDigest, group.Operation.ExpectedDisclosureDigest), "audit-disclosure", group.Operation.OperationID),
			SelfViewDisclosureDigest: group.Operation.ExpectedSelfViewDisclosureDigest,
			RecipientHash:            group.Operation.ExpectedRecipientHash,
			AmountHash:               group.Operation.ExpectedAmountHash,
			Denom:                    group.Operation.ExpectedDenom,
			BatchItemIndex:           group.Operation.BatchItemIndex,
			BatchItemIndexKnown:      group.Operation.BatchItemIndexKnown,
			NullifierSpent:           true,
			TxSucceeded:              true,
			TxKnown:                  true,
		}
		evidences = append(evidences, privacyreservation.OperationReservationEvidence{
			ReservationID: reservation.ReservationID,
			Evidence:      evidence,
		})
	}
	result, err := d.Reservation.ReconcileOperation(ctx, group.Operation.OperationID, evidences)
	if err != nil {
		return err
	}
	if result.RequiresReview {
		report.RequiresReview += len(group.Reservations)
	}
	report.Reconciled += len(group.Reservations)
	report.Items = append(report.Items, referenceDaemonItem(group, "reconciled", result.ReservationStatus, result.OperationStatus, result.RequiresReview, result.Reason))
	return nil
}

func referenceDaemonItem(group referenceReservationGroup, action string, reservationStatus privacyreservation.ReservationStatus, operationStatus privacyreservation.OperationStatus, requiresReview bool, reason string) ReferenceDaemonItemRunReport {
	return ReferenceDaemonItemRunReport{
		OperationID:       group.Operation.OperationID,
		ItemID:            group.Operation.ItemID,
		Action:            action,
		ReservationIDs:    referenceReservationIDs(group.Reservations),
		ReservationStatus: reservationStatus,
		OperationStatus:   operationStatus,
		RequiresReview:    requiresReview,
		Reason:            reason,
	}
}

func referenceReservationIDs(reservations []privacyreservation.NoteReservation) []string {
	ids := make([]string, 0, len(reservations))
	for _, reservation := range reservations {
		ids = append(ids, reservation.ReservationID)
	}
	sort.Strings(ids)
	return ids
}

func (d ReferenceDaemon) leaseOwner() string {
	if d.LeaseOwner != "" {
		return d.LeaseOwner
	}
	return "clairveil-payrolld"
}

func (d ReferenceDaemon) leaseTTL() time.Duration {
	if d.LeaseTTL > 0 {
		return d.LeaseTTL
	}
	return time.Minute
}

func (d ReferenceDaemon) now() time.Time {
	if d.Now != nil {
		return d.Now().UTC()
	}
	return time.Now().UTC()
}

func expectedOrSimulated(value string, label string, operationID string) string {
	if value != "" {
		return value
	}
	return simulatedHex(label, operationID)
}

func simulatedHex(label string, operationID string) string {
	sum := sha256.Sum256([]byte(label + ":" + operationID))
	return hex.EncodeToString(sum[:])
}

func simulatedSequence(operationID string) uint64 {
	sum := sha256.Sum256([]byte("sequence:" + operationID))
	return uint64(sum[0])<<24 | uint64(sum[1])<<16 | uint64(sum[2])<<8 | uint64(sum[3])
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
