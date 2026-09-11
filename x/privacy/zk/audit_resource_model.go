package zk

import (
	"fmt"
	"math/bits"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/auditfield"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

// AuditGasModelV1 is the fixed initial suite-2 policy. It is independent of
// measured wall time. Keeper charging and actual message sizing belong to P4.
type AuditGasModelV1 struct {
	BatchGasModelV1
	PerPointValidation uint64
	PerRootPermutation uint64
}

func DefaultAuditGasModelV1() AuditGasModelV1 {
	return AuditGasModelV1{BatchGasModelV1: BatchGasModelV1{VerifyBase: 1_000_000, PerInput: 25_000, PerOutput: 50_000, PerCanonicalPayloadByte: 4, PerTypedScanStateByte: 8, PerTreeWrite: 5_000, PerGlobalLookup: 10_000}, PerPointValidation: 200_000, PerRootPermutation: 25_000}
}

type AuditResourceUsageV1 struct {
	Kind                    auditfield.Kind
	InputCount, OutputCount uint64
	AuxBytes                uint64
	// Includes maximal typed privacy-scan projection encodings. The
	// message/state adapter must calculate this upper bound, not trust a tx.
	ProjectedStateBytes uint64
}
type AuditResourceBoundsV1 struct {
	MaxCanonicalAuxEnvelopeBytes uint64
	MaxProjectedStateBytes       uint64
}
type AuditGasBreakdownV1 struct {
	BatchGasBreakdownV1
	PointValidation, RootPermutations uint64
}

func ComputeAuditGasV1(model AuditGasModelV1, bounds AuditResourceBoundsV1, usage AuditResourceUsageV1) (AuditGasBreakdownV1, error) {
	fail := func(err error) (AuditGasBreakdownV1, error) { return AuditGasBreakdownV1{}, err }
	if err := validateBatchGasModelV1(model.BatchGasModelV1); err != nil {
		return fail(err)
	}
	if model.PerPointValidation == 0 || model.PerRootPermutation == 0 {
		return fail(fmt.Errorf("audit gas coefficients must be positive"))
	}
	if usage.InputCount > 16 || usage.OutputCount > 32 {
		return fail(fmt.Errorf("audit capacity exceeded"))
	}
	if err := usage.Kind.ValidateActiveCounts(uint8(usage.InputCount), uint8(usage.OutputCount)); err != nil {
		return fail(err)
	}
	envelopeSize, err := usage.Kind.EnvelopeSize()
	if err != nil {
		return fail(err)
	}
	n, err := usage.Kind.PlaintextFieldCount()
	if err != nil {
		return fail(err)
	}
	canonical, carry := bits.Add64(usage.AuxBytes, uint64(envelopeSize), 0)
	if carry != 0 {
		return fail(fmt.Errorf("audit canonical bytes overflow"))
	}
	if usage.AuxBytes < 4 {
		return fail(fmt.Errorf("audit Aux prefix is required"))
	}
	if bounds.MaxCanonicalAuxEnvelopeBytes == 0 || bounds.MaxCanonicalAuxEnvelopeBytes > privacytypes.MaxBatchTransferMessageBytesV1 || canonical > bounds.MaxCanonicalAuxEnvelopeBytes {
		return fail(fmt.Errorf("audit canonical bytes exceed bounded message capacity"))
	}
	if bounds.MaxProjectedStateBytes == 0 || usage.ProjectedStateBytes > bounds.MaxProjectedStateBytes {
		return fail(fmt.Errorf("audit projected scan state bytes exceed bounds"))
	}
	// Counts are already bounded; all multiplications/additions below still use
	// the common checked gas arithmetic, including the two new cost categories.
	base, err := computeGasComponentsV1(model.BatchGasModelV1, BatchResourceUsageV1{InputCount: usage.InputCount, OutputCount: usage.OutputCount, CanonicalPayloadBytes: canonical, TypedScanStateBytes: usage.ProjectedStateBytes, TreeNodeWrites: usage.OutputCount * 33, GlobalLookups: usage.InputCount + usage.OutputCount})
	if err != nil {
		return fail(err)
	}
	points, err := checkedGasProduct("audit point validation", model.PerPointValidation, 2)
	if err != nil {
		return fail(err)
	}
	root, err := checkedGasProduct("audit root permutations", model.PerRootPermutation, uint64(25+n+4))
	if err != nil {
		return fail(err)
	}
	for _, cost := range []uint64{points, root} {
		total, carry := bits.Add64(base.Total, cost, 0)
		if carry != 0 {
			return fail(fmt.Errorf("audit gas total overflows uint64"))
		}
		base.Total = total
	}
	return AuditGasBreakdownV1{BatchGasBreakdownV1: base, PointValidation: points, RootPermutations: root}, nil
}
