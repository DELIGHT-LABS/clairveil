package transfer

import (
	"fmt"
	"math/big"
	"sort"
	"strings"

	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"

	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
	privacyscan "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/scan"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

type InputSelection struct {
	Inputs         [2]privacyscan.FoundNote
	Total          *big.Int
	IsFinal        bool
	NeedsZeroDummy bool
}

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

func SelectInputs(notes []privacyscan.FoundNote, targetDenom string, target *big.Int) InputSelection {
	target = normalizedAmount(target)
	maxOutputAmount := privacytypes.MaxShieldedAmount()

	var inputs [2]privacyscan.FoundNote
	sameDenomNotes := plannerSortedSameDenomSpendableNotes(notes, targetDenom)
	requiresDummyForSingleNote := false

	for i, note := range sameDenomNotes {
		if note.Note.Amount.Cmp(target) >= 0 {
			zeroNoteIndex := FindZeroNote(sameDenomNotes, i)
			if zeroNoteIndex != -1 {
				inputs[0] = note
				inputs[1] = sameDenomNotes[zeroNoteIndex]
				return InputSelection{
					Inputs:  inputs,
					Total:   new(big.Int).Set(note.Note.Amount),
					IsFinal: true,
				}
			}
			requiresDummyForSingleNote = true
		}
	}

	bestPairFound := false
	var bestPair [2]privacyscan.FoundNote
	bestPairTotal := big.NewInt(0)

	for i := 0; i < len(sameDenomNotes); i++ {
		if sameDenomNotes[i].Note.Amount.Sign() == 0 {
			continue
		}
		for j := i + 1; j < len(sameDenomNotes); j++ {
			if sameDenomNotes[j].Note.Amount.Sign() == 0 {
				continue
			}
			sum := new(big.Int).Add(sameDenomNotes[i].Note.Amount, sameDenomNotes[j].Note.Amount)
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
	var bestMerge [2]privacyscan.FoundNote
	bestMergeTotal := big.NewInt(0)

	for i := 0; i < len(sameDenomNotes); i++ {
		if sameDenomNotes[i].Note.Amount.Sign() == 0 {
			continue
		}
		for j := i + 1; j < len(sameDenomNotes); j++ {
			if sameDenomNotes[j].Note.Amount.Sign() == 0 {
				continue
			}
			sum := new(big.Int).Add(sameDenomNotes[i].Note.Amount, sameDenomNotes[j].Note.Amount)
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

func SummarizeSpendableNotesByDenom(notes []privacyscan.FoundNote, denom string) ([]privacyscan.FoundNote, *big.Int) {
	targetAssetID := privacycrypto.HashString(denom)
	spendable := make([]privacyscan.FoundNote, 0, len(notes))
	total := new(big.Int)

	for _, note := range notes {
		if note.IsSpent {
			continue
		}
		if note.Note.AssetID == nil || note.Note.AssetID.Cmp(targetAssetID) != 0 {
			continue
		}

		spendable = append(spendable, note)
		total.Add(total, note.Note.Amount)
	}

	return spendable, total
}

func PlannerStateFingerprint(notes []privacyscan.FoundNote, denom string, targetAmount *big.Int) string {
	sameDenomNotes := plannerSortedSameDenomSpendableNotes(notes, denom)

	var builder strings.Builder
	builder.WriteString(denom)
	builder.WriteString("|")
	builder.WriteString(normalizedAmount(targetAmount).String())

	for _, note := range sameDenomNotes {
		builder.WriteString("|")
		builder.WriteString(foundNoteIdentityKey(note))
		builder.WriteString(":")
		builder.WriteString(note.Note.Amount.String())
	}

	return builder.String()
}

func FindExactMatchSpendableNoteByDenom(notes []privacyscan.FoundNote, denom string, targetAmount *big.Int) *privacyscan.FoundNote {
	targetAmount = normalizedAmount(targetAmount)
	sameDenomNotes := plannerSortedSameDenomSpendableNotes(notes, denom)
	for i := range sameDenomNotes {
		if sameDenomNotes[i].Note.Amount.Cmp(targetAmount) == 0 {
			selected := sameDenomNotes[i]
			return &selected
		}
	}
	return nil
}

func FindZeroNote(notes []privacyscan.FoundNote, excludeIndex int) int {
	for i, note := range notes {
		if i == excludeIndex {
			continue
		}
		if note.Note.Amount.Sign() == 0 {
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

func plannerSortedSameDenomSpendableNotes(notes []privacyscan.FoundNote, denom string) []privacyscan.FoundNote {
	sameDenomNotes, _ := SummarizeSpendableNotesByDenom(notes, denom)
	sort.Slice(sameDenomNotes, func(i, j int) bool {
		return foundNotePlannerLess(sameDenomNotes[i], sameDenomNotes[j])
	})
	return sameDenomNotes
}

func foundNoteIdentityKey(note privacyscan.FoundNote) string {
	if trimmed := strings.ToLower(strings.TrimSpace(note.Nullifier)); trimmed != "" {
		return "nullifier:" + trimmed
	}

	commitment := note.Note.ComputeCommitment()
	if commitmentHex, err := privacyfield.CanonicalHexFromBigInt(commitment); err == nil {
		return "commitment:" + commitmentHex
	}

	return fmt.Sprintf(
		"fallback:%d:%s:%s",
		note.Height,
		strings.ToLower(strings.TrimSpace(note.TxHash)),
		note.Note.Amount.String(),
	)
}

func foundNotePlannerLess(left, right privacyscan.FoundNote) bool {
	if cmp := left.Note.Amount.Cmp(right.Note.Amount); cmp != 0 {
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
	left privacyscan.FoundNote,
	right privacyscan.FoundNote,
	total *big.Int,
	bestLeft privacyscan.FoundNote,
	bestRight privacyscan.FoundNote,
	bestTotal *big.Int,
) bool {
	if cmp := total.Cmp(bestTotal); cmp != 0 {
		return cmp < 0
	}

	if cmp := right.Note.Amount.Cmp(bestRight.Note.Amount); cmp != 0 {
		return cmp < 0
	}
	if cmp := left.Note.Amount.Cmp(bestLeft.Note.Amount); cmp != 0 {
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
	left privacyscan.FoundNote,
	right privacyscan.FoundNote,
	total *big.Int,
	bestLeft privacyscan.FoundNote,
	bestRight privacyscan.FoundNote,
	bestTotal *big.Int,
) bool {
	if cmp := total.Cmp(bestTotal); cmp != 0 {
		return cmp > 0
	}

	if cmp := right.Note.Amount.Cmp(bestRight.Note.Amount); cmp != 0 {
		return cmp > 0
	}
	if cmp := left.Note.Amount.Cmp(bestLeft.Note.Amount); cmp != 0 {
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
