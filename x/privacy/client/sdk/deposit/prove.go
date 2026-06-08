package deposit

import (
	"bytes"
	"fmt"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/backend/witness"
	"github.com/consensys/gnark/constraint"
	"github.com/consensys/gnark/frontend"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/circuit"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

type DepositArtifactProvider interface {
	DepositR1CS() (constraint.ConstraintSystem, error)
	DepositProvingKey() (groth16.ProvingKey, error)
}

type DepositProofRunner interface {
	ProveDeposit(r1cs constraint.ConstraintSystem, provingKey groth16.ProvingKey, witness witness.Witness) (groth16.Proof, error)
}

func BuildDepositAssignment(note privacytypes.Note) (*circuit.DepositCircuit, error) {
	if err := privacytypes.ValidateShieldedAmount("deposit note amount", note.Amount); err != nil {
		return nil, err
	}
	if note.ReceiverSpendPubKeyX == nil || note.ReceiverSpendPubKeyY == nil {
		return nil, fmt.Errorf("deposit note spend public key is required")
	}
	if note.ReceiverViewPubKeyX == nil || note.ReceiverViewPubKeyY == nil {
		return nil, fmt.Errorf("deposit note view public key is required")
	}
	if note.AssetID == nil {
		return nil, fmt.Errorf("deposit note asset id is required")
	}
	if note.Randomness == nil {
		return nil, fmt.Errorf("deposit note randomness is required")
	}

	assignment := &circuit.DepositCircuit{
		Commitment: note.ComputeCommitment(),
		Amount:     note.Amount,
		AssetID:    note.AssetID,
		Randomness: note.Randomness,
	}
	assignment.ReceiverSpendPubKey.X = note.ReceiverSpendPubKeyX
	assignment.ReceiverSpendPubKey.Y = note.ReceiverSpendPubKeyY
	assignment.ReceiverViewPubKey.X = note.ReceiverViewPubKeyX
	assignment.ReceiverViewPubKey.Y = note.ReceiverViewPubKeyY
	return assignment, nil
}

func ProveDepositAssignment(
	assignment *circuit.DepositCircuit,
	artifacts DepositArtifactProvider,
	runner DepositProofRunner,
) ([]byte, error) {
	if assignment == nil {
		return nil, fmt.Errorf("a deposit assignment is required")
	}
	if artifacts == nil {
		return nil, fmt.Errorf("a deposit artifact provider is required")
	}
	if runner == nil {
		return nil, fmt.Errorf("a deposit proof runner is required")
	}

	depositWitness, err := frontend.NewWitness(assignment, ecc.BN254.ScalarField())
	if err != nil {
		return nil, fmt.Errorf("failed to build the deposit witness: %w", err)
	}

	depositR1CS, err := artifacts.DepositR1CS()
	if err != nil {
		return nil, fmt.Errorf("failed to load the deposit r1cs: %w", err)
	}

	depositProvingKey, err := artifacts.DepositProvingKey()
	if err != nil {
		return nil, fmt.Errorf("failed to load the deposit proving key: %w", err)
	}

	proof, err := runner.ProveDeposit(depositR1CS, depositProvingKey, depositWitness)
	if err != nil {
		return nil, fmt.Errorf("deposit proof generation failed: %w", err)
	}

	var proofBuffer bytes.Buffer
	if _, err := proof.WriteTo(&proofBuffer); err != nil {
		return nil, fmt.Errorf("failed to serialize the deposit proof: %w", err)
	}

	return proofBuffer.Bytes(), nil
}

func BuildDepositProof(
	note privacytypes.Note,
	artifacts DepositArtifactProvider,
	runner DepositProofRunner,
) ([]byte, error) {
	assignment, err := BuildDepositAssignment(note)
	if err != nil {
		return nil, err
	}
	return ProveDepositAssignment(assignment, artifacts, runner)
}
