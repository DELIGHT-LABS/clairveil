package keeper

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/backend/witness"
	"github.com/consensys/gnark/frontend"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	canonicalBN254Groth16ProofSize = 164

	DepositProofVerificationGas   uint64 = 1_000_000
	SpendProofVerificationGas     uint64 = 1_000_000
	JoinSplitProofVerificationGas uint64 = 1_000_000
)

var compressedProofPointOffsets = [...]int{0, 32, 96, 132}

func newPublicWitnessBN254(assignment frontend.Circuit) (witness.Witness, error) {
	return frontend.NewWitness(assignment, ecc.BN254.ScalarField(), frontend.PublicOnly())
}

func readProofBN254(proofBytes []byte) (groth16.Proof, error) {
	if err := validateCanonicalProofFramingBN254(proofBytes); err != nil {
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

func validateCanonicalProofFramingBN254(proofBytes []byte) error {
	if len(proofBytes) != canonicalBN254Groth16ProofSize {
		return fmt.Errorf("proof must be exactly %d bytes", canonicalBN254Groth16ProofSize)
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

func verifyProofBN254(
	ctx sdk.Context,
	proofBytes []byte,
	assignment frontend.Circuit,
	verificationGas uint64,
	gasDescriptor string,
	loadVerifyingKey func() (groth16.VerifyingKey, error),
) error {
	if err := validateCanonicalProofFramingBN254(proofBytes); err != nil {
		return err
	}
	ctx.GasMeter().ConsumeGas(verificationGas, gasDescriptor)
	proof, err := readProofBN254(proofBytes)
	if err != nil {
		return err
	}
	publicWitness, err := newPublicWitnessBN254(assignment)
	if err != nil {
		return err
	}
	verifyingKey, err := loadVerifyingKey()
	if err != nil {
		return err
	}
	return groth16.Verify(proof, verifyingKey, publicWitness)
}
