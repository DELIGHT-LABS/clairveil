package payroll

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
		return d.simulateProofAndSubmit(ctx, group, report)
	case privacyreservation.StatusProofReady:
		return d.simulateSubmit(ctx, group, report)
	case privacyreservation.StatusSubmitted, privacyreservation.StatusUnknown:
		return d.simulateReconcile(ctx, group, report)
	default:
		report.Skipped++
		report.Items = append(report.Items, referenceDaemonItem(group, "skipped", status, group.Operation.Status, false, "unsupported status"))
		return nil
	}
}

func (d ReferenceDaemon) simulateProofAndSubmit(ctx context.Context, group referenceReservationGroup, report *ReferenceDaemonRunReport) error {
	refs := make([]privacyreservation.SubmittedReservationRef, 0, len(group.Reservations))
	for _, reservation := range group.Reservations {
		lease, err := d.Reservation.AcquireLeaseForStatus(ctx, reservation.ReservationID, d.leaseOwner(), privacyreservation.StatusReserved, d.leaseTTL())
		if err != nil {
			return err
		}
		if _, err := d.Reservation.TransitionWithLease(ctx, reservation.ReservationID, lease.Token, privacyreservation.StatusReserved, privacyreservation.StatusProving); err != nil {
			return err
		}
		refs = append(refs, privacyreservation.SubmittedReservationRef{ReservationID: reservation.ReservationID, LeaseToken: lease.Token})
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
	report.ProofReady++
	operation := group.Operation
	if updatedOperation != nil {
		operation = *updatedOperation
	}
	report.Items = append(report.Items, referenceDaemonItem(referenceReservationGroup{Operation: operation, Reservations: updatedReservations}, "proof-ready", privacyreservation.StatusProofReady, operation.Status, false, "simulated proof artifact stored"))
	return d.submitWithRefs(ctx, operation, updatedReservations, refs, report)
}

func (d ReferenceDaemon) simulateSubmit(ctx context.Context, group referenceReservationGroup, report *ReferenceDaemonRunReport) error {
	refs := make([]privacyreservation.SubmittedReservationRef, 0, len(group.Reservations))
	for _, reservation := range group.Reservations {
		lease, err := d.Reservation.AcquireLeaseForStatus(ctx, reservation.ReservationID, d.leaseOwner(), privacyreservation.StatusProofReady, d.leaseTTL())
		if err != nil {
			return err
		}
		refs = append(refs, privacyreservation.SubmittedReservationRef{ReservationID: reservation.ReservationID, LeaseToken: lease.Token})
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
		result, err := d.Reservation.Reconcile(ctx, reservation.ReservationID, evidence)
		if err != nil {
			return err
		}
		if result.RequiresReview {
			report.RequiresReview++
		}
		report.Reconciled++
		report.Items = append(report.Items, ReferenceDaemonItemRunReport{
			OperationID:       group.Operation.OperationID,
			ItemID:            group.Operation.ItemID,
			Action:            "reconciled",
			ReservationIDs:    []string{reservation.ReservationID},
			ReservationStatus: result.ReservationStatus,
			OperationStatus:   result.OperationStatus,
			RequiresReview:    result.RequiresReview,
			Reason:            result.Reason,
		})
	}
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
