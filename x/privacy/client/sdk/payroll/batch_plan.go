package payroll

import (
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"

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

const (
	batchPayrollPlanSearchLimit       = 20000
	batchTreasuryCandidateSearchLimit = 4096
	batchTreasuryCandidateLimit       = 64
)

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
	search := batchPayrollPlanSearch{remaining: batchPayrollPlanSearchLimit}
	operations, ok := search.plan(input, available, 0, 0)
	if !ok {
		blockedOffset := search.farthestItemOffset
		if blockedOffset < 0 || blockedOffset >= len(input.Items) {
			blockedOffset = 0
		}
		return nil, fmt.Errorf("%w: payroll item %s cannot be funded by a bounded plan of 1..16 current notes", privacybatchtransfer.ErrPreparationRequired, input.Items[blockedOffset].ItemID)
	}
	plan.Operations = operations
	return plan, nil
}

type batchPayrollPlanSearch struct {
	remaining          int
	farthestItemOffset int
}

type batchTreasuryInputSelection struct {
	notes []TreasuryNote
	total *big.Int
}

func (s *batchPayrollPlanSearch) plan(input PayrollInput, available []TreasuryNote, itemOffset, operationIndex int) ([]BatchPayrollOperationPlan, bool) {
	if itemOffset == len(input.Items) {
		return []BatchPayrollOperationPlan{}, true
	}
	if itemOffset > s.farthestItemOffset {
		s.farthestItemOffset = itemOffset
	}
	if s.remaining <= 0 {
		return nil, false
	}
	s.remaining--
	maxPayments := len(input.Items) - itemOffset
	if maxPayments > int(privacytypes.BatchJoinSplitV1MaxOutputs) {
		maxPayments = int(privacytypes.BatchJoinSplitV1MaxOutputs)
	}
	for paymentCount := maxPayments; paymentCount >= 1; paymentCount-- {
		paymentTotal := sumPayrollItemAmounts(input.Items[itemOffset : itemOffset+paymentCount])
		for _, selection := range selectBatchTreasuryInputCandidates(available, paymentTotal) {
			change := new(big.Int).Sub(selection.total, paymentTotal)
			if change.Cmp(privacytypes.MaxShieldedAmount()) > 0 {
				continue
			}
			if paymentCount == int(privacytypes.BatchJoinSplitV1MaxOutputs) && change.Sign() != 0 {
				continue
			}
			operation := buildBatchPayrollOperation(input, itemOffset, paymentCount, operationIndex, selection, paymentTotal, change)
			remaining, ok := s.plan(input, removeBatchTreasuryInputs(available, selection.notes), itemOffset+paymentCount, operationIndex+1)
			if ok {
				return append([]BatchPayrollOperationPlan{operation}, remaining...), true
			}
		}
	}
	return nil, false
}

func buildBatchPayrollOperation(input PayrollInput, itemOffset, paymentCount, operationIndex int, selection batchTreasuryInputSelection, paymentTotal, change *big.Int) BatchPayrollOperationPlan {
	operationID := batchPayrollOperationID(input, operationIndex)
	items := make([]PayrollPlanItem, paymentCount)
	for i := 0; i < paymentCount; i++ {
		item := input.Items[itemOffset+i]
		recipientHash, _ := HashRecipient(item.RecipientAddress)
		amountHash, _ := HashAmount(input.Denom, item.Amount)
		items[i] = PayrollPlanItem{
			CompanyID: input.CompanyID, PayrollID: input.PayrollID, BatchID: input.BatchID, Attempt: input.Attempt,
			ChunkID: chunkID(input.CompanyID, input.BatchID, input.PayrollID, input.Attempt, operationIndex),
			ItemID:  item.ItemID, EmployeeID: item.EmployeeID, OperationID: operationID,
			RecipientAddress: item.RecipientAddress, ExpectedRecipientHash: recipientHash,
			Amount: cloneBigInt(item.Amount), ExpectedAmountHash: amountHash, Denom: input.Denom,
			DisclosurePolicy: item.DisclosurePolicy, ExpectedOutputCommitment: item.ExpectedOutputCommitment,
			ExpectedDisclosureDigest: preferredExpectedDisclosureDigest(item), Status: ItemStatusPlanned,
		}
	}
	outputCount := paymentCount
	if change.Sign() > 0 {
		outputCount++
	}
	return BatchPayrollOperationPlan{
		OperationID: operationID, Items: items, InputNotes: cloneTreasuryNotes(selection.notes),
		InputTotal: new(big.Int).Set(selection.total), PaymentTotal: new(big.Int).Set(paymentTotal), Change: new(big.Int).Set(change),
		OutputCount: outputCount, HasChange: change.Sign() > 0,
	}
}

func selectBatchTreasuryInputCandidates(available []TreasuryNote, target *big.Int) []batchTreasuryInputSelection {
	if target == nil || target.Sign() <= 0 {
		return nil
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
	selections := make([]batchTreasuryInputSelection, 0)
	for _, ownerID := range ownerIDs {
		notes := byOwner[ownerID]
		sort.Slice(notes, func(i, j int) bool {
			if cmp := notes[i].Amount.Cmp(notes[j].Amount); cmp != 0 {
				return cmp > 0
			}
			return notes[i].NoteID < notes[j].NoteID
		})
		selections = append(selections, enumerateOwnerBatchInputCandidates(notes, target)...)
	}
	sort.Slice(selections, func(i, j int) bool {
		leftWaste := new(big.Int).Sub(selections[i].total, target)
		rightWaste := new(big.Int).Sub(selections[j].total, target)
		if cmp := leftWaste.Cmp(rightWaste); cmp != 0 {
			return cmp < 0
		}
		if len(selections[i].notes) != len(selections[j].notes) {
			return len(selections[i].notes) < len(selections[j].notes)
		}
		return batchTreasurySelectionKey(selections[i].notes) < batchTreasurySelectionKey(selections[j].notes)
	})
	return selections
}

func enumerateOwnerBatchInputCandidates(notes []TreasuryNote, target *big.Int) []batchTreasuryInputSelection {
	if len(notes) == 0 {
		return nil
	}
	selected := make([]TreasuryNote, 0, privacytypes.BatchJoinSplitV1MaxInputs)
	selections := make([]batchTreasuryInputSelection, 0, batchTreasuryCandidateLimit)
	seen := make(map[string]struct{}, batchTreasuryCandidateLimit)
	visited := 0
	var visit func(int, *big.Int)
	visit = func(index int, total *big.Int) {
		if visited >= batchTreasuryCandidateSearchLimit || len(selections) >= batchTreasuryCandidateLimit {
			return
		}
		visited++
		if total.Cmp(target) >= 0 {
			candidate := cloneTreasuryNotes(selected)
			sort.Slice(candidate, func(i, j int) bool { return candidate[i].NoteID < candidate[j].NoteID })
			key := batchTreasurySelectionKey(candidate)
			if _, exists := seen[key]; !exists {
				seen[key] = struct{}{}
				selections = append(selections, batchTreasuryInputSelection{notes: candidate, total: new(big.Int).Set(total)})
			}
			return
		}
		if index >= len(notes) || len(selected) >= int(privacytypes.BatchJoinSplitV1MaxInputs) {
			return
		}
		slots := int(privacytypes.BatchJoinSplitV1MaxInputs) - len(selected)
		maxReachable := new(big.Int).Set(total)
		for i := index; i < len(notes) && i < index+slots; i++ {
			maxReachable.Add(maxReachable, notes[i].Amount)
		}
		if maxReachable.Cmp(target) < 0 {
			return
		}
		selected = append(selected, notes[index])
		visit(index+1, new(big.Int).Add(total, notes[index].Amount))
		selected = selected[:len(selected)-1]
		visit(index+1, total)
	}
	visit(0, new(big.Int))
	return selections
}

func batchTreasurySelectionKey(notes []TreasuryNote) string {
	ids := make([]string, len(notes))
	for i := range notes {
		ids[i] = notes[i].NoteID
	}
	sort.Strings(ids)
	return strings.Join(ids, "\x00")
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
