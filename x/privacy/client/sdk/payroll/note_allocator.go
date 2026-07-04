package payroll

import (
	"encoding/base64"
	"fmt"
	"math/big"
	"sort"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

type NoteAllocator struct{}

const (
	exactAllocationItemLimit          = 12
	exactAllocationFullNoteLimit      = 32
	exactAllocationCandidateNoteLimit = 48
	exactAllocationCandidatePairLimit = 16
)

func (a NoteAllocator) Allocate(input PayrollInput, notes []TreasuryNote) ([]PayrollPlanItem, error) {
	input = normalizePayrollInput(input)
	if err := ValidateInput(input); err != nil {
		return nil, err
	}

	available := filterAvailableNotes(input.Denom, notes)
	planned := make([]PayrollPlanItem, len(input.Items))
	itemOrder := payrollItemAllocationOrder(input.Items)
	allocated, err := allocatePayrollInputNotes(input.Items, available, itemOrder)
	if err != nil {
		return nil, err
	}

	for _, itemIndex := range itemOrder {
		item := input.Items[itemIndex]
		selected := allocated[itemIndex]

		itemDenom := item.Denom
		if itemDenom == "" {
			itemDenom = input.Denom
		}
		planned[itemIndex] = PayrollPlanItem{
			CompanyID:                input.CompanyID,
			PayrollID:                input.PayrollID,
			BatchID:                  input.BatchID,
			Attempt:                  input.Attempt,
			ItemID:                   item.ItemID,
			EmployeeID:               item.EmployeeID,
			OperationID:              operationID(input.CompanyID, input.BatchID, input.PayrollID, item.ItemID, input.Attempt),
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
			ChunkID:                  chunkID(input.CompanyID, input.BatchID, input.PayrollID, input.Attempt, itemIndex),
		}
	}

	return planned, nil
}

func allocatePayrollInputNotes(items []PayrollItemInput, available []TreasuryNote, itemOrder []int) ([][]TreasuryNote, error) {
	if len(items) <= exactAllocationItemLimit {
		if allocated, ok := exactAllocatePayrollInputNotesWithCandidates(items, available, itemOrder); ok {
			return allocated, nil
		}
	}

	allocation := newNoteAllocationState(available)
	allocated := make([][]TreasuryNote, len(items))
	var greedyErr error
	for _, itemIndex := range itemOrder {
		item := items[itemIndex]
		selected, err := allocation.selectInputNotes(item.Amount)
		if err != nil {
			greedyErr = fmt.Errorf("%w: item %s needs %s", err, item.ItemID, item.Amount.String())
			break
		}
		allocated[itemIndex] = selected
	}
	if greedyErr == nil {
		return allocated, nil
	}

	if allocated, ok := chunkedExactAllocatePayrollInputNotes(items, available, itemOrder); ok {
		return allocated, nil
	}
	return nil, greedyErr
}

type exactAllocationState struct {
	position int
	usedMask uint64
}

type exactPairCandidate struct {
	left  int
	right int
	total *big.Int
}

func exactAllocatePayrollInputNotesWithCandidates(items []PayrollItemInput, available []TreasuryNote, itemOrder []int) ([][]TreasuryNote, bool) {
	if len(available) <= exactAllocationFullNoteLimit {
		return exactAllocatePayrollInputNotes(items, available, itemOrder)
	}
	candidates := exactPayrollCandidateNotes(items, available, itemOrder)
	return exactAllocatePayrollInputNotes(items, candidates, itemOrder)
}

func chunkedExactAllocatePayrollInputNotes(items []PayrollItemInput, available []TreasuryNote, itemOrder []int) ([][]TreasuryNote, bool) {
	allocated := make([][]TreasuryNote, len(items))
	remaining := cloneTreasuryNotes(available)
	for start := 0; start < len(itemOrder); start += exactAllocationItemLimit {
		end := start + exactAllocationItemLimit
		if end > len(itemOrder) {
			end = len(itemOrder)
		}
		chunkOrder := itemOrder[start:end]
		chunkCandidates := exactPayrollCandidateNotes(items, remaining, chunkOrder)
		chunkAllocated, ok := exactAllocatePayrollInputNotes(items, chunkCandidates, chunkOrder)
		if !ok {
			return nil, false
		}
		for _, itemIndex := range chunkOrder {
			allocated[itemIndex] = chunkAllocated[itemIndex]
		}
		var removed bool
		remaining, removed = removeAllocatedTreasuryNotes(remaining, chunkAllocated, chunkOrder)
		if !removed {
			return nil, false
		}
	}
	return cloneAllocatedTreasuryNotes(allocated), true
}

func exactAllocatePayrollInputNotes(items []PayrollItemInput, available []TreasuryNote, itemOrder []int) ([][]TreasuryNote, bool) {
	if len(available) > exactAllocationCandidateNoteLimit {
		return nil, false
	}
	allocated := make([][]TreasuryNote, len(items))
	failed := make(map[exactAllocationState]struct{})

	var search func(position int, usedMask uint64) bool
	search = func(position int, usedMask uint64) bool {
		if position == len(itemOrder) {
			return true
		}
		state := exactAllocationState{position: position, usedMask: usedMask}
		if _, ok := failed[state]; ok {
			return false
		}

		itemIndex := itemOrder[position]
		for _, candidate := range exactPairCandidates(available, usedMask, items[itemIndex].Amount) {
			nextMask := usedMask | (uint64(1) << candidate.left) | (uint64(1) << candidate.right)
			allocated[itemIndex] = orderedInputPair(available[candidate.left], available[candidate.right])
			if search(position+1, nextMask) {
				return true
			}
			allocated[itemIndex] = nil
		}

		failed[state] = struct{}{}
		return false
	}

	if !search(0, 0) {
		return nil, false
	}
	return cloneAllocatedTreasuryNotes(allocated), true
}

func exactPayrollCandidateNotes(items []PayrollItemInput, available []TreasuryNote, itemOrder []int) []TreasuryNote {
	if len(available) <= exactAllocationCandidateNoteLimit {
		return cloneTreasuryNotes(available)
	}

	selected := make(map[int]struct{}, exactAllocationCandidateNoteLimit)
	candidates := make([]TreasuryNote, 0, exactAllocationCandidateNoteLimit)
	addIndex := func(index int) bool {
		if index < 0 || index >= len(available) {
			return true
		}
		if _, exists := selected[index]; exists {
			return true
		}
		if len(candidates) >= exactAllocationCandidateNoteLimit {
			return false
		}
		selected[index] = struct{}{}
		candidates = append(candidates, available[index])
		return true
	}

	for _, itemIndex := range itemOrder {
		target := items[itemIndex].Amount
		for _, index := range payrollZeroCandidateIndexes(available, exactAllocationCandidatePairLimit) {
			if !addIndex(index) {
				break
			}
		}
		for _, index := range payrollSingleCandidateIndexes(available, target, exactAllocationCandidatePairLimit) {
			if !addIndex(index) {
				break
			}
		}
		for _, candidate := range boundedPayrollPairCandidates(available, target, exactAllocationCandidatePairLimit) {
			if !addIndex(candidate.left) || !addIndex(candidate.right) {
				break
			}
		}
		if len(candidates) >= exactAllocationCandidateNoteLimit {
			break
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		if cmp := candidates[i].Amount.Cmp(candidates[j].Amount); cmp != 0 {
			return cmp < 0
		}
		return candidates[i].NoteID < candidates[j].NoteID
	})
	return cloneTreasuryNotes(candidates)
}

func payrollZeroCandidateIndexes(available []TreasuryNote, limit int) []int {
	indexes := make([]int, 0, limit)
	for i, note := range available {
		if note.Amount.Sign() > 0 {
			break
		}
		indexes = append(indexes, i)
		if len(indexes) >= limit {
			break
		}
	}
	return indexes
}

func payrollSingleCandidateIndexes(available []TreasuryNote, target *big.Int, limit int) []int {
	indexes := make([]int, 0, limit)
	if target == nil {
		return indexes
	}
	for i := sort.Search(len(available), func(i int) bool {
		return available[i].Amount.Cmp(target) >= 0
	}); i < len(available); i++ {
		if finalPayrollOutputsWithinBound(available[i].Amount, target) {
			indexes = append(indexes, i)
		}
		if len(indexes) >= limit {
			break
		}
	}
	return indexes
}

func boundedPayrollPairCandidates(available []TreasuryNote, target *big.Int, limit int) []exactPairCandidate {
	if target == nil || limit <= 0 {
		return nil
	}
	leftIndexes := make([]int, 0, limit*4)
	positiveFrom := sort.Search(len(available), func(i int) bool {
		return available[i].Amount.Sign() > 0
	})
	for i := positiveFrom; i < len(available); i++ {
		leftIndexes = append(leftIndexes, i)
		if len(leftIndexes) >= limit*4 {
			break
		}
	}

	candidates := make([]exactPairCandidate, 0, limit)
	for _, left := range leftIndexes {
		needed := new(big.Int).Sub(target, available[left].Amount)
		if needed.Sign() <= 0 {
			needed = big.NewInt(1)
		}
		right := sort.Search(len(available), func(i int) bool {
			return available[i].Amount.Cmp(needed) >= 0
		})
		addedForLeft := 0
		for ; right < len(available) && addedForLeft < 4; right++ {
			if right == left || available[right].Amount.Sign() <= 0 {
				continue
			}
			i, j := left, right
			if i > j {
				i, j = j, i
			}
			total := new(big.Int).Add(available[i].Amount, available[j].Amount)
			if finalPayrollOutputsWithinBound(total, target) {
				candidates = append(candidates, exactPairCandidate{left: i, right: j, total: total})
				addedForLeft++
			}
		}
	}
	sortExactPairCandidates(candidates, available)
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	return candidates
}

func removeAllocatedTreasuryNotes(available []TreasuryNote, allocated [][]TreasuryNote, itemOrder []int) ([]TreasuryNote, bool) {
	used := make(map[string]int, len(itemOrder)*2)
	for _, itemIndex := range itemOrder {
		for _, note := range allocated[itemIndex] {
			key := treasuryNoteAllocationKey(note)
			if key == "" {
				return nil, false
			}
			used[key]++
		}
	}
	out := make([]TreasuryNote, 0, len(available))
	for _, note := range available {
		key := treasuryNoteAllocationKey(note)
		if count := used[key]; count > 0 {
			used[key] = count - 1
			continue
		}
		out = append(out, note)
	}
	for _, count := range used {
		if count != 0 {
			return nil, false
		}
	}
	return cloneTreasuryNotes(out), true
}

func treasuryNoteAllocationKey(note TreasuryNote) string {
	if note.NoteID != "" {
		return "note:" + note.NoteID
	}
	if note.NullifierLookupKeyID != "" || note.NullifierLookupKey != "" {
		return "lookup:" + note.NullifierLookupKeyID + ":" + note.NullifierLookupKey
	}
	return ""
}

func exactPairCandidates(available []TreasuryNote, usedMask uint64, target *big.Int) []exactPairCandidate {
	candidates := make([]exactPairCandidate, 0)
	for i := range available {
		if usedMask&(uint64(1)<<i) != 0 {
			continue
		}
		for j := i + 1; j < len(available); j++ {
			if usedMask&(uint64(1)<<j) != 0 {
				continue
			}
			total := new(big.Int).Add(available[i].Amount, available[j].Amount)
			if !finalPayrollOutputsWithinBound(total, target) {
				continue
			}
			candidates = append(candidates, exactPairCandidate{left: i, right: j, total: total})
		}
	}
	sortExactPairCandidates(candidates, available)
	return candidates
}

func sortExactPairCandidates(candidates []exactPairCandidate, available []TreasuryNote) {
	sort.Slice(candidates, func(i, j int) bool {
		if cmp := candidates[i].total.Cmp(candidates[j].total); cmp != 0 {
			return cmp < 0
		}
		leftI := available[candidates[i].left]
		leftJ := available[candidates[j].left]
		if cmp := leftI.Amount.Cmp(leftJ.Amount); cmp != 0 {
			return cmp < 0
		}
		rightI := available[candidates[i].right]
		rightJ := available[candidates[j].right]
		if cmp := rightI.Amount.Cmp(rightJ.Amount); cmp != 0 {
			return cmp < 0
		}
		if leftI.NoteID != leftJ.NoteID {
			return leftI.NoteID < leftJ.NoteID
		}
		return rightI.NoteID < rightJ.NoteID
	})
}

func cloneAllocatedTreasuryNotes(allocated [][]TreasuryNote) [][]TreasuryNote {
	out := make([][]TreasuryNote, len(allocated))
	for i := range allocated {
		out[i] = cloneTreasuryNotes(allocated[i])
	}
	return out
}

func orderedInputPair(left TreasuryNote, right TreasuryNote) []TreasuryNote {
	if left.Amount != nil && left.Amount.Sign() == 0 && right.Amount != nil && right.Amount.Sign() > 0 {
		return []TreasuryNote{right, left}
	}
	return []TreasuryNote{left, right}
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

func payrollItemAllocationOrder(items []PayrollItemInput) []int {
	order := make([]int, len(items))
	for i := range items {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool {
		left := items[order[i]].Amount
		right := items[order[j]].Amount
		switch {
		case left == nil && right == nil:
			return order[i] < order[j]
		case left == nil:
			return true
		case right == nil:
			return false
		}
		if cmp := left.Cmp(right); cmp != 0 {
			return cmp > 0
		}
		return order[i] < order[j]
	})
	return order
}

type noteAllocationState struct {
	notes         []TreasuryNote
	next          []int
	prev          []int
	positiveFrom  int
	maxOutputPlus *big.Int
}

func newNoteAllocationState(available []TreasuryNote) *noteAllocationState {
	next := make([]int, len(available)+1)
	for i := range next {
		next[i] = i
	}
	prev := make([]int, len(available))
	for i := range prev {
		prev[i] = i
	}
	positiveFrom := sort.Search(len(available), func(i int) bool {
		return available[i].Amount.Sign() > 0
	})
	return &noteAllocationState{
		notes:         available,
		next:          next,
		prev:          prev,
		positiveFrom:  positiveFrom,
		maxOutputPlus: privacytypes.MaxShieldedAmount(),
	}
}

func (s *noteAllocationState) selectInputNotes(target *big.Int) ([]TreasuryNote, error) {
	if target == nil || target.Sign() <= 0 {
		return nil, fmt.Errorf("%w: target amount must be positive", ErrInvalidPayrollInput)
	}

	if zeroIndex, ok := s.firstAvailableZero(); ok {
		if positiveIndex, ok := s.firstPositiveWithinBound(target); ok {
			selected := []TreasuryNote{s.notes[positiveIndex], s.notes[zeroIndex]}
			s.remove(positiveIndex)
			s.remove(zeroIndex)
			return cloneTreasuryNotes(selected), nil
		}
	}

	return s.selectPositivePair(target)
}

func (s *noteAllocationState) firstAvailableZero() (int, bool) {
	index := s.findNext(0)
	if index < s.positiveFrom && index < len(s.notes) {
		return index, true
	}
	return -1, false
}

func (s *noteAllocationState) firstPositiveWithinBound(target *big.Int) (int, bool) {
	index := s.findNext(s.lowerBoundAmount(target))
	if index >= len(s.notes) {
		return -1, false
	}
	if !finalPayrollOutputsWithinBound(s.notes[index].Amount, target) {
		return -1, false
	}
	return index, true
}

func (s *noteAllocationState) selectPositivePair(target *big.Int) ([]TreasuryNote, error) {
	maxTotal := new(big.Int).Add(target, s.maxOutputPlus)
	left := s.findNext(s.positiveFrom)
	right := s.lastAvailablePositive()
	bestLeft := -1
	bestRight := -1
	for left < right {
		total := new(big.Int).Add(s.notes[left].Amount, s.notes[right].Amount)
		switch {
		case total.Cmp(target) < 0:
			left = s.findNext(left + 1)
			continue
		case total.Cmp(maxTotal) <= 0:
			bestLeft = left
			bestRight = right
			right = s.findPrev(right - 1)
		default:
			right = s.findPrev(right - 1)
		}
	}
	if bestLeft >= 0 {
		selected := []TreasuryNote{s.notes[bestLeft], s.notes[bestRight]}
		s.remove(bestLeft)
		s.remove(bestRight)
		return cloneTreasuryNotes(selected), nil
	}
	return nil, ErrInsufficientNotes
}

func (s *noteAllocationState) lastAvailablePositive() int {
	index := s.findPrev(len(s.notes) - 1)
	if index >= s.positiveFrom {
		return index
	}
	return -1
}

func (s *noteAllocationState) lowerBoundAmount(amount *big.Int) int {
	index := sort.Search(len(s.notes), func(i int) bool {
		return s.notes[i].Amount.Cmp(amount) >= 0
	})
	if index < s.positiveFrom {
		index = s.positiveFrom
	}
	return index
}

func (s *noteAllocationState) findNext(index int) int {
	if index < 0 {
		index = 0
	}
	if index >= len(s.next) {
		return len(s.notes)
	}
	if s.next[index] != index {
		s.next[index] = s.findNext(s.next[index])
	}
	return s.next[index]
}

func (s *noteAllocationState) findPrev(index int) int {
	if index < 0 || len(s.prev) == 0 {
		return -1
	}
	if index >= len(s.prev) {
		index = len(s.prev) - 1
	}
	if s.prev[index] != index {
		s.prev[index] = s.findPrev(s.prev[index])
	}
	return s.prev[index]
}

func (s *noteAllocationState) remove(index int) {
	if index < 0 || index >= len(s.notes) {
		return
	}
	s.next[index] = s.findNext(index + 1)
	s.prev[index] = s.findPrev(index - 1)
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

func operationID(companyID string, batchID string, payrollID string, itemID string, attempt int) string {
	base := "company:" + idComponent(companyID) + ":batch:" + idComponent(batchID) + ":payroll:" + idComponent(payrollID) + ":item:" + idComponent(itemID)
	if attempt <= 0 {
		return base
	}
	return fmt.Sprintf("%s:attempt:%03d", base, attempt)
}

func chunkID(companyID string, batchID string, payrollID string, attempt int, itemIndex int) string {
	base := "company:" + idComponent(companyID) + ":batch:" + idComponent(batchID) + ":payroll:" + idComponent(payrollID)
	if attempt <= 0 {
		return fmt.Sprintf("%s:chunk:%06d", base, itemIndex)
	}
	return fmt.Sprintf("%s:attempt:%03d:chunk:%06d", base, attempt, itemIndex)
}

func idComponent(value string) string {
	if value == "" {
		return "_"
	}
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}
