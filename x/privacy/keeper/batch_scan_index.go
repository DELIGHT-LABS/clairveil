package keeper

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

const (
	batchAttributeEffectID           = "effect_id"
	batchAttributeInputCount         = "input_count"
	batchAttributeOutputCount        = "output_count"
	batchAttributeNullifierRoot      = "nullifier_root"
	batchAttributeCommitmentRoot     = "commitment_root"
	batchAttributeUserDisclosureRoot = "user_disclosure_root"
	batchAttributeFullDisclosureRoot = "full_disclosure_root"
	batchAttributeExpiresAtUnix      = "expires_at_unix"
	batchAttributeCircuitSetID       = "circuit_set_id"
	batchAttributePayloadVersion     = "payload_version"
	batchAttributeScanSchemaVersion  = "scan_schema_version"
	batchAttributeAuditKeyID         = "audit_key_id"
	batchAttributeAuditKeyEpoch      = "audit_key_epoch"
	batchAttributeAuditTarget        = "audit_target_pubkey"
)

type batchPublicEffectV1 struct {
	nullifierRoot      []byte
	commitmentRoot     []byte
	userDisclosureRoot []byte
	fullDisclosureRoot []byte
	effectID           []byte
}

// storeBatchPrivacyEffectV1 stores output bytes only in PrivacyScanOutputV2.
// The generic indexed record and ABCI event contain the same minimal summary
// attributes and never duplicate ciphertext, disclosure payloads, commitments,
// or the full nullifier list.
func (k Keeper) storeBatchPrivacyEffectV1(
	ctx sdk.Context,
	msg *types.MsgBatchTransfer,
	effect batchPublicEffectV1,
) error {
	sequence, err := k.AllocatePrivacyGlobalSequence(ctx)
	if err != nil {
		return fmt.Errorf("allocate batch privacy sequence: %w", err)
	}

	txHashHex := txHashHexFromContext(ctx)
	var txHash []byte
	if txHashHex != "" {
		txHash, err = hex.DecodeString(txHashHex)
		if err != nil {
			return fmt.Errorf("decode batch transaction hash: %w", err)
		}
	}
	summary := &types.PrivacyScanSummaryV2{
		GlobalSequence:    sequence,
		Height:            ctx.BlockHeight(),
		TxHash:            append([]byte(nil), txHash...),
		EventType:         types.EventTypeBatchTransferV1,
		Nullifiers:        cloneBatchByteSlices(msg.Nullifiers),
		OutputCount:       uint32(len(msg.Outputs)),
		CircuitSetId:      types.ActiveCircuitSetID,
		PayloadVersion:    types.FixedPayloadVersionV1,
		ScanSchemaVersion: types.PrivacyScanSchemaVersionV2,
		AuditKeyId:        msg.AuditKeyId,
		AuditKeyEpoch:     msg.AuditKeyEpoch,
		AuditTargetPubkey: append([]byte(nil), msg.AuditDisclosureTargetPubkey...),
		EffectId:          append([]byte(nil), effect.effectID...),
	}

	outputs := make([]*types.PrivacyScanOutputV2, len(msg.Outputs))
	for i, batchOutput := range msg.Outputs {
		leafIndex, found, err := k.GetCommitmentIndex(ctx, batchOutput.Commitment)
		if err != nil {
			return fmt.Errorf("read batch commitment %d leaf index: %w", i, err)
		}
		if !found {
			return fmt.Errorf("batch commitment %d is missing after append", i)
		}
		outputs[i] = &types.PrivacyScanOutputV2{
			GlobalSequence:             sequence,
			Height:                     ctx.BlockHeight(),
			OutputIndex:                uint32(i),
			EffectId:                   append([]byte(nil), effect.effectID...),
			Commitment:                 append([]byte(nil), batchOutput.Commitment...),
			Ciphertext:                 append([]byte(nil), batchOutput.Ciphertext...),
			ViewTag:                    append([]byte(nil), batchOutput.ViewTag...),
			LeafIndex:                  leafIndex,
			LeafIndexFound:             true,
			UserPrivacyPolicy:          batchOutput.UserPrivacyPolicy,
			UserDisclosureMode:         batchOutput.UserDisclosureMode.String(),
			UserDisclosureDigest:       append([]byte(nil), batchOutput.UserDisclosureDigest...),
			UserDisclosureTargetPubkey: append([]byte(nil), batchOutput.UserDisclosureTargetPubkey...),
			UserDisclosurePayload:      append([]byte(nil), batchOutput.UserDisclosurePayload...),
			FullDisclosureDigest:       append([]byte(nil), batchOutput.FullDisclosureDigest...),
			AuditDisclosurePayload:     append([]byte(nil), batchOutput.AuditDisclosurePayload...),
			SelfViewDisclosurePayload:  append([]byte(nil), batchOutput.SelfViewDisclosurePayload...),
			CircuitSetId:               summary.CircuitSetId,
			PayloadVersion:             summary.PayloadVersion,
			ScanSchemaVersion:          summary.ScanSchemaVersion,
			AuditKeyId:                 summary.AuditKeyId,
			AuditKeyEpoch:              summary.AuditKeyEpoch,
			AuditTargetPubkey:          append([]byte(nil), summary.AuditTargetPubkey...),
			TxHash:                     append([]byte(nil), txHash...),
			EventType:                  summary.EventType,
		}
	}
	if err := k.StorePrivacyScanV2(ctx, summary, outputs); err != nil {
		return fmt.Errorf("store typed batch privacy scan index: %w", err)
	}
	if err := k.RecordCurrentMerkleRootSnapshotV1(ctx); err != nil {
		return fmt.Errorf("record batch merkle root snapshot: %w", err)
	}

	attrs := batchMinimalEventAttributes(msg, effect)
	event := &types.QueryPrivacyEvent{
		Sequence:   sequence,
		Height:     ctx.BlockHeight(),
		TxHashHex:  strings.ToUpper(txHashHex),
		EventType:  types.EventTypeBatchTransferV1,
		Attributes: make([]*types.QueryPrivacyEventAttribute, 0, len(attrs)),
	}
	for _, attr := range attrs {
		event.Attributes = append(event.Attributes, &types.QueryPrivacyEventAttribute{Key: attr.Key, Value: attr.Value})
	}
	encodedEvent, err := event.Marshal()
	if err != nil {
		return fmt.Errorf("encode minimal batch privacy event: %w", err)
	}
	if err := k.storeService.OpenKVStore(ctx).Set(types.GetPrivacyEventKey(ctx.BlockHeight(), sequence), encodedEvent); err != nil {
		return fmt.Errorf("store minimal batch privacy event: %w", err)
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeBatchTransferV1, attrs...))
	return nil
}

func batchMinimalEventAttributes(msg *types.MsgBatchTransfer, effect batchPublicEffectV1) []sdk.Attribute {
	return []sdk.Attribute{
		sdk.NewAttribute(batchAttributeEffectID, hex.EncodeToString(effect.effectID)),
		sdk.NewAttribute(types.AttributeKeyRelayer, msg.Creator),
		sdk.NewAttribute(batchAttributeInputCount, strconv.Itoa(len(msg.Nullifiers))),
		sdk.NewAttribute(batchAttributeOutputCount, strconv.Itoa(len(msg.Outputs))),
		sdk.NewAttribute(batchAttributeNullifierRoot, hex.EncodeToString(effect.nullifierRoot)),
		sdk.NewAttribute(batchAttributeCommitmentRoot, hex.EncodeToString(effect.commitmentRoot)),
		sdk.NewAttribute(batchAttributeUserDisclosureRoot, hex.EncodeToString(effect.userDisclosureRoot)),
		sdk.NewAttribute(batchAttributeFullDisclosureRoot, hex.EncodeToString(effect.fullDisclosureRoot)),
		sdk.NewAttribute(batchAttributeExpiresAtUnix, strconv.FormatInt(msg.ExpiresAtUnix, 10)),
		sdk.NewAttribute(batchAttributeCircuitSetID, types.ActiveCircuitSetID),
		sdk.NewAttribute(batchAttributePayloadVersion, types.FixedPayloadVersionV1),
		sdk.NewAttribute(batchAttributeScanSchemaVersion, types.PrivacyScanSchemaVersionV2),
		sdk.NewAttribute(batchAttributeAuditKeyID, msg.AuditKeyId),
		sdk.NewAttribute(batchAttributeAuditKeyEpoch, strconv.FormatUint(msg.AuditKeyEpoch, 10)),
		sdk.NewAttribute(batchAttributeAuditTarget, hex.EncodeToString(msg.AuditDisclosureTargetPubkey)),
	}
}

func cloneBatchByteSlices(values [][]byte) [][]byte {
	cloned := make([][]byte, len(values))
	for i, value := range values {
		cloned[i] = append([]byte(nil), value...)
	}
	return cloned
}
