package payroll

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	privacybatchtransfer "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/batchtransfer"
	privacyreservation "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/reservation"
)

// BatchReconcileRequest contains only evidence that was obtained from typed
// chain queries and verified disclosure scans. The prepared payload is used to
// bind nullifiers to the durable input reservation indexes.
type BatchReconcileRequest struct {
	OperationID     string
	Payload         *privacybatchtransfer.PreparedBatchTransferPayload
	ObservedOutputs []privacyreservation.ObservedOutputEvidence
	FailureReason   string
}

type BatchReconcileResult struct {
	Graph                *privacyreservation.BatchOperationGraph
	TxLookup             *BatchTxLookupResult
	CheckedNullifiers    []string
	SpentNullifiers      []string
	SpentReservationIDs  []string
	PendingChainEvidence bool
}

// BatchReconcileWorker always checks the stored tx hash before querying input
// nullifiers. It does not infer item success from spent nullifiers: the store
// receives output evidence independently and marks missing/mismatched items for
// manual review.
type BatchReconcileWorker struct {
	Store      privacyreservation.BatchOperationStore
	Reconciler BatchChainReconciler
	Now        func() time.Time
}

func (w BatchReconcileWorker) Reconcile(ctx context.Context, request BatchReconcileRequest) (*BatchReconcileResult, error) {
	if w.Store == nil || w.Reconciler == nil {
		return nil, fmt.Errorf("batch reconcile store and chain reconciler are required")
	}
	if strings.TrimSpace(request.OperationID) == "" || request.Payload == nil {
		return nil, fmt.Errorf("batch operation ID and prepared payload are required")
	}
	// Reconciliation remains possible after expiry. Passing zero time verifies
	// payload structure, signature and payload hash without re-opening its send
	// validity window.
	if err := privacybatchtransfer.ValidatePreparedBatchTransferPayloadMetadataAt(request.Payload, time.Time{}); err != nil {
		return nil, fmt.Errorf("validate reconciled prepared payload: %w", err)
	}
	graph, err := w.Store.GetBatchOperation(ctx, request.OperationID)
	if err != nil {
		return nil, err
	}
	if graph.Operation.PreparedPayloadHash != request.Payload.PayloadHash {
		return nil, fmt.Errorf("batch operation payload hash mismatch")
	}
	if len(graph.Inputs) != len(request.Payload.Inputs) || graph.Operation.InputCount != len(request.Payload.Inputs) {
		return nil, fmt.Errorf("batch operation input relation does not match prepared payload")
	}

	result := &BatchReconcileResult{Graph: graph}
	txHash := graph.Operation.TxHash
	if txHash != "" {
		lookup, lookupErr := w.Reconciler.LookupBatchTx(ctx, txHash)
		if lookupErr != nil {
			return nil, fmt.Errorf("lookup batch tx hash before nullifiers: %w", lookupErr)
		}
		if lookup != nil {
			if lookup.Found && lookup.Succeeded == lookup.Failed {
				return nil, fmt.Errorf("found batch tx lookup must be exactly one of succeeded or failed")
			}
			if !lookup.Found && (lookup.Succeeded || lookup.Failed) {
				return nil, fmt.Errorf("absent batch tx lookup cannot be succeeded or failed")
			}
			if lookup.TxHash != "" && !equalEvidenceHex(txHash, lookup.TxHash) {
				return nil, fmt.Errorf("batch tx lookup hash does not match durable tx hash")
			}
			result.TxLookup = lookup
		}
	}

	nullifiers := make([]string, len(request.Payload.Inputs))
	for i := range request.Payload.Inputs {
		nullifiers[i] = hex.EncodeToString(request.Payload.Inputs[i].Nullifier)
	}
	statuses, err := w.Reconciler.CheckBatchNullifiers(ctx, nullifiers)
	if err != nil {
		return nil, fmt.Errorf("check batch nullifiers after tx hash: %w", err)
	}
	normalizedStatuses, err := normalizeBatchNullifierStatuses(statuses)
	if err != nil {
		return nil, err
	}
	inputsByIndex, err := batchInputsByIndex(graph.Inputs, len(nullifiers))
	if err != nil {
		return nil, err
	}
	result.CheckedNullifiers = append([]string(nil), nullifiers...)
	for i, nullifier := range nullifiers {
		spent, exists := normalizedStatuses[normalizeBatchEvidenceHex(nullifier)]
		if !exists {
			return nil, fmt.Errorf("batch nullifier reconciliation response is incomplete at input %d", i)
		}
		if spent {
			result.SpentNullifiers = append(result.SpentNullifiers, nullifier)
			result.SpentReservationIDs = append(result.SpentReservationIDs, inputsByIndex[i].ReservationID)
		}
	}

	lookupFound := result.TxLookup != nil && result.TxLookup.Found
	if !lookupFound && len(result.SpentReservationIDs) == 0 {
		// Absence of both tx and spent-nullifier evidence is still an in-flight
		// state. Do not collapse it into Failed or ManualReview.
		result.PendingChainEvidence = true
		return result, nil
	}
	txSucceeded := lookupFound && result.TxLookup.Succeeded
	txFailed := lookupFound && result.TxLookup.Failed
	if result.TxLookup != nil && result.TxLookup.TxHash != "" {
		txHash = result.TxLookup.TxHash
	}
	failureReason := strings.TrimSpace(request.FailureReason)
	if failureReason == "" {
		switch {
		case txFailed:
			failureReason = "batch transaction failed on chain"
		case !lookupFound && len(result.SpentReservationIDs) > 0:
			failureReason = "input nullifier spent without confirmed batch transaction evidence"
		case txSucceeded && len(result.SpentReservationIDs) != len(nullifiers):
			failureReason = "confirmed batch transaction has incomplete nullifier-spent evidence"
		}
	}
	updated, err := w.Store.ReconcileBatchOperation(ctx, request.OperationID, privacyreservation.BatchReconcileUpdate{
		TxHash:              txHash,
		TxSucceeded:         txSucceeded,
		TxFailed:            txFailed,
		SpentReservationIDs: result.SpentReservationIDs,
		ObservedOutputs:     append([]privacyreservation.ObservedOutputEvidence(nil), request.ObservedOutputs...),
		FailureReason:       failureReason,
	}, w.now())
	if err != nil {
		return nil, err
	}
	result.Graph = updated
	return result, nil
}

func batchInputsByIndex(inputs []privacyreservation.OperationInputReservation, count int) ([]privacyreservation.OperationInputReservation, error) {
	indexed := make([]privacyreservation.OperationInputReservation, count)
	seen := make([]bool, count)
	for _, input := range inputs {
		if input.InputIndex < 0 || input.InputIndex >= count || seen[input.InputIndex] {
			return nil, fmt.Errorf("batch operation has invalid or duplicate input index %d", input.InputIndex)
		}
		indexed[input.InputIndex] = input
		seen[input.InputIndex] = true
	}
	for i, exists := range seen {
		if !exists || strings.TrimSpace(indexed[i].ReservationID) == "" {
			return nil, fmt.Errorf("batch operation is missing input relation %d", i)
		}
	}
	return indexed, nil
}

func normalizeBatchNullifierStatuses(statuses map[string]bool) (map[string]bool, error) {
	normalized := make(map[string]bool, len(statuses))
	for raw, spent := range statuses {
		key := normalizeBatchEvidenceHex(raw)
		if key == "" {
			return nil, fmt.Errorf("batch nullifier reconciliation contains an empty key")
		}
		if previous, duplicate := normalized[key]; duplicate && previous != spent {
			return nil, fmt.Errorf("batch nullifier reconciliation contains conflicting duplicate status")
		}
		normalized[key] = spent
	}
	return normalized, nil
}

func equalEvidenceHex(left, right string) bool {
	return normalizeBatchEvidenceHex(left) == normalizeBatchEvidenceHex(right)
}

func normalizeBatchEvidenceHex(value string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(value), "0x"))
}

func (w BatchReconcileWorker) now() time.Time {
	if w.Now != nil {
		return w.Now().UTC()
	}
	return time.Now().UTC()
}
