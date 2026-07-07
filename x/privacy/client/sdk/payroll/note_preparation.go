package payroll

import (
	"fmt"
	"math/big"
)

type NotePreparationPolicy struct {
	MaxMessagesPerTx int
}

type NotePreparationInput struct {
	PayrollInput  PayrollInput
	TreasuryNotes []TreasuryNote
	Policy        NotePreparationPolicy
}

type NotePreparationReport struct {
	CompanyID              string
	PayrollID              string
	BatchID                string
	Denom                  string
	TotalItems             int
	ReadyItems             int
	BlockedItems           int
	SpendableNoteCount     int
	ReservedNoteCount      int
	SpentNoteCount         int
	ZeroDummyAvailable     int
	ZeroDummyRequired      int
	TotalPayrollAmount     *big.Int
	TotalSpendableAmount   *big.Int
	EstimatedMessageChunks int
	Items                  []NotePreparationItemReport
	Recommendations        []NotePreparationRecommendation
	OperationHints         []NotePreparationOperationHint
}

type NotePreparationItemReport struct {
	ItemID          string
	EmployeeID      string
	Amount          *big.Int
	Ready           bool
	Reason          string
	SelectedNoteIDs []string
}

type NotePreparationRecommendation struct {
	Kind             string
	ItemID           string
	Message          string
	RequiredCount    int
	TargetAmount     *big.Int
	Denom            string
	CandidateNoteIDs []string
}

type NotePreparationOperationHint struct {
	Kind             string
	ItemID           string
	Message          string
	RequiredCount    int
	TargetAmount     *big.Int
	Denom            string
	CandidateNoteIDs []string
}

const (
	NotePreparationRecommendationAddFunds    = "add-funds"
	NotePreparationRecommendationMakeDummy   = "make-dummy"
	NotePreparationRecommendationSplitMerge  = "split-merge"
	NotePreparationRecommendationResolveLock = "resolve-reservation-lock"
)

func AnalyzeNotePreparation(input NotePreparationInput) (*NotePreparationReport, error) {
	payrollInput := normalizePayrollInput(input.PayrollInput)
	if err := ValidateInput(payrollInput); err != nil {
		return nil, err
	}

	report := &NotePreparationReport{
		CompanyID:            payrollInput.CompanyID,
		PayrollID:            payrollInput.PayrollID,
		BatchID:              payrollInput.BatchID,
		Denom:                payrollInput.Denom,
		TotalItems:           len(payrollInput.Items),
		TotalPayrollAmount:   big.NewInt(0),
		TotalSpendableAmount: big.NewInt(0),
		Items:                make([]NotePreparationItemReport, len(payrollInput.Items)),
	}

	for _, note := range input.TreasuryNotes {
		switch {
		case note.IsSpent:
			report.SpentNoteCount++
		case note.ReservationID != "":
			report.ReservedNoteCount++
		}
	}

	available := filterAvailableNotes(payrollInput.Denom, input.TreasuryNotes)
	report.SpendableNoteCount = len(available)
	for _, note := range available {
		if note.Amount == nil {
			continue
		}
		report.TotalSpendableAmount.Add(report.TotalSpendableAmount, note.Amount)
		if note.Amount.Sign() == 0 {
			report.ZeroDummyAvailable++
		}
	}
	for _, item := range payrollInput.Items {
		report.TotalPayrollAmount.Add(report.TotalPayrollAmount, item.Amount)
	}

	remaining := cloneTreasuryNotes(available)
	allocation := newNoteAllocationState(remaining)
	for i, item := range payrollInput.Items {
		itemReport := NotePreparationItemReport{
			ItemID:     item.ItemID,
			EmployeeID: item.EmployeeID,
			Amount:     cloneBigInt(item.Amount),
		}
		selected, err := allocation.selectInputNotes(item.Amount)
		if err == nil {
			itemReport.Ready = true
			itemReport.SelectedNoteIDs = noteIDs(selected)
			report.ReadyItems++
			report.Items[i] = itemReport
			continue
		}

		itemReport.Reason = err.Error()
		report.BlockedItems++
		report.Items[i] = itemReport
		report.addRecommendation(preparationRecommendationForBlockedItem(item, available, report.ZeroDummyAvailable, payrollInput.Denom))
	}

	if report.TotalSpendableAmount.Cmp(report.TotalPayrollAmount) < 0 {
		shortage := new(big.Int).Sub(report.TotalPayrollAmount, report.TotalSpendableAmount)
		report.addRecommendation(NotePreparationRecommendation{
			Kind:          NotePreparationRecommendationAddFunds,
			Message:       fmt.Sprintf("spendable total %s is below payroll total %s", report.TotalSpendableAmount.String(), report.TotalPayrollAmount.String()),
			TargetAmount:  shortage,
			Denom:         payrollInput.Denom,
			RequiredCount: 1,
		})
	}
	if report.ReservedNoteCount > 0 {
		report.addRecommendation(NotePreparationRecommendation{
			Kind:          NotePreparationRecommendationResolveLock,
			Message:       fmt.Sprintf("%d treasury notes are already reserved and excluded from preparation", report.ReservedNoteCount),
			RequiredCount: report.ReservedNoteCount,
		})
	}

	report.ZeroDummyRequired = zeroDummyShortage(payrollInput.Items, available)
	if report.ZeroDummyRequired > 0 {
		report.addRecommendation(NotePreparationRecommendation{
			Kind:          NotePreparationRecommendationMakeDummy,
			Message:       fmt.Sprintf("prepare at least %d additional zero-value dummy notes", report.ZeroDummyRequired),
			RequiredCount: report.ZeroDummyRequired,
			TargetAmount:  big.NewInt(0),
			Denom:         payrollInput.Denom,
		})
	}
	report.EstimatedMessageChunks = estimatePreparationMessageChunks(report.ReadyItems, input.Policy.MaxMessagesPerTx)
	return report, nil
}

func (r *NotePreparationReport) addRecommendation(recommendation NotePreparationRecommendation) {
	r.Recommendations = append(r.Recommendations, recommendation)
	r.OperationHints = append(r.OperationHints, NotePreparationOperationHint{
		Kind:             recommendation.Kind,
		ItemID:           recommendation.ItemID,
		Message:          recommendation.Message,
		RequiredCount:    recommendation.RequiredCount,
		TargetAmount:     cloneBigInt(recommendation.TargetAmount),
		Denom:            recommendation.Denom,
		CandidateNoteIDs: append([]string(nil), recommendation.CandidateNoteIDs...),
	})
}

func preparationRecommendationForBlockedItem(item PayrollItemInput, available []TreasuryNote, zeroAvailable int, denom string) NotePreparationRecommendation {
	if hasSingleNoteCandidate(item.Amount, available) && zeroAvailable == 0 {
		return NotePreparationRecommendation{
			Kind:             NotePreparationRecommendationMakeDummy,
			ItemID:           item.ItemID,
			Message:          "a sufficient single note exists, but a zero-value dummy note is required by the current 2-input transfer circuit",
			RequiredCount:    1,
			TargetAmount:     big.NewInt(0),
			Denom:            denom,
			CandidateNoteIDs: candidateNoteIDsForSingleNote(item.Amount, available, 3),
		}
	}
	return NotePreparationRecommendation{
		Kind:             NotePreparationRecommendationSplitMerge,
		ItemID:           item.ItemID,
		Message:          "prepare exact or pairable notes before batching this payroll item",
		RequiredCount:    1,
		TargetAmount:     cloneBigInt(item.Amount),
		Denom:            denom,
		CandidateNoteIDs: candidateNoteIDsForPreparation(item.Amount, available, 6),
	}
}

func hasSingleNoteCandidate(target *big.Int, notes []TreasuryNote) bool {
	for _, note := range notes {
		if note.Amount != nil && finalPayrollOutputsWithinBound(note.Amount, target) {
			return true
		}
	}
	return false
}

func zeroDummyShortage(items []PayrollItemInput, notes []TreasuryNote) int {
	zeroAvailable := 0
	for _, note := range notes {
		if note.Amount != nil && note.Amount.Sign() == 0 {
			zeroAvailable++
		}
	}
	required := 0
	for _, item := range items {
		if hasSingleNoteCandidate(item.Amount, notes) {
			required++
		}
	}
	if required <= zeroAvailable {
		return 0
	}
	return required - zeroAvailable
}

func estimatePreparationMessageChunks(readyItems int, maxMessagesPerTx int) int {
	if readyItems <= 0 {
		return 0
	}
	if maxMessagesPerTx <= 0 {
		maxMessagesPerTx = 1
	}
	return (readyItems + maxMessagesPerTx - 1) / maxMessagesPerTx
}

func candidateNoteIDsForSingleNote(target *big.Int, available []TreasuryNote, limit int) []string {
	if target == nil || limit <= 0 {
		return nil
	}
	ids := make([]string, 0, limit)
	for _, index := range payrollSingleCandidateIndexes(available, target, limit) {
		ids = append(ids, available[index].NoteID)
	}
	return ids
}

func candidateNoteIDsForPreparation(target *big.Int, available []TreasuryNote, limit int) []string {
	if target == nil || limit <= 0 {
		return nil
	}
	seen := make(map[string]struct{}, limit)
	ids := make([]string, 0, limit)
	add := func(note TreasuryNote) bool {
		if note.NoteID == "" {
			return true
		}
		if _, exists := seen[note.NoteID]; exists {
			return true
		}
		seen[note.NoteID] = struct{}{}
		ids = append(ids, note.NoteID)
		return len(ids) < limit
	}
	for _, index := range payrollZeroCandidateIndexes(available, limit) {
		if !add(available[index]) {
			return ids
		}
	}
	for _, index := range payrollSingleCandidateIndexes(available, target, limit) {
		if !add(available[index]) {
			return ids
		}
	}
	for _, candidate := range boundedPayrollPairCandidates(available, target, limit) {
		if !add(available[candidate.left]) || !add(available[candidate.right]) {
			return ids
		}
	}
	return ids
}

func noteIDs(notes []TreasuryNote) []string {
	out := make([]string, 0, len(notes))
	for _, note := range notes {
		out = append(out, note.NoteID)
	}
	return out
}
