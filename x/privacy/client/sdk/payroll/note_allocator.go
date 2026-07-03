package payroll

import (
	"encoding/base64"
	"fmt"
	"math/big"
	"sort"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

type NoteAllocator struct{}

func (a NoteAllocator) Allocate(input PayrollInput, notes []TreasuryNote) ([]PayrollPlanItem, error) {
	if err := ValidateInput(input); err != nil {
		return nil, err
	}

	available := filterAvailableNotes(input.Denom, notes)
	used := make(map[string]struct{}, len(available))
	planned := make([]PayrollPlanItem, 0, len(input.Items))

	for itemIndex, item := range input.Items {
		selected, err := selectInputNotes(item.Amount, available, used)
		if err != nil {
			return nil, fmt.Errorf("%w: item %s needs %s%s", err, item.ItemID, item.Amount.String(), input.Denom)
		}
		for _, note := range selected {
			used[note.NoteID] = struct{}{}
		}

		itemDenom := item.Denom
		if itemDenom == "" {
			itemDenom = input.Denom
		}
		planned = append(planned, PayrollPlanItem{
			CompanyID:                input.CompanyID,
			PayrollID:                input.PayrollID,
			BatchID:                  input.BatchID,
			Attempt:                  input.Attempt,
			ItemID:                   item.ItemID,
			EmployeeID:               item.EmployeeID,
			OperationID:              operationID(input.PayrollID, item.ItemID, input.Attempt),
			RecipientAddress:         item.RecipientAddress,
			ExpectedRecipientHash:    HashRecipient(item.RecipientAddress),
			Amount:                   cloneBigInt(item.Amount),
			ExpectedAmountHash:       HashAmount(itemDenom, item.Amount),
			Denom:                    itemDenom,
			ExpectedOutputCommitment: item.ExpectedOutputCommitment,
			ExpectedDisclosureDigest: item.ExpectedDisclosureDigest,
			InputNotes:               selected,
			Status:                   ItemStatusPlanned,
			RetryCount:               0,
			ChunkID:                  chunkID(input.PayrollID, input.Attempt, itemIndex),
		})
	}

	return planned, nil
}

func filterAvailableNotes(denom string, notes []TreasuryNote) []TreasuryNote {
	available := make([]TreasuryNote, 0, len(notes))
	for _, note := range notes {
		if note.IsSpent || note.ReservationID != "" || note.Denom != denom {
			continue
		}
		if note.Amount == nil {
			continue
		}
		available = append(available, note)
	}
	sort.Slice(available, func(i, j int) bool {
		if cmp := available[i].Amount.Cmp(available[j].Amount); cmp != 0 {
			return cmp < 0
		}
		return available[i].NoteID < available[j].NoteID
	})
	return cloneTreasuryNotes(available)
}

func selectInputNotes(target *big.Int, available []TreasuryNote, used map[string]struct{}) ([]TreasuryNote, error) {
	if target == nil || target.Sign() <= 0 {
		return nil, fmt.Errorf("%w: target amount must be positive", ErrInvalidPayrollInput)
	}

	zeroIndex := -1
	for i, note := range available {
		if _, ok := used[note.NoteID]; ok {
			continue
		}
		if note.Amount.Sign() == 0 {
			zeroIndex = i
			break
		}
	}
	if zeroIndex >= 0 {
		for i, note := range available {
			if i == zeroIndex {
				continue
			}
			if _, ok := used[note.NoteID]; ok {
				continue
			}
			if finalPayrollOutputsWithinBound(note.Amount, target) {
				return cloneTreasuryNotes([]TreasuryNote{note, available[zeroIndex]}), nil
			}
		}
	}

	bestTotal := (*big.Int)(nil)
	var best []TreasuryNote
	for i := 0; i < len(available); i++ {
		if _, ok := used[available[i].NoteID]; ok || available[i].Amount.Sign() == 0 {
			continue
		}
		for j := i + 1; j < len(available); j++ {
			if _, ok := used[available[j].NoteID]; ok || available[j].Amount.Sign() == 0 {
				continue
			}
			total := new(big.Int).Add(available[i].Amount, available[j].Amount)
			if !finalPayrollOutputsWithinBound(total, target) {
				continue
			}
			if bestTotal == nil || total.Cmp(bestTotal) < 0 {
				bestTotal = total
				best = []TreasuryNote{available[i], available[j]}
			}
		}
	}
	if len(best) > 0 {
		return cloneTreasuryNotes(best), nil
	}

	return nil, ErrInsufficientNotes
}

func finalPayrollOutputsWithinBound(total *big.Int, target *big.Int) bool {
	if total == nil || target == nil {
		return false
	}
	maxOutputAmount := privacytypes.MaxShieldedAmount()
	if target.Sign() <= 0 || target.Cmp(maxOutputAmount) > 0 {
		return false
	}
	if total.Cmp(target) < 0 {
		return false
	}
	change := new(big.Int).Sub(total, target)
	return change.Cmp(maxOutputAmount) <= 0
}

func operationID(payrollID string, itemID string, attempt int) string {
	base := "payroll:" + idComponent(payrollID) + ":item:" + idComponent(itemID)
	if attempt <= 0 {
		return base
	}
	return fmt.Sprintf("%s:attempt:%03d", base, attempt)
}

func chunkID(payrollID string, attempt int, itemIndex int) string {
	if attempt <= 0 {
		return fmt.Sprintf("payroll:%s:chunk:%06d", idComponent(payrollID), itemIndex)
	}
	return fmt.Sprintf("payroll:%s:attempt:%03d:chunk:%06d", idComponent(payrollID), attempt, itemIndex)
}

func idComponent(value string) string {
	if value == "" {
		return "_"
	}
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}
