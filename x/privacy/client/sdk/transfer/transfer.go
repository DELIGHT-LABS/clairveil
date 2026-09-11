package transfer

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"sort"
	"strings"

	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"

	privacyscan "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/scan"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

type InputSelection struct {
	Inputs         [2]privacyscan.SecretFoundNote
	Total          *big.Int
	IsFinal        bool
	NeedsZeroDummy bool
}

const (
	exactInputBatchItemLimit          = 12
	exactInputBatchFullNoteLimit      = 32
	exactInputBatchCandidateNoteLimit = 48
	exactInputBatchCandidatePairLimit = 16
)

// amountBig is confined to the public planning layer. SecretNoteV1 carries
// amounts as uint64; the existing planner accepts public user-requested
// targets as big.Int for Cosmos compatibility.
func amountBig(amount uint64) *big.Int { return new(big.Int).SetUint64(amount) }

func ResolveRecipient(targetAddr string) (*crypto_tedwards.PointAffine, *crypto_tedwards.PointAffine, error) {
	targetAddr = strings.TrimSpace(targetAddr)
	if !strings.HasPrefix(targetAddr, privacytypes.ShieldedBech32Prefix) {
		return nil, nil, fmt.Errorf("transfer recipient must be a shielded address with prefix '%s'", privacytypes.ShieldedBech32Prefix)
	}

	bundle, err := privacytypes.DecodeShieldedAddressBundle(targetAddr)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid shielded address: %w", err)
	}

	return bundle.SpendPubKey, bundle.ViewPubKey, nil
}

func SelectInputBatch(notes []privacyscan.SecretFoundNote, targetDenom string, targets []*big.Int) ([]InputSelection, error) {
	normalizedTargets := make([]*big.Int, len(targets))
	for i, target := range targets {
		normalizedTargets[i] = normalizedAmount(target)
		if normalizedTargets[i].Sign() <= 0 {
			return nil, fmt.Errorf("batch item %d target amount must be positive", i)
		}
	}
	if len(normalizedTargets) == 0 {
		return []InputSelection{}, nil
	}

	sameDenomNotes := plannerSortedSameDenomSpendableNotes(notes, targetDenom)
	targetOrder := inputBatchTargetOrder(normalizedTargets)
	if len(targets) <= exactInputBatchItemLimit {
		candidates := exactInputBatchCandidateNotes(sameDenomNotes, normalizedTargets, targetOrder)
		if selections, ok := exactSelectInputBatch(candidates, normalizedTargets, targetOrder); ok {
			return selections, nil
		}
	}

	available := append([]privacyscan.SecretFoundNote(nil), sameDenomNotes...)
	selections := make([]InputSelection, len(normalizedTargets))
	for _, targetIndex := range targetOrder {
		selection := SelectInputs(available, targetDenom, normalizedTargets[targetIndex])
		if selection.NeedsZeroDummy || !selection.IsFinal || selection.Total.Sign() == 0 {
			return nil, fmt.Errorf("batch item %d needs note preparation before batching", targetIndex)
		}
		selections[targetIndex] = selection
		available = removeSelectedInputNotes(available, selection.Inputs)
	}
	return selections, nil
}

func SelectInputs(notes []privacyscan.SecretFoundNote, targetDenom string, target *big.Int) InputSelection {
	target = normalizedAmount(target)
	maxOutputAmount := privacytypes.MaxShieldedAmount()

	var inputs [2]privacyscan.SecretFoundNote
	sameDenomNotes := plannerSortedSameDenomSpendableNotes(notes, targetDenom)
	requiresDummyForSingleNote := false

	for i, note := range sameDenomNotes {
		if amountBig(note.Note.Amount).Cmp(target) >= 0 {
			zeroNoteIndex := FindZeroNote(sameDenomNotes, i)
			if zeroNoteIndex != -1 {
				inputs[0] = note
				inputs[1] = sameDenomNotes[zeroNoteIndex]
				return InputSelection{
					Inputs:  inputs,
					Total:   new(big.Int).Set(amountBig(note.Note.Amount)),
					IsFinal: true,
				}
			}
			requiresDummyForSingleNote = true
		}
	}

	bestPairFound := false
	var bestPair [2]privacyscan.SecretFoundNote
	bestPairTotal := big.NewInt(0)

	for i := 0; i < len(sameDenomNotes); i++ {
		if amountBig(sameDenomNotes[i].Note.Amount).Sign() == 0 {
			continue
		}
		for j := i + 1; j < len(sameDenomNotes); j++ {
			if amountBig(sameDenomNotes[j].Note.Amount).Sign() == 0 {
				continue
			}
			sum := new(big.Int).Add(amountBig(sameDenomNotes[i].Note.Amount), amountBig(sameDenomNotes[j].Note.Amount))
			if finalTransferOutputsWithinBound(sum, target, maxOutputAmount) {
				if !bestPairFound || betterSufficientPairCandidate(sameDenomNotes[i], sameDenomNotes[j], sum, bestPair[0], bestPair[1], bestPairTotal) {
					bestPairFound = true
					bestPair[0] = sameDenomNotes[i]
					bestPair[1] = sameDenomNotes[j]
					bestPairTotal = new(big.Int).Set(sum)
				}
			}
		}
	}
	if bestPairFound {
		return InputSelection{
			Inputs:  bestPair,
			Total:   bestPairTotal,
			IsFinal: true,
		}
	}

	bestMergeFound := false
	var bestMerge [2]privacyscan.SecretFoundNote
	bestMergeTotal := big.NewInt(0)

	for i := 0; i < len(sameDenomNotes); i++ {
		if amountBig(sameDenomNotes[i].Note.Amount).Sign() == 0 {
			continue
		}
		for j := i + 1; j < len(sameDenomNotes); j++ {
			if amountBig(sameDenomNotes[j].Note.Amount).Sign() == 0 {
				continue
			}
			sum := new(big.Int).Add(amountBig(sameDenomNotes[i].Note.Amount), amountBig(sameDenomNotes[j].Note.Amount))
			if sum.Cmp(maxOutputAmount) > 0 {
				continue
			}
			if !bestMergeFound || betterMergePairCandidate(sameDenomNotes[i], sameDenomNotes[j], sum, bestMerge[0], bestMerge[1], bestMergeTotal) {
				bestMergeFound = true
				bestMerge[0] = sameDenomNotes[i]
				bestMerge[1] = sameDenomNotes[j]
				bestMergeTotal = new(big.Int).Set(sum)
			}
		}
	}
	if bestMergeFound {
		return InputSelection{
			Inputs:  bestMerge,
			Total:   bestMergeTotal,
			IsFinal: false,
		}
	}

	if requiresDummyForSingleNote {
		return InputSelection{
			Total:          big.NewInt(0),
			NeedsZeroDummy: true,
		}
	}

	return InputSelection{
		Total: big.NewInt(0),
	}
}

func finalTransferOutputsWithinBound(total *big.Int, target *big.Int, maxOutputAmount *big.Int) bool {
	if target.Sign() < 0 || target.Cmp(maxOutputAmount) > 0 {
		return false
	}
	if total.Cmp(target) < 0 {
		return false
	}

	change := new(big.Int).Sub(total, target)
	return change.Cmp(maxOutputAmount) <= 0
}

type exactInputBatchState struct {
	position int
	usedMask uint64
}

type exactInputPairCandidate struct {
	left  int
	right int
	total *big.Int
}

func exactSelectInputBatch(notes []privacyscan.SecretFoundNote, targets []*big.Int, targetOrder []int) ([]InputSelection, bool) {
	if len(notes) > exactInputBatchCandidateNoteLimit {
		return nil, false
	}
	selections := make([]InputSelection, len(targets))
	failed := make(map[exactInputBatchState]struct{})
	maxOutputAmount := privacytypes.MaxShieldedAmount()

	var search func(position int, usedMask uint64) bool
	search = func(position int, usedMask uint64) bool {
		if position == len(targetOrder) {
			return true
		}
		state := exactInputBatchState{position: position, usedMask: usedMask}
		if _, ok := failed[state]; ok {
			return false
		}

		targetIndex := targetOrder[position]
		for _, candidate := range exactInputPairCandidates(notes, usedMask, targets[targetIndex], maxOutputAmount) {
			nextMask := usedMask | (uint64(1) << candidate.left) | (uint64(1) << candidate.right)
			selections[targetIndex] = InputSelection{
				Inputs:  orderedFoundInputPair(notes[candidate.left], notes[candidate.right]),
				Total:   new(big.Int).Set(candidate.total),
				IsFinal: true,
			}
			if search(position+1, nextMask) {
				return true
			}
			selections[targetIndex] = InputSelection{}
		}

		failed[state] = struct{}{}
		return false
	}

	if !search(0, 0) {
		return nil, false
	}
	return cloneInputSelections(selections), true
}

func exactInputPairCandidates(notes []privacyscan.SecretFoundNote, usedMask uint64, target *big.Int, maxOutputAmount *big.Int) []exactInputPairCandidate {
	candidates := make([]exactInputPairCandidate, 0)
	for i := range notes {
		if usedMask&(uint64(1)<<i) != 0 {
			continue
		}
		for j := i + 1; j < len(notes); j++ {
			if usedMask&(uint64(1)<<j) != 0 {
				continue
			}
			total := new(big.Int).Add(amountBig(notes[i].Note.Amount), amountBig(notes[j].Note.Amount))
			if !finalTransferOutputsWithinBound(total, target, maxOutputAmount) {
				continue
			}
			candidates = append(candidates, exactInputPairCandidate{left: i, right: j, total: total})
		}
	}
	sortExactInputPairCandidates(candidates, notes)
	return candidates
}

func exactInputBatchCandidateNotes(notes []privacyscan.SecretFoundNote, targets []*big.Int, targetOrder []int) []privacyscan.SecretFoundNote {
	if len(notes) <= exactInputBatchFullNoteLimit {
		return append([]privacyscan.SecretFoundNote(nil), notes...)
	}

	selected := make(map[int]struct{}, exactInputBatchCandidateNoteLimit)
	candidates := make([]privacyscan.SecretFoundNote, 0, exactInputBatchCandidateNoteLimit)
	addIndex := func(index int) bool {
		if index < 0 || index >= len(notes) {
			return true
		}
		if _, exists := selected[index]; exists {
			return true
		}
		if len(candidates) >= exactInputBatchCandidateNoteLimit {
			return false
		}
		selected[index] = struct{}{}
		candidates = append(candidates, notes[index])
		return true
	}

	for _, targetIndex := range targetOrder {
		target := targets[targetIndex]
		for _, index := range inputBatchZeroCandidateIndexes(notes, exactInputBatchCandidatePairLimit) {
			if !addIndex(index) {
				break
			}
		}
		for _, index := range inputBatchSingleCandidateIndexes(notes, target, exactInputBatchCandidatePairLimit) {
			if !addIndex(index) {
				break
			}
		}
		for _, candidate := range boundedInputBatchPairCandidates(notes, target, exactInputBatchCandidatePairLimit) {
			if !addIndex(candidate.left) || !addIndex(candidate.right) {
				break
			}
		}
		if len(candidates) >= exactInputBatchCandidateNoteLimit {
			break
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		return foundNotePlannerLess(candidates[i], candidates[j])
	})
	return append([]privacyscan.SecretFoundNote(nil), candidates...)
}

func inputBatchTargetOrder(targets []*big.Int) []int {
	order := make([]int, len(targets))
	for i := range targets {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool {
		if cmp := targets[order[i]].Cmp(targets[order[j]]); cmp != 0 {
			return cmp > 0
		}
		return order[i] < order[j]
	})
	return order
}

func inputBatchZeroCandidateIndexes(notes []privacyscan.SecretFoundNote, limit int) []int {
	indexes := make([]int, 0, limit)
	for i, note := range notes {
		if amountBig(note.Note.Amount).Sign() > 0 {
			break
		}
		indexes = append(indexes, i)
		if len(indexes) >= limit {
			break
		}
	}
	return indexes
}

func inputBatchSingleCandidateIndexes(notes []privacyscan.SecretFoundNote, target *big.Int, limit int) []int {
	indexes := make([]int, 0, limit)
	maxOutputAmount := privacytypes.MaxShieldedAmount()
	for i := sort.Search(len(notes), func(i int) bool {
		return amountBig(notes[i].Note.Amount).Cmp(target) >= 0
	}); i < len(notes); i++ {
		if finalTransferOutputsWithinBound(amountBig(notes[i].Note.Amount), target, maxOutputAmount) {
			indexes = append(indexes, i)
		}
		if len(indexes) >= limit {
			break
		}
	}
	return indexes
}

func boundedInputBatchPairCandidates(notes []privacyscan.SecretFoundNote, target *big.Int, limit int) []exactInputPairCandidate {
	if limit <= 0 {
		return nil
	}
	leftIndexes := make([]int, 0, limit*4)
	positiveFrom := sort.Search(len(notes), func(i int) bool {
		return amountBig(notes[i].Note.Amount).Sign() > 0
	})
	for i := positiveFrom; i < len(notes); i++ {
		leftIndexes = append(leftIndexes, i)
		if len(leftIndexes) >= limit*4 {
			break
		}
	}

	candidates := make([]exactInputPairCandidate, 0, limit)
	maxOutputAmount := privacytypes.MaxShieldedAmount()
	for _, left := range leftIndexes {
		needed := new(big.Int).Sub(target, amountBig(notes[left].Note.Amount))
		if needed.Sign() <= 0 {
			needed = big.NewInt(1)
		}
		right := sort.Search(len(notes), func(i int) bool {
			return amountBig(notes[i].Note.Amount).Cmp(needed) >= 0
		})
		addedForLeft := 0
		for ; right < len(notes) && addedForLeft < 4; right++ {
			if right == left || amountBig(notes[right].Note.Amount).Sign() <= 0 {
				continue
			}
			i, j := left, right
			if i > j {
				i, j = j, i
			}
			total := new(big.Int).Add(amountBig(notes[i].Note.Amount), amountBig(notes[j].Note.Amount))
			if finalTransferOutputsWithinBound(total, target, maxOutputAmount) {
				candidates = append(candidates, exactInputPairCandidate{left: i, right: j, total: total})
				addedForLeft++
			}
		}
	}
	sortExactInputPairCandidates(candidates, notes)
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	return candidates
}

func sortExactInputPairCandidates(candidates []exactInputPairCandidate, notes []privacyscan.SecretFoundNote) {
	sort.Slice(candidates, func(i, j int) bool {
		if cmp := candidates[i].total.Cmp(candidates[j].total); cmp != 0 {
			return cmp < 0
		}
		leftI := notes[candidates[i].left]
		leftJ := notes[candidates[j].left]
		if cmp := amountBig(leftI.Note.Amount).Cmp(amountBig(leftJ.Note.Amount)); cmp != 0 {
			return cmp < 0
		}
		rightI := notes[candidates[i].right]
		rightJ := notes[candidates[j].right]
		if cmp := amountBig(rightI.Note.Amount).Cmp(amountBig(rightJ.Note.Amount)); cmp != 0 {
			return cmp < 0
		}
		if foundNotePlannerLess(leftI, leftJ) {
			return true
		}
		if foundNotePlannerLess(leftJ, leftI) {
			return false
		}
		return foundNotePlannerLess(rightI, rightJ)
	})
}

func orderedFoundInputPair(left privacyscan.SecretFoundNote, right privacyscan.SecretFoundNote) [2]privacyscan.SecretFoundNote {
	if amountBig(left.Note.Amount).Sign() == 0 && amountBig(right.Note.Amount).Sign() > 0 {
		return [2]privacyscan.SecretFoundNote{right, left}
	}
	return [2]privacyscan.SecretFoundNote{left, right}
}

func cloneInputSelections(selections []InputSelection) []InputSelection {
	out := make([]InputSelection, len(selections))
	for i := range selections {
		out[i] = selections[i]
		out[i].Total = normalizedAmount(selections[i].Total)
	}
	return out
}

func removeSelectedInputNotes(notes []privacyscan.SecretFoundNote, inputs [2]privacyscan.SecretFoundNote) []privacyscan.SecretFoundNote {
	used := map[string]int{
		foundNoteIdentityKey(inputs[0]): 1,
		foundNoteIdentityKey(inputs[1]): 1,
	}
	if key := foundNoteIdentityKey(inputs[0]); key == foundNoteIdentityKey(inputs[1]) {
		used[key] = 2
	}
	out := make([]privacyscan.SecretFoundNote, 0, len(notes))
	for _, note := range notes {
		key := foundNoteIdentityKey(note)
		if count := used[key]; count > 0 {
			used[key] = count - 1
			continue
		}
		out = append(out, note)
	}
	return out
}

func SummarizeSpendableNotesByDenom(notes []privacyscan.SecretFoundNote, denom string) ([]privacyscan.SecretFoundNote, *big.Int) {
	targetAssetID := privacytypes.ComputeSecretAssetIDV1(denom)
	spendable := make([]privacyscan.SecretFoundNote, 0, len(notes))
	total := new(big.Int)

	for _, note := range notes {
		if note.IsSpent {
			continue
		}
		if !note.Note.AssetID.Equal(targetAssetID) {
			continue
		}

		spendable = append(spendable, note)
		total.Add(total, amountBig(note.Note.Amount))
	}

	return spendable, total
}

func PlannerStateFingerprint(notes []privacyscan.SecretFoundNote, denom string, targetAmount *big.Int) string {
	sameDenomNotes := plannerSortedSameDenomSpendableNotes(notes, denom)

	var builder strings.Builder
	builder.WriteString(denom)
	builder.WriteString("|")
	builder.WriteString(normalizedAmount(targetAmount).String())

	for _, note := range sameDenomNotes {
		builder.WriteString("|")
		builder.WriteString(foundNoteIdentityKey(note))
		builder.WriteString(":")
		builder.WriteString(amountBig(note.Note.Amount).String())
	}

	return builder.String()
}

func FindExactMatchSpendableNoteByDenom(notes []privacyscan.SecretFoundNote, denom string, targetAmount *big.Int) *privacyscan.SecretFoundNote {
	targetAmount = normalizedAmount(targetAmount)
	sameDenomNotes := plannerSortedSameDenomSpendableNotes(notes, denom)
	for i := range sameDenomNotes {
		if amountBig(sameDenomNotes[i].Note.Amount).Cmp(targetAmount) == 0 {
			selected := sameDenomNotes[i]
			return &selected
		}
	}
	return nil
}

func FindZeroNote(notes []privacyscan.SecretFoundNote, excludeIndex int) int {
	for i, note := range notes {
		if i == excludeIndex {
			continue
		}
		if amountBig(note.Note.Amount).Sign() == 0 {
			return i
		}
	}
	return -1
}

func normalizedAmount(amount *big.Int) *big.Int {
	if amount == nil {
		return big.NewInt(0)
	}
	return new(big.Int).Set(amount)
}

func plannerSortedSameDenomSpendableNotes(notes []privacyscan.SecretFoundNote, denom string) []privacyscan.SecretFoundNote {
	sameDenomNotes, _ := SummarizeSpendableNotesByDenom(notes, denom)
	sort.Slice(sameDenomNotes, func(i, j int) bool {
		return foundNotePlannerLess(sameDenomNotes[i], sameDenomNotes[j])
	})
	return sameDenomNotes
}

func foundNoteIdentityKey(note privacyscan.SecretFoundNote) string {
	if trimmed := strings.ToLower(strings.TrimSpace(note.Nullifier)); trimmed != "" {
		return "nullifier:" + trimmed
	}

	if commitment, err := note.Note.CommitmentV1(); err == nil {
		commitmentBytes := commitment.Bytes()
		return "commitment:" + hex.EncodeToString(commitmentBytes[:])
	}

	return fmt.Sprintf(
		"fallback:%d:%s:%s",
		note.Height,
		strings.ToLower(strings.TrimSpace(note.TxHash)),
		amountBig(note.Note.Amount).String(),
	)
}

func foundNotePlannerLess(left, right privacyscan.SecretFoundNote) bool {
	if cmp := amountBig(left.Note.Amount).Cmp(amountBig(right.Note.Amount)); cmp != 0 {
		return cmp < 0
	}

	if left.Height != right.Height {
		return left.Height < right.Height
	}

	leftTxHash := strings.ToLower(strings.TrimSpace(left.TxHash))
	rightTxHash := strings.ToLower(strings.TrimSpace(right.TxHash))
	if leftTxHash != rightTxHash {
		return leftTxHash < rightTxHash
	}

	leftNullifier := strings.ToLower(strings.TrimSpace(left.Nullifier))
	rightNullifier := strings.ToLower(strings.TrimSpace(right.Nullifier))
	if leftNullifier != rightNullifier {
		return leftNullifier < rightNullifier
	}

	return foundNoteIdentityKey(left) < foundNoteIdentityKey(right)
}

func betterSufficientPairCandidate(
	left privacyscan.SecretFoundNote,
	right privacyscan.SecretFoundNote,
	total *big.Int,
	bestLeft privacyscan.SecretFoundNote,
	bestRight privacyscan.SecretFoundNote,
	bestTotal *big.Int,
) bool {
	if cmp := total.Cmp(bestTotal); cmp != 0 {
		return cmp < 0
	}

	if cmp := amountBig(right.Note.Amount).Cmp(amountBig(bestRight.Note.Amount)); cmp != 0 {
		return cmp < 0
	}
	if cmp := amountBig(left.Note.Amount).Cmp(amountBig(bestLeft.Note.Amount)); cmp != 0 {
		return cmp < 0
	}

	if foundNotePlannerLess(left, bestLeft) {
		return true
	}
	if foundNotePlannerLess(bestLeft, left) {
		return false
	}

	return foundNotePlannerLess(right, bestRight)
}

func betterMergePairCandidate(
	left privacyscan.SecretFoundNote,
	right privacyscan.SecretFoundNote,
	total *big.Int,
	bestLeft privacyscan.SecretFoundNote,
	bestRight privacyscan.SecretFoundNote,
	bestTotal *big.Int,
) bool {
	if cmp := total.Cmp(bestTotal); cmp != 0 {
		return cmp > 0
	}

	if cmp := amountBig(right.Note.Amount).Cmp(amountBig(bestRight.Note.Amount)); cmp != 0 {
		return cmp > 0
	}
	if cmp := amountBig(left.Note.Amount).Cmp(amountBig(bestLeft.Note.Amount)); cmp != 0 {
		return cmp > 0
	}

	if foundNotePlannerLess(left, bestLeft) {
		return true
	}
	if foundNotePlannerLess(bestLeft, left) {
		return false
	}

	return foundNotePlannerLess(right, bestRight)
}
