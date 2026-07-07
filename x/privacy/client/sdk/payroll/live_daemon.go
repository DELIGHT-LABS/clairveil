package payroll

import (
	"context"
	"errors"
	"fmt"
	"time"

	privacyreservation "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/reservation"
)

var ErrLiveDaemonSkip = errors.New("live daemon operation skipped")

type LiveOperationExecutor interface {
	BuildProofReady(ctx context.Context, group LiveOperationGroup) (privacyreservation.ProofReadyOperationUpdate, string, error)
	BroadcastProofReady(ctx context.Context, group LiveOperationGroup) (*BroadcastResult, string, error)
	ScanSubmitted(ctx context.Context, group LiveOperationGroup) (map[string]privacyreservation.OperationEvidence, string, error)
}

type LiveOperationGroup struct {
	Operation    privacyreservation.PayrollOperation
	Reservations []privacyreservation.NoteReservation
}

type LiveDaemon struct {
	Reservation   privacyreservation.Service
	Executor      LiveOperationExecutor
	LeaseOwner    string
	LeaseTTL      time.Duration
	MaxOperations int
	Now           func() time.Time
}

func (d LiveDaemon) RunOnce(ctx context.Context) (*ReferenceDaemonRunReport, error) {
	if d.Reservation.Store == nil {
		return nil, fmt.Errorf("reservation service is required")
	}
	if d.Executor == nil {
		return nil, fmt.Errorf("live operation executor is required")
	}
	started := d.now()
	report := &ReferenceDaemonRunReport{
		StartedAt: started,
		Mode:      "live",
		Items:     make([]ReferenceDaemonItemRunReport, 0),
	}
	processed := 0
	for _, status := range []privacyreservation.ReservationStatus{
		privacyreservation.StatusReserved,
		privacyreservation.StatusProofReady,
		privacyreservation.StatusSubmitted,
		privacyreservation.StatusUnknown,
	} {
		groups, err := referenceDaemonGroupsByStatus(ctx, d.Reservation.Store, status)
		if err != nil {
			return nil, err
		}
		for _, group := range groups {
			if d.MaxOperations > 0 && processed >= d.MaxOperations {
				report.Skipped++
				continue
			}
			if err := d.processGroup(ctx, status, liveGroupFromReference(group), report); err != nil {
				return nil, err
			}
			processed++
		}
	}
	report.FinishedAt = d.now()
	return report, nil
}

func (d LiveDaemon) processGroup(ctx context.Context, status privacyreservation.ReservationStatus, group LiveOperationGroup, report *ReferenceDaemonRunReport) error {
	switch status {
	case privacyreservation.StatusReserved:
		return d.buildProofReady(ctx, group, report)
	case privacyreservation.StatusProofReady:
		return d.broadcastProofReady(ctx, group, report)
	case privacyreservation.StatusSubmitted, privacyreservation.StatusUnknown:
		return d.reconcileSubmitted(ctx, group, report)
	default:
		report.Skipped++
		report.Items = append(report.Items, liveDaemonItem(group, "skipped", status, group.Operation.Status, false, "unsupported status"))
		return nil
	}
}

func (d LiveDaemon) buildProofReady(ctx context.Context, group LiveOperationGroup, report *ReferenceDaemonRunReport) error {
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
	update, reason, err := d.Executor.BuildProofReady(ctx, group)
	if err != nil {
		rollbackErr := d.rollbackProving(ctx, refs)
		if errors.Is(err, ErrLiveDaemonSkip) {
			report.Skipped++
			report.Items = append(report.Items, liveDaemonItem(group, "skipped", privacyreservation.StatusReserved, group.Operation.Status, false, firstNonEmptyString(reason, err.Error())))
			return rollbackErr
		}
		return errors.Join(err, rollbackErr)
	}
	if update.OperationID == "" {
		update.OperationID = group.Operation.OperationID
	}
	reservations, operation, err := d.Reservation.MarkProofReadyBatch(ctx, refs, update)
	if err != nil {
		return err
	}
	report.ProofReady++
	operationStatus := group.Operation.Status
	if operation != nil {
		operationStatus = operation.Status
	}
	report.Items = append(report.Items, referenceDaemonItem(referenceReservationGroup{Operation: group.Operation, Reservations: reservations}, "proof-ready", privacyreservation.StatusProofReady, operationStatus, false, firstNonEmptyString(reason, "proof artifact stored")))
	return nil
}

func (d LiveDaemon) rollbackProving(ctx context.Context, refs []privacyreservation.SubmittedReservationRef) error {
	var rollbackErr error
	for _, ref := range refs {
		_, err := d.Reservation.TransitionWithLease(ctx, ref.ReservationID, ref.LeaseToken, privacyreservation.StatusProving, privacyreservation.StatusReserved)
		if err != nil {
			rollbackErr = errors.Join(rollbackErr, err)
		}
	}
	return rollbackErr
}

func (d LiveDaemon) broadcastProofReady(ctx context.Context, group LiveOperationGroup, report *ReferenceDaemonRunReport) error {
	refs := make([]privacyreservation.SubmittedReservationRef, 0, len(group.Reservations))
	for _, reservation := range group.Reservations {
		lease, err := d.proofReadyLease(ctx, reservation)
		if err != nil {
			return err
		}
		refs = append(refs, privacyreservation.SubmittedReservationRef{ReservationID: reservation.ReservationID, LeaseToken: lease.Token})
	}
	broadcast, reason, err := d.Executor.BroadcastProofReady(ctx, group)
	if err != nil {
		if errors.Is(err, ErrLiveDaemonSkip) {
			report.Skipped++
			report.Items = append(report.Items, liveDaemonItem(group, "skipped", privacyreservation.StatusProofReady, group.Operation.Status, false, firstNonEmptyString(reason, err.Error())))
			return nil
		}
		return err
	}
	if broadcast == nil {
		return fmt.Errorf("live executor returned nil broadcast result")
	}
	operationIDs := []string{group.Operation.OperationID}
	if broadcast.Code != 0 {
		_, _, markErr := d.Reservation.MarkBroadcastUnknownBatch(ctx, refs, operationIDs, privacyreservation.BroadcastAttemptUpdate{
			TxHash:             broadcast.TxHash,
			TxBytesHash:        broadcast.TxBytesHash,
			SignDocHash:        broadcast.SignDocHash,
			AccountSequence:    broadcast.AccountSequence,
			LastBroadcastError: broadcastCodeError(broadcast).Error(),
		})
		if markErr != nil {
			return markErr
		}
		report.Submitted++
		report.Items = append(report.Items, liveDaemonItem(group, "broadcast-unknown", privacyreservation.StatusUnknown, privacyreservation.OperationStatusUnknown, true, firstNonEmptyString(reason, broadcastCodeError(broadcast).Error())))
		return nil
	}
	reservations, operations, err := d.Reservation.MarkSubmittedBatch(ctx, refs, operationIDs, privacyreservation.SubmittedReservationUpdate{
		TxHash:          broadcast.TxHash,
		TxBytesHash:     broadcast.TxBytesHash,
		SignDocHash:     broadcast.SignDocHash,
		AccountSequence: broadcast.AccountSequence,
	})
	if err != nil {
		return err
	}
	operationStatus := privacyreservation.OperationStatusSubmitted
	if len(operations) > 0 {
		operationStatus = operations[0].Status
	}
	report.Submitted++
	report.Items = append(report.Items, referenceDaemonItem(referenceReservationGroup{Operation: group.Operation, Reservations: reservations}, "submitted", privacyreservation.StatusSubmitted, operationStatus, false, firstNonEmptyString(reason, "tx broadcast")))
	return nil
}

func (d LiveDaemon) proofReadyLease(ctx context.Context, reservation privacyreservation.NoteReservation) (*privacyreservation.Lease, error) {
	if reservation.LeaseToken != "" {
		lease, err := d.Reservation.HeartbeatLeaseForStatus(ctx, reservation.ReservationID, reservation.LeaseToken, privacyreservation.StatusProofReady, d.leaseTTL())
		if err == nil {
			return lease, nil
		}
	}
	return d.Reservation.AcquireLeaseForStatus(ctx, reservation.ReservationID, d.leaseOwner(), privacyreservation.StatusProofReady, d.leaseTTL())
}

func (d LiveDaemon) reconcileSubmitted(ctx context.Context, group LiveOperationGroup, report *ReferenceDaemonRunReport) error {
	evidenceByReservation, reason, err := d.Executor.ScanSubmitted(ctx, group)
	if err != nil {
		if errors.Is(err, ErrLiveDaemonSkip) {
			report.Skipped++
			report.Items = append(report.Items, liveDaemonItem(group, "skipped", group.Reservations[0].Status, group.Operation.Status, false, firstNonEmptyString(reason, err.Error())))
			return nil
		}
		return err
	}
	if len(evidenceByReservation) == 0 {
		report.Skipped++
		report.Items = append(report.Items, liveDaemonItem(group, "skipped", group.Reservations[0].Status, group.Operation.Status, false, firstNonEmptyString(reason, "no evidence available")))
		return nil
	}
	worker := ReconcileWorker{Reservation: d.Reservation}
	for _, reservation := range group.Reservations {
		evidence, ok := evidenceByReservation[reservation.ReservationID]
		if !ok {
			report.Skipped++
			continue
		}
		result, err := worker.ReconcileReservation(ctx, reservation.ReservationID, evidence)
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

func liveGroupFromReference(group referenceReservationGroup) LiveOperationGroup {
	return LiveOperationGroup{
		Operation:    group.Operation,
		Reservations: group.Reservations,
	}
}

func liveDaemonItem(group LiveOperationGroup, action string, reservationStatus privacyreservation.ReservationStatus, operationStatus privacyreservation.OperationStatus, requiresReview bool, reason string) ReferenceDaemonItemRunReport {
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

func (d LiveDaemon) leaseOwner() string {
	if d.LeaseOwner != "" {
		return d.LeaseOwner
	}
	return "clairveil-payrolld-live"
}

func (d LiveDaemon) leaseTTL() time.Duration {
	if d.LeaseTTL > 0 {
		return d.LeaseTTL
	}
	return time.Minute
}

func (d LiveDaemon) now() time.Time {
	if d.Now != nil {
		return d.Now().UTC()
	}
	return time.Now().UTC()
}
