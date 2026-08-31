package payroll

import (
	"errors"
	"fmt"
)

type ManualReviewBroadcastError struct {
	Cause error
}

func (e *ManualReviewBroadcastError) Error() string {
	if e == nil || e.Cause == nil {
		return "broadcast result requires manual review"
	}
	return fmt.Sprintf("broadcast result requires manual review: %v", e.Cause)
}

func (e *ManualReviewBroadcastError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

var (
	ErrInvalidPayrollInput = errors.New("invalid payroll input")
	ErrInsufficientNotes   = errors.New("insufficient treasury notes")
	ErrDuplicatePayrollRow = errors.New("duplicate payroll row")
)

type SpentNullifierError struct{}

func (SpentNullifierError) Error() string {
	return "an input nullifier is already spent"
}
