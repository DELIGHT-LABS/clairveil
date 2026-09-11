package zk

import (
	"bytes"
	"encoding/binary"
	"fmt"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/backend/witness"
)

const CanonicalBN254Groth16ProofSize = 164
const MaxBN254Groth16ProofSize = 4 << 10

var compressedProofPointOffsets = [...]int{0, 32, 96, 132}

func ValidateCanonicalProofFramingBN254(proofBytes []byte) error {
	if len(proofBytes) > MaxBN254Groth16ProofSize {
		return fmt.Errorf("proof exceeds 4 KiB hard cap")
	}
	if len(proofBytes) != CanonicalBN254Groth16ProofSize {
		return fmt.Errorf("proof must be exactly %d bytes", CanonicalBN254Groth16ProofSize)
	}
	for _, offset := range compressedProofPointOffsets {
		if proofBytes[offset]&0xc0 == 0 {
			return fmt.Errorf("proof point at offset %d is not compressed", offset)
		}
	}
	if binary.BigEndian.Uint32(proofBytes[128:132]) != 0 {
		return fmt.Errorf("proof commitments are not supported")
	}
	return nil
}

func ReadCanonicalProofBN254(proofBytes []byte) (groth16.Proof, error) {
	if err := ValidateCanonicalProofFramingBN254(proofBytes); err != nil {
		return nil, err
	}

	proof := groth16.NewProof(ecc.BN254)
	read, err := proof.ReadFrom(bytes.NewReader(proofBytes))
	if err != nil {
		return nil, err
	}
	if read != int64(len(proofBytes)) {
		return nil, fmt.Errorf("proof decoder did not consume the full canonical frame")
	}

	var canonical bytes.Buffer
	if _, err := proof.WriteTo(&canonical); err != nil {
		return nil, err
	}
	if !bytes.Equal(canonical.Bytes(), proofBytes) {
		return nil, fmt.Errorf("proof is not canonically encoded")
	}

	return proof, nil
}

func ValidateCanonicalProofBN254(proofBytes []byte) error {
	_, err := ReadCanonicalProofBN254(proofBytes)
	return err
}

// VerifyProof selects only this registry's exact circuit and expected identity.
// It does not derive public inputs from chain messages or retry another key/set.
func (r *ArtifactRegistry) VerifyProof(circuitID CircuitID, encoded []byte, public witness.Witness, expected *privacytypes.CircuitSetIdentity) error {
	proof, err := ReadCanonicalProofBN254(encoded)
	if err != nil {
		return err
	}
	if public == nil {
		return fmt.Errorf("public witness is required")
	}
	fields, err := PublicInputSchema(string(circuitID))
	if err != nil {
		return err
	}
	vector, ok := public.Vector().(fr.Vector)
	if !ok || len(vector) != len(fields) {
		return fmt.Errorf("public witness does not match circuit schema")
	}
	if err := r.CheckReadiness(ArtifactRoleValidator, []CircuitID{circuitID}, expected); err != nil {
		return err
	}
	vk, err := r.VerifyingKey(circuitID)
	if err != nil {
		return err
	}
	return groth16.Verify(proof, vk, public)
}
