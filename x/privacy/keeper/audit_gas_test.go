package keeper

import (
	"testing"

	af "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/auditfield"
	pt "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	v2 "github.com/DELIGHT-LABS/clairveil/x/privacy/types/v2"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestAuditGasMaxBatchProjectionAndPrecharge(t *testing.T) {
	_, ctx, _ := setupMsgServerKeeper()
	m := &v2.MsgBatchTransfer{Creator: testAddress(1), Root: af.Field32FromUint64(1).Bytes(), Proof: make([]byte, 164), ExpiresAtUnix: msgServerTestExpiry, Audit: &v2.AuditAuthorization{KeyId: make([]byte, 32), Epoch: 1, Envelope: make([]byte, 5808)}}
	for i := 0; i < 16; i++ {
		m.Nullifiers = append(m.Nullifiers, af.Field32FromUint64(uint64(i+1)).Bytes())
	}
	for i := 0; i < 32; i++ {
		m.Outputs = append(m.Outputs, &v2.OutputEffect{Commitment: af.Field32FromUint64(uint64(i + 33)).Bytes(), Ciphertext: testKeeperEnvelopeTB(t, pt.EnvelopeTransferNoteV1), ViewTag: []byte{1, 2}, SelfFullDisclosureDigest: af.Field32FromUint64(1).Bytes(), SelfViewDisclosurePayload: testKeeperEnvelopeTB(t, pt.EnvelopeSelfViewDisclosureV1)})
	}
	before := ctx.GasMeter().GasConsumed()
	bound, err := prechargeAuditMessage(ctx, m)
	require.NoError(t, err)
	require.Greater(t, bound, uint64(len(m.Audit.Envelope)))
	require.Less(t, bound, uint64(1<<20))
	require.Greater(t, ctx.GasMeter().GasConsumed(), before)
	dto, err := pt.ValidateAuditMessage(m)
	require.NoError(t, err)
	_, actualAux, err := pt.AuditOutputsToAux(af.KindBatch16x32, m.Outputs)
	require.NoError(t, err)
	require.Equal(t, actualAux, dto.Aux())
	m.Outputs[0].Ciphertext = make([]byte, 128<<10)
	_, err = prechargeAuditMessage(ctx, m)
	require.Error(t, err)
}

func TestAuditPrechargeBoundsProjectedScanForAllTransactionKinds(t *testing.T) {
	_, ctx, _ := setupMsgServerKeeper()
	output := func(n uint64) *v2.OutputEffect {
		return &v2.OutputEffect{Commitment: af.Field32FromUint64(n).Bytes(), Ciphertext: testKeeperEnvelopeTB(t, pt.EnvelopeTransferNoteV1), ViewTag: []byte{1, 2}, SelfFullDisclosureDigest: af.Field32FromUint64(1).Bytes(), SelfViewDisclosurePayload: testKeeperEnvelopeTB(t, pt.EnvelopeSelfViewDisclosureV1)}
	}
	audit := &v2.AuditAuthorization{KeyId: make([]byte, 32), Epoch: 1, Envelope: make([]byte, 5808)}
	cases := []struct {
		name    string
		msg     sdk.Msg
		kind    af.Kind
		inputs  int
		outputs []*v2.OutputEffect
	}{
		{"deposit", &v2.MsgDeposit{Creator: testAddress(1), Amount: "1uclair", Output: output(1), Audit: audit}, af.KindDeposit, 0, []*v2.OutputEffect{output(1)}},
		{"withdraw", &v2.MsgWithdraw{Creator: testAddress(1), Recipient: testAddress(2), Amount: "1uclair", Nullifier: af.Field32FromUint64(2).Bytes(), Audit: audit}, af.KindWithdraw, 1, nil},
		{"transfer", &v2.MsgTransfer{Creator: testAddress(1), Nullifiers: [][]byte{af.Field32FromUint64(3).Bytes(), af.Field32FromUint64(4).Bytes()}, Outputs: []*v2.OutputEffect{output(3), output(4)}, Audit: audit}, af.KindTransfer2x2, 2, []*v2.OutputEffect{output(3), output(4)}},
	}
	batch := &v2.MsgBatchTransfer{Creator: testAddress(1), Audit: audit}
	for i := 0; i < 16; i++ {
		batch.Nullifiers = append(batch.Nullifiers, af.Field32FromUint64(uint64(10+i)).Bytes())
	}
	for i := 0; i < 32; i++ {
		batch.Outputs = append(batch.Outputs, output(uint64(30+i)))
	}
	cases = append(cases, struct {
		name    string
		msg     sdk.Msg
		kind    af.Kind
		inputs  int
		outputs []*v2.OutputEffect
	}{"batch", batch, af.KindBatch16x32, 16, batch.Outputs})

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bound, err := prechargeAuditMessage(ctx, tc.msg)
			require.NoError(t, err)
			record := &auditTransitionRecord{Kind: tc.kind, SetID: af.CircuitSetID, Epoch: 1, PK: make([]byte, 64), Origin: auditOrigin{Kind: 1, Height: uint64(ctx.BlockHeight())}, Inputs: make([]af.Field32, tc.inputs), Outputs: make([]auditOutputRef, len(tc.outputs))}
			for i := range record.Outputs {
				record.Outputs[i].LeafIndex = uint64(i)
			}
			summary, outputs := auditScanProjection(1, ctx.BlockHeight(), record, tc.outputs, [32]byte{1})
			actual := uint64(summary.Size() + 17 + 9 + 8)
			for _, projected := range outputs {
				actual += uint64(projected.Size() + 21)
			}
			require.LessOrEqual(t, actual, bound)
		})
	}
}
