package zk

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
)

const CanonicalBN254Groth16ProofSize = 164

var compressedProofPointOffsets = [...]int{0, 32, 96, 132}

func ValidateCanonicalProofFramingBN254(proofBytes []byte) error {
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
