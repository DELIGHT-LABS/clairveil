package withdraw

import (
	"context"
	"fmt"
	"math/big"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	privacyscan "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/scan"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestResolveExactMatchSpendableNoteReturnsExistingNote(t *testing.T) {
	source := &stubExactMatchNoteSource{
		responses: [][]privacyscan.FoundNote{
			{
				{Note: privacytypes.Note{Amount: big.NewInt(10), AssetID: privacycrypto.HashString("uclair")}},
			},
		},
	}
	planner := &stubExactMatchAutoPlanner{}

	selected, err := ResolveExactMatchSpendableNote(context.Background(), source, planner, sdk.NewInt64Coin("uclair", 10), false)
	require.NoError(t, err)
	require.NotNil(t, selected)
	require.Equal(t, int64(10), selected.Note.Amount.Int64())
	require.Len(t, source.calls, 1)
	require.Len(t, planner.calls, 0)
}

func TestResolveExactMatchSpendableNoteAutoPlansAndRescans(t *testing.T) {
	source := &stubExactMatchNoteSource{
		responses: [][]privacyscan.FoundNote{
			{
				{Note: privacytypes.Note{Amount: big.NewInt(7), AssetID: privacycrypto.HashString("uclair")}},
			},
			{
				{Note: privacytypes.Note{Amount: big.NewInt(10), AssetID: privacycrypto.HashString("uclair")}},
			},
		},
	}
	planner := &stubExactMatchAutoPlanner{}

	selected, err := ResolveExactMatchSpendableNote(context.Background(), source, planner, sdk.NewInt64Coin("uclair", 10), true)
	require.NoError(t, err)
	require.NotNil(t, selected)
	require.Equal(t, int64(10), selected.Note.Amount.Int64())
	require.Len(t, source.calls, 2)
	require.Len(t, planner.calls, 1)
	require.Equal(t, "10uclair", planner.calls[0].String())
}

func TestResolveExactMatchSpendableNoteReturnsGuidanceWithoutAutoPlan(t *testing.T) {
	source := &stubExactMatchNoteSource{
		responses: [][]privacyscan.FoundNote{
			{
				{Note: privacytypes.Note{Amount: big.NewInt(7), AssetID: privacycrypto.HashString("uclair")}},
			},
		},
	}

	_, err := ResolveExactMatchSpendableNote(context.Background(), source, nil, sdk.NewInt64Coin("uclair", 10), false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "exact-match note")
	require.Contains(t, err.Error(), "shielded self-transfer")
}

func TestResolveExactMatchSpendableNoteWrapsPlannerError(t *testing.T) {
	source := &stubExactMatchNoteSource{
		responses: [][]privacyscan.FoundNote{
			{
				{Note: privacytypes.Note{Amount: big.NewInt(7), AssetID: privacycrypto.HashString("uclair")}},
			},
		},
	}
	planner := &stubExactMatchAutoPlanner{returnErr: fmt.Errorf("boom")}

	_, err := ResolveExactMatchSpendableNote(context.Background(), source, planner, sdk.NewInt64Coin("uclair", 10), true)
	require.ErrorContains(t, err, "withdraw auto-plan failed")
	require.ErrorContains(t, err, "boom")
}

type stubExactMatchNoteSource struct {
	responses [][]privacyscan.FoundNote
	calls     []struct{}
	returnErr error
}

func (s *stubExactMatchNoteSource) LoadFoundNotes(_ context.Context) ([]privacyscan.FoundNote, error) {
	if s.returnErr != nil {
		return nil, s.returnErr
	}
	s.calls = append(s.calls, struct{}{})
	if len(s.responses) == 0 {
		return nil, nil
	}
	index := len(s.calls) - 1
	if index >= len(s.responses) {
		index = len(s.responses) - 1
	}
	return append([]privacyscan.FoundNote(nil), s.responses[index]...), nil
}

type stubExactMatchAutoPlanner struct {
	calls     []sdk.Coin
	returnErr error
}

func (s *stubExactMatchAutoPlanner) AutoPlanExactMatchNote(_ context.Context, targetCoin sdk.Coin) error {
	s.calls = append(s.calls, targetCoin)
	return s.returnErr
}
