package payroll

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAnalyzeNotePreparationReportsReadyItems(t *testing.T) {
	input := testPayrollInput()
	input.Items[0].Amount = big.NewInt(70)
	notes := []TreasuryNote{
		testTreasuryNote("large", "uclair", 100, false, ""),
		testTreasuryNote("zero", "uclair", 0, false, ""),
	}

	report, err := AnalyzeNotePreparation(NotePreparationInput{
		PayrollInput:  input,
		TreasuryNotes: notes,
		Policy:        NotePreparationPolicy{MaxMessagesPerTx: 20},
	})
	require.NoError(t, err)
	require.Equal(t, 1, report.TotalItems)
	require.Equal(t, 1, report.ReadyItems)
	require.Equal(t, 0, report.BlockedItems)
	require.Equal(t, 2, report.SpendableNoteCount)
	require.Equal(t, 1, report.ZeroDummyAvailable)
	require.Equal(t, 1, report.EstimatedMessageChunks)
	require.Equal(t, []string{"large", "zero"}, report.Items[0].SelectedNoteIDs)
}

func TestAnalyzeNotePreparationRecommendsDummyForSingleNote(t *testing.T) {
	input := testPayrollInput()
	input.Items[0].Amount = big.NewInt(70)
	notes := []TreasuryNote{
		testTreasuryNote("large", "uclair", 100, false, ""),
	}

	report, err := AnalyzeNotePreparation(NotePreparationInput{
		PayrollInput:  input,
		TreasuryNotes: notes,
	})
	require.NoError(t, err)
	require.Equal(t, 0, report.ReadyItems)
	require.Equal(t, 1, report.BlockedItems)
	require.Equal(t, 1, report.ZeroDummyRequired)
	require.NotEmpty(t, report.Recommendations)
	require.Equal(t, NotePreparationRecommendationMakeDummy, report.Recommendations[0].Kind)
	require.NotEmpty(t, report.OperationHints)
	require.Equal(t, NotePreparationRecommendationMakeDummy, report.OperationHints[0].Kind)
	require.Equal(t, 1, report.OperationHints[0].RequiredCount)
	require.Equal(t, []string{"large"}, report.OperationHints[0].CandidateNoteIDs)
}

func TestAnalyzeNotePreparationExcludesReservedNotes(t *testing.T) {
	input := testPayrollInput()
	input.Items[0].Amount = big.NewInt(70)
	notes := []TreasuryNote{
		testTreasuryNote("large", "uclair", 100, false, "reservation-a"),
		testTreasuryNote("zero", "uclair", 0, false, ""),
	}

	report, err := AnalyzeNotePreparation(NotePreparationInput{
		PayrollInput:  input,
		TreasuryNotes: notes,
	})
	require.NoError(t, err)
	require.Equal(t, 1, report.ReservedNoteCount)
	require.Equal(t, 1, report.SpendableNoteCount)
	require.Equal(t, 0, report.ReadyItems)
	require.Contains(t, report.Recommendations[len(report.Recommendations)-1].Kind, NotePreparationRecommendationResolveLock)
}
