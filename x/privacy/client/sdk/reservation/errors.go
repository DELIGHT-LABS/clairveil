package reservation

import "errors"

var (
	ErrInvalidReservation      = errors.New("invalid reservation")
	ErrReservationNotFound     = errors.New("reservation not found")
	ErrOperationNotFound       = errors.New("payroll operation not found")
	ErrActiveReservationExists = errors.New("active reservation already exists")
	ErrInvalidTransition       = errors.New("invalid reservation transition")
	ErrCompareAndSetFailed     = errors.New("reservation compare-and-set failed")
	ErrLeaseUnavailable        = errors.New("reservation lease is unavailable")
	ErrLeaseMismatch           = errors.New("reservation lease token mismatch")
	ErrManualReviewRequired    = errors.New("manual review required")
)
