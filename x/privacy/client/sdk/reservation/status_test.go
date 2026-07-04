package reservation

import "testing"

func TestReservationStatusContract(t *testing.T) {
	activeStatuses := []ReservationStatus{
		StatusReserved,
		StatusProving,
		StatusProofReady,
		StatusSubmitted,
		StatusUnknown,
		StatusManualReview,
	}
	for _, status := range activeStatuses {
		if !IsActiveReservationStatus(status) {
			t.Fatalf("expected %s to be active", status)
		}
	}

	if !CanTransitionReservation(StatusReserved, StatusProving) {
		t.Fatalf("Reserved -> Proving should be allowed")
	}
	if CanTransitionReservation(StatusReserved, StatusReserved) {
		t.Fatalf("Reserved -> Reserved self-transition should not be allowed")
	}
	if CanTransitionReservation(StatusSubmitted, StatusAvailable) {
		t.Fatalf("Submitted -> Available should not be allowed")
	}
	if !CanTransitionReservation(StatusProofReady, StatusConfirmedSpent) {
		t.Fatalf("ProofReady -> ConfirmedSpent should be allowed for reconcile recovery")
	}
}
