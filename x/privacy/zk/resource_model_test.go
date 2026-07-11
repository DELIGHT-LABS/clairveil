package zk

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestComputeBatchGasV1ExactBreakdown(t *testing.T) {
	model := BatchGasModelV1{
		VerifyBase:              1000,
		PerInput:                10,
		PerOutput:               20,
		PerCanonicalPayloadByte: 2,
		PerTypedScanStateByte:   3,
		PerTreeWrite:            4,
		PerGlobalLookup:         5,
	}
	bounds := BatchResourceBoundsV1{
		MaxCanonicalPayloadBytes: 10_000,
		MaxTypedScanStateBytes:   20_000,
		MaxTreeNodeWrites:        2_000,
		MaxGlobalLookups:         48,
	}
	usage := BatchResourceUsageV1{
		InputCount:            16,
		OutputCount:           32,
		CanonicalPayloadBytes: 100,
		TypedScanStateBytes:   200,
		TreeNodeWrites:        50,
		GlobalLookups:         48,
	}

	breakdown, err := ComputeBatchGasV1(model, bounds, usage)
	require.NoError(t, err)
	require.Equal(t, uint64(1000), breakdown.Verification)
	require.Equal(t, uint64(160), breakdown.Inputs)
	require.Equal(t, uint64(640), breakdown.Outputs)
	require.Equal(t, uint64(200), breakdown.CanonicalPayload)
	require.Equal(t, uint64(600), breakdown.TypedScanState)
	require.Equal(t, uint64(200), breakdown.TreeWrites)
	require.Equal(t, uint64(240), breakdown.GlobalLookups)
	require.Equal(t, uint64(3040), breakdown.Total)
}

func TestComputeBatchGasV1RejectsUnmeteredOrOutOfBoundsShape(t *testing.T) {
	model := BatchGasModelV1{1, 1, 1, 1, 1, 1, 1}
	bounds := BatchResourceBoundsV1{100, 100, 100, 48}
	usage := BatchResourceUsageV1{16, 32, 100, 100, 100, 48}

	zeroCoefficient := model
	zeroCoefficient.PerTreeWrite = 0
	_, err := ComputeBatchGasV1(zeroCoefficient, bounds, usage)
	require.ErrorContains(t, err, "per tree write must be positive")

	tooManyInputs := usage
	tooManyInputs.InputCount = 17
	_, err = ComputeBatchGasV1(model, bounds, tooManyInputs)
	require.ErrorContains(t, err, "input count")

	tooMuchState := usage
	tooMuchState.TypedScanStateBytes = 101
	_, err = ComputeBatchGasV1(model, bounds, tooMuchState)
	require.ErrorContains(t, err, "typed scan state bytes 101 exceeds bound 100")

	underreported := usage
	underreported.GlobalLookups = 47
	_, err = ComputeBatchGasV1(model, bounds, underreported)
	require.ErrorContains(t, err, "cannot be smaller than input plus output count")
}

func TestComputeBatchGasV1RejectsOverflow(t *testing.T) {
	model := BatchGasModelV1{1, math.MaxUint64, 1, 1, 1, 1, 1}
	bounds := BatchResourceBoundsV1{1, 1, 1, 3}
	usage := BatchResourceUsageV1{2, 1, 1, 1, 1, 3}
	_, err := ComputeBatchGasV1(model, bounds, usage)
	require.ErrorContains(t, err, "inputs overflows")

	model = BatchGasModelV1{math.MaxUint64, 1, 1, 1, 1, 1, 1}
	usage = BatchResourceUsageV1{1, 1, 1, 1, 1, 2}
	_, err = ComputeBatchGasV1(model, bounds, usage)
	require.ErrorContains(t, err, "total overflows")
}
