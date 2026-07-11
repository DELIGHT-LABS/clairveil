package keeper

import (
	"bytes"
	"math"
	"strings"
	"testing"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	privacyzk "github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
)

func TestBatchGasPrechargeV1MetersEveryFrozenCategory(t *testing.T) {
	msg := testMaxBatchTransferMessage(t)
	payloadBytes, err := privacytypes.CanonicalMsgBatchTransferPayloadSizeV1(msg)
	require.NoError(t, err)
	require.Equal(t, BatchMaxCanonicalPayloadBytesV1, payloadBytes)

	typedStateBytes, err := estimateBatchTypedStateBytesV1(msg)
	require.NoError(t, err)
	require.Positive(t, typedStateBytes)
	require.LessOrEqual(t, typedStateBytes, BatchMaxTypedStateBytesV1)

	breakdown, err := computeBatchGasPrechargeV1(msg)
	require.NoError(t, err)
	require.Equal(t, BatchVerifyBaseGasV1, breakdown.Verification)
	require.Equal(t, BatchPerInputGasV1*uint64(privacytypes.BatchJoinSplitV1MaxInputs), breakdown.Inputs)
	require.Equal(t, BatchPerOutputGasV1*uint64(privacytypes.BatchJoinSplitV1MaxOutputs), breakdown.Outputs)
	require.Equal(t, BatchPerCanonicalPayloadByteGasV1*payloadBytes, breakdown.CanonicalPayload)
	require.Equal(t, BatchPerTypedStateByteGasV1*typedStateBytes, breakdown.TypedScanState)
	require.Equal(t, BatchPerTreeNodeWriteGasV1*BatchMaxTreeNodeWritesV1, breakdown.TreeWrites)
	require.Equal(t, BatchPerGlobalLookupGasV1*BatchMaxGlobalLookupsV1, breakdown.GlobalLookups)
	require.Positive(t, breakdown.Total)
}

func TestBatchGasShapeProfile(t *testing.T) {
	base := testMaxBatchTransferMessage(t)
	for _, shape := range [][2]int{{1, 1}, {3, 4}, {8, 16}, {16, 31}, {16, 32}} {
		msg := *base
		msg.Nullifiers = base.Nullifiers[:shape[0]]
		msg.Outputs = base.Outputs[:shape[1]]
		payloadBytes, err := privacytypes.CanonicalMsgBatchTransferPayloadSizeV1(&msg)
		require.NoError(t, err)
		typedStateBytes, err := estimateBatchTypedStateBytesV1(&msg)
		require.NoError(t, err)
		breakdown, err := computeBatchGasPrechargeV1(&msg)
		require.NoError(t, err)
		t.Logf(
			"BATCH_GAS_SHAPE inputs=%d outputs=%d payload_bytes=%d typed_state_bytes=%d verification=%d inputs_gas=%d outputs_gas=%d payload_gas=%d typed_state_gas=%d tree_gas=%d lookup_gas=%d total=%d",
			shape[0], shape[1], payloadBytes, typedStateBytes, breakdown.Verification, breakdown.Inputs,
			breakdown.Outputs, breakdown.CanonicalPayload, breakdown.TypedScanState,
			breakdown.TreeWrites, breakdown.GlobalLookups, breakdown.Total,
		)
	}
}

func TestBatchTransferConsumesPrechargeBeforeSemanticValidation(t *testing.T) {
	k, ctx, _ := setupRegisteredMsgServerKeeper(t)
	msg := testMaxBatchTransferMessage(t)
	msg.Root = bytes.Repeat([]byte{0xff}, 32) // length-valid, non-canonical field
	breakdown, err := computeBatchGasPrechargeV1(msg)
	require.NoError(t, err)
	ctx = ctx.WithGasMeter(storetypes.NewGasMeter(2 * breakdown.Total))

	_, err = (msgServer{Keeper: *k}).BatchTransfer(sdk.WrapSDKContext(ctx), msg)
	require.ErrorContains(t, err, "canonical")
	require.Equal(t, storetypes.Gas(breakdown.Total), ctx.GasMeter().GasConsumed())
	require.Zero(t, k.GetLeafCount(ctx))
}

func TestBatchTransferOutOfGasStopsBeforeStateOrSemanticWork(t *testing.T) {
	k, ctx, _ := setupRegisteredMsgServerKeeper(t)
	msg := testMaxBatchTransferMessage(t)
	msg.Root = bytes.Repeat([]byte{0xff}, 32)
	breakdown, err := computeBatchGasPrechargeV1(msg)
	require.NoError(t, err)
	require.Greater(t, breakdown.Total, uint64(1))
	ctx = ctx.WithGasMeter(storetypes.NewGasMeter(breakdown.Total - 1))

	require.Panics(t, func() {
		_, _ = (msgServer{Keeper: *k}).BatchTransfer(sdk.WrapSDKContext(ctx), msg)
	})
	queryCtx := ctx.WithGasMeter(storetypes.NewInfiniteGasMeter())
	require.Zero(t, k.GetLeafCount(queryCtx))
	sequence, err := k.GetPrivacyGlobalSequence(queryCtx)
	require.NoError(t, err)
	require.Zero(t, sequence)
}

func TestBatchGasLayersRemainSeparateForMaxStateTransition(t *testing.T) {
	k, ctx, _ := setupRegisteredMsgServerKeeper(t)
	ctx = ctx.WithBlockHeight(79).WithTxBytes([]byte("max-batch-gas-layer-accounting"))
	msg := testMaxBatchTransferMessage(t)
	meter := newRecordingGasMeter()
	ctx = ctx.WithGasMeter(meter)

	breakdown, err := consumeBatchGasPrechargeV1(ctx, msg)
	require.NoError(t, err)
	derived, err := deriveBatchPublicV1(ctx, msg)
	require.NoError(t, err)
	for _, nullifier := range msg.Nullifiers {
		require.NoError(t, k.setNullifierStrict(ctx, nullifier))
	}
	for _, output := range msg.Outputs {
		require.NoError(t, k.AppendCommitment(ctx, output.Commitment))
	}
	require.NoError(t, k.storeBatchPrivacyEffectV1(ctx, msg, derived.effect))

	assertBatchGasLayerAccounting(t, meter, breakdown)
	require.Equal(t, uint64(32), k.GetLeafCount(ctx))
}

type recordingGasMeter struct {
	storetypes.GasMeter
	consumed map[string]storetypes.Gas
	refunded map[string]storetypes.Gas
}

func newRecordingGasMeter() *recordingGasMeter {
	return &recordingGasMeter{
		GasMeter: storetypes.NewInfiniteGasMeter(),
		consumed: make(map[string]storetypes.Gas),
		refunded: make(map[string]storetypes.Gas),
	}
}

func (m *recordingGasMeter) ConsumeGas(amount storetypes.Gas, descriptor string) {
	m.GasMeter.ConsumeGas(amount, descriptor)
	m.consumed[descriptor] += amount
}

func (m *recordingGasMeter) RefundGas(amount storetypes.Gas, descriptor string) {
	m.GasMeter.RefundGas(amount, descriptor)
	m.refunded[descriptor] += amount
}

func assertBatchGasLayerAccounting(t testing.TB, meter *recordingGasMeter, breakdown privacyzk.BatchGasBreakdownV1) {
	t.Helper()
	explicit := meter.consumed[BatchGasPrechargeDescriptorV1]
	require.Equal(t, storetypes.Gas(breakdown.Total), explicit)
	require.Zero(t, meter.refunded[BatchGasPrechargeDescriptorV1])

	kvDescriptors := []string{
		storetypes.GasHasDesc,
		storetypes.GasDeleteDesc,
		storetypes.GasIterNextCostFlatDesc,
		storetypes.GasValuePerByteDesc,
		storetypes.GasReadCostFlatDesc,
		storetypes.GasReadPerByteDesc,
		storetypes.GasWriteCostFlatDesc,
		storetypes.GasWritePerByteDesc,
	}
	var kvGas storetypes.Gas
	for _, descriptor := range kvDescriptors {
		kvGas += meter.consumed[descriptor] - meter.refunded[descriptor]
	}
	require.Positive(t, kvGas)
	require.Positive(t, meter.consumed[storetypes.GasReadCostFlatDesc])
	require.Positive(t, meter.consumed[storetypes.GasWriteCostFlatDesc])

	var allOtherGas storetypes.Gas
	for descriptor, amount := range meter.consumed {
		if descriptor != BatchGasPrechargeDescriptorV1 {
			allOtherGas += amount - meter.refunded[descriptor]
		}
	}
	require.Equal(t, kvGas, allOtherGas, "non-precharge gas must be Cosmos KV gas only: consumed=%v refunded=%v", meter.consumed, meter.refunded)
	require.Equal(t, explicit+kvGas, meter.GasConsumed())
}

func testMaxBatchTransferMessage(t testing.TB) *privacytypes.MsgBatchTransfer {
	t.Helper()
	point := testKeeperDisclosurePubKey()
	msg := &privacytypes.MsgBatchTransfer{
		Creator:                     testAddress(0x41),
		Proof:                       testBatchProofFrame(),
		Root:                        fixedFieldBytes(31),
		AuditKeyId:                  strings.Repeat("a", privacytypes.AuditKeyIDV1MaxBytes),
		AuditKeyEpoch:               math.MaxUint64,
		AuditDisclosureTargetPubkey: append([]byte(nil), point...),
		ExpiresAtUnix:               math.MaxInt64,
	}
	for i := 0; i < int(privacytypes.BatchJoinSplitV1MaxInputs); i++ {
		msg.Nullifiers = append(msg.Nullifiers, fixedFieldBytes(uint64(101+i)))
	}
	for i := 0; i < int(privacytypes.BatchJoinSplitV1MaxOutputs); i++ {
		msg.Outputs = append(msg.Outputs, &privacytypes.BatchTransferOutput{
			Commitment:                 fixedFieldBytes(uint64(1_001 + i)),
			Ciphertext:                 testKeeperEnvelopeTB(t, privacytypes.EnvelopeTransferNoteV1),
			ViewTag:                    []byte{byte(i), byte(i + 1)},
			UserPrivacyPolicy:          privacytypes.TransferPrivacyPolicyDiscloseAmount,
			UserDisclosureMode:         privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_RECIPIENT_ENCRYPTED,
			UserDisclosureDigest:       fixedFieldBytes(uint64(2_001 + i)),
			UserDisclosureTargetPubkey: append([]byte(nil), point...),
			UserDisclosurePayload:      testKeeperEnvelopeTB(t, privacytypes.EnvelopeUserDisclosureV1),
			FullDisclosureDigest:       fixedFieldBytes(uint64(3_001 + i)),
			AuditDisclosurePayload:     testKeeperEnvelopeTB(t, privacytypes.EnvelopeAuditDisclosureV1),
			SelfViewDisclosurePayload:  testKeeperEnvelopeTB(t, privacytypes.EnvelopeSelfViewDisclosureV1),
		})
	}
	return msg
}

func testBatchProofFrame() []byte {
	proof := make([]byte, privacytypes.BatchTransferProofSizeV1)
	for _, offset := range []int{0, 32, 96, 132} {
		proof[offset] = 0x80
	}
	return proof
}

func testKeeperEnvelopeTB(t testing.TB, kind privacytypes.EncryptedEnvelopeKindV1) []byte {
	t.Helper()
	total, err := privacytypes.EncryptedEnvelopeV1Size(kind)
	require.NoError(t, err)
	raw := make([]byte, total-privacytypes.EncryptedEnvelopeV1HeaderSize)
	wrapper, err := privacytypes.WrapEncryptedEnvelopeV1(kind, raw)
	require.NoError(t, err)
	return wrapper
}
