package payroll

import (
	"math/big"
	"time"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

type PayrollInput struct {
	CompanyID               string
	PayrollID               string
	BatchID                 string
	Denom                   string
	Attempt                 int
	DefaultDisclosurePolicy PayrollDisclosurePolicy
	Items                   []PayrollItemInput
	CreatedAt               time.Time
}

type PayrollItemInput struct {
	ItemID                   string
	EmployeeID               string
	RecipientAddress         string
	Amount                   *big.Int
	Denom                    string
	DisclosurePolicy         PayrollDisclosurePolicy
	DisclosurePolicySet      bool
	ExpectedOutputCommitment string
	ExpectedDisclosureDigest string
}

type PayrollDisclosurePolicy struct {
	UserPrivacyPolicy                uint32
	UserDisclosureMode               privacytypes.UserDisclosureMode
	UserDisclosureTargetPubKeyHex    string
	UserDisclosureTargetKeyID        string
	ExpectedUserDisclosureDigest     string
	ExpectedAuditDisclosureDigest    string
	ExpectedSelfViewDisclosureDigest string
}

type TreasuryNote struct {
	NoteID               string
	OwnerKeyID           string
	NullifierLookupKey   string
	NullifierLookupKeyID string
	Denom                string
	Amount               *big.Int
	IsSpent              bool
	VerifiedUnspent      bool
	ReservationID        string
}

func (n TreasuryNote) IsVerifiedUnspent() bool {
	return !n.IsSpent && n.VerifiedUnspent
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
	DisclosurePolicy         PayrollDisclosurePolicy
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
