package keeper

import (
	"fmt"
	"math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
)

const (
	BatchVerifyBaseGasV1              uint64 = 1_000_000
	BatchPerInputGasV1                uint64 = 25_000
	BatchPerOutputGasV1               uint64 = 50_000
	BatchPerCanonicalPayloadByteGasV1 uint64 = 4
	BatchPerTypedStateByteGasV1       uint64 = 8
	BatchPerTreeNodeWriteGasV1        uint64 = 5_000
	BatchPerGlobalLookupGasV1         uint64 = 10_000

	BatchMaxCanonicalPayloadBytesV1 uint64 = 65_384
	BatchMaxTypedStateBytesV1       uint64 = 256 << 10
	BatchMaxTreeNodeWritesV1        uint64 = uint64(types.BatchJoinSplitV1MaxOutputs) * (MerkleDepth + 1)
	BatchMaxGlobalLookupsV1         uint64 = uint64(types.BatchJoinSplitV1MaxInputs + types.BatchJoinSplitV1MaxOutputs)

	// The typed scan records are measured exactly below. This small allowance
	// covers the separately stored minimal QueryPrivacyEvent summary/key without
	// charging any ciphertext or disclosure bytes twice.
	batchMinimalEventStateAllowanceV1 uint64 = 2 << 10
)

var (
	BatchGasModelV1 = zk.BatchGasModelV1{
		VerifyBase:              BatchVerifyBaseGasV1,
		PerInput:                BatchPerInputGasV1,
		PerOutput:               BatchPerOutputGasV1,
		PerCanonicalPayloadByte: BatchPerCanonicalPayloadByteGasV1,
		PerTypedScanStateByte:   BatchPerTypedStateByteGasV1,
		PerTreeWrite:            BatchPerTreeNodeWriteGasV1,
		PerGlobalLookup:         BatchPerGlobalLookupGasV1,
	}
	BatchResourceBoundsV1 = zk.BatchResourceBoundsV1{
		MaxCanonicalPayloadBytes: BatchMaxCanonicalPayloadBytesV1,
		MaxTypedScanStateBytes:   BatchMaxTypedStateBytesV1,
		MaxTreeNodeWrites:        BatchMaxTreeNodeWritesV1,
		MaxGlobalLookups:         BatchMaxGlobalLookupsV1,
	}
)

func computeBatchGasPrechargeV1(msg *types.MsgBatchTransfer) (zk.BatchGasBreakdownV1, error) {
	canonicalPayloadBytes, err := types.CanonicalMsgBatchTransferPayloadSizeV1(msg)
	if err != nil {
		return zk.BatchGasBreakdownV1{}, fmt.Errorf("measure canonical batch payload: %w", err)
	}
	typedStateBytes, err := estimateBatchTypedStateBytesV1(msg)
	if err != nil {
		return zk.BatchGasBreakdownV1{}, fmt.Errorf("measure typed batch state: %w", err)
	}
	outputCount := uint64(len(msg.Outputs))
	usage := zk.BatchResourceUsageV1{
		InputCount:            uint64(len(msg.Nullifiers)),
		OutputCount:           outputCount,
		CanonicalPayloadBytes: canonicalPayloadBytes,
		TypedScanStateBytes:   typedStateBytes,
		TreeNodeWrites:        outputCount * (MerkleDepth + 1),
		GlobalLookups:         uint64(len(msg.Nullifiers) + len(msg.Outputs)),
	}
	return zk.ComputeBatchGasV1(BatchGasModelV1, BatchResourceBoundsV1, usage)
}

func consumeBatchGasPrechargeV1(ctx sdk.Context, msg *types.MsgBatchTransfer) (zk.BatchGasBreakdownV1, error) {
	breakdown, err := computeBatchGasPrechargeV1(msg)
	if err != nil {
		return zk.BatchGasBreakdownV1{}, err
	}
	ctx.GasMeter().ConsumeGas(breakdown.Total, "privacy batch transfer deterministic precharge v1")
	return breakdown, nil
}

func estimateBatchTypedStateBytesV1(msg *types.MsgBatchTransfer) (uint64, error) {
	if msg == nil {
		return 0, fmt.Errorf("batch transfer message is required")
	}
	// Maximal cursor/identity encodings make this an upper bound independent of
	// current chain state. Payload byte slices are referenced, not copied.
	effectID := make([]byte, 32)
	effectID[31] = 1
	txHash := make([]byte, 32)
	summary := &types.PrivacyScanSummaryV2{
		GlobalSequence:    math.MaxUint64,
		Height:            math.MaxInt64,
		TxHash:            txHash,
		EventType:         types.EventTypeBatchTransferV1,
		Nullifiers:        msg.Nullifiers,
		OutputCount:       uint32(len(msg.Outputs)),
		CircuitSetId:      types.ActiveCircuitSetID,
		PayloadVersion:    types.FixedPayloadVersionV1,
		ScanSchemaVersion: types.PrivacyScanSchemaVersionV2,
		AuditKeyId:        msg.AuditKeyId,
		AuditKeyEpoch:     msg.AuditKeyEpoch,
		AuditTargetPubkey: msg.AuditDisclosureTargetPubkey,
		EffectId:          effectID,
	}

	total := uint64(summary.Size()) + uint64(len(types.GetPrivacyScanSummaryKey(math.MaxInt64, math.MaxUint64)))
	// Global-sequence-to-height index key and fixed-width height value.
	total += uint64(len(types.GetPrivacyScanSequenceKey(math.MaxUint64)) + 8)
	for i, batchOutput := range msg.Outputs {
		if batchOutput == nil {
			return 0, fmt.Errorf("batch output %d is required", i)
		}
		output := &types.PrivacyScanOutputV2{
			GlobalSequence:             math.MaxUint64,
			Height:                     math.MaxInt64,
			OutputIndex:                uint32(i),
			EffectId:                   effectID,
			Commitment:                 batchOutput.Commitment,
			Ciphertext:                 batchOutput.Ciphertext,
			ViewTag:                    batchOutput.ViewTag,
			LeafIndex:                  math.MaxUint64,
			LeafIndexFound:             true,
			UserPrivacyPolicy:          batchOutput.UserPrivacyPolicy,
			UserDisclosureMode:         batchOutput.UserDisclosureMode.String(),
			UserDisclosureDigest:       batchOutput.UserDisclosureDigest,
			UserDisclosureTargetPubkey: batchOutput.UserDisclosureTargetPubkey,
			UserDisclosurePayload:      batchOutput.UserDisclosurePayload,
			FullDisclosureDigest:       batchOutput.FullDisclosureDigest,
			AuditDisclosurePayload:     batchOutput.AuditDisclosurePayload,
			SelfViewDisclosurePayload:  batchOutput.SelfViewDisclosurePayload,
			CircuitSetId:               summary.CircuitSetId,
			PayloadVersion:             summary.PayloadVersion,
			ScanSchemaVersion:          summary.ScanSchemaVersion,
			AuditKeyId:                 summary.AuditKeyId,
			AuditKeyEpoch:              summary.AuditKeyEpoch,
			AuditTargetPubkey:          summary.AuditTargetPubkey,
			TxHash:                     txHash,
			EventType:                  summary.EventType,
		}
		total += uint64(output.Size()) + uint64(len(types.GetPrivacyScanOutputKey(math.MaxInt64, math.MaxUint64, uint32(i))))
		if total > math.MaxUint64-batchMinimalEventStateAllowanceV1 {
			return 0, fmt.Errorf("typed batch state byte count overflows uint64")
		}
	}
	total += batchMinimalEventStateAllowanceV1
	return total, nil
}
