package audit

import (
	"context"
	"fmt"
	"strings"

	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
)

type AuditSecretHistory interface {
	AuditSecret([32]byte) (privacycrypto.AuditSecretKey, bool)
}

type LastProcessedPosition struct {
	Height         uint64 `json:"height"`
	TxIndex        uint32 `json:"tx_index"`
	MessageIndex   uint32 `json:"message_index"`
	GlobalSequence uint64 `json:"global_sequence"`
}

// ProvenanceReport keeps transport completeness separate from cryptographic
// proof/decryption/lineage completeness. An incomplete report must never be
// interpreted as an empty or zero-balance result.
type ProvenanceReport struct {
	RequestedFrom      uint64                 `json:"requested_from"`
	RequestedTo        uint64                 `json:"requested_to"`
	ProcessedFrom      uint64                 `json:"processed_from,omitempty"`
	ProcessedTo        uint64                 `json:"processed_to,omitempty"`
	LastProcessedBlock uint64                 `json:"last_processed_block"`
	LastExecution      *LastProcessedPosition `json:"last_execution,omitempty"`
	CollectedCount     uint64                 `json:"collected_count"`
	CollectionComplete bool                   `json:"collection_complete"`
	ProvenanceComplete bool                   `json:"provenance_complete"`
	IncompleteReason   string                 `json:"incomplete_reason,omitempty"`
	Lineage            LineageReport          `json:"lineage"`
}

// IncompleteProvenanceReport creates the same machine-readable range and
// checkpoint summary used by a completed run when collection or public key
// resolution stops early. collectionComplete distinguishes a fully fetched
// block range from complete provenance.
func IncompleteProvenanceReport(state CosmosCacheState, collectionComplete bool, reason error) ProvenanceReport {
	report := ProvenanceReport{
		RequestedFrom: state.FromHeight, RequestedTo: state.ToHeight,
		LastProcessedBlock: state.LastProcessedHeight,
		CollectionComplete: collectionComplete && state.LastProcessedHeight == state.ToHeight && state.ToHeight >= state.FromHeight,
	}
	if state.LastProcessedHeight >= state.FromHeight {
		report.ProcessedFrom, report.ProcessedTo = state.FromHeight, state.LastProcessedHeight
	}
	if reason != nil {
		report.IncompleteReason = reason.Error()
		if !strings.HasPrefix(report.IncompleteReason, "AUDIT_INCOMPLETE:") {
			report.IncompleteReason = "AUDIT_INCOMPLETE: " + report.IncompleteReason
		}
	}
	return report
}

func BuildProvenanceReport(ctx context.Context, state CosmosCacheState, network [32]byte, rows []CollectedAuditTx, verifier CollectedProofVerifier, secrets AuditSecretHistory) (ProvenanceReport, error) {
	report := ProvenanceReport{RequestedFrom: state.FromHeight, RequestedTo: state.ToHeight, CollectedCount: uint64(len(rows))}
	if state.LastProcessedHeight >= state.FromHeight {
		report.ProcessedFrom, report.ProcessedTo = state.FromHeight, state.LastProcessedHeight
	}
	report.CollectionComplete = state.LastProcessedHeight == state.ToHeight && state.ToHeight >= state.FromHeight
	report.LastProcessedBlock = state.LastProcessedHeight
	if len(rows) > 0 {
		last := rows[len(rows)-1]
		position := LastProcessedPosition{Height: last.Height, TxIndex: last.TxIndex, MessageIndex: last.MessageIndex, GlobalSequence: last.GlobalSequence}
		report.LastExecution = &position
	}
	if !report.CollectionComplete {
		report.IncompleteReason = "AUDIT_INCOMPLETE: requested block range was not completely collected"
		return report, fmt.Errorf("%s", report.IncompleteReason)
	}
	if verifier == nil || secrets == nil {
		report.IncompleteReason = "AUDIT_INCOMPLETE: proof verifier and audit keyring are required"
		return report, fmt.Errorf("%s", report.IncompleteReason)
	}
	verified, err := VerifyCollectedTransactions(ctx, network, rows, verifier)
	if err != nil {
		report.IncompleteReason = "AUDIT_INCOMPLETE: " + err.Error()
		return report, fmt.Errorf("%s", report.IncompleteReason)
	}
	decrypted := make([]DecryptedAuditRecord, len(verified))
	for i, record := range verified {
		secret, found := secrets.AuditSecret(record.KeyID())
		if !found {
			report.IncompleteReason = fmt.Sprintf("AUDIT_INCOMPLETE: audit key %x for transition %d is missing", record.KeyID(), record.Sequence())
			return report, fmt.Errorf("%s", report.IncompleteReason)
		}
		decrypted[i], err = DecryptVerifiedRecord(secret, record)
		if err != nil {
			report.IncompleteReason = fmt.Sprintf("AUDIT_INCOMPLETE: transition %d decrypt: %v", record.Sequence(), err)
			return report, fmt.Errorf("%s", report.IncompleteReason)
		}
	}
	report.Lineage, err = BuildLineageReport(decrypted)
	if err != nil {
		report.IncompleteReason = err.Error()
		return report, err
	}
	report.ProvenanceComplete = true
	return report, nil
}
