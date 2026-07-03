package payroll

import (
	"fmt"
	"math/big"
	"strings"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func ValidateInput(input PayrollInput) error {
	if strings.TrimSpace(input.CompanyID) == "" {
		return fmt.Errorf("%w: company_id is required", ErrInvalidPayrollInput)
	}
	if strings.TrimSpace(input.PayrollID) == "" {
		return fmt.Errorf("%w: payroll_id is required", ErrInvalidPayrollInput)
	}
	if strings.TrimSpace(input.Denom) == "" {
		return fmt.Errorf("%w: denom is required", ErrInvalidPayrollInput)
	}
	if len(input.Items) == 0 {
		return fmt.Errorf("%w: at least one payroll item is required", ErrInvalidPayrollInput)
	}

	seenItemIDs := make(map[string]struct{}, len(input.Items))
	seenEmployees := make(map[string]struct{}, len(input.Items))
	for i, item := range input.Items {
		if strings.TrimSpace(item.ItemID) == "" {
			return fmt.Errorf("%w: item %d item_id is required", ErrInvalidPayrollInput, i)
		}
		if _, exists := seenItemIDs[item.ItemID]; exists {
			return fmt.Errorf("%w: item_id %s", ErrDuplicatePayrollRow, item.ItemID)
		}
		seenItemIDs[item.ItemID] = struct{}{}

		if strings.TrimSpace(item.EmployeeID) != "" {
			if _, exists := seenEmployees[item.EmployeeID]; exists {
				return fmt.Errorf("%w: employee_id %s", ErrDuplicatePayrollRow, item.EmployeeID)
			}
			seenEmployees[item.EmployeeID] = struct{}{}
		}
		if !strings.HasPrefix(strings.TrimSpace(item.RecipientAddress), privacytypes.ShieldedBech32Prefix) {
			return fmt.Errorf("%w: item %s recipient must use shielded prefix %s", ErrInvalidPayrollInput, item.ItemID, privacytypes.ShieldedBech32Prefix)
		}
		if item.Amount == nil || item.Amount.Cmp(big.NewInt(0)) <= 0 {
			return fmt.Errorf("%w: item %s amount must be positive", ErrInvalidPayrollInput, item.ItemID)
		}
		itemDenom := strings.TrimSpace(item.Denom)
		if itemDenom != "" && itemDenom != input.Denom {
			return fmt.Errorf("%w: item %s denom %s does not match payroll denom %s", ErrInvalidPayrollInput, item.ItemID, itemDenom, input.Denom)
		}
	}
	return nil
}
