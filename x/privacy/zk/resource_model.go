package zk

import (
	"fmt"
	"math/bits"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

const (
	BatchResourceMaxInputsV1  = uint64(privacytypes.BatchJoinSplitV1MaxInputs)
	BatchResourceMaxOutputsV1 = uint64(privacytypes.BatchJoinSplitV1MaxOutputs)
)

// BatchGasModelV1 defines the explicit batch resource formula. Concrete
// consensus coefficients are supplied by governance and keeper configuration;
// zero coefficients are rejected so no represented work category is silently
// unmetered.
type BatchGasModelV1 struct {
	VerifyBase              uint64
	PerInput                uint64
	PerOutput               uint64
	PerCanonicalPayloadByte uint64
	PerTypedScanStateByte   uint64
	PerTreeWrite            uint64
	PerGlobalLookup         uint64
}

type BatchResourceBoundsV1 struct {
	MaxCanonicalPayloadBytes uint64
	MaxTypedScanStateBytes   uint64
	MaxTreeNodeWrites        uint64
	MaxGlobalLookups         uint64
}

type BatchResourceUsageV1 struct {
	InputCount            uint64
	OutputCount           uint64
	CanonicalPayloadBytes uint64
	TypedScanStateBytes   uint64
	TreeNodeWrites        uint64
	GlobalLookups         uint64
}

type BatchGasBreakdownV1 struct {
	Verification     uint64
	Inputs           uint64
	Outputs          uint64
	CanonicalPayload uint64
	TypedScanState   uint64
	TreeWrites       uint64
	GlobalLookups    uint64
	Total            uint64
}

func ComputeBatchGasV1(model BatchGasModelV1, bounds BatchResourceBoundsV1, usage BatchResourceUsageV1) (BatchGasBreakdownV1, error) {
	if err := validateBatchGasModelV1(model); err != nil {
		return BatchGasBreakdownV1{}, err
	}
	if err := validateBatchResourceUsageV1(bounds, usage); err != nil {
		return BatchGasBreakdownV1{}, err
	}

	breakdown := BatchGasBreakdownV1{Verification: model.VerifyBase}
	var err error
	if breakdown.Inputs, err = checkedGasProduct("inputs", model.PerInput, usage.InputCount); err != nil {
		return BatchGasBreakdownV1{}, err
	}
	if breakdown.Outputs, err = checkedGasProduct("outputs", model.PerOutput, usage.OutputCount); err != nil {
		return BatchGasBreakdownV1{}, err
	}
	if breakdown.CanonicalPayload, err = checkedGasProduct("canonical payload bytes", model.PerCanonicalPayloadByte, usage.CanonicalPayloadBytes); err != nil {
		return BatchGasBreakdownV1{}, err
	}
	if breakdown.TypedScanState, err = checkedGasProduct("typed scan state bytes", model.PerTypedScanStateByte, usage.TypedScanStateBytes); err != nil {
		return BatchGasBreakdownV1{}, err
	}
	if breakdown.TreeWrites, err = checkedGasProduct("tree writes", model.PerTreeWrite, usage.TreeNodeWrites); err != nil {
		return BatchGasBreakdownV1{}, err
	}
	if breakdown.GlobalLookups, err = checkedGasProduct("global lookups", model.PerGlobalLookup, usage.GlobalLookups); err != nil {
		return BatchGasBreakdownV1{}, err
	}

	components := []uint64{
		breakdown.Verification,
		breakdown.Inputs,
		breakdown.Outputs,
		breakdown.CanonicalPayload,
		breakdown.TypedScanState,
		breakdown.TreeWrites,
		breakdown.GlobalLookups,
	}
	for _, component := range components {
		total, carry := bits.Add64(breakdown.Total, component, 0)
		if carry != 0 {
			return BatchGasBreakdownV1{}, fmt.Errorf("batch gas total overflows uint64")
		}
		breakdown.Total = total
	}
	return breakdown, nil
}

func validateBatchGasModelV1(model BatchGasModelV1) error {
	for _, coefficient := range []struct {
		name  string
		value uint64
	}{
		{"verify base", model.VerifyBase},
		{"per input", model.PerInput},
		{"per output", model.PerOutput},
		{"per canonical payload byte", model.PerCanonicalPayloadByte},
		{"per typed scan state byte", model.PerTypedScanStateByte},
		{"per tree write", model.PerTreeWrite},
		{"per global lookup", model.PerGlobalLookup},
	} {
		if coefficient.value == 0 {
			return fmt.Errorf("batch gas coefficient %s must be positive", coefficient.name)
		}
	}
	return nil
}

func validateBatchResourceUsageV1(bounds BatchResourceBoundsV1, usage BatchResourceUsageV1) error {
	if usage.InputCount == 0 || usage.InputCount > BatchResourceMaxInputsV1 {
		return fmt.Errorf("batch input count must be in [1,%d]", BatchResourceMaxInputsV1)
	}
	if usage.OutputCount == 0 || usage.OutputCount > BatchResourceMaxOutputsV1 {
		return fmt.Errorf("batch output count must be in [1,%d]", BatchResourceMaxOutputsV1)
	}
	if usage.CanonicalPayloadBytes == 0 {
		return fmt.Errorf("batch canonical payload bytes must be positive")
	}
	if usage.TypedScanStateBytes == 0 {
		return fmt.Errorf("batch typed scan state bytes must be positive")
	}
	if usage.TreeNodeWrites < usage.OutputCount {
		return fmt.Errorf("batch tree node writes %d cannot be smaller than output count %d", usage.TreeNodeWrites, usage.OutputCount)
	}
	minimumGlobalLookups, carry := bits.Add64(usage.InputCount, usage.OutputCount, 0)
	if carry != 0 || usage.GlobalLookups < minimumGlobalLookups {
		return fmt.Errorf("batch global lookups %d cannot be smaller than input plus output count %d", usage.GlobalLookups, minimumGlobalLookups)
	}
	for _, bounded := range []struct {
		name  string
		value uint64
		max   uint64
	}{
		{"canonical payload bytes", usage.CanonicalPayloadBytes, bounds.MaxCanonicalPayloadBytes},
		{"typed scan state bytes", usage.TypedScanStateBytes, bounds.MaxTypedScanStateBytes},
		{"tree node writes", usage.TreeNodeWrites, bounds.MaxTreeNodeWrites},
		{"global lookups", usage.GlobalLookups, bounds.MaxGlobalLookups},
	} {
		if bounded.max == 0 {
			return fmt.Errorf("batch resource bound %s must be positive", bounded.name)
		}
		if bounded.value > bounded.max {
			return fmt.Errorf("batch %s %d exceeds bound %d", bounded.name, bounded.value, bounded.max)
		}
	}
	return nil
}

func checkedGasProduct(name string, unit, count uint64) (uint64, error) {
	high, low := bits.Mul64(unit, count)
	if high != 0 {
		return 0, fmt.Errorf("batch gas component %s overflows uint64", name)
	}
	return low, nil
}
