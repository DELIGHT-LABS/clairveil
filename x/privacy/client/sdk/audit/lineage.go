package audit

import (
	"bytes"
	"fmt"
	"sort"

	privacyamount "github.com/DELIGHT-LABS/clairveil/x/privacy/amount"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/auditfield"
)

// LineageNode is one C node in the auditor-only graph.  RootDeposits contains
// deposit transition sequences rather than a claimed unique human source:
// fungible merges may legitimately have more than one public funder.
type LineageNode struct {
	AuditNote
	CreatedSequence uint64
	CreatedHeight   uint64
	CreatedSlot     uint8
	RootDeposits    []uint64
	DepositFunder   []byte // set only on deposit roots
	SpentSequence   uint64
	SpentNullifier  auditfield.Field32
}

// LineageWithdrawal is a terminal public recipient effect.  It is never
// represented as a synthetic note or as a future nullifier prediction.
type LineageWithdrawal struct {
	Sequence        uint64
	Height          uint64
	InputCommitment auditfield.Field32
	Recipient       []byte
	Denom           string
	Amount          privacyamount.Amount128
	RootDeposits    []uint64
}

type LineageReport struct {
	TransitionCount uint64
	Nodes           []LineageNode
	Unspent         []LineageNode
	Withdrawals     []LineageWithdrawal
}

type mutableLineageNode struct {
	LineageNode
	spent bool
}

// BuildLineageReport builds C -> Cin -> N edges exclusively from decrypted,
// cryptographically verified records. The input must be the ordered result of
// a completely collected block range; reordering, duplicate C/N, a missing
// deposit root, or spend-before-create is an AUDIT_INCOMPLETE error rather
// than a zero balance.
func BuildLineageReport(records []DecryptedAuditRecord) (LineageReport, error) {
	if len(records) == 0 {
		return LineageReport{}, nil
	}
	nodes := map[auditfield.Field32]*mutableLineageNode{}
	usedNullifiers := map[auditfield.Field32]bool{}
	withdrawals := make([]LineageWithdrawal, 0)
	for index, decrypted := range records {
		if !decrypted.valid() {
			return LineageReport{}, fmt.Errorf("AUDIT_INCOMPLETE: record %d was not decrypted from a verified audit record", index+1)
		}
		record := decrypted.Record()
		if index > 0 && records[index-1].Record().Sequence() >= record.Sequence() {
			return LineageReport{}, fmt.Errorf("AUDIT_INCOMPLETE: audit transitions are not in strictly increasing order")
		}
		inputNodes, err := consumeInputs(record, decrypted, nodes, usedNullifiers)
		if err != nil {
			return LineageReport{}, err
		}
		switch record.Kind() {
		case auditfield.KindDeposit:
			if len(inputNodes) != 0 || len(decrypted.output) != 1 {
				return LineageReport{}, fmt.Errorf("AUDIT_INCOMPLETE: invalid deposit lineage shape")
			}
			effect, ok := record.TransparentEffect()
			if !ok || effect.Kind != auditfield.KindDeposit || decrypted.output[0].Amount != effect.Amount {
				return LineageReport{}, fmt.Errorf("AUDIT_INCOMPLETE: invalid deposit public effect")
			}
			if err := addOutputs(record, decrypted.output, []uint64{record.Sequence()}, effect.From, nodes); err != nil {
				return LineageReport{}, err
			}
		case auditfield.KindWithdraw:
			if len(inputNodes) != 1 || len(decrypted.output) != 0 {
				return LineageReport{}, fmt.Errorf("AUDIT_INCOMPLETE: invalid withdraw lineage shape")
			}
			effect, ok := record.TransparentEffect()
			if !ok || effect.Kind != auditfield.KindWithdraw || inputNodes[0].Asset != decrypted.asset || inputNodes[0].Amount != effect.Amount {
				return LineageReport{}, fmt.Errorf("AUDIT_INCOMPLETE: withdraw asset or amount mismatch")
			}
			withdrawals = append(withdrawals, LineageWithdrawal{Sequence: record.Sequence(), Height: record.Height(), InputCommitment: inputNodes[0].Commitment, Recipient: bytes.Clone(effect.To), Denom: effect.Denom, Amount: effect.Amount, RootDeposits: append([]uint64(nil), inputNodes[0].RootDeposits...)})
		case auditfield.KindTransfer2x2, auditfield.KindBatch16x32:
			if len(inputNodes) == 0 || len(inputNodes) != len(decrypted.inputs) || len(decrypted.output) == 0 {
				return LineageReport{}, fmt.Errorf("AUDIT_INCOMPLETE: invalid private lineage shape")
			}
			if err := validatePrivateConservation(decrypted.asset, inputNodes, decrypted.output); err != nil {
				return LineageReport{}, err
			}
			if err := addOutputs(record, decrypted.output, unionRoots(inputNodes), nil, nodes); err != nil {
				return LineageReport{}, err
			}
		default:
			return LineageReport{}, fmt.Errorf("AUDIT_INCOMPLETE: unknown audit transition kind")
		}
	}
	report := LineageReport{TransitionCount: uint64(len(records)), Nodes: make([]LineageNode, 0, len(nodes)), Withdrawals: withdrawals}
	for _, node := range nodes {
		value := cloneLineageNode(node.LineageNode)
		report.Nodes = append(report.Nodes, value)
		if !node.spent {
			report.Unspent = append(report.Unspent, value)
		}
	}
	sort.Slice(report.Nodes, func(i, j int) bool {
		if report.Nodes[i].CreatedSequence != report.Nodes[j].CreatedSequence {
			return report.Nodes[i].CreatedSequence < report.Nodes[j].CreatedSequence
		}
		return report.Nodes[i].CreatedSlot < report.Nodes[j].CreatedSlot
	})
	sort.Slice(report.Unspent, func(i, j int) bool {
		if report.Unspent[i].CreatedSequence != report.Unspent[j].CreatedSequence {
			return report.Unspent[i].CreatedSequence < report.Unspent[j].CreatedSequence
		}
		return report.Unspent[i].CreatedSlot < report.Unspent[j].CreatedSlot
	})
	return report, nil
}

func consumeInputs(record VerifiedAuditRecord, decrypted DecryptedAuditRecord, nodes map[auditfield.Field32]*mutableLineageNode, usedNullifiers map[auditfield.Field32]bool) ([]*mutableLineageNode, error) {
	commitments, nullifiers := decrypted.inputs, record.Inputs()
	if len(commitments) != len(nullifiers) {
		return nil, fmt.Errorf("AUDIT_INCOMPLETE: decrypted Cin/N input cardinality mismatch")
	}
	inputNodes := make([]*mutableLineageNode, len(commitments))
	for slot, commitment := range commitments {
		if commitment.IsZero() || nullifiers[slot].IsZero() || usedNullifiers[nullifiers[slot]] {
			return nil, fmt.Errorf("AUDIT_INCOMPLETE: duplicate or zero audit input")
		}
		node := nodes[commitment]
		if node == nil || node.spent || node.CreatedSequence >= record.Sequence() {
			return nil, fmt.Errorf("AUDIT_INCOMPLETE: Cin has no earlier unspent C")
		}
		usedNullifiers[nullifiers[slot]] = true
		node.spent, node.SpentSequence, node.SpentNullifier = true, record.Sequence(), nullifiers[slot]
		inputNodes[slot] = node
	}
	return inputNodes, nil
}

func validatePrivateConservation(asset auditfield.Field32, inputs []*mutableLineageNode, outputs []AuditNote) error {
	if asset.IsZero() || len(inputs) == 0 || len(outputs) == 0 {
		return fmt.Errorf("AUDIT_INCOMPLETE: empty private conservation relation")
	}
	var inputTotal, outputTotal privacyamount.Amount128
	var err error
	owner := inputs[0].AuditNote
	for _, input := range inputs {
		if input.Asset != asset || input.SpendX != owner.SpendX || input.SpendY != owner.SpendY || input.ViewX != owner.ViewX || input.ViewY != owner.ViewY {
			return fmt.Errorf("AUDIT_INCOMPLETE: private inputs have different asset or owner")
		}
		inputTotal, err = inputTotal.Add(input.Amount)
		if err != nil {
			return fmt.Errorf("AUDIT_INCOMPLETE: input total exceeds uint128")
		}
	}
	for _, output := range outputs {
		if output.Asset != asset {
			return fmt.Errorf("AUDIT_INCOMPLETE: private output has different asset")
		}
		outputTotal, err = outputTotal.Add(output.Amount)
		if err != nil {
			return fmt.Errorf("AUDIT_INCOMPLETE: output total exceeds uint128")
		}
	}
	if inputTotal.Cmp(outputTotal) != 0 {
		return fmt.Errorf("AUDIT_INCOMPLETE: private amounts do not conserve")
	}
	return nil
}

func addOutputs(record VerifiedAuditRecord, outputs []AuditNote, roots []uint64, funder []byte, nodes map[auditfield.Field32]*mutableLineageNode) error {
	recordOutputs := record.Outputs()
	if len(outputs) != len(recordOutputs) || len(roots) == 0 {
		return fmt.Errorf("AUDIT_INCOMPLETE: output/provenance cardinality mismatch")
	}
	for slot, output := range outputs {
		if output.Commitment.IsZero() || nodes[output.Commitment] != nil || output.Commitment != recordOutputs[slot].Commitment {
			return fmt.Errorf("AUDIT_INCOMPLETE: duplicate, zero, or mismatched output commitment")
		}
		nodes[output.Commitment] = &mutableLineageNode{LineageNode: LineageNode{AuditNote: output, CreatedSequence: record.Sequence(), CreatedHeight: record.Height(), CreatedSlot: uint8(slot), RootDeposits: append([]uint64(nil), roots...), DepositFunder: bytes.Clone(funder)}}
	}
	return nil
}

func unionRoots(inputs []*mutableLineageNode) []uint64 {
	seen := map[uint64]bool{}
	for _, input := range inputs {
		for _, root := range input.RootDeposits {
			seen[root] = true
		}
	}
	roots := make([]uint64, 0, len(seen))
	for root := range seen {
		roots = append(roots, root)
	}
	sort.Slice(roots, func(i, j int) bool { return roots[i] < roots[j] })
	return roots
}

func cloneLineageNode(node LineageNode) LineageNode {
	node.RootDeposits, node.DepositFunder = append([]uint64(nil), node.RootDeposits...), bytes.Clone(node.DepositFunder)
	return node
}
