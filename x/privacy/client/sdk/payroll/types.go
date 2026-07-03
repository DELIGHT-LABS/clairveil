package payroll

import (
	"math/big"
	"time"
)

type PayrollInput struct {
	CompanyID string
	PayrollID string
	BatchID   string
	Denom     string
	Attempt   int
	Items     []PayrollItemInput
	CreatedAt time.Time
}

type PayrollItemInput struct {
	ItemID                   string
	EmployeeID               string
	RecipientAddress         string
	Amount                   *big.Int
	Denom                    string
	ExpectedOutputCommitment string
	ExpectedDisclosureDigest string
}

type TreasuryNote struct {
	NoteID               string
	OwnerKeyID           string
	NullifierLookupKey   string
	NullifierLookupKeyID string
	Denom                string
	Amount               *big.Int
	IsSpent              bool
	ReservationID        string
}

type PayrollPlan struct {
	CompanyID string
	PayrollID string
	BatchID   string
	Denom     string
	Attempt   int
	Status    PlanStatus
	Items     []PayrollPlanItem
	CreatedAt time.Time
	UpdatedAt time.Time
}

type PayrollPlanItem struct {
	CompanyID                string
	PayrollID                string
	BatchID                  string
	Attempt                  int
	ChunkID                  string
	ItemID                   string
	EmployeeID               string
	OperationID              string
	RecipientAddress         string
	ExpectedRecipientHash    string
	Amount                   *big.Int
	ExpectedAmountHash       string
	Denom                    string
	ExpectedOutputCommitment string
	ExpectedDisclosureDigest string
	InputNotes               []TreasuryNote
	Status                   ItemStatus
	FailureReason            string
	RetryCount               int
}

func clonePlan(plan PayrollPlan) PayrollPlan {
	plan.Items = clonePlanItems(plan.Items)
	return plan
}

func clonePlanItems(items []PayrollPlanItem) []PayrollPlanItem {
	out := make([]PayrollPlanItem, len(items))
	for i := range items {
		out[i] = clonePlanItem(items[i])
	}
	return out
}

func clonePlanItem(item PayrollPlanItem) PayrollPlanItem {
	item.Amount = cloneBigInt(item.Amount)
	item.InputNotes = cloneTreasuryNotes(item.InputNotes)
	return item
}

func cloneTreasuryNotes(notes []TreasuryNote) []TreasuryNote {
	out := make([]TreasuryNote, len(notes))
	for i := range notes {
		out[i] = notes[i]
		out[i].Amount = cloneBigInt(notes[i].Amount)
	}
	return out
}

func cloneBigInt(v *big.Int) *big.Int {
	if v == nil {
		return nil
	}
	return new(big.Int).Set(v)
}
