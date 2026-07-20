package payroll

import (
	"context"
	"errors"
	"fmt"
	"time"

	privacyreservation "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/reservation"
)

var ErrLiveDaemonSkip = errors.New("live daemon operation skipped")

// LiveBroadcastSubmit crosses the external submission boundary. Implementations
// must return ErrLiveDaemonSkip from PrepareBroadcastProofReady, never from
// this callback: the daemon records the durable attempt immediately before it
// invokes the callback.
type LiveBroadcastSubmit func(context.Context) (*BroadcastResult, error)

type LiveOperationExecutor interface {
	BuildProofReady(ctx context.Context, group LiveOperationGroup) (privacyreservation.ProofReadyOperationUpdate, string, error)
	PrepareBroadcastProofReady(ctx context.Context, group LiveOperationGroup) (LiveBroadcastSubmit, string, error)
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
	ownedProofReadyLeases := make(map[string]string)
	for _, status := range []privacyreservation.ReservationStatus{
		privacyreservation.StatusReserved,
		privacyreservation.StatusProving,
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
			if err := d.processGroup(ctx, status, liveGroupFromReference(group), report, ownedProofReadyLeases); err != nil {
				return nil, err
			}
			processed++
		}
	}
	report.FinishedAt = d.now()
	return report, nil
}

func (d LiveDaemon) processGroup(ctx context.Context, status privacyreservation.ReservationStatus, group LiveOperationGroup, report *ReferenceDaemonRunReport, ownedProofReadyLeases map[string]string) error {
	switch status {
	case privacyreservation.StatusReserved:
		if mixed, err := referenceDaemonHasActiveReservationsOutsideStatus(ctx, d.Reservation.Store, group.Operation.OperationID, status); err != nil {
			return err
		} else if mixed {
			report.Skipped++
			report.Items = append(report.Items, liveDaemonItem(group, "skipped", status, group.Operation.Status, false, "operation has active reservations in another status"))
			return nil
		}
		return d.buildProofReady(ctx, group, report, ownedProofReadyLeases)
	case privacyreservation.StatusProving:
		return d.rollbackExpiredProving(ctx, group, report)
	case privacyreservation.StatusProofReady:
		if mixed, err := referenceDaemonHasActiveReservationsOutsideStatus(ctx, d.Reservation.Store, group.Operation.OperationID, status); err != nil {
			return err
		} else if mixed {
			report.Skipped++
			report.Items = append(report.Items, liveDaemonItem(group, "skipped", status, group.Operation.Status, false, "operation has active reservations in another status"))
			return nil
		}
		if proofReadyBroadcastAttemptPending(group.Reservations) {
			return d.recoverExpiredProofReadyBroadcastAttempt(ctx, group, report)
		}
		return d.broadcastProofReady(ctx, group, report, ownedProofReadyLeases)
	case privacyreservation.StatusSubmitted, privacyreservation.StatusUnknown:
		return d.reconcileSubmitted(ctx, group, report)
	default:
		report.Skipped++
		report.Items = append(report.Items, liveDaemonItem(group, "skipped", status, group.Operation.Status, false, "unsupported status"))
		return nil
	}
}

func (d LiveDaemon) recoverExpiredProofReadyBroadcastAttempt(ctx context.Context, group LiveOperationGroup, report *ReferenceDaemonRunReport) error {
	updated, err := d.Reservation.RecoverOperationAfterLeaseExpiry(
		ctx,
		group.Operation.OperationID,
		referenceReservationIDs(group.Reservations),
		privacyreservation.StatusProofReady,
		privacyreservation.StatusManualReview,
	)
	if err != nil {
		if errors.Is(err, privacyreservation.ErrLeaseUnavailable) || errors.Is(err, privacyreservation.ErrLeaseMismatch) || errors.Is(err, privacyreservation.ErrCompareAndSetFailed) {
			report.Skipped++
			report.Items = append(report.Items, liveDaemonItem(group, "skipped", privacyreservation.StatusProofReady, group.Operation.Status, false, "proof-ready broadcast attempt is owned by another worker"))
			return nil
		}
		return err
	}
	report.RequiresReview++
	report.Items = append(report.Items, liveDaemonItem(LiveOperationGroup{Operation: group.Operation, Reservations: updated}, "manual-review", privacyreservation.StatusManualReview, privacyreservation.OperationStatusManualReview, true, "expired proof-ready broadcast attempt requires manual reconciliation"))
	return nil
}

func (d LiveDaemon) buildProofReady(ctx context.Context, group LiveOperationGroup, report *ReferenceDaemonRunReport, ownedProofReadyLeases map[string]string) (runErr error) {
	refs, _, err := d.Reservation.BeginProvingOperation(
		ctx,
		group.Operation.OperationID,
		referenceReservationIDs(group.Reservations),
		d.leaseOwner(),
		d.leaseTTL(),
	)
	if err != nil {
		if errors.Is(err, privacyreservation.ErrLeaseUnavailable) || errors.Is(err, privacyreservation.ErrLeaseMismatch) || errors.Is(err, privacyreservation.ErrCompareAndSetFailed) {
			report.Skipped++
			report.Items = append(report.Items, liveDaemonItem(group, "skipped", privacyreservation.StatusReserved, group.Operation.Status, false, "reserved lease is owned by another worker"))
			return nil
		}
		return err
	}
	leases := make(map[string]string, len(refs))
	for _, ref := range refs {
		leases[ref.ReservationID] = ref.LeaseToken
	}
	rollbackRequired := true
	defer func() {
		if !rollbackRequired || len(refs) == 0 {
			return
		}
		if _, _, rollbackErr := d.Reservation.RollbackProvingOperation(ctx, group.Operation.OperationID, refs); rollbackErr != nil {
			runErr = errors.Join(runErr, rollbackErr)
		}
	}()

	proofCtx, stopHeartbeat := (ProofWorker{Reservation: d.Reservation}).startProvingHeartbeat(ctx, leases, reservationIDsFromRefs(refs), d.leaseTTL(), d.leaseOwner())
	defer func() {
		if stopHeartbeat == nil {
			return
		}
		if heartbeatErr := stopHeartbeat(); heartbeatErr != nil {
			runErr = errors.Join(runErr, heartbeatErr)
		}
	}()

	update, reason, err := d.Executor.BuildProofReady(proofCtx, group)
	if err != nil {
		if errors.Is(err, ErrLiveDaemonSkip) {
			report.Skipped++
			report.Items = append(report.Items, liveDaemonItem(group, "skipped", privacyreservation.StatusReserved, group.Operation.Status, false, firstNonEmptyString(reason, err.Error())))
			return nil
		}
		return err
	}
	if update.OperationID == "" {
		update.OperationID = group.Operation.OperationID
	}
	if heartbeatErr := stopHeartbeat(); heartbeatErr != nil {
		stopHeartbeat = nil
		return heartbeatErr
	}
	stopHeartbeat = nil
	reservations, operation, err := d.Reservation.MarkProofReadyBatch(ctx, refs, update)
	if err != nil {
		return err
	}
	rollbackRequired = false
	for _, ref := range refs {
		if ref.ReservationID != "" && ref.LeaseToken != "" {
			ownedProofReadyLeases[ref.ReservationID] = ref.LeaseToken
		}
	}
	report.ProofReady++
	operationStatus := group.Operation.Status
	if operation != nil {
		operationStatus = operation.Status
	}
	report.Items = append(report.Items, referenceDaemonItem(referenceReservationGroup{Operation: group.Operation, Reservations: reservations}, "proof-ready", privacyreservation.StatusProofReady, operationStatus, false, firstNonEmptyString(reason, "proof artifact stored")))
	return nil
}

func (d LiveDaemon) rollbackExpiredProving(ctx context.Context, group LiveOperationGroup, report *ReferenceDaemonRunReport) error {
	updated, err := d.Reservation.RecoverOperationAfterLeaseExpiry(
		ctx,
		group.Operation.OperationID,
		referenceReservationIDs(group.Reservations),
		privacyreservation.StatusProving,
		privacyreservation.StatusReplanRequired,
	)
	if err != nil {
		if errors.Is(err, privacyreservation.ErrLeaseUnavailable) || errors.Is(err, privacyreservation.ErrLeaseMismatch) || errors.Is(err, privacyreservation.ErrCompareAndSetFailed) {
			report.Skipped++
			report.Items = append(report.Items, liveDaemonItem(group, "skipped", privacyreservation.StatusProving, group.Operation.Status, false, "proving lease is owned by another worker"))
			return nil
		}
		return err
	}
	report.Skipped++
	report.Items = append(report.Items, liveDaemonItem(LiveOperationGroup{Operation: group.Operation, Reservations: updated}, "replan-required", privacyreservation.StatusReplanRequired, privacyreservation.OperationStatusReplanRequired, false, "expired proving reservations require a new plan"))
	return nil
}

func reservationIDsFromRefs(refs []privacyreservation.SubmittedReservationRef) []string {
	ids := make([]string, 0, len(refs))
	for _, ref := range refs {
		if ref.ReservationID != "" {
			ids = append(ids, ref.ReservationID)
		}
	}
	return ids
}

func (d LiveDaemon) broadcastProofReady(ctx context.Context, group LiveOperationGroup, report *ReferenceDaemonRunReport, ownedProofReadyLeases map[string]string) error {
	refs, err := d.proofReadyRefs(ctx, group, ownedProofReadyLeases)
	if err != nil {
		if errors.Is(err, privacyreservation.ErrLeaseUnavailable) || errors.Is(err, privacyreservation.ErrLeaseMismatch) || errors.Is(err, privacyreservation.ErrCompareAndSetFailed) {
			report.Skipped++
			report.Items = append(report.Items, liveDaemonItem(group, "skipped", privacyreservation.StatusProofReady, group.Operation.Status, false, "proof-ready lease is owned by another worker"))
			return nil
		}
		return err
	}
	submit, reason, err := d.Executor.PrepareBroadcastProofReady(ctx, group)
	if err != nil {
		if errors.Is(err, ErrLiveDaemonSkip) {
			report.Skipped++
			report.Items = append(report.Items, liveDaemonItem(group, "skipped", privacyreservation.StatusProofReady, group.Operation.Status, false, firstNonEmptyString(reason, err.Error())))
			return nil
		}
		return err
	}
	if submit == nil {
		return fmt.Errorf("live executor returned nil broadcast submission")
	}
	submitInvoked := false
	terminalFailureRecorded := false
	_, err = broadcastWithSubmissionLeaseHeartbeat(ctx, d.Reservation, refs, d.leaseTTL(), func(broadcastCtx context.Context, commit submissionLeaseCommit, _ submissionLeaseRefresh) (*BroadcastResult, error) {
		if _, _, markErr := d.Reservation.MarkBroadcastAttempting(broadcastCtx, refs, []string{group.Operation.OperationID}, privacyreservation.BroadcastAttemptStart{
			Reason: "live payroll broadcast boundary crossed",
		}); markErr != nil {
			return nil, markErr
		}
		submitInvoked = true
		broadcast, submitErr := submit(broadcastCtx)
		if submitErr != nil {
			if recordErr := d.recordBroadcastFailure(commit, group, refs, report, broadcast, submitErr, reason); recordErr != nil {
				return broadcast, errors.Join(submitErr, recordErr)
			}
			terminalFailureRecorded = true
			return broadcast, submitErr
		}
		if broadcast == nil {
			nilResultErr := fmt.Errorf("live executor returned nil broadcast result")
			if err := d.recordBroadcastFailure(commit, group, refs, report, nil, nilResultErr, reason); err != nil {
				return nil, errors.Join(nilResultErr, err)
			}
			terminalFailureRecorded = true
			return nil, nil
		}
		if broadcast.Code != 0 {
			broadcastErr := broadcastCodeError(broadcast)
			if err := d.recordBroadcastFailure(commit, group, refs, report, broadcast, broadcastErr, reason); err != nil {
				return broadcast, errors.Join(broadcastErr, err)
			}
			terminalFailureRecorded = true
			return broadcast, nil
		}
		if err := d.recordBroadcastSuccess(commit, group, refs, report, broadcast, reason); err != nil {
			return broadcast, err
		}
		return broadcast, nil
	})
	if err != nil {
		if !submitInvoked || !terminalFailureRecorded {
			return err
		}
	}
	return nil
}

func (d LiveDaemon) proofReadyRefs(ctx context.Context, group LiveOperationGroup, ownedProofReadyLeases map[string]string) ([]privacyreservation.SubmittedReservationRef, error) {
	owned := len(group.Reservations) > 0
	for _, reservation := range group.Reservations {
		token := ownedProofReadyLeases[reservation.ReservationID]
		if reservation.LeaseOwner == "" || token == "" || token != reservation.LeaseToken {
			owned = false
			break
		}
	}
	if !owned {
		refs, _, err := d.Reservation.ReclaimExpiredOperation(
			ctx,
			group.Operation.OperationID,
			referenceReservationIDs(group.Reservations),
			privacyreservation.StatusProofReady,
			d.leaseOwner(),
			d.leaseTTL(),
		)
		if err != nil {
			return nil, err
		}
		for _, ref := range refs {
			if ownedProofReadyLeases != nil {
				ownedProofReadyLeases[ref.ReservationID] = ref.LeaseToken
			}
		}
		return refs, nil
	}

	refs := make([]privacyreservation.SubmittedReservationRef, 0, len(group.Reservations))
	for _, reservation := range group.Reservations {
		lease, err := d.Reservation.HeartbeatLeaseForStatus(ctx, reservation.ReservationID, reservation.LeaseOwner, ownedProofReadyLeases[reservation.ReservationID], privacyreservation.StatusProofReady, d.leaseTTL())
		if err != nil {
			return nil, err
		}
		refs = append(refs, privacyreservation.SubmittedReservationRef{ReservationID: reservation.ReservationID, LeaseOwner: lease.Owner, LeaseToken: lease.Token})
		if ownedProofReadyLeases != nil {
			ownedProofReadyLeases[reservation.ReservationID] = lease.Token
		}
	}
	return refs, nil
}

func (d LiveDaemon) recordBroadcastSuccess(commit submissionLeaseCommit, group LiveOperationGroup, refs []privacyreservation.SubmittedReservationRef, report *ReferenceDaemonRunReport, broadcast *BroadcastResult, reason string) error {
	return commit(func(terminalCtx context.Context, heartbeatErr error) error {
		lastBroadcastError := ""
		if heartbeatErr != nil {
			lastBroadcastError = heartbeatErr.Error()
		}
		reservations, operations, err := d.Reservation.MarkSubmittedBatch(terminalCtx, refs, []string{group.Operation.OperationID}, privacyreservation.SubmittedReservationUpdate{
			TxHash:             broadcast.TxHash,
			TxBytesHash:        broadcast.TxBytesHash,
			SignDocHash:        broadcast.SignDocHash,
			AccountSequence:    broadcast.AccountSequence,
			LastBroadcastError: lastBroadcastError,
		})
		if err != nil {
			if submittedStateMatches(terminalCtx, d.Reservation.Store, refs, broadcast) {
				report.Submitted++
				report.Items = append(report.Items, liveDaemonItem(group, "submitted", privacyreservation.StatusSubmitted, privacyreservation.OperationStatusSubmitted, false, firstNonEmptyString(reason, "tx broadcast")))
				return nil
			}
			fallbackErr := markProofResultsBroadcastUnknown(terminalCtx, d.Reservation, refs, []string{group.Operation.OperationID}, broadcast, errors.Join(fmt.Errorf("submitted bookkeeping failed: %w", err), heartbeatErr))
			return errors.Join(err, fallbackErr)
		}
		operationStatus := privacyreservation.OperationStatusSubmitted
		if len(operations) > 0 {
			operationStatus = operations[0].Status
		}
		report.Submitted++
		report.Items = append(report.Items, referenceDaemonItem(referenceReservationGroup{Operation: group.Operation, Reservations: reservations}, "submitted", privacyreservation.StatusSubmitted, operationStatus, false, firstNonEmptyString(reason, "tx broadcast")))
		return nil
	})
}

func (d LiveDaemon) recordBroadcastFailure(commit submissionLeaseCommit, group LiveOperationGroup, refs []privacyreservation.SubmittedReservationRef, report *ReferenceDaemonRunReport, broadcast *BroadcastResult, broadcastErr error, reason string) error {
	return commit(func(terminalCtx context.Context, heartbeatErr error) error {
		terminalErr := errors.Join(broadcastErr, heartbeatErr)
		operationIDs := []string{group.Operation.OperationID}
		action := "broadcast-ambiguous"
		reservationStatus := privacyreservation.StatusManualReview
		operationStatus := privacyreservation.OperationStatusManualReview
		if broadcastHasAttemptIdentity(broadcast) {
			action = "broadcast-unknown"
			reservationStatus = privacyreservation.StatusUnknown
			operationStatus = privacyreservation.OperationStatusUnknown
			if _, _, err := d.Reservation.MarkBroadcastUnknownBatch(terminalCtx, refs, operationIDs, privacyreservation.BroadcastAttemptUpdate{
				TxHash:             broadcast.TxHash,
				TxBytesHash:        broadcast.TxBytesHash,
				SignDocHash:        broadcast.SignDocHash,
				AccountSequence:    broadcast.AccountSequence,
				LastBroadcastError: terminalErr.Error(),
			}); err != nil {
				return err
			}
		} else if _, _, err := d.Reservation.MarkBroadcastAmbiguousBatch(terminalCtx, refs, operationIDs, privacyreservation.BroadcastAmbiguityUpdate{
			LastBroadcastError: terminalErr.Error(),
		}); err != nil {
			return err
		}
		report.RequiresReview++
		report.Items = append(report.Items, liveDaemonItem(group, action, reservationStatus, operationStatus, true, firstNonEmptyString(reason, terminalErr.Error())))
		return nil
	})
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
	evidences := make([]privacyreservation.OperationReservationEvidence, 0, len(group.Reservations))
	for _, reservation := range group.Reservations {
		evidence, ok := evidenceByReservation[reservation.ReservationID]
		if !ok {
			report.Skipped++
			report.Items = append(report.Items, liveDaemonItem(group, "skipped", group.Reservations[0].Status, group.Operation.Status, false, "operation reconciliation requires evidence for every reservation"))
			return nil
		}
		evidences = append(evidences, privacyreservation.OperationReservationEvidence{
			ReservationID: reservation.ReservationID,
			Evidence:      evidence,
		})
	}
	result, err := (ReconcileWorker{Reservation: d.Reservation}).ReconcileOperation(ctx, group.Operation.OperationID, evidences)
	if err != nil {
		return err
	}
	if result.RequiresReview {
		report.RequiresReview += len(group.Reservations)
	}
	report.Reconciled += len(group.Reservations)
	report.Items = append(report.Items, liveDaemonItem(group, "reconciled", result.ReservationStatus, result.OperationStatus, result.RequiresReview, result.Reason))
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
