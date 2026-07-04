package payroll

import (
	"errors"
	"fmt"
)

var (
	ErrInvalidPayrollInput = errors.New("invalid payroll input")
	ErrInsufficientNotes   = errors.New("insufficient treasury notes")
	ErrDuplicatePayrollRow = errors.New("duplicate payroll row")
)

type SpentNullifierError struct {
	NullifierHex string
}

func (e SpentNullifierError) Error() string {
	return fmt.Sprintf("nullifier %s is already spent", e.NullifierHex)
}
