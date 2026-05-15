package transfer

import (
	"bytes"
	"fmt"

	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/backend/witness"
	"github.com/consensys/gnark/constraint"
	"github.com/consensys/gnark/frontend"

	"github.com/consensys/gnark-crypto/ecc"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/circuit"
)

type JoinSplitArtifactProvider interface {
	JoinSplitR1CS() (constraint.ConstraintSystem, error)
	JoinSplitProvingKey() (groth16.ProvingKey, error)
}

type JoinSplitProofRunner interface {
	ProveJoinSplit(r1cs constraint.ConstraintSystem, provingKey groth16.ProvingKey, witness witness.Witness) (groth16.Proof, error)
}

func ProveJoinSplitAssignment(
	assignment *circuit.JoinSplitCircuit,
	artifacts JoinSplitArtifactProvider,
	runner JoinSplitProofRunner,
) ([]byte, error) {
	if assignment == nil {
		return nil, fmt.Errorf("a joinsplit assignment is required")
	}
	if artifacts == nil {
		return nil, fmt.Errorf("a joinsplit artifact provider is required")
	}
	if runner == nil {
		return nil, fmt.Errorf("a joinsplit proof runner is required")
	}

	joinSplitWitness, err := frontend.NewWitness(assignment, ecc.BN254.ScalarField())
	if err != nil {
		return nil, fmt.Errorf("failed to build the joinsplit witness: %w", err)
	}

	joinSplitR1CS, err := artifacts.JoinSplitR1CS()
	if err != nil {
		return nil, fmt.Errorf("failed to load the joinsplit r1cs: %w", err)
	}

	joinSplitProvingKey, err := artifacts.JoinSplitProvingKey()
	if err != nil {
		return nil, fmt.Errorf("failed to load the joinsplit proving key: %w", err)
	}

	proof, err := runner.ProveJoinSplit(joinSplitR1CS, joinSplitProvingKey, joinSplitWitness)
	if err != nil {
		return nil, fmt.Errorf("joinsplit proof generation failed: %w", err)
	}

	var proofBuffer bytes.Buffer
	if _, err := proof.WriteTo(&proofBuffer); err != nil {
		return nil, fmt.Errorf("failed to serialize the joinsplit proof: %w", err)
	}

	return proofBuffer.Bytes(), nil
}
