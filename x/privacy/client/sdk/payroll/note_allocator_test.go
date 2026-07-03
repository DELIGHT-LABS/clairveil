package payroll

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNoteAllocatorAllocatesAvailableTwoInputNotes(t *testing.T) {
	input := testPayrollInput()
	input.Items[0].Amount = big.NewInt(70)
	notes := []TreasuryNote{
		testTreasuryNote("spent", "uclair", 100, true, ""),
		testTreasuryNote("reserved", "uclair", 100, false, "reservation-x"),
		testTreasuryNote("zero", "uclair", 0, false, ""),
		testTreasuryNote("large", "uclair", 100, false, ""),
		testTreasuryNote("other-denom", "uatom", 100, false, ""),
	}

	items, err := NoteAllocator{}.Allocate(input, notes)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Len(t, items[0].InputNotes, 2)
	require.Equal(t, "large", items[0].InputNotes[0].NoteID)
	require.Equal(t, "zero", items[0].InputNotes[1].NoteID)
	require.Equal(t, ItemStatusPlanned, items[0].Status)
}

func TestNoteAllocatorUsesBestPairWhenNoZeroDummy(t *testing.T) {
	input := testPayrollInput()
	input.Items[0].Amount = big.NewInt(90)
	notes := []TreasuryNote{
		testTreasuryNote("n1", "uclair", 30, false, ""),
		testTreasuryNote("n2", "uclair", 60, false, ""),
		testTreasuryNote("n3", "uclair", 80, false, ""),
	}

	items, err := NoteAllocator{}.Allocate(input, notes)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Len(t, items[0].InputNotes, 2)
	require.Equal(t, "n1", items[0].InputNotes[0].NoteID)
	require.Equal(t, "n2", items[0].InputNotes[1].NoteID)
}

func TestNoteAllocatorRejectsInsufficientNotes(t *testing.T) {
	input := testPayrollInput()
	input.Items[0].Amount = big.NewInt(90)
	notes := []TreasuryNote{
		testTreasuryNote("n1", "uclair", 30, false, ""),
	}

	_, err := NoteAllocator{}.Allocate(input, notes)
	require.ErrorIs(t, err, ErrInsufficientNotes)
}
