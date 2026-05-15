package withdraw

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	privacyscan "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/scan"
)

type ExactMatchNoteSource interface {
	LoadFoundNotes(ctx context.Context) ([]privacyscan.FoundNote, error)
}

type ExactMatchAutoPlanner interface {
	AutoPlanExactMatchNote(ctx context.Context, targetCoin sdk.Coin) error
}

func ResolveExactMatchSpendableNote(
	ctx context.Context,
	source ExactMatchNoteSource,
	planner ExactMatchAutoPlanner,
	targetCoin sdk.Coin,
	autoPlan bool,
) (*privacyscan.FoundNote, error) {
	if source == nil {
		return nil, fmt.Errorf("an exact-match note source is required")
	}

	foundNotes, err := source.LoadFoundNotes(ctx)
	if err != nil {
		return nil, err
	}

	targetAmount := targetCoin.Amount.BigInt()
	selected := FindExactMatchSpendableNoteByDenom(foundNotes, targetCoin.Denom, targetAmount)
	if selected != nil {
		return selected, nil
	}
	if !autoPlan {
		return nil, BuildExactMatchError(targetCoin, foundNotes)
	}
	if planner == nil {
		return nil, fmt.Errorf("an exact-match auto planner is required when auto-plan is enabled")
	}

	if err := planner.AutoPlanExactMatchNote(ctx, targetCoin); err != nil {
		return nil, fmt.Errorf("withdraw auto-plan failed: %w", err)
	}

	foundNotes, err = source.LoadFoundNotes(ctx)
	if err != nil {
		return nil, err
	}

	selected = FindExactMatchSpendableNoteByDenom(foundNotes, targetCoin.Denom, targetAmount)
	if selected != nil {
		return selected, nil
	}

	return nil, BuildExactMatchError(targetCoin, foundNotes)
}
