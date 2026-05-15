package keeper

import (
	"bytes"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/backend/witness"
	"github.com/consensys/gnark/frontend"
)

func newPublicWitnessBN254(assignment frontend.Circuit) (witness.Witness, error) {
	return frontend.NewWitness(assignment, ecc.BN254.ScalarField(), frontend.PublicOnly())
}

func readProofBN254(proofBytes []byte) (groth16.Proof, error) {
	proof := groth16.NewProof(ecc.BN254)
	if _, err := proof.ReadFrom(bytes.NewReader(proofBytes)); err != nil {
		return nil, err
	}

	return proof, nil
}
