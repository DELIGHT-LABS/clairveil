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
