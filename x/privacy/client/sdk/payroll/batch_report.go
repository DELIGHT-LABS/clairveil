package payroll

import (
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	privacyreservation "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/reservation"
)

const BatchOperationReportSchemaVersionV1 = "privacy-payroll-batch-report-v1"

type BatchOutputStateCost struct {
	PaymentOutputs       int `json:"payment_outputs"`
	ChangeOutputs        int `json:"change_outputs"`
	PaddingOutputs       int `json:"padding_outputs"`
	PersistedCommitments int `json:"persisted_commitments"`
}

type BatchOutputReport struct {
	EffectID           string                                     `json:"effect_id,omitempty"`
	OutputIndex        int                                        `json:"output_index"`
	Role               privacyreservation.BatchOutputRole         `json:"role"`
	ItemID             string                                     `json:"item_id,omitempty"`
	EmployeeID         string                                     `json:"employee_id,omitempty"`
	EvidenceStatus     privacyreservation.BatchItemEvidenceStatus `json:"evidence_status"`
	ManualReviewReason string                                     `json:"manual_review_reason,omitempty"`
	DisclosureFindings []string                                   `json:"disclosure_findings,omitempty"`
}

type BatchOperationReport struct {
	SchemaVersion         string                             `json:"schema_version"`
	OperationID           string                             `json:"operation_id"`
	CompanyID             string                             `json:"company_id"`
	PayrollID             string                             `json:"payroll_id"`
	BatchID               string                             `json:"batch_id"`
	EffectID              string                             `json:"effect_id,omitempty"`
	TxHash                string                             `json:"tx_hash,omitempty"`
	ChainStatus           privacyreservation.OperationStatus `json:"chain_status"`
	InputCount            int                                `json:"input_count"`
	OutputCount           int                                `json:"output_count"`
	ProofCount            int                                `json:"proof_count"`
	TxEnvelopeCount       int                                `json:"tx_envelope_count"`
	BroadcastAttemptCount int                                `json:"broadcast_attempt_count"`
	RetryCount            int                                `json:"retry_count"`
	RetryReasons          []string                           `json:"retry_reasons,omitempty"`
	OutputStateCost       BatchOutputStateCost               `json:"output_state_cost"`
	Outputs               []BatchOutputReport                `json:"outputs"`
	PaymentItems          int                                `json:"payment_items"`
	SucceededItems        int                                `json:"succeeded_items"`
	FailedItems           int                                `json:"failed_items"`
	ManualReviewItems     int                                `json:"manual_review_items"`
	PendingItems          int                                `json:"pending_items"`
}

// BuildBatchOperationReport produces a non-sensitive operation report. It
// maps every output index to the typed batch effect ID, while item totals count
// payment outputs only (change and padding are state cost, not payroll success).
func BuildBatchOperationReport(graph privacyreservation.BatchOperationGraph, effectID string) (*BatchOperationReport, error) {
	op := graph.Operation
	if op.SchemaVersion != privacyreservation.BatchOperationSchemaVersionV1 || strings.TrimSpace(op.OperationID) == "" {
		return nil, fmt.Errorf("invalid batch operation graph")
	}
	if len(graph.Inputs) != op.InputCount || len(graph.Items) != op.OutputCount || len(graph.Evidence) != op.OutputCount {
		return nil, fmt.Errorf("batch operation graph relations are incomplete")
	}
	normalizedEffectID, err := normalizeBatchEffectID(effectID)
	if err != nil {
		return nil, err
	}
	evidenceByIndex := make(map[int]privacyreservation.ExpectedOutputEvidence, len(graph.Evidence))
	for _, evidence := range graph.Evidence {
		if evidence.OutputIndex < 0 || evidence.OutputIndex >= op.OutputCount {
			return nil, fmt.Errorf("batch report evidence index %d is out of range", evidence.OutputIndex)
		}
		if _, duplicate := evidenceByIndex[evidence.OutputIndex]; duplicate {
			return nil, fmt.Errorf("batch report contains duplicate evidence index %d", evidence.OutputIndex)
		}
		evidenceByIndex[evidence.OutputIndex] = evidence
	}
	items := append([]privacyreservation.PayrollItemOutput(nil), graph.Items...)
	sort.Slice(items, func(i, j int) bool { return items[i].OutputIndex < items[j].OutputIndex })
	report := &BatchOperationReport{
		SchemaVersion:         BatchOperationReportSchemaVersionV1,
		OperationID:           op.OperationID,
		CompanyID:             op.CompanyID,
		PayrollID:             op.PayrollID,
		BatchID:               op.BatchID,
		EffectID:              normalizedEffectID,
		TxHash:                normalizeBatchEvidenceHex(op.TxHash),
		ChainStatus:           op.Status,
		InputCount:            op.InputCount,
		OutputCount:           op.OutputCount,
		BroadcastAttemptCount: op.BroadcastAttemptCount,
		Outputs:               make([]BatchOutputReport, 0, op.OutputCount),
	}
	if strings.TrimSpace(op.ProofHash) != "" && len(op.ProofCiphertext) > 0 {
		report.ProofCount = 1
	}
	if strings.TrimSpace(op.TxBytesHash) != "" && len(op.SignedTxBytesCiphertext) > 0 {
		report.TxEnvelopeCount = 1
	}
	if op.BroadcastAttemptCount > 1 {
		report.RetryCount = op.BroadcastAttemptCount - 1
	}
	for _, attempt := range op.BroadcastHistory {
		reason := strings.TrimSpace(attempt.BroadcastError)
		if reason == "" && attempt.Unknown {
			reason = "broadcast outcome unknown; exact signed bytes retained for retry"
		}
		if reason != "" {
			report.RetryReasons = append(report.RetryReasons, reason)
		}
	}
	for _, item := range items {
		expected, exists := evidenceByIndex[item.OutputIndex]
		if !exists || expected.Role != item.Role {
			return nil, fmt.Errorf("batch output %d is missing matching expected evidence", item.OutputIndex)
		}
		output := BatchOutputReport{
			EffectID:           report.EffectID,
			OutputIndex:        item.OutputIndex,
			Role:               item.Role,
			ItemID:             item.ItemID,
			EmployeeID:         item.EmployeeID,
			EvidenceStatus:     item.EvidenceStatus,
			ManualReviewReason: item.ManualReviewReason,
			DisclosureFindings: batchDisclosureFindings(expected, item),
		}
		report.Outputs = append(report.Outputs, output)
		report.OutputStateCost.PersistedCommitments++
		switch item.Role {
		case privacyreservation.BatchOutputRolePayment:
			report.OutputStateCost.PaymentOutputs++
			report.PaymentItems++
			switch item.EvidenceStatus {
			case privacyreservation.BatchItemEvidenceSucceeded:
				report.SucceededItems++
			case privacyreservation.BatchItemEvidenceFailed:
				report.FailedItems++
			case privacyreservation.BatchItemEvidenceManualReview:
				report.ManualReviewItems++
			default:
				report.PendingItems++
			}
		case privacyreservation.BatchOutputRoleChange:
			report.OutputStateCost.ChangeOutputs++
		case privacyreservation.BatchOutputRolePadding:
			report.OutputStateCost.PaddingOutputs++
		default:
			return nil, fmt.Errorf("batch output %d has unknown role %q", item.OutputIndex, item.Role)
		}
	}
	return report, nil
}

func normalizeBatchEffectID(effectID string) (string, error) {
	normalized := normalizeBatchEvidenceHex(effectID)
	if normalized == "" {
		return "", nil
	}
	decoded, err := hex.DecodeString(normalized)
	if err != nil || len(decoded) != 32 {
		return "", fmt.Errorf("batch effect ID must be canonical 32-byte hex")
	}
	allZero := true
	for _, value := range decoded {
		allZero = allZero && value == 0
	}
	if allZero {
		return "", fmt.Errorf("batch effect ID must be non-zero")
	}
	return normalized, nil
}

func batchDisclosureFindings(expected privacyreservation.ExpectedOutputEvidence, item privacyreservation.PayrollItemOutput) []string {
	if item.EvidenceStatus != privacyreservation.BatchItemEvidenceManualReview {
		return nil
	}
	findings := make([]string, 0, 6)
	if normalizeBatchEvidenceHex(expected.ObservedCommitment) == "" {
		return []string{"expected output evidence is missing"}
	}
	if !equalEvidenceHex(expected.Commitment, expected.ObservedCommitment) {
		findings = append(findings, "commitment mismatch")
	}
	if !equalEvidenceHex(expected.UserDisclosureDigest, expected.ObservedUserDigest) {
		findings = append(findings, "user disclosure digest mismatch")
	}
	if !equalEvidenceHex(expected.FullDisclosureDigest, expected.ObservedFullDigest) {
		findings = append(findings, "full disclosure digest mismatch")
	}
	if expected.RecipientHash != "" && !equalEvidenceHex(expected.RecipientHash, expected.ObservedRecipientHash) {
		findings = append(findings, "recipient evidence mismatch")
	}
	if expected.AuditDeliveryFailed {
		findings = append(findings, "audit disclosure delivery failed")
	}
	if expected.SelfViewDeliveryFailed {
		findings = append(findings, "self-view disclosure delivery failed")
	}
	if len(findings) == 0 && strings.TrimSpace(item.ManualReviewReason) != "" {
		findings = append(findings, item.ManualReviewReason)
	}
	return findings
}
