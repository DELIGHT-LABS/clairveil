package payroll

import (
	"errors"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateInputRejectsDuplicateEmployee(t *testing.T) {
	input := testPayrollInput()
	input.Items = append(input.Items, PayrollItemInput{
		ItemID:           "item-2",
		EmployeeID:       "employee-1",
		RecipientAddress: testRecipientAddress("2"),
		Amount:           big.NewInt(7),
	})

	err := ValidateInput(input)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrDuplicatePayrollRow), "expected duplicate row error, got %v", err)
}

func TestValidateInputRejectsCanonicalDuplicateIDs(t *testing.T) {
	input := testPayrollInput()
	input.Items = append(input.Items, PayrollItemInput{
		ItemID:           " item-1 ",
		EmployeeID:       "employee-2",
		RecipientAddress: testRecipientAddress("2"),
		Amount:           big.NewInt(7),
	})

	err := ValidateInput(input)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrDuplicatePayrollRow), "expected duplicate item row error, got %v", err)

	input = testPayrollInput()
	input.Items = append(input.Items, PayrollItemInput{
		ItemID:           "item-2",
		EmployeeID:       " employee-1 ",
		RecipientAddress: testRecipientAddress("2"),
		Amount:           big.NewInt(7),
	})

	err = ValidateInput(input)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrDuplicatePayrollRow), "expected duplicate employee row error, got %v", err)
}

func TestValidateInputRejectsInvalidRecipient(t *testing.T) {
	input := testPayrollInput()
	input.Items[0].RecipientAddress = "cosmos1notshielded"

	err := ValidateInput(input)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrInvalidPayrollInput), "expected invalid input, got %v", err)
}

func TestValidateInputRejectsMalformedShieldedRecipient(t *testing.T) {
	input := testPayrollInput()
	input.Items[0].RecipientAddress = "clairs1notshielded"

	err := ValidateInput(input)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrInvalidPayrollInput), "expected invalid input, got %v", err)
}

func TestValidateInputRejectsNonPositiveAmount(t *testing.T) {
	input := testPayrollInput()
	input.Items[0].Amount = big.NewInt(0)

	err := ValidateInput(input)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrInvalidPayrollInput), "expected invalid input, got %v", err)
}
