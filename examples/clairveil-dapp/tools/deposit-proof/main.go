package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"

	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/backend/witness"
	"github.com/consensys/gnark/constraint"

	privacydeposit "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/deposit"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	privacyzk "github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
)

type request struct {
	NoteJSON          string `json:"note_json"`
	NoteCommitmentHex string `json:"note_commitment_hex"`
}

type response struct {
	Version           string `json:"version"`
	ProofHex          string `json:"proof_hex"`
	NoteCommitmentHex string `json:"note_commitment_hex"`
}

type depositArtifacts struct{}

func (depositArtifacts) DepositR1CS() (constraint.ConstraintSystem, error) {
	return privacyzk.GetDepositR1CS()
}

func (depositArtifacts) DepositProvingKey() (groth16.ProvingKey, error) {
	return privacyzk.GetDepositProvingKey()
}

type depositRunner struct{}

func (depositRunner) ProveDeposit(r1cs constraint.ConstraintSystem, provingKey groth16.ProvingKey, depositWitness witness.Witness) (groth16.Proof, error) {
	return groth16.Prove(r1cs, provingKey, depositWitness)
}

func main() {
	var req request
	if err := json.NewDecoder(os.Stdin).Decode(&req); err != nil {
		fatalf("failed to decode request: %v", err)
	}
	out, err := buildDepositProof(req)
	if err != nil {
		fatalf("%v", err)
	}
	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		fatalf("failed to encode response: %v", err)
	}
}

func buildDepositProof(req request) (response, error) {
	if req.NoteJSON == "" {
		return response{}, fmt.Errorf("note_json is required")
	}
	var note privacytypes.Note
	if err := json.Unmarshal([]byte(req.NoteJSON), &note); err != nil {
		return response{}, fmt.Errorf("invalid note_json: %w", err)
	}
	commitment := note.ComputeCommitment()
	commitmentBytes := make([]byte, 32)
	commitment.FillBytes(commitmentBytes)
	commitmentHex := hex.EncodeToString(commitmentBytes)

	if req.NoteCommitmentHex != "" {
		expected, err := hex.DecodeString(req.NoteCommitmentHex)
		if err != nil {
			return response{}, fmt.Errorf("invalid note_commitment_hex: %w", err)
		}
		if !bytes.Equal(expected, commitmentBytes) {
			return response{}, fmt.Errorf("note commitment mismatch: expected %s, got %s", req.NoteCommitmentHex, commitmentHex)
		}
	}

	proof, err := privacydeposit.BuildDepositProof(note, depositArtifacts{}, depositRunner{})
	if err != nil {
		return response{}, err
	}
	return response{
		Version:           "v1",
		ProofHex:          hex.EncodeToString(proof),
		NoteCommitmentHex: commitmentHex,
	}, nil
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
