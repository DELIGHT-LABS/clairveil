package payroll

type PlanReport struct {
	PayrollID           string
	TotalItems          int
	PlannedItems        int
	ReservedItems       int
	SubmittedItems      int
	ConfirmedItems      int
	FailedItems         int
	ReplanRequiredItems int
	ManualReviewItems   int
}

func BuildPlanReport(plan PayrollPlan) PlanReport {
	report := PlanReport{
		PayrollID:  plan.PayrollID,
		TotalItems: len(plan.Items),
	}
	for _, item := range plan.Items {
		switch item.Status {
		case ItemStatusPlanned:
			report.PlannedItems++
		case ItemStatusReserved, ItemStatusProving, ItemStatusProofReady:
			report.ReservedItems++
		case ItemStatusSubmitted:
			report.SubmittedItems++
		case ItemStatusConfirmed:
			report.ConfirmedItems++
		case ItemStatusFailed:
			report.FailedItems++
		case ItemStatusReplanRequired:
			report.ReplanRequiredItems++
		case ItemStatusManualReview:
			report.ManualReviewItems++
		}
	}
	return report
}
