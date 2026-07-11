package types

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"strings"

	sdkmath "cosmossdk.io/math"
	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
	sdk "github.com/cosmos/cosmos-sdk/types"

	clairveiltypes "github.com/DELIGHT-LABS/clairveil/types"
)

const genesisFieldElementByteSize = 32

const (
	CircuitSetIdentitySchemaVersion = "v1"
	CircuitCurveBN254               = "BN254"
)

var RequiredCircuitIdentityOrder = []string{"deposit", "spend", "joinsplit", "batch-joinsplit-16x32-v1"}

// DefaultGenesis returns the default genesis state for a specific circuit set.
func DefaultGenesis(identity *CircuitSetIdentity) *GenesisState {
	if identity == nil {
		panic("default privacy genesis requires circuit set identity")
	}
	return &GenesisState{
		Commitments:        [][]byte{},
		HistoricalRoots:    [][]byte{},
		Nullifiers:         [][]byte{},
		CircuitSetIdentity: CloneCircuitSetIdentity(identity),
		AssetRegistry: []*AssetRegistryEntryV1{
			defaultAssetRegistryEntryV1(clairveiltypes.DefaultDenom),
		},
		PrivacyEvents:        []*PrivacyEventRecordV1{},
		PrivacyScanSummaries: []*PrivacyScanSummaryV2{},
		PrivacyScanOutputs:   []*PrivacyScanOutputV2{},
		MerkleRootSnapshots:  []*MerkleRootSnapshotV1{},
		ReserveBalances:      []*ReserveBalanceV1{},
		StateVersion:         PrivacyStateVersionV2,
	}
}

// Validate performs basic genesis state validation returning an error upon any failure.
func (gs GenesisState) Validate() error {
	if gs.StateVersion != PrivacyStateVersionV2 {
		return fmt.Errorf("state_version must be %d; legacy/mixed privacy state requires a fresh reset", PrivacyStateVersionV2)
	}
	for i, commitment := range gs.Commitments {
		if err := validateGenesisFieldElementBytes(commitment); err != nil {
			return fmt.Errorf("commitments[%d]: %w", i, err)
		}
		if isZeroGenesisField(commitment) {
			return fmt.Errorf("commitments[%d]: active commitment must not be zero", i)
		}
	}
	if err := ValidateDistinctCanonicalFieldElements("commitment", gs.Commitments); err != nil {
		return fmt.Errorf("commitments: %w", err)
	}

	for i, root := range gs.HistoricalRoots {
		if err := validateGenesisFieldElementBytes(root); err != nil {
			return fmt.Errorf("historical_roots[%d]: %w", i, err)
		}
	}
	if err := ValidateDistinctCanonicalFieldElements("historical root", gs.HistoricalRoots); err != nil {
		return fmt.Errorf("historical_roots: %w", err)
	}

	for i, nullifier := range gs.Nullifiers {
		if err := validateGenesisFieldElementBytes(nullifier); err != nil {
			return fmt.Errorf("nullifiers[%d]: %w", i, err)
		}
		if isZeroGenesisField(nullifier) {
			return fmt.Errorf("nullifiers[%d]: active nullifier must not be zero", i)
		}
	}
	if err := ValidateDistinctCanonicalFieldElements("nullifier", gs.Nullifiers); err != nil {
		return fmt.Errorf("nullifiers: %w", err)
	}

	if len(gs.AuditMasterPubkey) != 0 {
		if _, err := decodePublicKey(gs.AuditMasterPubkey); err != nil {
			return fmt.Errorf("audit_master_pubkey: %w", err)
		}
	}
	if gs.CircuitSetIdentity == nil {
		return fmt.Errorf("circuit_set_identity: is required")
	}
	if err := ValidateCircuitSetIdentity(gs.CircuitSetIdentity); err != nil {
		return fmt.Errorf("circuit_set_identity: %w", err)
	}
	if err := validateGenesisAssetRegistryV1(gs.AssetRegistry); err != nil {
		return fmt.Errorf("asset_registry: %w", err)
	}
	if err := validateGenesisPrivacyIndexV2(gs); err != nil {
		return err
	}
	if err := validateGenesisRootSnapshotsV1(gs.MerkleRootSnapshots, uint64(len(gs.Commitments))); err != nil {
		return fmt.Errorf("merkle_root_snapshots: %w", err)
	}
	if err := validateGenesisReserveBalancesV1(gs.ReserveBalances, gs.AssetRegistry); err != nil {
		return fmt.Errorf("reserve_balances: %w", err)
	}

	return nil
}

func defaultAssetRegistryEntryV1(canonicalDenom string) *AssetRegistryEntryV1 {
	assetID := make([]byte, genesisFieldElementByteSize)
	ComputeAssetIDV1(canonicalDenom).FillBytes(assetID)
	return &AssetRegistryEntryV1{CanonicalDenom: canonicalDenom, AssetId: assetID}
}

func isZeroGenesisField(value []byte) bool {
	return bytes.Equal(value, make([]byte, genesisFieldElementByteSize))
}

func validateGenesisAssetRegistryV1(entries []*AssetRegistryEntryV1) error {
	seenDenoms := make(map[string]struct{}, len(entries))
	seenIDs := make(map[string]string, len(entries))
	previousDenom := ""
	for i, entry := range entries {
		if entry == nil {
			return fmt.Errorf("entry %d is nil", i)
		}
		if entry.CanonicalDenom != strings.TrimSpace(entry.CanonicalDenom) {
			return fmt.Errorf("entry %d denom has surrounding whitespace", i)
		}
		if err := sdk.ValidateDenom(entry.CanonicalDenom); err != nil {
			return fmt.Errorf("entry %d denom is invalid: %w", i, err)
		}
		if i > 0 && entry.CanonicalDenom <= previousDenom {
			return fmt.Errorf("entries must be strictly sorted by canonical denom")
		}
		previousDenom = entry.CanonicalDenom
		if err := validateGenesisFieldElementBytes(entry.AssetId); err != nil {
			return fmt.Errorf("entry %d asset_id: %w", i, err)
		}
		expected := defaultAssetRegistryEntryV1(entry.CanonicalDenom).AssetId
		if !bytes.Equal(expected, entry.AssetId) {
			return fmt.Errorf("entry %d asset_id does not match canonical denom", i)
		}
		if _, exists := seenDenoms[entry.CanonicalDenom]; exists {
			return fmt.Errorf("entry %d duplicates denom", i)
		}
		seenDenoms[entry.CanonicalDenom] = struct{}{}
		if otherDenom, exists := seenIDs[string(entry.AssetId)]; exists {
			return fmt.Errorf("entry %d asset_id collides with denom %q", i, otherDenom)
		}
		seenIDs[string(entry.AssetId)] = entry.CanonicalDenom
	}
	return nil
}

type genesisScanEventKey struct {
	height   int64
	sequence uint64
}

func validateGenesisPrivacyIndexV2(gs GenesisState) error {
	maxSequence := uint64(0)
	sequenceHeights := make(map[uint64]int64)
	eventKeys := make(map[genesisScanEventKey]*PrivacyEventRecordV1, len(gs.PrivacyEvents))
	var previousEvent genesisScanEventKey
	for i, event := range gs.PrivacyEvents {
		if event == nil || event.Height < 0 || event.GlobalSequence == 0 || event.EventType == "" || event.EventType != strings.TrimSpace(event.EventType) {
			return fmt.Errorf("privacy_events[%d]: invalid event identity", i)
		}
		if len(event.TxHash) != 0 && len(event.TxHash) != 32 {
			return fmt.Errorf("privacy_events[%d]: tx_hash must be empty or 32 bytes", i)
		}
		key := genesisScanEventKey{height: event.Height, sequence: event.GlobalSequence}
		if i > 0 && !genesisEventKeyLess(previousEvent, key) {
			return fmt.Errorf("privacy_events must be strictly cursor-sorted")
		}
		previousEvent = key
		if _, duplicate := eventKeys[key]; duplicate {
			return fmt.Errorf("privacy_events[%d]: duplicate cursor", i)
		}
		eventKeys[key] = event
		for j, attr := range event.Attributes {
			if attr == nil || attr.Key == "" {
				return fmt.Errorf("privacy_events[%d].attributes[%d]: invalid attribute", i, j)
			}
		}
		if err := registerGenesisGlobalSequence(sequenceHeights, event.GlobalSequence, event.Height); err != nil {
			return fmt.Errorf("privacy_events[%d]: %w", i, err)
		}
		if event.GlobalSequence > maxSequence {
			maxSequence = event.GlobalSequence
		}
	}

	summaries := make(map[genesisScanEventKey]*PrivacyScanSummaryV2, len(gs.PrivacyScanSummaries))
	var previousSummary genesisScanEventKey
	for i, summary := range gs.PrivacyScanSummaries {
		if summary == nil || summary.Height < 0 || summary.GlobalSequence == 0 || summary.EventType == "" || summary.EventType != strings.TrimSpace(summary.EventType) {
			return fmt.Errorf("privacy_scan_summaries[%d]: invalid summary identity", i)
		}
		if summary.OutputCount > 32 {
			return fmt.Errorf("privacy_scan_summaries[%d]: output_count exceeds 32", i)
		}
		if summary.CircuitSetId != ActiveCircuitSetID || summary.PayloadVersion != FixedPayloadVersionV1 || summary.ScanSchemaVersion != PrivacyScanSchemaVersionV2 {
			return fmt.Errorf("privacy_scan_summaries[%d]: unsupported version identity", i)
		}
		if len(summary.TxHash) != 0 && len(summary.TxHash) != 32 {
			return fmt.Errorf("privacy_scan_summaries[%d]: tx_hash must be empty or 32 bytes", i)
		}
		if err := ValidateDistinctCanonicalFieldElements("privacy scan nullifier", summary.Nullifiers); err != nil {
			return fmt.Errorf("privacy_scan_summaries[%d]: %w", i, err)
		}
		for j, nullifier := range summary.Nullifiers {
			if isZeroGenesisField(nullifier) {
				return fmt.Errorf("privacy_scan_summaries[%d].nullifiers[%d]: active nullifier must not be zero", i, j)
			}
		}
		key := genesisScanEventKey{height: summary.Height, sequence: summary.GlobalSequence}
		if i > 0 && !genesisEventKeyLess(previousSummary, key) {
			return fmt.Errorf("privacy_scan_summaries must be strictly cursor-sorted")
		}
		previousSummary = key
		if _, duplicate := summaries[key]; duplicate {
			return fmt.Errorf("privacy_scan_summaries[%d]: duplicate cursor", i)
		}
		summaries[key] = summary
		event, found := eventKeys[key]
		if !found {
			return fmt.Errorf("privacy_scan_summaries[%d]: matching privacy event is missing", i)
		}
		if event.EventType != summary.EventType || !bytes.Equal(event.TxHash, summary.TxHash) {
			return fmt.Errorf("privacy_scan_summaries[%d]: identity does not match privacy event", i)
		}
		if err := registerGenesisGlobalSequence(sequenceHeights, summary.GlobalSequence, summary.Height); err != nil {
			return fmt.Errorf("privacy_scan_summaries[%d]: %w", i, err)
		}
		if summary.GlobalSequence > maxSequence {
			maxSequence = summary.GlobalSequence
		}
	}
	if len(summaries) != len(eventKeys) {
		return fmt.Errorf("privacy scan summaries and privacy events must be 1:1")
	}

	commitmentIndices := make(map[string]uint64, len(gs.Commitments))
	for i, commitment := range gs.Commitments {
		commitmentIndices[string(commitment)] = uint64(i)
	}
	outputCounts := make(map[genesisScanEventKey]uint32, len(summaries))
	var previousOutput *PrivacyScanCursorV1
	for i, output := range gs.PrivacyScanOutputs {
		if output == nil || output.Height < 0 || output.GlobalSequence == 0 {
			return fmt.Errorf("privacy_scan_outputs[%d]: invalid cursor", i)
		}
		cursor := &PrivacyScanCursorV1{Height: output.Height, GlobalSequence: output.GlobalSequence, OutputIndex: output.OutputIndex}
		if previousOutput != nil && compareGenesisScanCursor(previousOutput, cursor) >= 0 {
			return fmt.Errorf("privacy_scan_outputs must be strictly cursor-sorted")
		}
		previousOutput = cursor
		key := genesisScanEventKey{height: output.Height, sequence: output.GlobalSequence}
		summary, found := summaries[key]
		if !found {
			return fmt.Errorf("privacy_scan_outputs[%d]: matching summary is missing", i)
		}
		expectedIndex := outputCounts[key]
		if output.OutputIndex != expectedIndex || output.OutputIndex >= summary.OutputCount {
			return fmt.Errorf("privacy_scan_outputs[%d]: output_index is not a contiguous prefix", i)
		}
		outputCounts[key]++
		if output.EventType != summary.EventType || !bytes.Equal(output.TxHash, summary.TxHash) || !bytes.Equal(output.EffectId, summary.EffectId) {
			return fmt.Errorf("privacy_scan_outputs[%d]: identity does not match summary", i)
		}
		if output.CircuitSetId != summary.CircuitSetId || output.PayloadVersion != summary.PayloadVersion || output.ScanSchemaVersion != summary.ScanSchemaVersion {
			return fmt.Errorf("privacy_scan_outputs[%d]: version does not match summary", i)
		}
		if output.AuditKeyId != summary.AuditKeyId || output.AuditKeyEpoch != summary.AuditKeyEpoch || !bytes.Equal(output.AuditTargetPubkey, summary.AuditTargetPubkey) {
			return fmt.Errorf("privacy_scan_outputs[%d]: audit identity does not match summary", i)
		}
		if err := validateGenesisFieldElementBytes(output.Commitment); err != nil || isZeroGenesisField(output.Commitment) {
			return fmt.Errorf("privacy_scan_outputs[%d]: active commitment is invalid", i)
		}
		leafIndex, found := commitmentIndices[string(output.Commitment)]
		if !found || !output.LeafIndexFound || leafIndex != output.LeafIndex {
			return fmt.Errorf("privacy_scan_outputs[%d]: leaf index does not match commitments", i)
		}
		if len(output.ViewTag) != 0 && len(output.ViewTag) != 2 {
			return fmt.Errorf("privacy_scan_outputs[%d]: view_tag must be empty or 2 bytes", i)
		}
	}
	for key, summary := range summaries {
		if outputCounts[key] != summary.OutputCount {
			return fmt.Errorf("privacy scan summary at %d/%d has incomplete outputs", key.height, key.sequence)
		}
	}
	if gs.PrivacyGlobalSequence < maxSequence {
		return fmt.Errorf("privacy_global_sequence is behind indexed records")
	}
	return nil
}

func registerGenesisGlobalSequence(heights map[uint64]int64, sequence uint64, height int64) error {
	if existingHeight, exists := heights[sequence]; exists && existingHeight != height {
		return fmt.Errorf("global sequence %d is reused at another height", sequence)
	}
	heights[sequence] = height
	return nil
}

func genesisEventKeyLess(left, right genesisScanEventKey) bool {
	return left.height < right.height || (left.height == right.height && left.sequence < right.sequence)
}

func compareGenesisScanCursor(left, right *PrivacyScanCursorV1) int {
	if left.Height < right.Height {
		return -1
	}
	if left.Height > right.Height {
		return 1
	}
	if left.GlobalSequence < right.GlobalSequence {
		return -1
	}
	if left.GlobalSequence > right.GlobalSequence {
		return 1
	}
	if left.OutputIndex < right.OutputIndex {
		return -1
	}
	if left.OutputIndex > right.OutputIndex {
		return 1
	}
	return 0
}

func validateGenesisRootSnapshotsV1(snapshots []*MerkleRootSnapshotV1, commitmentCount uint64) error {
	if uint64(len(snapshots)) != commitmentCount {
		return fmt.Errorf("must contain every commitment prefix: got %d want %d", len(snapshots), commitmentCount)
	}
	seenRoots := make(map[string]struct{}, len(snapshots))
	seenCounts := make(map[uint64]struct{}, len(snapshots))
	previousRoot := []byte(nil)
	for i, snapshot := range snapshots {
		if snapshot == nil || snapshot.Height < 0 || snapshot.LeafCount == 0 || snapshot.LeafCount > commitmentCount {
			return fmt.Errorf("snapshot %d has invalid metadata", i)
		}
		if err := validateGenesisFieldElementBytes(snapshot.Root); err != nil {
			return fmt.Errorf("snapshot %d root: %w", i, err)
		}
		if i > 0 && bytes.Compare(previousRoot, snapshot.Root) >= 0 {
			return fmt.Errorf("snapshots must be strictly root-sorted")
		}
		previousRoot = snapshot.Root
		if _, exists := seenRoots[string(snapshot.Root)]; exists {
			return fmt.Errorf("snapshot %d duplicates root", i)
		}
		seenRoots[string(snapshot.Root)] = struct{}{}
		if _, exists := seenCounts[snapshot.LeafCount]; exists {
			return fmt.Errorf("snapshot %d duplicates leaf_count", i)
		}
		seenCounts[snapshot.LeafCount] = struct{}{}
	}
	return nil
}

func validateGenesisReserveBalancesV1(balances []*ReserveBalanceV1, assets []*AssetRegistryEntryV1) error {
	registeredDenoms := make(map[string]struct{}, len(assets))
	for _, asset := range assets {
		if asset != nil {
			registeredDenoms[asset.CanonicalDenom] = struct{}{}
		}
	}
	previousDenom := ""
	for i, balance := range balances {
		if balance == nil || balance.CanonicalDenom != strings.TrimSpace(balance.CanonicalDenom) {
			return fmt.Errorf("balance %d has invalid denom", i)
		}
		if err := sdk.ValidateDenom(balance.CanonicalDenom); err != nil {
			return fmt.Errorf("balance %d denom: %w", i, err)
		}
		if _, registered := registeredDenoms[balance.CanonicalDenom]; !registered {
			return fmt.Errorf("balance %d denom %q is not registered in asset_registry", i, balance.CanonicalDenom)
		}
		if i > 0 && balance.CanonicalDenom <= previousDenom {
			return fmt.Errorf("balances must be strictly denom-sorted")
		}
		previousDenom = balance.CanonicalDenom
		deposited, ok := sdkmath.NewIntFromString(balance.TotalDeposited)
		if !ok || deposited.IsNegative() {
			return fmt.Errorf("balance %d total_deposited is invalid", i)
		}
		withdrawn, ok := sdkmath.NewIntFromString(balance.TotalWithdrawn)
		if !ok || withdrawn.IsNegative() {
			return fmt.Errorf("balance %d total_withdrawn is invalid", i)
		}
		if withdrawn.GT(deposited) {
			return fmt.Errorf("balance %d withdrawals exceed deposits", i)
		}
	}
	return nil
}

func ValidateCircuitSetIdentity(identity *CircuitSetIdentity) error {
	if identity == nil {
		return fmt.Errorf("is required")
	}
	if identity.SchemaVersion != CircuitSetIdentitySchemaVersion {
		return fmt.Errorf("schema_version must be %q", CircuitSetIdentitySchemaVersion)
	}
	if identity.CircuitSetId != ActiveCircuitSetID {
		return fmt.Errorf("circuit_set_id must be %q", ActiveCircuitSetID)
	}
	if identity.Curve != CircuitCurveBN254 {
		return fmt.Errorf("curve must be %q", CircuitCurveBN254)
	}
	if len(identity.Circuits) != len(RequiredCircuitIdentityOrder) {
		return fmt.Errorf("must contain exactly %d circuits", len(RequiredCircuitIdentityOrder))
	}
	for i, expectedID := range RequiredCircuitIdentityOrder {
		circuit := identity.Circuits[i]
		if circuit == nil {
			return fmt.Errorf("circuits[%d] is required", i)
		}
		if circuit.CircuitId != expectedID {
			return fmt.Errorf("circuits[%d].circuit_id must be %q", i, expectedID)
		}
		if err := validateLowercaseSHA256(circuit.VerifyingKeySha256); err != nil {
			return fmt.Errorf("circuits[%d].verifying_key_sha256: %w", i, err)
		}
		if err := validateLowercaseSHA256(circuit.PublicInputSchemaSha256); err != nil {
			return fmt.Errorf("circuits[%d].public_input_schema_sha256: %w", i, err)
		}
	}
	return nil
}

func CloneCircuitSetIdentity(identity *CircuitSetIdentity) *CircuitSetIdentity {
	if identity == nil {
		return nil
	}
	cloned := &CircuitSetIdentity{
		SchemaVersion: identity.SchemaVersion,
		CircuitSetId:  identity.CircuitSetId,
		Curve:         identity.Curve,
		Circuits:      make([]*CircuitIdentity, len(identity.Circuits)),
	}
	for i, circuit := range identity.Circuits {
		if circuit == nil {
			continue
		}
		copyCircuit := *circuit
		cloned.Circuits[i] = &copyCircuit
	}
	return cloned
}

func validateLowercaseSHA256(value string) error {
	if len(value) != 64 || value != strings.ToLower(value) {
		return fmt.Errorf("must be a lowercase 64-character hex string")
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != 32 {
		return fmt.Errorf("must be a lowercase 64-character hex string")
	}
	return nil
}

func validateGenesisFieldElementBytes(bz []byte) error {
	if len(bz) != genesisFieldElementByteSize {
		return fmt.Errorf("must be %d bytes", genesisFieldElementByteSize)
	}

	var elem fr.Element
	if err := elem.SetBytesCanonical(bz); err != nil {
		return fmt.Errorf("must be canonical field bytes")
	}

	return nil
}
