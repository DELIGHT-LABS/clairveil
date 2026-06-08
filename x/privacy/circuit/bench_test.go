package circuit

import (
	"math/big"
	"testing"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	"github.com/stretchr/testify/require"
)

func BenchmarkDepositCircuitProve(b *testing.B) {
	assignment := buildValidDepositAssignment(b, big.NewInt(7), big.NewInt(11))
	ccs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &DepositCircuit{})
	require.NoError(b, err)
	pk, _, err := groth16.Setup(ccs)
	require.NoError(b, err)
	witness, err := frontend.NewWitness(assignment, ecc.BN254.ScalarField())
	require.NoError(b, err)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := groth16.Prove(ccs, pk, witness)
		require.NoError(b, err)
	}
}

func BenchmarkSpendCircuitProve(b *testing.B) {
	assignment := buildValidSpendAssignment(b, big.NewInt(424242))
	ccs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &SpendCircuit{})
	require.NoError(b, err)
	pk, _, err := groth16.Setup(ccs)
	require.NoError(b, err)
	witness, err := frontend.NewWitness(assignment, ecc.BN254.ScalarField())
	require.NoError(b, err)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := groth16.Prove(ccs, pk, witness)
		require.NoError(b, err)
	}
}

func BenchmarkJoinSplitCircuitProve(b *testing.B) {
	assignment := buildValidJoinSplitAssignment(b)
	ccs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &JoinSplitCircuit{})
	require.NoError(b, err)
	pk, _, err := groth16.Setup(ccs)
	require.NoError(b, err)
	witness, err := frontend.NewWitness(assignment, ecc.BN254.ScalarField())
	require.NoError(b, err)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := groth16.Prove(ccs, pk, witness)
		require.NoError(b, err)
	}
}
