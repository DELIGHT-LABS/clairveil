package circuit

import (
	"testing"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	"github.com/stretchr/testify/require"
)

// BenchmarkBatchJoinSplit16x32Compile measures only production R1CS compile.
// It intentionally performs no Groth16 setup; the opt-in Session 2 resource
// gate remains the single setup/prove artifact-size benchmark.
func BenchmarkBatchJoinSplit16x32Compile(b *testing.B) {
	for i := 0; i < b.N; i++ {
		ccs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &BatchJoinSplit16x32{})
		require.NoError(b, err)
		b.ReportMetric(float64(ccs.GetNbConstraints()), "constraints")
	}
}

// BenchmarkBatchJoinSplit16x32Solve reuses one compiled production CCS and
// one witness per shape so it reports constraint solving rather than fixture,
// compile, or trusted-setup work.
func BenchmarkBatchJoinSplit16x32Solve(b *testing.B) {
	ccs := compiledBatchProductionCCS(b)
	for _, shape := range []struct {
		name        string
		inputCount  int
		outputCount int
	}{
		{name: "1x1", inputCount: 1, outputCount: 1},
		{name: "3x4", inputCount: 3, outputCount: 4},
		{name: "8x16", inputCount: 8, outputCount: 16},
		{name: "16x31", inputCount: 16, outputCount: 31},
		{name: "16x32", inputCount: 16, outputCount: 32},
	} {
		b.Run(shape.name, func(b *testing.B) {
			assignment := buildBatchFeasibilityAssignment(b, shape.inputCount, shape.outputCount)
			witness, err := frontend.NewWitness(assignment, ecc.BN254.ScalarField())
			require.NoError(b, err)
			b.ReportMetric(float64(ccs.GetNbConstraints()), "constraints")
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, err := ccs.Solve(witness)
				require.NoError(b, err)
			}
		})
	}
}
