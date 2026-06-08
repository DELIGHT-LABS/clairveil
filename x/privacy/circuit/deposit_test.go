package circuit

import (
	"math/big"
	"testing"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	"github.com/consensys/gnark/test"
	"github.com/stretchr/testify/require"

	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestDepositCircuitValidProof(t *testing.T) {
	assignment := buildValidDepositAssignment(t, big.NewInt(7), big.NewInt(11))

	ccs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &DepositCircuit{})
	require.NoError(t, err)

	pk, vk, err := groth16.Setup(ccs)
	require.NoError(t, err)

	witness, err := frontend.NewWitness(assignment, ecc.BN254.ScalarField())
	require.NoError(t, err)

	proof, err := groth16.Prove(ccs, pk, witness)
	require.NoError(t, err)

	publicWitness, err := frontend.NewWitness(assignment, ecc.BN254.ScalarField(), frontend.PublicOnly())
	require.NoError(t, err)
	require.NoError(t, groth16.Verify(proof, vk, publicWitness))
}

func TestDepositCircuitBindsAmount(t *testing.T) {
	assignment := buildValidDepositAssignment(t, big.NewInt(7), big.NewInt(11))

	ccs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &DepositCircuit{})
	require.NoError(t, err)

	pk, vk, err := groth16.Setup(ccs)
	require.NoError(t, err)

	witness, err := frontend.NewWitness(assignment, ecc.BN254.ScalarField())
	require.NoError(t, err)

	proof, err := groth16.Prove(ccs, pk, witness)
	require.NoError(t, err)

	tampered := *assignment
	tampered.Amount = big.NewInt(8)
	publicWitness, err := frontend.NewWitness(&tampered, ecc.BN254.ScalarField(), frontend.PublicOnly())
	require.NoError(t, err)
	require.Error(t, groth16.Verify(proof, vk, publicWitness))
}

func TestDepositCircuitRejectsAmountOutsideRange(t *testing.T) {
	tooLarge := new(big.Int).Add(privacytypes.MaxShieldedAmount(), big.NewInt(1))
	assignment := buildValidDepositAssignment(t, tooLarge, big.NewInt(11))

	assert := test.NewAssert(t)
	assert.ProverFailed(&DepositCircuit{}, assignment, test.WithCurves(ecc.BN254))
}

func buildValidDepositAssignment(t *testing.T, amount, assetID *big.Int) *DepositCircuit {
	t.Helper()

	randomness := big.NewInt(13)
	spendPubKey := scalarMulBase(big.NewInt(17))
	viewPubKey := scalarMulBase(big.NewInt(19))
	spendPubKeyX, spendPubKeyY := pointBigInts(spendPubKey)
	viewPubKeyX, viewPubKeyY := pointBigInts(viewPubKey)

	commitment := privacycrypto.MimcHash(
		spendPubKeyX,
		spendPubKeyY,
		viewPubKeyX,
		viewPubKeyY,
		amount,
		assetID,
		randomness,
	)

	assignment := &DepositCircuit{
		Commitment: commitment,
		Amount:     amount,
		AssetID:    assetID,
		Randomness: randomness,
	}
	assignment.ReceiverSpendPubKey.X = spendPubKeyX
	assignment.ReceiverSpendPubKey.Y = spendPubKeyY
	assignment.ReceiverViewPubKey.X = viewPubKeyX
	assignment.ReceiverViewPubKey.Y = viewPubKeyY

	return assignment
}
