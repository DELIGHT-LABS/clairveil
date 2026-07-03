package payroll

import "errors"

var (
	ErrInvalidPayrollInput = errors.New("invalid payroll input")
	ErrInsufficientNotes   = errors.New("insufficient treasury notes")
	ErrDuplicatePayrollRow = errors.New("duplicate payroll row")
)
