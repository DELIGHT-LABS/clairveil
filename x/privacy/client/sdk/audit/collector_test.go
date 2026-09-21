package audit

import (
	"context"
	"testing"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/auditfield"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	privacyv2 "github.com/DELIGHT-LABS/clairveil/x/privacy/types/v2"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

type collectorSource []SuccessfulAuditTx

func (s collectorSource) SuccessfulAuditTransactions(_ context.Context, _ uint64) ([]SuccessfulAuditTx, error) {
	return append([]SuccessfulAuditTx(nil), s...), nil
}

type collectorKeys struct{ record KeyRecord }

func (k collectorKeys) AuditKey(_ context.Context, epoch uint64) (KeyRecord, error) {
	if epoch != k.record.Epoch {
		return KeyRecord{}, context.Canceled
	}
	return k.record, nil
}

func TestCollectorConsumesOriginalSuccessfulTransactionsForAllFourKinds(t *testing.T) {
	key := testAuditKey(t)
	creator := sdk.AccAddress(make([]byte, 20)).String()
	recipient := sdk.AccAddress(append(make([]byte, 19), 1)).String()
	network := [32]byte{1}
	makeOutput := func(n uint64, kind privacytypes.EncryptedEnvelopeKindV1, fullDigest bool) *privacyv2.OutputEffect {
		output := &privacyv2.OutputEffect{Commitment: auditfield.Field32FromUint64(n).Bytes(), Ciphertext: testEnvelope(t, kind)}
		if kind != privacytypes.EnvelopeDepositNoteV1 {
			output.ViewTag = []byte{1, 2}
			if fullDigest {
				output.SelfFullDisclosureDigest = auditfield.Field32FromUint64(99).Bytes()
			}
		}
		return output
	}
	auth := func(kind auditfield.Kind) *privacyv2.AuditAuthorization {
		size, err := kind.EnvelopeSize()
		require.NoError(t, err)
		return &privacyv2.AuditAuthorization{KeyId: key.IDBytes(), Epoch: 1, Envelope: make([]byte, size)}
	}
	field := func(n uint64) []byte { return auditfield.Field32FromUint64(n).Bytes() }
	rows := []SuccessfulAuditTx{
		{Message: &privacyv2.MsgDeposit{Creator: creator, Amount: "1uclair", Output: makeOutput(1, privacytypes.EnvelopeDepositNoteV1, false), Proof: make([]byte, 164), ExpiresAtUnix: 1, Audit: auth(auditfield.KindDeposit)}},
		{Message: &privacyv2.MsgWithdraw{Creator: creator, Recipient: recipient, Amount: "1uclair", Root: field(2), Nullifier: field(3), Proof: make([]byte, 164), ExpiresAtUnix: 1, Audit: auth(auditfield.KindWithdraw)}},
		{Message: &privacyv2.MsgTransfer{Creator: creator, Root: field(4), Nullifiers: [][]byte{field(5), field(6)}, Outputs: []*privacyv2.OutputEffect{makeOutput(7, privacytypes.EnvelopeTransferNoteV1, true), makeOutput(8, privacytypes.EnvelopeTransferNoteV1, false)}, Proof: make([]byte, 164), ExpiresAtUnix: 1, Audit: auth(auditfield.KindTransfer2x2)}},
	}
	batch := &privacyv2.MsgBatchTransfer{Creator: creator, Root: field(9), Proof: make([]byte, 164), ExpiresAtUnix: 1, Audit: auth(auditfield.KindBatch16x32)}
	for i := 0; i < 16; i++ {
		batch.Nullifiers = append(batch.Nullifiers, field(uint64(10+i)))
	}
	for i := 0; i < 32; i++ {
		batch.Outputs = append(batch.Outputs, makeOutput(uint64(30+i), privacytypes.EnvelopeTransferNoteV1, true))
	}
	rows = append(rows, SuccessfulAuditTx{Message: batch})
	for i := range rows {
		txHash := make([]byte, 32)
		txHash[31] = byte(i + 1)
		id := collectedExecutionID(network, [32]byte{31: byte(i + 1)}, uint64(i+1), uint32(i))
		rows[i].Event = ExecutionEvent{Height: 100, TxHash: txHash, EventType: eventTypeForKind(auditfield.Kind(i + 1)), GlobalSequence: uint64(i + 1), MessageIndex: uint32(i), ExecutionID: id[:]}
	}
	rows[0].Event.Funder = sdk.AccAddress(append(make([]byte, 19), 9)).String()

	collector := Collector{Network: network, Keys: collectorKeys{record: KeyRecord{Epoch: 1, Key: key}}}
	collected, err := collector.Collect(context.Background(), collectorSource(rows), 0)
	require.NoError(t, err)
	require.Len(t, collected, 4)
	require.Equal(t, rows[0].Event.Funder, collected[0].Funder)
	require.Equal(t, []auditfield.Kind{auditfield.KindDeposit, auditfield.KindWithdraw, auditfield.KindTransfer2x2, auditfield.KindBatch16x32}, []auditfield.Kind{collected[0].Kind(), collected[1].Kind(), collected[2].Kind(), collected[3].Kind()})

	rows[2].Event.ExecutionID[0] ^= 1
	_, err = collector.Collect(context.Background(), collectorSource(rows), 0)
	require.ErrorContains(t, err, "execution ID mismatch")
}
