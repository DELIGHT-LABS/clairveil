package payroll

import (
	"errors"
	"fmt"
	"math/big"
	"sort"

	privacybatchtransfer "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/batchtransfer"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

type BatchPayrollPlan struct {
	CompanyID  string
	PayrollID  string
	BatchID    string
	Denom      string
	Attempt    int
	Operations []BatchPayrollOperationPlan
}

type BatchPayrollOperationPlan struct {
	OperationID  string
	Items        []PayrollPlanItem
	InputNotes   []TreasuryNote
	InputTotal   *big.Int
	PaymentTotal *big.Int
	Change       *big.Int
	OutputCount  int
	HasChange    bool
}

type BatchPayrollPlanner struct{}

// Plan groups payments under the frozen 16-input/32-output limits. It never
// schedules an implicit merge: an item that cannot be funded by at most
// sixteen currently available notes returns ErrPreparationRequired.
func (BatchPayrollPlanner) Plan(input PayrollInput, treasuryNotes []TreasuryNote) (*BatchPayrollPlan, error) {
	input = normalizePayrollInput(input)
	if err := ValidateInput(input); err != nil {
		return nil, err
	}
	available := filterAvailableNotes(input.Denom, treasuryNotes)
	plan := &BatchPayrollPlan{CompanyID: input.CompanyID, PayrollID: input.PayrollID, BatchID: input.BatchID, Denom: input.Denom, Attempt: input.Attempt}
	itemOffset := 0
	for itemOffset < len(input.Items) {
		maxPayments := len(input.Items) - itemOffset
		if maxPayments > int(privacytypes.BatchJoinSplitV1MaxOutputs) {
			maxPayments = int(privacytypes.BatchJoinSplitV1MaxOutputs)
		}

		var selected []TreasuryNote
		var selectedTotal, paymentTotal *big.Int
		paymentCount := 0
		for candidateCount := maxPayments; candidateCount >= 1; candidateCount-- {
			candidatePaymentTotal := sumPayrollItemAmounts(input.Items[itemOffset : itemOffset+candidateCount])
			candidateInputs, candidateInputTotal, ok := selectBatchTreasuryInputs(available, candidatePaymentTotal)
			if !ok {
				continue
			}
			change := new(big.Int).Sub(candidateInputTotal, candidatePaymentTotal)
			if change.Cmp(privacytypes.MaxShieldedAmount()) > 0 {
				continue
			}
			if candidateCount == int(privacytypes.BatchJoinSplitV1MaxOutputs) && change.Sign() != 0 {
				// A change output consumes slot 32, so this candidate must be
				// reduced to at most 31 payments.
				continue
			}
			selected, selectedTotal, paymentTotal, paymentCount = candidateInputs, candidateInputTotal, candidatePaymentTotal, candidateCount
			break
		}
		if paymentCount == 0 {
			return nil, fmt.Errorf("%w: payroll item %s cannot be funded by 1..16 current notes", privacybatchtransfer.ErrPreparationRequired, input.Items[itemOffset].ItemID)
		}

		operationIndex := len(plan.Operations)
		operationID := batchPayrollOperationID(input, operationIndex)
		items := make([]PayrollPlanItem, paymentCount)
		for i := 0; i < paymentCount; i++ {
			item := input.Items[itemOffset+i]
			items[i] = PayrollPlanItem{
				CompanyID: input.CompanyID, PayrollID: input.PayrollID, BatchID: input.BatchID, Attempt: input.Attempt,
				ChunkID: chunkID(input.CompanyID, input.BatchID, input.PayrollID, input.Attempt, operationIndex),
				ItemID:  item.ItemID, EmployeeID: item.EmployeeID, OperationID: operationID,
				RecipientAddress: item.RecipientAddress, ExpectedRecipientHash: HashRecipient(item.RecipientAddress),
				Amount: cloneBigInt(item.Amount), ExpectedAmountHash: HashAmount(input.Denom, item.Amount), Denom: input.Denom,
				DisclosurePolicy: item.DisclosurePolicy, ExpectedOutputCommitment: item.ExpectedOutputCommitment,
				ExpectedDisclosureDigest: preferredExpectedDisclosureDigest(item), Status: ItemStatusPlanned,
			}
		}
		change := new(big.Int).Sub(selectedTotal, paymentTotal)
		outputCount := paymentCount
		if change.Sign() > 0 {
			outputCount++
		}
		plan.Operations = append(plan.Operations, BatchPayrollOperationPlan{
			OperationID: operationID, Items: items, InputNotes: cloneTreasuryNotes(selected),
			InputTotal: new(big.Int).Set(selectedTotal), PaymentTotal: new(big.Int).Set(paymentTotal), Change: change,
			OutputCount: outputCount, HasChange: change.Sign() > 0,
		})
		available = removeBatchTreasuryInputs(available, selected)
		itemOffset += paymentCount
	}
	return plan, nil
}

func selectBatchTreasuryInputs(available []TreasuryNote, target *big.Int) ([]TreasuryNote, *big.Int, bool) {
	if target == nil || target.Sign() <= 0 {
		return nil, nil, false
	}
	byOwner := make(map[string][]TreasuryNote)
	ownerIDs := make([]string, 0)
	for _, note := range available {
		if note.Amount == nil || note.Amount.Sign() <= 0 || note.OwnerKeyID == "" {
			continue
		}
		if _, exists := byOwner[note.OwnerKeyID]; !exists {
			ownerIDs = append(ownerIDs, note.OwnerKeyID)
		}
		byOwner[note.OwnerKeyID] = append(byOwner[note.OwnerKeyID], note)
	}
	sort.Strings(ownerIDs)
	for _, ownerID := range ownerIDs {
		notes := byOwner[ownerID]
		sort.Slice(notes, func(i, j int) bool {
			if cmp := notes[i].Amount.Cmp(notes[j].Amount); cmp != 0 {
				return cmp < 0
			}
			return notes[i].NoteID < notes[j].NoteID
		})
		for _, note := range notes {
			if note.Amount.Cmp(target) >= 0 {
				return []TreasuryNote{note}, new(big.Int).Set(note.Amount), true
			}
		}
		selected := make([]TreasuryNote, 0, privacytypes.BatchJoinSplitV1MaxInputs)
		total := new(big.Int)
		for i := len(notes) - 1; i >= 0 && len(selected) < int(privacytypes.BatchJoinSplitV1MaxInputs); i-- {
			selected = append(selected, notes[i])
			total.Add(total, notes[i].Amount)
			if total.Cmp(target) >= 0 {
				sort.Slice(selected, func(i, j int) bool { return selected[i].NoteID < selected[j].NoteID })
				return selected, new(big.Int).Set(total), true
			}
		}
	}
	return nil, nil, false
}

func sumPayrollItemAmounts(items []PayrollItemInput) *big.Int {
	total := new(big.Int)
	for _, item := range items {
		total.Add(total, item.Amount)
	}
	return total
}

func removeBatchTreasuryInputs(available, selected []TreasuryNote) []TreasuryNote {
	remove := make(map[string]struct{}, len(selected))
	for _, note := range selected {
		remove[note.NoteID] = struct{}{}
	}
	out := make([]TreasuryNote, 0, len(available)-len(selected))
	for _, note := range available {
		if _, exists := remove[note.NoteID]; !exists {
			out = append(out, note)
		}
	}
	return out
}

func batchPayrollOperationID(input PayrollInput, operationIndex int) string {
	base := operationID(input.CompanyID, input.BatchID, input.PayrollID, fmt.Sprintf("batch-%06d", operationIndex), input.Attempt)
	return base + ":one-proof-16x32"
}

func IsBatchPreparationRequired(err error) bool {
	return errors.Is(err, privacybatchtransfer.ErrPreparationRequired)
}
