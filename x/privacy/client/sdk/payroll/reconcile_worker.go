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
