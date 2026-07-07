package payroll

import (
	"fmt"
	"math/big"
	"strings"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func ValidateInput(input PayrollInput) error {
	input = normalizePayrollInput(input)
	if strings.TrimSpace(input.CompanyID) == "" {
		return fmt.Errorf("%w: company_id is required", ErrInvalidPayrollInput)
	}
	if strings.TrimSpace(input.PayrollID) == "" {
		return fmt.Errorf("%w: payroll_id is required", ErrInvalidPayrollInput)
	}
	if strings.TrimSpace(input.Denom) == "" {
		return fmt.Errorf("%w: denom is required", ErrInvalidPayrollInput)
	}
	if err := ValidateDisclosurePolicy(input.DefaultDisclosurePolicy); err != nil {
		return err
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
		if _, err := privacytypes.DecodeShieldedAddressBundle(item.RecipientAddress); err != nil {
			return fmt.Errorf("%w: item %s recipient must be a valid shielded address with prefix %s: %v", ErrInvalidPayrollInput, item.ItemID, privacytypes.ShieldedBech32Prefix, err)
		}
		if item.Amount == nil || item.Amount.Cmp(big.NewInt(0)) <= 0 {
			return fmt.Errorf("%w: item %s amount must be positive", ErrInvalidPayrollInput, item.ItemID)
		}
		if item.Denom != "" && item.Denom != input.Denom {
			return fmt.Errorf("%w: item %s denom %s does not match payroll denom %s", ErrInvalidPayrollInput, item.ItemID, item.Denom, input.Denom)
		}
		if err := ValidateDisclosurePolicy(effectiveDisclosurePolicy(input.DefaultDisclosurePolicy, item.DisclosurePolicy)); err != nil {
			return fmt.Errorf("item %s disclosure policy: %w", item.ItemID, err)
		}
	}
	return nil
}

func normalizePayrollInput(input PayrollInput) PayrollInput {
	input.CompanyID = strings.TrimSpace(input.CompanyID)
	input.PayrollID = strings.TrimSpace(input.PayrollID)
	input.BatchID = strings.TrimSpace(input.BatchID)
	input.Denom = strings.TrimSpace(input.Denom)
	input.DefaultDisclosurePolicy = normalizeDisclosurePolicy(input.DefaultDisclosurePolicy)
	input.Items = append([]PayrollItemInput(nil), input.Items...)
	for i := range input.Items {
		input.Items[i] = normalizePayrollItemInput(input.Items[i], input.DefaultDisclosurePolicy)
	}
	return input
}

func normalizePayrollItemInput(item PayrollItemInput, defaultDisclosure PayrollDisclosurePolicy) PayrollItemInput {
	item.ItemID = strings.TrimSpace(item.ItemID)
	item.EmployeeID = strings.TrimSpace(item.EmployeeID)
	item.RecipientAddress = strings.TrimSpace(item.RecipientAddress)
	item.Denom = strings.TrimSpace(item.Denom)
	item.DisclosurePolicy = effectiveDisclosurePolicy(defaultDisclosure, item.DisclosurePolicy)
	item.ExpectedOutputCommitment = strings.TrimSpace(item.ExpectedOutputCommitment)
	item.ExpectedDisclosureDigest = strings.TrimSpace(item.ExpectedDisclosureDigest)
	return item
}
