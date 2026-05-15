package transfer

import (
	"fmt"
	"math/big"
	"strings"

	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"

	privacyscan "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/scan"
)

type RecursivePlannerAction string

const (
	RecursivePlannerActionPrepareDummy  RecursivePlannerAction = "prepare_dummy"
	RecursivePlannerActionSelfMerge     RecursivePlannerAction = "self_merge"
	RecursivePlannerActionFinalTransfer RecursivePlannerAction = "final_transfer"
)

type RecursivePlannerRuntime struct {
	seenFingerprints map[string]int
}

type RecursivePlannerInput struct {
	FoundNotes                []privacyscan.FoundNote
	TargetDenom               string
	TargetAmount              *big.Int
	Step                      int
	AutoDummy                 bool
	FinalRecipientSpendPubKey *crypto_tedwards.PointAffine
	FinalRecipientViewPubKey  *crypto_tedwards.PointAffine
	SelfSpendPubKey           *crypto_tedwards.PointAffine
	SelfViewPubKey            *crypto_tedwards.PointAffine
}

type RecursivePlannerDecision struct {
	Action               RecursivePlannerAction
	Fingerprint          string
	Inputs               [2]privacyscan.FoundNote
	InputsTotal          *big.Int
	RecipientSpendPubKey *crypto_tedwards.PointAffine
	RecipientViewPubKey  *crypto_tedwards.PointAffine
	SendAmount           *big.Int
	IsFinal              bool
}

func NewRecursivePlannerRuntime() *RecursivePlannerRuntime {
	return &RecursivePlannerRuntime{
		seenFingerprints: make(map[string]int),
	}
}

func (r *RecursivePlannerRuntime) DecideNextStep(input RecursivePlannerInput) (*RecursivePlannerDecision, error) {
	if r == nil {
		return nil, fmt.Errorf("a planner runtime is required")
	}
	if input.Step <= 0 {
		return nil, fmt.Errorf("planner step must be positive")
	}

	fingerprint := PlannerStateFingerprint(input.FoundNotes, input.TargetDenom, input.TargetAmount)
	if firstSeenStep, seen := r.seenFingerprints[fingerprint]; seen {
		return nil, fmt.Errorf(
			"planner state repeated at step %d (first seen at step %d); inspect note state with list-notes or retry with --auto-plan=false / --auto-dummy=false for manual control",
			input.Step,
			firstSeenStep,
		)
	}
	r.seenFingerprints[fingerprint] = input.Step

	selection := SelectInputs(input.FoundNotes, input.TargetDenom, input.TargetAmount)
	if selection.NeedsZeroDummy {
		if !input.AutoDummy {
			return nil, buildDummyRequiredError(input.TargetDenom, input.TargetAmount)
		}
		return &RecursivePlannerDecision{
			Action:      RecursivePlannerActionPrepareDummy,
			Fingerprint: fingerprint,
			InputsTotal: big.NewInt(0),
			SendAmount:  big.NewInt(0),
		}, nil
	}
	if selection.Total.Cmp(big.NewInt(0)) == 0 {
		return nil, buildInsufficientFundsError(input.FoundNotes, input.TargetDenom, input.TargetAmount)
	}

	decision := &RecursivePlannerDecision{
		Fingerprint: fingerprint,
		Inputs:      selection.Inputs,
		InputsTotal: normalizedAmount(selection.Total),
	}

	if selection.IsFinal {
		if input.FinalRecipientSpendPubKey == nil || input.FinalRecipientViewPubKey == nil {
			return nil, fmt.Errorf("final recipient spend/view public keys are required for the final transfer step")
		}
		decision.Action = RecursivePlannerActionFinalTransfer
		decision.RecipientSpendPubKey = input.FinalRecipientSpendPubKey
		decision.RecipientViewPubKey = input.FinalRecipientViewPubKey
		decision.SendAmount = normalizedAmount(input.TargetAmount)
		decision.IsFinal = true
		return decision, nil
	}

	if input.SelfSpendPubKey == nil || input.SelfViewPubKey == nil {
		return nil, fmt.Errorf("self spend/view public keys are required for self-merge steps")
	}
	decision.Action = RecursivePlannerActionSelfMerge
	decision.RecipientSpendPubKey = input.SelfSpendPubKey
	decision.RecipientViewPubKey = input.SelfViewPubKey
	decision.SendAmount = normalizedAmount(selection.Total)
	return decision, nil
}

func buildDummyRequiredError(targetDenom string, targetAmount *big.Int) error {
	target := formatAmountWithDenom(targetDenom, targetAmount)
	return fmt.Errorf(
		"transfer needs an explicit zero-value dummy note: it cannot split one larger %s note by itself for %s; the current two-input transfer circuit needs a same-denom zero-value dummy note in the second input slot. Retry with --auto-dummy=true or create 0%s explicitly before retrying",
		targetDenom,
		target,
		targetDenom,
	)
}

func buildInsufficientFundsError(foundNotes []privacyscan.FoundNote, targetDenom string, targetAmount *big.Int) error {
	target := formatAmountWithDenom(targetDenom, targetAmount)
	sameDenomNotes, sameDenomTotal := SummarizeSpendableNotesByDenom(foundNotes, targetDenom)
	if len(sameDenomNotes) == 0 {
		return fmt.Errorf(
			"insufficient shielded funds for %s; no spendable %s notes were found. Run list-notes to inspect wallet state, then deposit more shielded funds",
			target,
			targetDenom,
		)
	}

	return fmt.Errorf(
		"insufficient shielded funds for %s; spendable %s total is only %s across %d notes. Run list-notes to inspect wallet state, then deposit more shielded funds",
		target,
		targetDenom,
		formatAmountWithDenom(targetDenom, sameDenomTotal),
		len(sameDenomNotes),
	)
}

func formatAmountWithDenom(denom string, amount *big.Int) string {
	return normalizedAmount(amount).String() + strings.TrimSpace(denom)
}
