package audit

import (
	"testing"

	privacyamount "github.com/DELIGHT-LABS/clairveil/x/privacy/amount"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/auditfield"
	"github.com/stretchr/testify/require"
)

func TestBuildLineageReportTracksDirectWithdrawAndUnspent(t *testing.T) {
	asset := auditfield.Field32FromUint64(9)
	commitment := auditfield.Field32FromUint64(11)
	nullifier := auditfield.Field32FromUint64(13)
	deposit := testDecryptedRecord(1, auditfield.KindDeposit, nil, []AuditNote{{Commitment: commitment, Asset: asset, Amount: privacyamount.FromUint64(7)}}, nil, &TransparentAuditEffect{Kind: auditfield.KindDeposit, From: []byte{1}, Amount: privacyamount.FromUint64(7)})
	withdraw := testDecryptedRecord(2, auditfield.KindWithdraw, []auditfield.Field32{commitment}, nil, []auditfield.Field32{nullifier}, &TransparentAuditEffect{Kind: auditfield.KindWithdraw, To: []byte{2}, Denom: "uclair", Amount: privacyamount.FromUint64(7)})

	report, err := BuildLineageReport([]DecryptedAuditRecord{deposit, withdraw})
	require.NoError(t, err)
	require.Len(t, report.Nodes, 1)
	require.Empty(t, report.Unspent)
	require.Len(t, report.Withdrawals, 1)
	require.Equal(t, []uint64{1}, report.Withdrawals[0].RootDeposits)
	require.EqualValues(t, 2, report.Nodes[0].SpentSequence)
}

func TestBuildLineageReportRejectsPrivateRootWithoutDeposit(t *testing.T) {
	asset := auditfield.Field32FromUint64(9)
	transfer := testDecryptedRecord(1, auditfield.KindTransfer2x2,
		[]auditfield.Field32{auditfield.Field32FromUint64(7), auditfield.Field32FromUint64(8)},
		[]AuditNote{{Commitment: auditfield.Field32FromUint64(21), Asset: asset, Amount: privacyamount.FromUint64(3)}, {Commitment: auditfield.Field32FromUint64(22), Asset: asset, Amount: privacyamount.FromUint64(4)}},
		[]auditfield.Field32{auditfield.Field32FromUint64(31), auditfield.Field32FromUint64(32)}, nil)
	_, err := BuildLineageReport([]DecryptedAuditRecord{transfer})
	require.ErrorContains(t, err, "Cin has no earlier unspent C")
}

func TestBuildLineageReportRetainsUnspentDeposit(t *testing.T) {
	asset := auditfield.Field32FromUint64(9)
	deposit := testDecryptedRecord(1, auditfield.KindDeposit, nil, []AuditNote{{Commitment: auditfield.Field32FromUint64(11), Asset: asset, Amount: privacyamount.FromUint64(7)}}, nil, &TransparentAuditEffect{Kind: auditfield.KindDeposit, From: []byte{1}, Amount: privacyamount.FromUint64(7)})
	report, err := BuildLineageReport([]DecryptedAuditRecord{deposit})
	require.NoError(t, err)
	require.Len(t, report.Unspent, 1)
	require.Equal(t, []uint64{1}, report.Unspent[0].RootDeposits)
}

func testDecryptedRecord(sequence uint64, kind auditfield.Kind, inputs []auditfield.Field32, outputs []AuditNote, nullifiers []auditfield.Field32, effect *TransparentAuditEffect) DecryptedAuditRecord {
	recordOutputs := make([]AuditOutput, len(outputs))
	for i, output := range outputs {
		recordOutputs[i] = AuditOutput{Commitment: output.Commitment, LeafIndex: uint64(i)}
	}
	record := VerifiedAuditRecord{record: &auditRecord{sequence: sequence, height: sequence, kind: kind, inputs: nullifiers, outputs: recordOutputs, transparent: effect}}
	plain := []auditfield.Field32{auditfield.Field32FromUint64(1)}
	asset := auditfield.Field32FromUint64(9)
	if len(outputs) > 0 {
		asset = outputs[0].Asset
	}
	return DecryptedAuditRecord{record: &record, plain: plain, asset: asset, inputs: inputs, output: outputs}
}

func TestPrivateConservationRejectsUint128OperationOverflow(t *testing.T) {
	max, err := privacyamount.Parse("340282366920938463463374607431768211455")
	require.NoError(t, err)
	one := privacyamount.FromUint64(1)
	asset := auditfield.Field32FromUint64(9)
	inputs := []*mutableLineageNode{{LineageNode: LineageNode{AuditNote: AuditNote{Asset: asset, Amount: max}}}, {LineageNode: LineageNode{AuditNote: AuditNote{Asset: asset, Amount: one}}}}
	outputs := []AuditNote{{Asset: asset, Amount: max}, {Asset: asset, Amount: one}}
	require.ErrorContains(t, validatePrivateConservation(asset, inputs, outputs), "input total exceeds uint128")
	inputs[0].Amount, err = max.Sub(one)
	require.NoError(t, err)
	outputs[1].Amount = privacyamount.Amount128{}
	require.NoError(t, validatePrivateConservation(asset, inputs, outputs))
}
