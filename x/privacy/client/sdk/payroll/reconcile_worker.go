package payroll

import (
	"context"
	"fmt"

	privacyreservation "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/reservation"
)

type ReconcileWorker struct {
	Reservation privacyreservation.Service
}

func (w ReconcileWorker) ReconcileReservation(ctx context.Context, reservationID string, evidence privacyreservation.OperationEvidence) (*privacyreservation.ReconcileResult, error) {
	if w.Reservation.Store == nil {
		return nil, fmt.Errorf("reservation service is required")
	}
	return w.Reservation.Reconcile(ctx, reservationID, evidence)
}

// ReconcileOperation atomically reconciles every input reservation that
// belongs to one payroll operation.
func (w ReconcileWorker) ReconcileOperation(ctx context.Context, operationID string, evidences []privacyreservation.OperationReservationEvidence) (*privacyreservation.ReconcileResult, error) {
	if w.Reservation.Store == nil {
		return nil, fmt.Errorf("reservation service is required")
	}
	return w.Reservation.ReconcileOperation(ctx, operationID, evidences)
}
