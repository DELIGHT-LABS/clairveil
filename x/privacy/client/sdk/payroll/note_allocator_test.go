package payroll

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
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

func TestNoteAllocatorExcludesUnverifiedNotes(t *testing.T) {
	input := testPayrollInput()
	input.Items[0].Amount = big.NewInt(70)
	notes := []TreasuryNote{
		testTreasuryNote("zero", "uclair", 0, false, ""),
		testTreasuryNote("large", "uclair", 100, false, ""),
	}
	notes[0].VerifiedUnspent = false

	_, err := NoteAllocator{}.Allocate(input, notes)
	require.ErrorContains(t, err, "insufficient treasury notes")
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

func TestNoteAllocatorKeepsUnselectedSmallNotesForLaterItems(t *testing.T) {
	input := testPayrollInput()
	input.Items = []PayrollItemInput{
		{
			ItemID:           "item-1",
			EmployeeID:       "employee-1",
			RecipientAddress: testRecipientAddress("1"),
			Amount:           big.NewInt(52),
		},
		{
			ItemID:           "item-2",
			EmployeeID:       "employee-2",
			RecipientAddress: testRecipientAddress("2"),
			Amount:           big.NewInt(55),
		},
	}
	notes := []TreasuryNote{
		testTreasuryNote("n1", "uclair", 1, false, ""),
		testTreasuryNote("n2", "uclair", 2, false, ""),
		testTreasuryNote("n50", "uclair", 50, false, ""),
		testTreasuryNote("n60", "uclair", 60, false, ""),
	}

	items, err := NoteAllocator{}.Allocate(input, notes)
	require.NoError(t, err)
	require.Len(t, items, 2)
	require.Equal(t, []string{"n2", "n50"}, []string{items[0].InputNotes[0].NoteID, items[0].InputNotes[1].NoteID})
	require.Equal(t, []string{"n1", "n60"}, []string{items[1].InputNotes[0].NoteID, items[1].InputNotes[1].NoteID})
}

func TestNoteAllocatorPreservesZeroDummyForLargerItems(t *testing.T) {
	input := testPayrollInput()
	input.Items = []PayrollItemInput{
		{
			ItemID:           "small",
			EmployeeID:       "employee-small",
			RecipientAddress: testRecipientAddress("1"),
			Amount:           big.NewInt(5),
		},
		{
			ItemID:           "large",
			EmployeeID:       "employee-large",
			RecipientAddress: testRecipientAddress("2"),
			Amount:           big.NewInt(100),
		},
	}
	notes := []TreasuryNote{
		testTreasuryNote("zero", "uclair", 0, false, ""),
		testTreasuryNote("two", "uclair", 2, false, ""),
		testTreasuryNote("three", "uclair", 3, false, ""),
		testTreasuryNote("hundred", "uclair", 100, false, ""),
	}

	items, err := NoteAllocator{}.Allocate(input, notes)
	require.NoError(t, err)
	require.Len(t, items, 2)
	require.Equal(t, []string{"two", "three"}, []string{items[0].InputNotes[0].NoteID, items[0].InputNotes[1].NoteID})
	require.Equal(t, []string{"hundred", "zero"}, []string{items[1].InputNotes[0].NoteID, items[1].InputNotes[1].NoteID})
}

func TestNoteAllocatorBacktracksSmallFeasiblePayroll(t *testing.T) {
	input := testPayrollInput()
	input.Items = []PayrollItemInput{
		{
			ItemID:           "tiny",
			EmployeeID:       "employee-tiny",
			RecipientAddress: testRecipientAddress("1"),
			Amount:           big.NewInt(1),
		},
		{
			ItemID:           "medium",
			EmployeeID:       "employee-medium",
			RecipientAddress: testRecipientAddress("2"),
			Amount:           big.NewInt(3),
		},
		{
			ItemID:           "large",
			EmployeeID:       "employee-large",
			RecipientAddress: testRecipientAddress("3"),
			Amount:           big.NewInt(4),
		},
	}
	notes := []TreasuryNote{
		testTreasuryNote("zero-a", "uclair", 0, false, ""),
		testTreasuryNote("zero-b", "uclair", 0, false, ""),
		testTreasuryNote("one", "uclair", 1, false, ""),
		testTreasuryNote("two-a", "uclair", 2, false, ""),
		testTreasuryNote("two-b", "uclair", 2, false, ""),
		testTreasuryNote("three", "uclair", 3, false, ""),
	}

	items, err := NoteAllocator{}.Allocate(input, notes)
	require.NoError(t, err)
	require.Len(t, items, 3)
	require.Equal(t, "one", items[0].InputNotes[0].NoteID)
	require.Equal(t, "0", items[0].InputNotes[1].Amount.String())
	require.Equal(t, "three", items[1].InputNotes[0].NoteID)
	require.Equal(t, "0", items[1].InputNotes[1].Amount.String())
	require.Equal(t, []string{"two-a", "two-b"}, []string{items[2].InputNotes[0].NoteID, items[2].InputNotes[1].NoteID})
}

func TestNoteAllocatorBacktracksSmallPayrollWithManyAvailableNotes(t *testing.T) {
	input := testPayrollInput()
	input.Items = []PayrollItemInput{
		{
			ItemID:           "item-20",
			EmployeeID:       "employee-20",
			RecipientAddress: testRecipientAddress("1"),
			Amount:           big.NewInt(20),
		},
		{
			ItemID:           "item-15",
			EmployeeID:       "employee-15",
			RecipientAddress: testRecipientAddress("2"),
			Amount:           big.NewInt(15),
		},
		{
			ItemID:           "item-5",
			EmployeeID:       "employee-5",
			RecipientAddress: testRecipientAddress("3"),
			Amount:           big.NewInt(5),
		},
	}
	notes := []TreasuryNote{
		testTreasuryNote("n20", "uclair", 20, false, ""),
		testTreasuryNote("n15", "uclair", 15, false, ""),
		testTreasuryNote("n6", "uclair", 6, false, ""),
		testTreasuryNote("n3", "uclair", 3, false, ""),
	}
	for i := 0; i < 21; i++ {
		notes = append(notes, testTreasuryNote("extra-one-"+big.NewInt(int64(i)).String(), "uclair", 1, false, ""))
	}

	items, err := NoteAllocator{}.Allocate(input, notes)
	require.NoError(t, err)
	require.Len(t, items, 3)
	for i, item := range items {
		require.Len(t, item.InputNotes, 2)
		total := new(big.Int).Add(item.InputNotes[0].Amount, item.InputNotes[1].Amount)
		require.GreaterOrEqual(t, total.Cmp(input.Items[i].Amount), 0)
	}
}

func TestNoteAllocatorRetriesChunkedExactWhenLargeGreedyBatchFails(t *testing.T) {
	input := testPayrollInput()
	input.Items = make([]PayrollItemInput, 0, 13)
	notes := make([]TreasuryNote, 0, 26)
	for i := 0; i < 10; i++ {
		id := big.NewInt(int64(i)).String()
		input.Items = append(input.Items, PayrollItemInput{
			ItemID:           "large-" + id,
			EmployeeID:       "employee-large-" + id,
			RecipientAddress: testRecipientAddress(id),
			Amount:           big.NewInt(1000),
		})
		notes = append(notes,
			testTreasuryNote("large-note-"+id, "uclair", 1000, false, ""),
			testTreasuryNote("large-zero-"+id, "uclair", 0, false, ""),
		)
	}
	input.Items = append(input.Items,
		PayrollItemInput{ItemID: "core-4", EmployeeID: "employee-core-4", RecipientAddress: testRecipientAddress("1"), Amount: big.NewInt(4)},
		PayrollItemInput{ItemID: "core-2a", EmployeeID: "employee-core-2a", RecipientAddress: testRecipientAddress("2"), Amount: big.NewInt(2)},
		PayrollItemInput{ItemID: "core-2b", EmployeeID: "employee-core-2b", RecipientAddress: testRecipientAddress("3"), Amount: big.NewInt(2)},
	)
	notes = append(notes,
		testTreasuryNote("core-zero-a", "uclair", 0, false, ""),
		testTreasuryNote("core-zero-b", "uclair", 0, false, ""),
		testTreasuryNote("core-one", "uclair", 1, false, ""),
		testTreasuryNote("core-two-a", "uclair", 2, false, ""),
		testTreasuryNote("core-two-b", "uclair", 2, false, ""),
		testTreasuryNote("core-three", "uclair", 3, false, ""),
	)

	items, err := NoteAllocator{}.Allocate(input, notes)
	require.NoError(t, err)
	require.Len(t, items, 13)
	for i, item := range items {
		require.Len(t, item.InputNotes, 2)
		total := new(big.Int).Add(item.InputNotes[0].Amount, item.InputNotes[1].Amount)
		require.GreaterOrEqual(t, total.Cmp(input.Items[i].Amount), 0)
	}
	require.Equal(t, []string{"core-one", "core-three"}, []string{items[10].InputNotes[0].NoteID, items[10].InputNotes[1].NoteID})
}

func TestNoteAllocatorLargeFragmentedTreasuryProperties(t *testing.T) {
	const itemCount = 40
	input := testPayrollInput()
	input.Items = make([]PayrollItemInput, 0, itemCount)
	notes := make([]TreasuryNote, 0, itemCount*2+12)
	for i := 0; i < itemCount; i++ {
		id := big.NewInt(int64(i)).String()
		amount := int64(25 + i%11)
		left := amount / 2
		right := amount - left
		input.Items = append(input.Items, PayrollItemInput{
			ItemID:           "item-" + id,
			EmployeeID:       "employee-" + id,
			RecipientAddress: testRecipientAddress(id),
			Amount:           big.NewInt(amount),
		})
		notes = append(notes,
			testTreasuryNote("pair-left-"+id, "uclair", left, false, ""),
			testTreasuryNote("pair-right-"+id, "uclair", right, false, ""),
		)
	}
	for i := 0; i < 4; i++ {
		id := big.NewInt(int64(i)).String()
		notes = append(notes,
			testTreasuryNote("spent-"+id, "uclair", 100, true, ""),
			testTreasuryNote("reserved-"+id, "uclair", 100, false, "reservation-"+id),
			testTreasuryNote("other-denom-"+id, "uatom", 100, false, ""),
		)
	}

	items, err := NoteAllocator{}.Allocate(input, notes)
	require.NoError(t, err)
	require.Len(t, items, itemCount)
	used := make(map[string]struct{}, itemCount*2)
	for i, item := range items {
		require.Equal(t, input.Items[i].ItemID, item.ItemID)
		require.Len(t, item.InputNotes, 2)
		total := new(big.Int).Add(item.InputNotes[0].Amount, item.InputNotes[1].Amount)
		require.GreaterOrEqual(t, total.Cmp(input.Items[i].Amount), 0)
		change := new(big.Int).Sub(total, input.Items[i].Amount)
		require.LessOrEqual(t, change.Cmp(privacytypes.MaxShieldedAmount()), 0)
		for _, note := range item.InputNotes {
			require.Equal(t, "uclair", note.Denom)
			require.False(t, note.IsSpent)
			require.Empty(t, note.ReservationID)
			require.NotContains(t, used, note.NoteID)
			used[note.NoteID] = struct{}{}
		}
	}
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

func TestNoteAllocatorRejectsPairThatWouldOverflowChangeOutput(t *testing.T) {
	input := testPayrollInput()
	input.Items[0].Amount = big.NewInt(1)
	tooLargeChange := new(big.Int).Add(privacytypes.MaxShieldedAmount(), big.NewInt(2))
	notes := []TreasuryNote{
		testTreasuryNoteBig("huge", "uclair", tooLargeChange, false, ""),
		testTreasuryNote("one", "uclair", 1, false, ""),
	}

	_, err := NoteAllocator{}.Allocate(input, notes)
	require.ErrorIs(t, err, ErrInsufficientNotes)
}

func TestNoteAllocatorAllocatesLargeZeroDummyPayroll(t *testing.T) {
	const itemCount = 5000
	input := testPayrollInput()
	input.Items = make([]PayrollItemInput, 0, itemCount)
	notes := make([]TreasuryNote, 0, itemCount*2)
	for i := 0; i < itemCount; i++ {
		id := string(rune('a'+(i%26))) + "-" + big.NewInt(int64(i)).String()
		input.Items = append(input.Items, PayrollItemInput{
			ItemID:           "item-" + id,
			EmployeeID:       "employee-" + id,
			RecipientAddress: testRecipientAddress(id),
			Amount:           big.NewInt(70),
		})
		notes = append(notes,
			testTreasuryNote("large-"+id, "uclair", 100, false, ""),
			testTreasuryNote("zero-"+id, "uclair", 0, false, ""),
		)
	}

	items, err := NoteAllocator{}.Allocate(input, notes)
	require.NoError(t, err)
	require.Len(t, items, itemCount)
	for i := range items {
		require.Equal(t, input.Items[i].ItemID, items[i].ItemID)
		require.Len(t, items[i].InputNotes, 2)
		require.Equal(t, "100", items[i].InputNotes[0].Amount.String())
		require.Equal(t, "0", items[i].InputNotes[1].Amount.String())
	}
}

func TestPayrollIDsDoNotCollideAcrossDelimitedInputs(t *testing.T) {
	require.NotEqual(t,
		operationID("company", "batch", "payroll", "item:attempt:001", 0),
		operationID("company", "batch", "payroll:item", "attempt", 1),
	)
	require.NotEqual(t,
		chunkID("company", "batch", "payroll:attempt:001", 0, 7),
		chunkID("company", "batch", "payroll", 1, 7),
	)
	require.NotEqual(t,
		reservationID(operationID("company", "batch", "payroll", "item", 1), "note:abc"),
		reservationID(operationID("company", "batch", "payroll", "item:note", 0), "abc"),
	)
	require.NotEqual(t,
		operationID("company-a", "batch-a", "payroll", "item", 0),
		operationID("company-b", "batch-a", "payroll", "item", 0),
	)
}
