package circuit

import (
	"io"
	"math/big"
	"testing"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/backend/witness"
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

func BenchmarkDepositCircuitPublicWitness(b *testing.B) {
	assignment := buildValidDepositAssignment(b, big.NewInt(7), big.NewInt(11))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := frontend.NewWitness(assignment, ecc.BN254.ScalarField(), frontend.PublicOnly())
		require.NoError(b, err)
	}
}

func BenchmarkDepositCircuitVerify(b *testing.B) {
	assignment := buildValidDepositAssignment(b, big.NewInt(7), big.NewInt(11))
	proof, vk, publicWitness := buildBenchmarkProof(b, &DepositCircuit{}, assignment)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		require.NoError(b, groth16.Verify(proof, vk, publicWitness))
	}
}

func BenchmarkDepositCircuitCompile(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &DepositCircuit{})
		require.NoError(b, err)
	}
}

func BenchmarkDepositCircuitSetup(b *testing.B) {
	ccs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &DepositCircuit{})
	require.NoError(b, err)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pk, vk, err := groth16.Setup(ccs)
		require.NoError(b, err)
		_, _ = pk, vk
	}
}

func BenchmarkDepositCircuitArtifactWrite(b *testing.B) {
	ccs, pk, vk := buildBenchmarkArtifacts(b, &DepositCircuit{})
	objects := []interface {
		WriteTo(io.Writer) (int64, error)
	}{ccs, pk, vk}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, object := range objects {
			_, err := object.WriteTo(io.Discard)
			require.NoError(b, err)
		}
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

func BenchmarkSpendCircuitPublicWitness(b *testing.B) {
	assignment := buildValidSpendAssignment(b, big.NewInt(424242))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := frontend.NewWitness(assignment, ecc.BN254.ScalarField(), frontend.PublicOnly())
		require.NoError(b, err)
	}
}

func BenchmarkSpendCircuitVerify(b *testing.B) {
	assignment := buildValidSpendAssignment(b, big.NewInt(424242))
	proof, vk, publicWitness := buildBenchmarkProof(b, &SpendCircuit{}, assignment)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		require.NoError(b, groth16.Verify(proof, vk, publicWitness))
	}
}

func BenchmarkSpendCircuitCompile(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &SpendCircuit{})
		require.NoError(b, err)
	}
}

func BenchmarkSpendCircuitSetup(b *testing.B) {
	ccs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &SpendCircuit{})
	require.NoError(b, err)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pk, vk, err := groth16.Setup(ccs)
		require.NoError(b, err)
		_, _ = pk, vk
	}
}

func BenchmarkSpendCircuitArtifactWrite(b *testing.B) {
	ccs, pk, vk := buildBenchmarkArtifacts(b, &SpendCircuit{})
	objects := []interface {
		WriteTo(io.Writer) (int64, error)
	}{ccs, pk, vk}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, object := range objects {
			_, err := object.WriteTo(io.Discard)
			require.NoError(b, err)
		}
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

func BenchmarkJoinSplitCircuitPublicWitness(b *testing.B) {
	assignment := buildValidJoinSplitAssignment(b)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := frontend.NewWitness(assignment, ecc.BN254.ScalarField(), frontend.PublicOnly())
		require.NoError(b, err)
	}
}

func BenchmarkJoinSplitCircuitVerify(b *testing.B) {
	assignment := buildValidJoinSplitAssignment(b)
	proof, vk, publicWitness := buildBenchmarkProof(b, &JoinSplitCircuit{}, assignment)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		require.NoError(b, groth16.Verify(proof, vk, publicWitness))
	}
}

func BenchmarkJoinSplitCircuitCompile(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &JoinSplitCircuit{})
		require.NoError(b, err)
	}
}

func BenchmarkJoinSplitCircuitSetup(b *testing.B) {
	ccs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &JoinSplitCircuit{})
	require.NoError(b, err)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pk, vk, err := groth16.Setup(ccs)
		require.NoError(b, err)
		_, _ = pk, vk
	}
}

func BenchmarkJoinSplitCircuitArtifactWrite(b *testing.B) {
	ccs, pk, vk := buildBenchmarkArtifacts(b, &JoinSplitCircuit{})
	objects := []interface {
		WriteTo(io.Writer) (int64, error)
	}{ccs, pk, vk}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, object := range objects {
			_, err := object.WriteTo(io.Discard)
			require.NoError(b, err)
		}
	}
}

func buildBenchmarkProof(
	b testing.TB,
	circuit frontend.Circuit,
	assignment frontend.Circuit,
) (groth16.Proof, groth16.VerifyingKey, witness.Witness) {
	b.Helper()

	ccs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, circuit)
	require.NoError(b, err)
	pk, vk, err := groth16.Setup(ccs)
	require.NoError(b, err)
	witness, err := frontend.NewWitness(assignment, ecc.BN254.ScalarField())
	require.NoError(b, err)
	proof, err := groth16.Prove(ccs, pk, witness)
	require.NoError(b, err)
	publicWitness, err := frontend.NewWitness(assignment, ecc.BN254.ScalarField(), frontend.PublicOnly())
	require.NoError(b, err)
	return proof, vk, publicWitness
}

func buildBenchmarkArtifacts(
	b testing.TB,
	circuit frontend.Circuit,
) (interface {
	WriteTo(io.Writer) (int64, error)
}, groth16.ProvingKey, groth16.VerifyingKey) {
	b.Helper()

	ccs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, circuit)
	require.NoError(b, err)
	pk, vk, err := groth16.Setup(ccs)
	require.NoError(b, err)
	return ccs, pk, vk
}
