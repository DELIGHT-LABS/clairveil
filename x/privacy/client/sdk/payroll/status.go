package payroll

type PlanStatus string

const (
	PlanStatusDraft     PlanStatus = "Draft"
	PlanStatusConfirmed PlanStatus = "Confirmed"
	PlanStatusCancelled PlanStatus = "Cancelled"
)

type ItemStatus string

const (
	ItemStatusPlanned        ItemStatus = "Planned"
	ItemStatusReserved       ItemStatus = "Reserved"
	ItemStatusProving        ItemStatus = "Proving"
	ItemStatusProofReady     ItemStatus = "ProofReady"
	ItemStatusSubmitted      ItemStatus = "Submitted"
	ItemStatusConfirmed      ItemStatus = "Confirmed"
	ItemStatusFailed         ItemStatus = "Failed"
	ItemStatusReplanRequired ItemStatus = "ReplanRequired"
	ItemStatusManualReview   ItemStatus = "ManualReview"
)
