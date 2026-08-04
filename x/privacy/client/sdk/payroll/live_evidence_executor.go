package payroll

import (
	"context"
	"fmt"

	privacyreservation "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/reservation"
)

type TransferBatchEvidenceExecutor struct {
	Store      privacyreservation.Store
	Plan       PayrollPlan
	Tx         TxObservation
	Nullifiers []NullifierStatus
}

func (e TransferBatchEvidenceExecutor) BuildProofReady(context.Context, LiveOperationGroup) (privacyreservation.ProofReadyOperationUpdate, string, error) {
	return privacyreservation.ProofReadyOperationUpdate{}, "proof generation is handled by an external live worker", ErrLiveDaemonSkip
}

func (e TransferBatchEvidenceExecutor) PrepareBroadcastProofReady(context.Context, LiveOperationGroup) (*PreparedLiveBroadcast, string, error) {
	return nil, "broadcast is handled by an external live worker", ErrLiveDaemonSkip
}

func (e TransferBatchEvidenceExecutor) ScanSubmitted(ctx context.Context, group LiveOperationGroup) (map[string]privacyreservation.OperationEvidence, string, error) {
	if e.Store == nil {
		return nil, "", fmt.Errorf("reservation store is required")
	}
	report, err := (EvidenceScanner{Store: e.Store}).ScanTransferBatch(ctx, e.Plan, e.Tx, e.Nullifiers)
	if err != nil {
		return nil, "", err
	}
	out := make(map[string]privacyreservation.OperationEvidence)
	for _, item := range report.Evidence {
		if item.OperationID != group.Operation.OperationID {
			continue
		}
		out[item.ReservationID] = item.Evidence
	}
	if len(out) == 0 {
		return nil, "no scanned evidence matched operation", ErrLiveDaemonSkip
	}
	return out, "scanned transfer-batch tx evidence", nil
}
