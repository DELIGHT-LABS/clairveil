package keeper

import (
	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/backend/witness"
	"github.com/consensys/gnark/frontend"
	sdk "github.com/cosmos/cosmos-sdk/types"

	privacyzk "github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
)

const (
	DepositProofVerificationGas   uint64 = 1_000_000
	SpendProofVerificationGas     uint64 = 1_000_000
	JoinSplitProofVerificationGas uint64 = 1_000_000
)

func newPublicWitnessBN254(assignment frontend.Circuit) (witness.Witness, error) {
	return frontend.NewWitness(assignment, ecc.BN254.ScalarField(), frontend.PublicOnly())
}

func readProofBN254(proofBytes []byte) (groth16.Proof, error) {
	return privacyzk.ReadCanonicalProofBN254(proofBytes)
}

func validateCanonicalProofFramingBN254(proofBytes []byte) error {
	return privacyzk.ValidateCanonicalProofFramingBN254(proofBytes)
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
