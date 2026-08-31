package reservation

type ReservationStatus string

const (
	StatusDiscovered     ReservationStatus = "Discovered"
	StatusAvailable      ReservationStatus = "Available"
	StatusReserved       ReservationStatus = "Reserved"
	StatusProving        ReservationStatus = "Proving"
	StatusProofReady     ReservationStatus = "ProofReady"
	StatusSubmitted      ReservationStatus = "Submitted"
	StatusConfirmedSpent ReservationStatus = "ConfirmedSpent"
	StatusFailed         ReservationStatus = "Failed"
	StatusReplanRequired ReservationStatus = "ReplanRequired"
	StatusReleased       ReservationStatus = "Released"
	StatusUnknown        ReservationStatus = "Unknown"
	StatusManualReview   ReservationStatus = "ManualReview"
)

type OperationStatus string

const (
	OperationStatusPlanned        OperationStatus = "Planned"
	OperationStatusProving        OperationStatus = "Proving"
	OperationStatusProofReady     OperationStatus = "ProofReady"
	OperationStatusSubmitted      OperationStatus = "Submitted"
	OperationStatusSucceeded      OperationStatus = "Succeeded"
	OperationStatusFailed         OperationStatus = "Failed"
	OperationStatusReplanRequired OperationStatus = "ReplanRequired"
	OperationStatusUnknown        OperationStatus = "Unknown"
	OperationStatusManualReview   OperationStatus = "ManualReview"
	OperationStatusConflictSpent  OperationStatus = "ConflictSpent"
)

func IsActiveReservationStatus(status ReservationStatus) bool {
	switch status {
	case StatusReserved, StatusProving, StatusProofReady, StatusSubmitted, StatusUnknown, StatusManualReview:
		return true
	default:
		return false
	}
}

func CanTransitionReservation(from ReservationStatus, to ReservationStatus) bool {
	switch from {
	case StatusDiscovered:
		return to == StatusAvailable || to == StatusFailed
	case StatusAvailable:
		return to == StatusReserved
	case StatusReserved:
		return to == StatusProving || to == StatusReleased || to == StatusReplanRequired || to == StatusManualReview
	case StatusProving:
		return to == StatusProofReady || to == StatusReserved || to == StatusReplanRequired || to == StatusManualReview
	case StatusProofReady:
		return to == StatusSubmitted || to == StatusUnknown || to == StatusConfirmedSpent || to == StatusReplanRequired || to == StatusManualReview
	case StatusSubmitted:
		return to == StatusConfirmedSpent || to == StatusFailed || to == StatusUnknown || to == StatusReplanRequired || to == StatusManualReview
	case StatusUnknown:
		return to == StatusConfirmedSpent || to == StatusFailed || to == StatusReplanRequired || to == StatusManualReview
	case StatusManualReview:
		return to == StatusConfirmedSpent || to == StatusFailed || to == StatusReleased || to == StatusReplanRequired
	case StatusFailed:
		return to == StatusReplanRequired
	case StatusReleased:
		return to == StatusAvailable
	case StatusReplanRequired:
		return to == StatusReserved || to == StatusFailed || to == StatusManualReview
	default:
		return false
	}
}

func RequiresLeaseToken(from ReservationStatus, to ReservationStatus) bool {
	switch {
	case from == StatusReserved && to == StatusProving:
		return true
	case from == StatusProving && to == StatusProofReady:
		return true
	case from == StatusProving && to == StatusReserved:
		return true
	case from == StatusProving && to == StatusReplanRequired:
		return true
	case from == StatusProving && to == StatusManualReview:
		return true
	case from == StatusProofReady && to == StatusSubmitted:
		return true
	case from == StatusProofReady && to == StatusUnknown:
		return true
	case from == StatusProofReady && to == StatusReplanRequired:
		return true
	case from == StatusProofReady && to == StatusManualReview:
		return true
	default:
		return false
	}
}

func CanRecoverAfterLeaseExpiry(from ReservationStatus, to ReservationStatus) bool {
	switch {
	case from == StatusProving && (to == StatusReplanRequired || to == StatusManualReview):
		return true
	case from == StatusProofReady && to == StatusManualReview:
		return true
	default:
		return false
	}
}

func RequiresReconcileEvidence(from ReservationStatus, to ReservationStatus) bool {
	if from == StatusSubmitted || from == StatusUnknown || from == StatusManualReview {
		return true
	}
	return from == StatusProofReady && to == StatusConfirmedSpent
}

func IsTerminalReservationStatus(status ReservationStatus) bool {
	switch status {
	case StatusConfirmedSpent, StatusFailed:
		return true
	default:
		return false
	}
}

func IsTerminalOperationStatus(status OperationStatus) bool {
	switch status {
	case OperationStatusSucceeded, OperationStatusFailed, OperationStatusConflictSpent:
		return true
	default:
		return false
	}
}
