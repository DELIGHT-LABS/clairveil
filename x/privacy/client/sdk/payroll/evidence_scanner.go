package payroll

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	privacyreservation "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/reservation"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

type TxObservation struct {
	TxHash string       `json:"tx_hash,omitempty"`
	Height int64        `json:"height,omitempty"`
	Code   uint32       `json:"code,omitempty"`
	RawLog string       `json:"raw_log,omitempty"`
	Events []ChainEvent `json:"events,omitempty"`
}

type ChainEvent struct {
	Type       string                `json:"type"`
	Attributes []ChainEventAttribute `json:"attributes,omitempty"`
}

type ChainEventAttribute struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type NullifierStatus struct {
	Nullifier string `json:"nullifier"`
	Used      bool   `json:"used"`
}

type ScannedOperationEvidence struct {
	ReservationID string                               `json:"reservation_id"`
	OperationID   string                               `json:"operation_id,omitempty"`
	ItemID        string                               `json:"item_id,omitempty"`
	Evidence      privacyreservation.OperationEvidence `json:"evidence"`
}

type EvidenceScanReport struct {
	TxHash              string                     `json:"tx_hash,omitempty"`
	TxKnown             bool                       `json:"tx_known"`
	TxSucceeded         bool                       `json:"tx_succeeded"`
	TxFailed            bool                       `json:"tx_failed"`
	ObservedEvents      int                        `json:"observed_events"`
	ScannedReservations int                        `json:"scanned_reservations"`
	Evidence            []ScannedOperationEvidence `json:"evidence"`
	Warnings            []string                   `json:"warnings,omitempty"`
}

type EvidenceScanner struct {
	Store privacyreservation.Store
}

func (s EvidenceScanner) ScanTransferBatch(ctx context.Context, plan PayrollPlan, tx TxObservation, nullifiers []NullifierStatus) (*EvidenceScanReport, error) {
	if s.Store == nil {
		return nil, fmt.Errorf("reservation store is required")
	}
	plan = normalizePayrollPlan(plan)
	tx = normalizeTxObservation(tx)
	spentByNullifier := nullifierStatusMap(nullifiers)
	transferEvents := shieldedTransferEvents(tx.Events)
	report := &EvidenceScanReport{
		TxHash:         tx.TxHash,
		TxKnown:        tx.TxHash != "" || tx.RawLog != "" || tx.Height != 0 || len(tx.Events) > 0,
		TxSucceeded:    tx.Code == 0 && (tx.TxHash != "" || len(tx.Events) > 0 || tx.Height != 0),
		TxFailed:       tx.Code != 0 && (tx.TxHash != "" || tx.RawLog != "" || tx.Height != 0 || len(tx.Events) > 0),
		ObservedEvents: len(transferEvents),
		Evidence:       make([]ScannedOperationEvidence, 0),
	}
	if len(transferEvents) < len(plan.Items) {
		report.Warnings = append(report.Warnings, fmt.Sprintf("observed %d shielded_transfer events for %d payroll items", len(transferEvents), len(plan.Items)))
	}

	usedEvents := make(map[int]struct{}, len(transferEvents))
	for itemIndex, item := range plan.Items {
		attrs, hasEvent := transferEventAttributesForItem(item, itemIndex, transferEvents, usedEvents)
		hasFailedTxEvidence := report.TxFailed && report.TxKnown
		if !hasEvent {
			if !hasFailedTxEvidence && !itemHasExplicitNullifierStatus(item, spentByNullifier) {
				continue
			}
			attrs = map[string]string{}
		}
		for _, note := range item.InputNotes {
			reservationID := note.ReservationID
			if reservationID == "" {
				reservationID = ReservationIDForInputNote(item.OperationID, note.NoteID)
			}
			reservation, err := s.Store.GetReservation(ctx, reservationID)
			if err != nil {
				return nil, err
			}
			operation, err := s.Store.GetOperation(ctx, reservation.OperationID)
			if err != nil {
				return nil, err
			}
			if !hasEvent && hasFailedTxEvidence && !reservationOrOperationTxMatchesObservation(reservation, operation, tx) {
				continue
			}
			evidence := privacyreservation.OperationEvidence{
				TxHash:                   tx.TxHash,
				OutputCommitment:         attrs[privacytypes.AttributeKeyCommitment1],
				DisclosureDigest:         attrs[privacytypes.AttributeKeyAuditDisclosureDigest],
				UserDisclosureDigest:     attrs[privacytypes.AttributeKeyUserDisclosureDigest],
				AuditDisclosureDigest:    attrs[privacytypes.AttributeKeyAuditDisclosureDigest],
				SelfViewDisclosureDigest: attrs[privacytypes.AttributeKeySelfViewDisclosureDigest],
				RecipientHash:            item.ExpectedRecipientHash,
				AmountHash:               item.ExpectedAmountHash,
				Denom:                    item.Denom,
				BatchItemIndex:           itemIndex,
				BatchItemIndexKnown:      true,
				NullifierSpent:           reservationNullifierSpent(reservation, attrs, spentByNullifier),
				TxSucceeded:              report.TxSucceeded && hasEvent,
				TxFailed:                 hasFailedTxEvidence,
				TxKnown:                  report.TxKnown && (hasEvent || hasFailedTxEvidence),
			}
			report.Evidence = append(report.Evidence, ScannedOperationEvidence{
				ReservationID: reservation.ReservationID,
				OperationID:   operation.OperationID,
				ItemID:        operation.ItemID,
				Evidence:      evidence,
			})
			report.ScannedReservations++
		}
	}
	return report, nil
}

func transferEventAttributesForItem(item PayrollPlanItem, fallbackIndex int, events []ChainEvent, usedEvents map[int]struct{}) (map[string]string, bool) {
	if transferEventsHaveNullifierEvidence(events) {
		for eventIndex, event := range events {
			if _, used := usedEvents[eventIndex]; used {
				continue
			}
			attrs := eventAttributes(event)
			if eventMatchesItemNullifiers(item, attrs) {
				usedEvents[eventIndex] = struct{}{}
				return attrs, true
			}
		}
		return nil, false
	}
	if fallbackIndex >= len(events) {
		return nil, false
	}
	usedEvents[fallbackIndex] = struct{}{}
	return eventAttributes(events[fallbackIndex]), true
}

func transferEventsHaveNullifierEvidence(events []ChainEvent) bool {
	for _, event := range events {
		attrs := eventAttributes(event)
		if normalizeEvidenceHex(attrs[privacytypes.AttributeKeyNullifier1]) != "" || normalizeEvidenceHex(attrs[privacytypes.AttributeKeyNullifier2]) != "" {
			return true
		}
	}
	return false
}

func eventMatchesItemNullifiers(item PayrollPlanItem, attrs map[string]string) bool {
	eventNullifiers := map[string]struct{}{}
	for _, key := range []string{privacytypes.AttributeKeyNullifier1, privacytypes.AttributeKeyNullifier2} {
		if value := normalizeEvidenceHex(attrs[key]); value != "" {
			eventNullifiers[value] = struct{}{}
		}
	}
	if len(eventNullifiers) == 0 {
		return false
	}
	required := 0
	for _, note := range item.InputNotes {
		lookup := normalizeEvidenceHex(note.NullifierLookupKey)
		if lookup == "" {
			continue
		}
		required++
		if _, ok := eventNullifiers[lookup]; !ok {
			return false
		}
	}
	return required > 0
}

func itemHasExplicitNullifierStatus(item PayrollPlanItem, spentByNullifier map[string]bool) bool {
	for _, note := range item.InputNotes {
		lookup := normalizeEvidenceHex(note.NullifierLookupKey)
		if lookup == "" {
			continue
		}
		if _, ok := spentByNullifier[lookup]; ok {
			return true
		}
	}
	return false
}

func reservationOrOperationTxMatchesObservation(reservation *privacyreservation.NoteReservation, operation *privacyreservation.PayrollOperation, tx TxObservation) bool {
	txHash := normalizedTxIdentity(tx.TxHash)
	if txHash == "" {
		return false
	}
	if reservation != nil && normalizedTxIdentity(reservation.TxHash) == txHash {
		return true
	}
	if operation != nil && normalizedTxIdentity(operation.TxHash) == txHash {
		return true
	}
	return false
}

func ParseTxObservationJSON(bz []byte) (TxObservation, error) {
	var direct TxObservation
	if err := json.Unmarshal(bz, &direct); err == nil && (direct.TxHash != "" || direct.RawLog != "" || direct.Height != 0 || len(direct.Events) > 0) {
		return normalizeTxObservation(direct), nil
	}

	var raw map[string]any
	if err := json.Unmarshal(bz, &raw); err != nil {
		return TxObservation{}, err
	}
	tx := TxObservation{}
	source := raw
	if txResponse, ok := raw["tx_response"].(map[string]any); ok {
		source = txResponse
	}
	tx.TxHash = firstMapString(source, "txhash", "tx_hash", "hash")
	tx.RawLog = firstMapString(source, "raw_log", "rawLog", "log")
	tx.Height = firstMapInt64(source, "height")
	tx.Code = uint32(firstMapInt64(source, "code"))
	tx.Events = parseChainEvents(source["events"])
	return normalizeTxObservation(tx), nil
}

func normalizeTxObservation(tx TxObservation) TxObservation {
	tx.TxHash = strings.TrimSpace(tx.TxHash)
	tx.RawLog = strings.TrimSpace(tx.RawLog)
	for i := range tx.Events {
		tx.Events[i].Type = strings.TrimSpace(tx.Events[i].Type)
		for j := range tx.Events[i].Attributes {
			tx.Events[i].Attributes[j].Key = strings.TrimSpace(tx.Events[i].Attributes[j].Key)
			tx.Events[i].Attributes[j].Value = strings.TrimSpace(tx.Events[i].Attributes[j].Value)
		}
	}
	return tx
}

func parseChainEvents(value any) []ChainEvent {
	values, ok := value.([]any)
	if !ok {
		return nil
	}
	events := make([]ChainEvent, 0, len(values))
	for _, item := range values {
		eventMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		event := ChainEvent{Type: firstMapString(eventMap, "type")}
		if attrs, ok := eventMap["attributes"].([]any); ok {
			event.Attributes = make([]ChainEventAttribute, 0, len(attrs))
			for _, attr := range attrs {
				attrMap, ok := attr.(map[string]any)
				if !ok {
					continue
				}
				event.Attributes = append(event.Attributes, ChainEventAttribute{
					Key:   firstMapString(attrMap, "key"),
					Value: firstMapString(attrMap, "value"),
				})
			}
		}
		events = append(events, event)
	}
	return events
}

func shieldedTransferEvents(events []ChainEvent) []ChainEvent {
	out := make([]ChainEvent, 0, len(events))
	for _, event := range events {
		if event.Type == privacytypes.EventTypeShieldedTransfer {
			out = append(out, event)
			continue
		}
		attrs := eventAttributes(event)
		if attrs[privacytypes.AttributeKeyCommitment1] != "" || attrs[privacytypes.AttributeKeyNullifier1] != "" {
			out = append(out, event)
		}
	}
	return out
}

func eventAttributes(event ChainEvent) map[string]string {
	attrs := make(map[string]string, len(event.Attributes))
	for _, attr := range event.Attributes {
		key := strings.TrimSpace(attr.Key)
		if key == "" {
			continue
		}
		attrs[key] = strings.TrimSpace(attr.Value)
	}
	return attrs
}

func nullifierStatusMap(statuses []NullifierStatus) map[string]bool {
	out := make(map[string]bool, len(statuses))
	for _, status := range statuses {
		nullifier := normalizeEvidenceHex(status.Nullifier)
		if nullifier == "" {
			continue
		}
		out[nullifier] = status.Used
	}
	return out
}

func reservationNullifierSpent(reservation *privacyreservation.NoteReservation, attrs map[string]string, spentByNullifier map[string]bool) bool {
	lookup := normalizeEvidenceHex(reservation.NullifierLookupKey)
	if lookup != "" {
		if spent, ok := spentByNullifier[lookup]; ok {
			return spent
		}
		if lookup == normalizeEvidenceHex(attrs[privacytypes.AttributeKeyNullifier1]) || lookup == normalizeEvidenceHex(attrs[privacytypes.AttributeKeyNullifier2]) {
			return true
		}
	}
	return false
}

func normalizeEvidenceHex(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.TrimPrefix(value, "0x")
	return value
}

func normalizedTxIdentity(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstMapString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := values[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case string:
			if strings.TrimSpace(typed) != "" {
				return strings.TrimSpace(typed)
			}
		case json.Number:
			return typed.String()
		case float64:
			return strconv.FormatInt(int64(typed), 10)
		}
	}
	return ""
}

func firstMapInt64(values map[string]any, keys ...string) int64 {
	for _, key := range keys {
		value, ok := values[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case string:
			parsed, _ := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
			if parsed != 0 {
				return parsed
			}
		case json.Number:
			parsed, _ := typed.Int64()
			if parsed != 0 {
				return parsed
			}
		case float64:
			return int64(typed)
		}
	}
	return 0
}
