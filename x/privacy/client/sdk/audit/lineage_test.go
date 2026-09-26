package audit

import (
	"testing"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/auditfield"
	"github.com/stretchr/testify/require"
)

func TestBuildLineageReportTracksDirectWithdrawAndUnspent(t *testing.T) {
	asset := auditfield.Field32FromUint64(9)
	commitment := auditfield.Field32FromUint64(11)
	nullifier := auditfield.Field32FromUint64(13)
	deposit := testDecryptedRecord(1, auditfield.KindDeposit, nil, []AuditNote{{Commitment: commitment, Asset: asset, Amount: 7}}, nil, &TransparentAuditEffect{Kind: auditfield.KindDeposit, From: []byte{1}, Amount: 7})
	withdraw := testDecryptedRecord(2, auditfield.KindWithdraw, []auditfield.Field32{commitment}, nil, []auditfield.Field32{nullifier}, &TransparentAuditEffect{Kind: auditfield.KindWithdraw, To: []byte{2}, Denom: "uclair", Amount: 7})

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
		[]AuditNote{{Commitment: auditfield.Field32FromUint64(21), Asset: asset, Amount: 3}, {Commitment: auditfield.Field32FromUint64(22), Asset: asset, Amount: 4}},
		[]auditfield.Field32{auditfield.Field32FromUint64(31), auditfield.Field32FromUint64(32)}, nil)
	_, err := BuildLineageReport([]DecryptedAuditRecord{transfer})
	require.ErrorContains(t, err, "Cin has no earlier unspent C")
}

func TestBuildLineageReportRetainsUnspentDeposit(t *testing.T) {
	asset := auditfield.Field32FromUint64(9)
	deposit := testDecryptedRecord(1, auditfield.KindDeposit, nil, []AuditNote{{Commitment: auditfield.Field32FromUint64(11), Asset: asset, Amount: 7}}, nil, &TransparentAuditEffect{Kind: auditfield.KindDeposit, From: []byte{1}, Amount: 7})
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

func TestBuildLineageReportSeparatesZeroDepositFunding(t *testing.T) {
	asset := auditfield.Field32FromUint64(9)
	field := auditfield.Field32FromUint64
	positive := testDecryptedRecord(1, auditfield.KindDeposit, nil, []AuditNote{{Commitment: field(11), Asset: asset, Amount: 10}}, nil, &TransparentAuditEffect{Kind: auditfield.KindDeposit, From: []byte{1}, Amount: 10})
	zero := testDecryptedRecord(2, auditfield.KindDeposit, nil, []AuditNote{{Commitment: field(12), Asset: asset}}, nil, &TransparentAuditEffect{Kind: auditfield.KindDeposit, From: []byte{2}})
	transfer := testDecryptedRecord(3, auditfield.KindTransfer2x2, []auditfield.Field32{field(11), field(12)},
		[]AuditNote{{Commitment: field(21), Asset: asset, Amount: 10}, {Commitment: field(22), Asset: asset}}, []auditfield.Field32{field(31), field(32)}, nil)
	withdraw := testDecryptedRecord(4, auditfield.KindWithdraw, []auditfield.Field32{field(21)}, nil, []auditfield.Field32{field(33)}, &TransparentAuditEffect{Kind: auditfield.KindWithdraw, To: []byte{3}, Amount: 10})

	report, err := BuildLineageReport([]DecryptedAuditRecord{positive, zero, transfer, withdraw})
	require.NoError(t, err)
	require.Len(t, report.Nodes, 4)
	require.Equal(t, []uint64{1}, report.Nodes[0].FundingRootDeposits)
	require.Equal(t, []uint64{2}, report.Nodes[1].RootDeposits)
	require.Empty(t, report.Nodes[1].FundingRootDeposits)
	require.Equal(t, []uint64{1, 2}, report.Nodes[2].RootDeposits)
	require.Equal(t, []uint64{1}, report.Nodes[2].FundingRootDeposits)
	require.Equal(t, []uint64{1, 2}, report.Nodes[3].RootDeposits)
	require.Empty(t, report.Nodes[3].FundingRootDeposits)
	require.Equal(t, []uint64{1, 2}, report.Withdrawals[0].RootDeposits)
	require.Equal(t, []uint64{1}, report.Withdrawals[0].FundingRootDeposits)
	require.Len(t, report.Unspent, 1)
	require.Empty(t, report.Unspent[0].FundingRootDeposits)

	t.Run("missing zero deposit remains incomplete", func(t *testing.T) {
		_, err := BuildLineageReport([]DecryptedAuditRecord{positive, transfer, withdraw})
		require.ErrorContains(t, err, "AUDIT_INCOMPLETE: Cin has no earlier unspent C")
	})

	t.Run("derived zero does not transfer funding ancestry", func(t *testing.T) {
		second := testDecryptedRecord(4, auditfield.KindDeposit, nil, []AuditNote{{Commitment: field(41), Asset: asset, Amount: 4}}, nil, &TransparentAuditEffect{Kind: auditfield.KindDeposit, From: []byte{4}, Amount: 4})
		merge := testDecryptedRecord(5, auditfield.KindTransfer2x2, []auditfield.Field32{field(41), field(22)},
			[]AuditNote{{Commitment: field(51), Asset: asset, Amount: 4}, {Commitment: field(52), Asset: asset}}, []auditfield.Field32{field(61), field(62)}, nil)
		report, err := BuildLineageReport([]DecryptedAuditRecord{positive, zero, transfer, second, merge})
		require.NoError(t, err)
		require.Len(t, report.Nodes, 7)
		require.Equal(t, []uint64{1, 2, 4}, report.Nodes[5].RootDeposits)
		require.Equal(t, []uint64{4}, report.Nodes[5].FundingRootDeposits)
		require.Equal(t, []uint64{1, 2, 4}, report.Nodes[6].RootDeposits)
		require.Empty(t, report.Nodes[6].FundingRootDeposits)
	})
}
