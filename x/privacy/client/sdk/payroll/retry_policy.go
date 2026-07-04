package payroll

import "strings"

type RetryAction string

const (
	RetryActionReconcileUnknown  RetryAction = "ReconcileUnknown"
	RetryActionRebuildTx         RetryAction = "RebuildTx"
	RetryActionReplan            RetryAction = "Replan"
	RetryActionManualReview      RetryAction = "ManualReview"
	RetryActionMarkConflictSpent RetryAction = "ConflictSpent"
)

type RetryDecision struct {
	Action RetryAction
	Reason string
}

func ClassifyBroadcastError(message string) RetryDecision {
	normalized := strings.ToLower(message)
	switch {
	case strings.Contains(normalized, "timeout") || strings.Contains(normalized, "mempool"):
		return RetryDecision{Action: RetryActionReconcileUnknown, Reason: "broadcast result is ambiguous; reconcile tx_hash and nullifier before rebuilding"}
	case strings.Contains(normalized, "sequence") || strings.Contains(normalized, "account") || strings.Contains(normalized, "gas"):
		return RetryDecision{Action: RetryActionRebuildTx, Reason: "tx envelope can be rebuilt after nullifier is confirmed unspent"}
	case strings.Contains(normalized, "invalid proof") || strings.Contains(normalized, "root") || strings.Contains(normalized, "payload"):
		return RetryDecision{Action: RetryActionReplan, Reason: "proof or root is invalid for current chain state"}
	case strings.Contains(normalized, "nullifier") && strings.Contains(normalized, "spent"):
		return RetryDecision{Action: RetryActionMarkConflictSpent, Reason: "nullifier is spent; operation success needs matching evidence"}
	default:
		return RetryDecision{Action: RetryActionManualReview, Reason: "error class is unknown"}
	}
}
