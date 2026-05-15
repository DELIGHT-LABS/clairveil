package withdraw

import (
	"bytes"
	"fmt"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/backend/witness"
	"github.com/consensys/gnark/constraint"
	"github.com/consensys/gnark/frontend"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/circuit"
)

type SpendArtifactProvider interface {
	SpendR1CS() (constraint.ConstraintSystem, error)
	SpendProvingKey() (groth16.ProvingKey, error)
}

type SpendProofRunner interface {
	ProveSpend(r1cs constraint.ConstraintSystem, provingKey groth16.ProvingKey, witness witness.Witness) (groth16.Proof, error)
}

func ProveSpendWithdrawAssignment(
	assignment *circuit.SpendCircuit,
	artifacts SpendArtifactProvider,
	runner SpendProofRunner,
) ([]byte, error) {
	if assignment == nil {
		return nil, fmt.Errorf("a spend assignment is required")
	}
	if artifacts == nil {
		return nil, fmt.Errorf("a spend artifact provider is required")
	}
	if runner == nil {
		return nil, fmt.Errorf("a spend proof runner is required")
	}

	spendWitness, err := frontend.NewWitness(assignment, ecc.BN254.ScalarField())
	if err != nil {
		return nil, fmt.Errorf("failed to build the spend witness: %w", err)
	}

	spendR1CS, err := artifacts.SpendR1CS()
	if err != nil {
		return nil, fmt.Errorf("failed to load the spend r1cs: %w", err)
	}

	spendProvingKey, err := artifacts.SpendProvingKey()
	if err != nil {
		return nil, fmt.Errorf("failed to load the spend proving key: %w", err)
	}

	proof, err := runner.ProveSpend(spendR1CS, spendProvingKey, spendWitness)
	if err != nil {
		return nil, fmt.Errorf("spend proof generation failed: %w", err)
	}

	var proofBuffer bytes.Buffer
	if _, err := proof.WriteTo(&proofBuffer); err != nil {
		return nil, fmt.Errorf("failed to serialize the spend proof: %w", err)
	}

	return proofBuffer.Bytes(), nil
}
