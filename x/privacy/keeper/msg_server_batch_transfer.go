package keeper

import (
	"bytes"
	"context"
	"fmt"
	"math/big"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/circuit"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
)

const (
	batchTransitionAfterNullifiers  = "after_nullifiers"
	batchTransitionAfterCommitments = "after_commitments"
	batchTransitionAfterScanIndex   = "after_scan_index"
)

type batchDerivedPublicV1 struct {
	assignment circuit.BatchJoinSplit16x32
	effect     batchPublicEffectV1
}

// BatchTransfer validates and executes one atomic 1..16 input / 1..32 output
// shielded JoinSplit. The Msg RPC is registered in tx.proto in the same core
// implementation commit as this method.
func (k msgServer) BatchTransfer(goCtx context.Context, msg *types.MsgBatchTransfer) (*types.MsgBatchTransferResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// Only cheap bounded framing is allowed before the deterministic precharge.
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	if err := validateCanonicalProofFramingBN254(msg.Proof); err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "batch proof framing is invalid: %v", err)
	}
	if _, err := consumeBatchGasPrechargeV1(ctx, msg); err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "batch gas precharge failed: %v", err)
	}

	// Full canonical, point, envelope, disclosure, and digest validation is
	// intentionally after precharge.
	if err := types.ValidateMsgBatchTransferEffectsV1(msg); err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, err.Error())
	}

	auditConfig, found, err := k.GetAuditConfigV1(ctx)
	if err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "active audit configuration is invalid: %v", err)
	}
	if !found {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "active audit configuration is not set")
	}
	if auditConfig.AuditKeyID != msg.AuditKeyId || auditConfig.AuditKeyEpoch != msg.AuditKeyEpoch ||
		!bytes.Equal(auditConfig.AuditTargetPubkey, msg.AuditDisclosureTargetPubkey) {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "batch audit key id, epoch, and target must exactly match active chain configuration")
	}

	for i, nullifier := range msg.Nullifiers {
		used, err := k.hasNullifierStrict(ctx, nullifier)
		if err != nil {
			return nil, fmt.Errorf("check batch nullifier %d: %w", i, err)
		}
		if used {
			return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "batch nullifier %d was already used", i)
		}
	}
	for i, output := range msg.Outputs {
		exists, err := k.HasCommitment(ctx, output.Commitment)
		if err != nil {
			return nil, fmt.Errorf("check batch commitment %d: %w", i, err)
		}
		if exists {
			return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "batch commitment %d already exists", i)
		}
	}
	rootExists, err := k.hasHistoricalRootStrict(ctx, msg.Root)
	if err != nil {
		return nil, fmt.Errorf("check batch historical root: %w", err)
	}
	if !rootExists {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "batch root was not found in historical merkle roots")
	}
	if err := k.EnsureCanAppendCommitments(ctx, uint64(len(msg.Outputs))); err != nil {
		return nil, wrapMerkleAppendPreconditionErr(err, "not enough merkle tree capacity for batch outputs")
	}

	derived, err := deriveBatchPublicV1(ctx, msg)
	if err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "derive batch public witness: %v", err)
	}
	if ctx.BlockTime().Unix() >= msg.ExpiresAtUnix {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "batch transfer payload has expired")
	}
	if err := verifyPrechargedProofBN254(msg.Proof, &derived.assignment, zk.GetBatchJoinSplit16x32VerifyingKey); err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "batch joinsplit proof verification failed: %v", err)
	}

	cacheCtx, writeCache := ctx.CacheContext()
	for i, nullifier := range msg.Nullifiers {
		if err := k.setNullifierStrict(cacheCtx, nullifier); err != nil {
			return nil, fmt.Errorf("write batch nullifier %d: %w", i, err)
		}
	}
	if err := k.runBatchTransitionHook(batchTransitionAfterNullifiers); err != nil {
		return nil, err
	}
	for i, output := range msg.Outputs {
		if err := k.AppendCommitment(cacheCtx, output.Commitment); err != nil {
			return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "append batch commitment %d: %v", i, err)
		}
	}
	if err := k.runBatchTransitionHook(batchTransitionAfterCommitments); err != nil {
		return nil, err
	}
	if err := k.storeBatchPrivacyEffectV1(cacheCtx, msg, derived.effect); err != nil {
		return nil, err
	}
	if err := k.runBatchTransitionHook(batchTransitionAfterScanIndex); err != nil {
		return nil, err
	}
	writeCache()
	return &types.MsgBatchTransferResponse{}, nil
}

func deriveBatchPublicV1(ctx sdk.Context, msg *types.MsgBatchTransfer) (*batchDerivedPublicV1, error) {
	inputCount := uint32(len(msg.Nullifiers))
	outputCount := uint32(len(msg.Outputs))

	nullifiers := zeroBatchFieldVector(int(types.BatchJoinSplitV1MaxInputs))
	for i, value := range msg.Nullifiers {
		nullifiers[i] = new(big.Int).SetBytes(value)
	}
	commitments := zeroBatchFieldVector(int(types.BatchJoinSplitV1MaxOutputs))
	userRawDigests := zeroBatchFieldVector(int(types.BatchJoinSplitV1MaxOutputs))
	fullDigests := zeroBatchFieldVector(int(types.BatchJoinSplitV1MaxOutputs))
	policies := make([]uint32, types.BatchJoinSplitV1MaxOutputs)
	for i, output := range msg.Outputs {
		commitments[i] = new(big.Int).SetBytes(output.Commitment)
		policies[i] = output.UserPrivacyPolicy
		if len(output.UserDisclosureDigest) != 0 {
			userRawDigests[i] = new(big.Int).SetBytes(output.UserDisclosureDigest)
		}
		fullDigests[i] = new(big.Int).SetBytes(output.FullDisclosureDigest)
	}

	nullifierRoot, err := types.ComputeBatchVectorRootV1(types.BatchVectorNullifierV1, inputCount, nullifiers)
	if err != nil {
		return nil, err
	}
	commitmentRoot, err := types.ComputeBatchVectorRootV1(types.BatchVectorCommitmentV1, outputCount, commitments)
	if err != nil {
		return nil, err
	}
	userDisclosureRoot, err := types.ComputeBatchUserDisclosureVectorRootV1(outputCount, policies, userRawDigests)
	if err != nil {
		return nil, err
	}
	fullDisclosureRoot, err := types.ComputeBatchVectorRootV1(types.BatchVectorFullDisclosureV1, outputCount, fullDigests)
	if err != nil {
		return nil, err
	}
	payloadDigest, err := types.ComputeMsgBatchTransferPayloadDigestV1(msg)
	if err != nil {
		return nil, err
	}
	chainDomain, err := types.ComputeChainDomainV1(ctx.ChainID(), types.ActiveCircuitSetID)
	if err != nil {
		return nil, err
	}
	merkleRoot := new(big.Int).SetBytes(msg.Root)
	effectID, err := types.ComputeBatchEffectIDV1(types.BatchEffectIDV1Input{
		ChainDomainHi: chainDomain.Hi, ChainDomainLo: chainDomain.Lo,
		MerkleRoot: merkleRoot, InputCount: inputCount, OutputCount: outputCount,
		NullifierRoot: nullifierRoot, CommitmentRoot: commitmentRoot,
		UserDisclosureRoot: userDisclosureRoot, FullDisclosureRoot: fullDisclosureRoot,
		PayloadDigestHi: payloadDigest.Hi, PayloadDigestLo: payloadDigest.Lo,
		ExpiresAtUnix: msg.ExpiresAtUnix,
	})
	if err != nil {
		return nil, err
	}

	derived := &batchDerivedPublicV1{}
	derived.assignment.MerkleRoot = merkleRoot
	derived.assignment.ChainDomainHi = chainDomain.Hi
	derived.assignment.ChainDomainLo = chainDomain.Lo
	derived.assignment.ExpiresAtUnix = big.NewInt(msg.ExpiresAtUnix)
	derived.assignment.InputCount = new(big.Int).SetUint64(uint64(inputCount))
	derived.assignment.OutputCount = new(big.Int).SetUint64(uint64(outputCount))
	derived.assignment.NullifierRoot = nullifierRoot
	derived.assignment.CommitmentRoot = commitmentRoot
	derived.assignment.UserDisclosureRoot = userDisclosureRoot
	derived.assignment.FullDisclosureRoot = fullDisclosureRoot
	derived.assignment.PayloadDigestHi = payloadDigest.Hi
	derived.assignment.PayloadDigestLo = payloadDigest.Lo
	derived.effect = batchPublicEffectV1{
		nullifierRoot:      batchFieldBytes32(nullifierRoot),
		commitmentRoot:     batchFieldBytes32(commitmentRoot),
		userDisclosureRoot: batchFieldBytes32(userDisclosureRoot),
		fullDisclosureRoot: batchFieldBytes32(fullDisclosureRoot),
		effectID:           append([]byte(nil), effectID[:]...),
	}
	return derived, nil
}

func zeroBatchFieldVector(size int) []*big.Int {
	values := make([]*big.Int, size)
	for i := range values {
		values[i] = new(big.Int)
	}
	return values
}

func batchFieldBytes32(value *big.Int) []byte {
	return value.FillBytes(make([]byte, 32))
}

func (k Keeper) hasNullifierStrict(ctx sdk.Context, nullifier []byte) (bool, error) {
	canonical, err := validateFieldElementBytesStrict(nullifier)
	if err != nil {
		return false, err
	}
	return k.storeService.OpenKVStore(ctx).Has(types.GetNullifierKey(canonical))
}

func (k Keeper) setNullifierStrict(ctx sdk.Context, nullifier []byte) error {
	canonical, err := validateFieldElementBytesStrict(nullifier)
	if err != nil {
		return err
	}
	return k.storeService.OpenKVStore(ctx).Set(types.GetNullifierKey(canonical), []byte{0x01})
}

func (k Keeper) hasHistoricalRootStrict(ctx sdk.Context, root []byte) (bool, error) {
	canonical, err := validateFieldElementBytesStrict(root)
	if err != nil {
		return false, err
	}
	return k.storeService.OpenKVStore(ctx).Has(types.GetHistoricalRootKey(canonical))
}

func (k Keeper) runBatchTransitionHook(stage string) error {
	if k.batchTransitionHook == nil {
		return nil
	}
	if err := k.batchTransitionHook(stage); err != nil {
		return fmt.Errorf("batch state transition failed at %s: %w", stage, err)
	}
	return nil
}
