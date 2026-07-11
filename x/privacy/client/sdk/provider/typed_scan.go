package provider

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

// PrivacyScan queries the ciphertext-bearing V2 index. Errors are returned to
// the caller unchanged: this method deliberately never falls back to ABCI
// events, whose minimal batch event does not contain output ciphertexts.
func (p ScanQueryProvider) PrivacyScan(ctx context.Context, after *privacytypes.PrivacyScanCursorV1, outputLimit, eventLimit uint32, maxEncodedBytes uint64, eventTypes []string) (*privacytypes.QueryPrivacyScanResponse, error) {
	if p.PrivacyScanQuerier == nil {
		return nil, fmt.Errorf("a typed privacy scan querier is required")
	}
	req := &privacytypes.QueryPrivacyScanRequest{After: cloneCursor(after), OutputLimit: outputLimit, EventLimit: eventLimit, MaxEncodedBytes: maxEncodedBytes, EventTypes: append([]string(nil), eventTypes...)}
	resp, err := p.PrivacyScanQuerier.PrivacyScan(ctx, req)
	if err != nil {
		return nil, err
	}
	if err := ValidatePrivacyScanPage(req, resp); err != nil {
		return nil, fmt.Errorf("corrupt typed privacy scan response: %w", err)
	}
	return resp, nil
}

// ScanPrivacyV2 is a request-shaped companion useful to adapters that already
// persist the complete V1 cursor and page limits.
func (p ScanQueryProvider) ScanPrivacyV2(ctx context.Context, req *privacytypes.QueryPrivacyScanRequest) (*privacytypes.QueryPrivacyScanResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("privacy scan request is required")
	}
	return p.PrivacyScan(ctx, req.After, req.OutputLimit, req.EventLimit, req.MaxEncodedBytes, req.EventTypes)
}

func cloneCursor(cursor *privacytypes.PrivacyScanCursorV1) *privacytypes.PrivacyScanCursorV1 {
	if cursor == nil {
		return nil
	}
	return &privacytypes.PrivacyScanCursorV1{Height: cursor.Height, GlobalSequence: cursor.GlobalSequence, OutputIndex: cursor.OutputIndex}
}

func compareCursor(a, b *privacytypes.PrivacyScanCursorV1) int {
	if a.Height != b.Height {
		if a.Height < b.Height {
			return -1
		}
		return 1
	}
	if a.GlobalSequence != b.GlobalSequence {
		if a.GlobalSequence < b.GlobalSequence {
			return -1
		}
		return 1
	}
	if a.OutputIndex < b.OutputIndex {
		return -1
	}
	if a.OutputIndex > b.OutputIndex {
		return 1
	}
	return 0
}

func outputCursor(out *privacytypes.PrivacyScanOutputV2) *privacytypes.PrivacyScanCursorV1 {
	return &privacytypes.PrivacyScanCursorV1{Height: out.Height, GlobalSequence: out.GlobalSequence, OutputIndex: out.OutputIndex}
}

func hasPrivacyScanEventFilter(eventTypes []string) bool {
	for _, eventType := range eventTypes {
		if strings.TrimSpace(eventType) != "" {
			return true
		}
	}
	return false
}

// ValidatePrivacyScanPage applies client-side order, identity and framing
// checks. A malformed response is never partially accepted.
func ValidatePrivacyScanPage(req *privacytypes.QueryPrivacyScanRequest, resp *privacytypes.QueryPrivacyScanResponse) error {
	if req == nil || resp == nil {
		return fmt.Errorf("request and response are required")
	}
	after := req.After
	if after == nil {
		after = &privacytypes.PrivacyScanCursorV1{}
	}
	if resp.ScanSchemaVersion != privacytypes.PrivacyScanSchemaVersionV2 {
		return fmt.Errorf("unsupported scan schema version %q", resp.ScanSchemaVersion)
	}
	if req.OutputLimit > 0 && uint32(len(resp.Outputs)) > req.OutputLimit {
		return fmt.Errorf("response exceeds requested output limit")
	}
	if req.EventLimit > 0 && resp.ScannedEventCount > req.EventLimit {
		return fmt.Errorf("response exceeds requested event limit")
	}
	if req.MaxEncodedBytes > 0 && resp.EncodedBytes > req.MaxEncodedBytes {
		return fmt.Errorf("response exceeds requested byte limit")
	}

	summaries := make(map[string]*privacytypes.PrivacyScanSummaryV2, len(resp.Summaries))
	var previousSummary *privacytypes.PrivacyScanCursorV1
	for _, summary := range resp.Summaries {
		if err := validateSummary(summary); err != nil {
			return err
		}
		cursor := &privacytypes.PrivacyScanCursorV1{Height: summary.Height, GlobalSequence: summary.GlobalSequence}
		if previousSummary != nil && compareCursor(previousSummary, cursor) >= 0 {
			return fmt.Errorf("summaries are not strictly ordered")
		}
		previousSummary = cursor
		key := eventKey(summary.Height, summary.GlobalSequence)
		if _, exists := summaries[key]; exists {
			return fmt.Errorf("duplicate summary %s", key)
		}
		summaries[key] = summary
	}

	previous := after
	seen := make(map[string]struct{}, len(resp.Outputs))
	for _, output := range resp.Outputs {
		if output == nil {
			return fmt.Errorf("nil output")
		}
		cursor := outputCursor(output)
		if previous != nil && compareCursor(previous, cursor) >= 0 {
			return fmt.Errorf("outputs are not strictly ordered after cursor")
		}
		if previous != nil && previous.Height == cursor.Height && previous.GlobalSequence == cursor.GlobalSequence && cursor.OutputIndex != previous.OutputIndex+1 {
			return fmt.Errorf("output indices are not contiguous")
		}
		if previous != nil && (previous.Height != cursor.Height || previous.GlobalSequence != cursor.GlobalSequence) && cursor.OutputIndex != 0 {
			return fmt.Errorf("new event output must start at index zero")
		}
		key := fmt.Sprintf("%s/%d", eventKey(output.Height, output.GlobalSequence), output.OutputIndex)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate output cursor %s", key)
		}
		seen[key] = struct{}{}
		summary := summaries[eventKey(output.Height, output.GlobalSequence)]
		if summary == nil {
			return fmt.Errorf("output has no page summary")
		}
		if err := validateOutput(summary, output); err != nil {
			return fmt.Errorf("output %s: %w", key, err)
		}
		previous = cursor
	}
	if resp.NextCursor == nil {
		return fmt.Errorf("next cursor is required")
	}
	if compareCursor(resp.NextCursor, after) < 0 {
		return fmt.Errorf("next cursor regressed")
	}
	if err := validatePrivacyScanNextCursor(req, resp, summaries); err != nil {
		return err
	}
	if resp.HasMore && compareCursor(resp.NextCursor, after) <= 0 {
		return fmt.Errorf("has_more page did not advance cursor")
	}
	return nil
}

func validatePrivacyScanNextCursor(req *privacytypes.QueryPrivacyScanRequest, resp *privacytypes.QueryPrivacyScanResponse, summaries map[string]*privacytypes.PrivacyScanSummaryV2) error {
	after := req.After
	if after == nil {
		after = &privacytypes.PrivacyScanCursorV1{}
	}
	if len(resp.Outputs) == 0 {
		if compareCursor(resp.NextCursor, after) > 0 && !hasPrivacyScanEventFilter(req.EventTypes) {
			return validateZeroOutputCursorAdvance(after, resp.NextCursor, resp.Summaries, summaries)
		}
		return nil
	}

	last := resp.Outputs[len(resp.Outputs)-1]
	lastCursor := outputCursor(last)
	switch comparison := compareCursor(resp.NextCursor, lastCursor); {
	case comparison < 0:
		return fmt.Errorf("next cursor precedes last output cursor")
	case comparison == 0:
		return nil
	}

	lastSummary := summaries[eventKey(last.Height, last.GlobalSequence)]
	if lastSummary == nil || last.OutputIndex+1 != lastSummary.OutputCount {
		return fmt.Errorf("next cursor advances past an incomplete output event")
	}
	return validateZeroOutputCursorAdvance(lastCursor, resp.NextCursor, resp.Summaries, summaries)
}

func validateZeroOutputCursorAdvance(from, next *privacytypes.PrivacyScanCursorV1, orderedSummaries []*privacytypes.PrivacyScanSummaryV2, summaries map[string]*privacytypes.PrivacyScanSummaryV2) error {
	fromEvent := &privacytypes.PrivacyScanCursorV1{Height: from.Height, GlobalSequence: from.GlobalSequence}
	nextEvent := &privacytypes.PrivacyScanCursorV1{Height: next.Height, GlobalSequence: next.GlobalSequence}
	if next.OutputIndex != 0 || compareCursor(nextEvent, fromEvent) <= 0 {
		return fmt.Errorf("next cursor advance after outputs is not an event boundary")
	}
	nextSummary := summaries[eventKey(next.Height, next.GlobalSequence)]
	if nextSummary == nil || nextSummary.OutputCount != 0 {
		return fmt.Errorf("next cursor advance after outputs lacks a zero-output summary")
	}
	for _, summary := range orderedSummaries {
		cursor := &privacytypes.PrivacyScanCursorV1{Height: summary.Height, GlobalSequence: summary.GlobalSequence}
		if compareCursor(cursor, fromEvent) > 0 && compareCursor(cursor, nextEvent) <= 0 && summary.OutputCount != 0 {
			return fmt.Errorf("next cursor skips an output-bearing summary")
		}
	}
	return nil
}

func eventKey(height int64, sequence uint64) string { return fmt.Sprintf("%d/%d", height, sequence) }

func validateSummary(s *privacytypes.PrivacyScanSummaryV2) error {
	if s == nil || s.Height < 0 || s.GlobalSequence == 0 {
		return fmt.Errorf("invalid summary cursor")
	}
	if s.CircuitSetId != privacytypes.ActiveCircuitSetID || s.PayloadVersion != privacytypes.FixedPayloadVersionV1 || s.ScanSchemaVersion != privacytypes.PrivacyScanSchemaVersionV2 {
		return fmt.Errorf("invalid summary version identity")
	}
	if len(s.TxHash) != 0 && len(s.TxHash) != 32 {
		return fmt.Errorf("invalid summary tx hash")
	}
	switch s.EventType {
	case privacytypes.EventTypeDeposit:
		if s.OutputCount != 1 || len(s.EffectId) != 0 {
			return fmt.Errorf("invalid deposit summary")
		}
	case privacytypes.EventTypeShieldedTransfer:
		if s.OutputCount != 2 || len(s.EffectId) != 0 {
			return fmt.Errorf("invalid 2x2 summary")
		}
	case privacytypes.EventTypeBatchTransferV1:
		if s.OutputCount == 0 || s.OutputCount > privacytypes.BatchJoinSplitV1MaxOutputs || len(s.EffectId) != 32 || allZero(s.EffectId) {
			return fmt.Errorf("invalid batch summary")
		}
		if len(s.Nullifiers) == 0 || len(s.Nullifiers) > int(privacytypes.BatchJoinSplitV1MaxInputs) {
			return fmt.Errorf("invalid batch summary nullifier count")
		}
		if err := privacytypes.ValidateDistinctCanonicalFieldElements("batch summary nullifier", s.Nullifiers); err != nil {
			return err
		}
		if err := privacytypes.ValidateAuditKeyIDV1(s.AuditKeyId); err != nil || s.AuditKeyEpoch == 0 {
			return fmt.Errorf("invalid batch summary audit identity")
		}
		if _, err := privacycrypto.DecodeCanonicalPoint(s.AuditTargetPubkey); err != nil {
			return fmt.Errorf("invalid batch summary audit target: %w", err)
		}
	case privacytypes.EventTypeWithdraw:
		if s.OutputCount != 0 || len(s.EffectId) != 0 {
			return fmt.Errorf("invalid withdraw summary")
		}
	default:
		return fmt.Errorf("unsupported event type %q", s.EventType)
	}
	return nil
}

func validateOutput(s *privacytypes.PrivacyScanSummaryV2, o *privacytypes.PrivacyScanOutputV2) error {
	if o.OutputIndex >= s.OutputCount || o.EventType != s.EventType || o.Height != s.Height || o.GlobalSequence != s.GlobalSequence {
		return fmt.Errorf("cursor/event identity mismatch")
	}
	if !bytes.Equal(o.TxHash, s.TxHash) || !bytes.Equal(o.EffectId, s.EffectId) {
		return fmt.Errorf("tx/effect identity mismatch")
	}
	if o.CircuitSetId != s.CircuitSetId || o.PayloadVersion != s.PayloadVersion || o.ScanSchemaVersion != s.ScanSchemaVersion {
		return fmt.Errorf("version identity mismatch")
	}
	if o.AuditKeyId != s.AuditKeyId || o.AuditKeyEpoch != s.AuditKeyEpoch || !bytes.Equal(o.AuditTargetPubkey, s.AuditTargetPubkey) {
		return fmt.Errorf("audit identity mismatch")
	}
	if !o.LeafIndexFound {
		return fmt.Errorf("leaf index is absent")
	}
	if err := canonicalActiveField(o.Commitment, "commitment"); err != nil {
		return err
	}
	switch o.EventType {
	case privacytypes.EventTypeDeposit:
		if len(o.Ciphertext) != 0 || len(o.ViewTag) != 0 {
			return fmt.Errorf("deposit ciphertext sentinel mismatch")
		}
		if _, err := privacytypes.UnwrapEncryptedEnvelopeV1(o.EncryptedNote, privacytypes.EnvelopeDepositNoteV1); err != nil {
			return fmt.Errorf("invalid deposit envelope: %w", err)
		}
	case privacytypes.EventTypeShieldedTransfer, privacytypes.EventTypeBatchTransferV1:
		if len(o.EncryptedNote) != 0 || len(o.ViewTag) != privacytypes.ViewTagLength {
			return fmt.Errorf("transfer framing mismatch")
		}
		if _, err := privacytypes.UnwrapEncryptedEnvelopeV1(o.Ciphertext, privacytypes.EnvelopeTransferNoteV1); err != nil {
			return fmt.Errorf("invalid transfer envelope: %w", err)
		}
		if o.EventType == privacytypes.EventTypeBatchTransferV1 {
			if err := validateBatchOutputDisclosureFraming(o); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateBatchOutputDisclosureFraming(o *privacytypes.PrivacyScanOutputV2) error {
	if o.UserPrivacyPolicy > privacytypes.TransferPrivacyPolicyDiscloseAmountToFrom {
		return fmt.Errorf("invalid batch user privacy policy")
	}
	if err := canonicalActiveField(o.FullDisclosureDigest, "full disclosure digest"); err != nil {
		return err
	}
	if _, err := privacytypes.UnwrapEncryptedEnvelopeV1(o.AuditDisclosurePayload, privacytypes.EnvelopeAuditDisclosureV1); err != nil {
		return fmt.Errorf("invalid audit disclosure envelope: %w", err)
	}
	if len(o.SelfViewDisclosurePayload) != 0 {
		if _, err := privacytypes.UnwrapEncryptedEnvelopeV1(o.SelfViewDisclosurePayload, privacytypes.EnvelopeSelfViewDisclosureV1); err != nil {
			return fmt.Errorf("invalid self-view disclosure envelope: %w", err)
		}
	}
	switch {
	case o.UserPrivacyPolicy == privacytypes.TransferPrivacyPolicyAllPrivate:
		if o.UserDisclosureMode != privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_NONE.String() || len(o.UserDisclosureDigest) != 0 || len(o.UserDisclosureTargetPubkey) != 0 || len(o.UserDisclosurePayload) != 0 {
			return fmt.Errorf("all-private batch disclosure framing mismatch")
		}
	case o.UserDisclosureMode == privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_PUBLIC.String():
		if err := canonicalActiveField(o.UserDisclosureDigest, "user disclosure digest"); err != nil {
			return err
		}
		if len(o.UserDisclosureTargetPubkey) != 0 || len(o.UserDisclosurePayload) != privacytypes.DisclosurePlaintextV1Size {
			return fmt.Errorf("public batch disclosure framing mismatch")
		}
	case o.UserDisclosureMode == privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_RECIPIENT_ENCRYPTED.String():
		if err := canonicalActiveField(o.UserDisclosureDigest, "user disclosure digest"); err != nil {
			return err
		}
		if _, err := privacycrypto.DecodeCanonicalPoint(o.UserDisclosureTargetPubkey); err != nil {
			return fmt.Errorf("invalid user disclosure target: %w", err)
		}
		if _, err := privacytypes.UnwrapEncryptedEnvelopeV1(o.UserDisclosurePayload, privacytypes.EnvelopeUserDisclosureV1); err != nil {
			return fmt.Errorf("invalid user disclosure envelope: %w", err)
		}
	default:
		return fmt.Errorf("invalid batch user disclosure mode")
	}
	return nil
}

func canonicalActiveField(value []byte, name string) error {
	if len(value) != 32 || allZero(value) {
		return fmt.Errorf("%s must be an active canonical field", name)
	}
	_, err := privacyfield.DecodeCanonicalHex(hex.EncodeToString(value), name)
	return err
}
func allZero(value []byte) bool {
	for _, b := range value {
		if b != 0 {
			return false
		}
	}
	return true
}

type CommitmentPathSnapshot struct {
	RootHex        string
	SnapshotHeight int64
	LeafCount      uint64
	Paths          []*privacytypes.QueryCommitmentPathAtRoot
}

func (p ScanQueryProvider) CommitmentPathsAtRoot(ctx context.Context, commitmentHexes []string, rootHex string, snapshotHeight int64) (*CommitmentPathSnapshot, error) {
	if p.PathsAtRootQuerier == nil {
		return nil, fmt.Errorf("a commitment paths at root querier is required")
	}
	if len(commitmentHexes) == 0 || len(commitmentHexes) > int(privacytypes.BatchJoinSplitV1MaxInputs) {
		return nil, fmt.Errorf("commitment path request must contain 1..16 commitments")
	}
	canonicalCommitments := make([][]byte, len(commitmentHexes))
	for i, value := range commitmentHexes {
		decoded, err := privacyfield.DecodeCanonicalHex(value, fmt.Sprintf("commitment %d", i))
		if err != nil || len(value) != 64 || value != strings.ToLower(value) {
			return nil, fmt.Errorf("commitment %d is not canonical 32-byte lowercase hex", i)
		}
		canonicalCommitments[i] = decoded
	}
	if err := privacytypes.ValidateDistinctCanonicalFieldElements("commitment path request", canonicalCommitments); err != nil {
		return nil, err
	}
	resp, err := p.PathsAtRootQuerier.CommitmentPathsAtRoot(ctx, &privacytypes.QueryCommitmentPathsAtRootRequest{CommitmentHexes: append([]string(nil), commitmentHexes...), RootHex: rootHex, SnapshotHeight: snapshotHeight})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("commitment path snapshot response is unavailable")
	}
	root, err := privacyfield.DecodeCanonicalHex(resp.RootHex, "snapshot root")
	if err != nil {
		return nil, err
	}
	var want []byte
	if strings.TrimSpace(rootHex) != "" {
		want, err = privacyfield.DecodeCanonicalHex(rootHex, "requested root")
		if err != nil {
			return nil, err
		}
	}
	if (len(want) != 0 && !bytes.Equal(root, want)) || (snapshotHeight > 0 && resp.SnapshotHeight != snapshotHeight) || len(resp.Paths) != len(commitmentHexes) {
		return nil, fmt.Errorf("commitment path snapshot identity mismatch")
	}
	for i, path := range resp.Paths {
		if path == nil || path.CommitmentHex != commitmentHexes[i] || len(path.Path) != 32 || len(path.PathHelper) != 32 || path.LeafIndex >= resp.LeafCount {
			return nil, fmt.Errorf("commitment path order mismatch at %d", i)
		}
		current := new(big.Int).SetBytes(canonicalCommitments[i])
		for level, siblingHex := range path.Path {
			if len(siblingHex) != 64 || siblingHex != strings.ToLower(siblingHex) {
				return nil, fmt.Errorf("commitment path %d sibling %d is not canonical", i, level)
			}
			siblingBytes, decodeErr := privacyfield.DecodeCanonicalHex(siblingHex, fmt.Sprintf("path %d sibling %d", i, level))
			if decodeErr != nil || path.PathHelper[level] > 1 {
				return nil, fmt.Errorf("commitment path %d is malformed", i)
			}
			sibling := new(big.Int).SetBytes(siblingBytes)
			if path.PathHelper[level] == 0 {
				current = privacytypes.ComputeNoteTreeNodeV1(uint32(level), current, sibling)
			} else {
				current = privacytypes.ComputeNoteTreeNodeV1(uint32(level), sibling, current)
			}
		}
		if !bytes.Equal(current.FillBytes(make([]byte, 32)), root) {
			return nil, fmt.Errorf("commitment path %d does not reconstruct the snapshot root", i)
		}
	}
	return &CommitmentPathSnapshot{RootHex: hex.EncodeToString(root), SnapshotHeight: resp.SnapshotHeight, LeafCount: resp.LeafCount, Paths: resp.Paths}, nil
}

func (p ScanQueryProvider) AssetDenomByID(ctx context.Context, assetID []byte) (string, error) {
	if p.AssetByIDQuerier == nil {
		return "", fmt.Errorf("an asset by id querier is required")
	}
	if err := canonicalActiveField(assetID, "asset id"); err != nil {
		return "", err
	}
	resp, err := p.AssetByIDQuerier.AssetByID(ctx, &privacytypes.QueryAssetByIDRequest{AssetIdHex: hex.EncodeToString(assetID)})
	if err != nil {
		return "", err
	}
	if resp == nil || resp.Asset == nil || resp.MappingVersion != privacytypes.AssetRegistryVersionV1 {
		return "", fmt.Errorf("invalid AssetRegistryV1 response")
	}
	if !bytes.Equal(resp.Asset.AssetId, assetID) || strings.TrimSpace(resp.Asset.CanonicalDenom) == "" {
		return "", fmt.Errorf("asset registry identity mismatch")
	}
	computed := privacytypes.ComputeAssetIDV1(resp.Asset.CanonicalDenom)
	if !bytes.Equal(computed.FillBytes(make([]byte, 32)), assetID) {
		return "", fmt.Errorf("asset registry denom does not derive the requested asset id")
	}
	return resp.Asset.CanonicalDenom, nil
}
