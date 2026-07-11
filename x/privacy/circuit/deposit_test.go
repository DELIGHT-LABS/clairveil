package circuit

import (
	"math/big"
	"testing"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	"github.com/consensys/gnark/test"
	"github.com/stretchr/testify/require"

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

func TestDepositCircuitBindsAssetID(t *testing.T) {
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
	tampered.AssetID = big.NewInt(12)
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

func TestDepositCircuitRejectsMalformedSpendPubKey(t *testing.T) {
	assignment := buildValidDepositAssignment(t, big.NewInt(7), big.NewInt(11))
	x, y := invalidEdwardsPointForTest(t)
	assignment.ReceiverSpendPubKey.X = x
	assignment.ReceiverSpendPubKey.Y = y

	assert := test.NewAssert(t)
	assert.ProverFailed(&DepositCircuit{}, assignment, test.WithCurves(ecc.BN254))
}

func TestDepositCircuitRejectsIdentityAndNonSubgroupKeys(t *testing.T) {
	for _, tc := range []struct {
		name string
		x    *big.Int
		y    *big.Int
	}{
		{name: "identity", x: big.NewInt(0), y: big.NewInt(1)},
		{name: "order two", x: big.NewInt(0), y: new(big.Int).Sub(fr.Modulus(), big.NewInt(1))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assignment := buildValidDepositAssignment(t, big.NewInt(7), big.NewInt(11))
			assignment.ReceiverViewPubKey.X = tc.x
			assignment.ReceiverViewPubKey.Y = tc.y

			assert := test.NewAssert(t)
			assert.ProverFailed(&DepositCircuit{}, assignment, test.WithCurves(ecc.BN254))
		})
	}

	t.Run("on-curve x-nonzero outside prime subgroup", func(t *testing.T) {
		assignment := buildValidDepositAssignment(t, big.NewInt(7), big.NewInt(11))
		point := onCurveNonSubgroupPointForTest(t)
		x, y := pointBigInts(point)
		assignment.ReceiverViewPubKey.X = x
		assignment.ReceiverViewPubKey.Y = y
		assignment.Commitment = privacytypes.ComputeNoteCommitmentV1(
			assignment.ReceiverSpendPubKey.X.(*big.Int),
			assignment.ReceiverSpendPubKey.Y.(*big.Int),
			x, y,
			assignment.Amount.(*big.Int),
			assignment.AssetID.(*big.Int),
			assignment.Randomness.(*big.Int),
		)

		assert := test.NewAssert(t)
		assert.ProverFailed(&DepositCircuit{}, assignment, test.WithCurves(ecc.BN254))
	})
}

func onCurveNonSubgroupPointForTest(t testing.TB) crypto_tedwards.PointAffine {
	t.Helper()
	subgroupPoint := scalarMulBase(big.NewInt(17))
	var orderTwo crypto_tedwards.PointAffine
	orderTwo.X.SetZero()
	orderTwo.Y.SetBigInt(new(big.Int).Sub(fr.Modulus(), big.NewInt(1)))
	var point crypto_tedwards.PointAffine
	point.Add(&subgroupPoint, &orderTwo)
	require.True(t, point.IsOnCurve())
	require.False(t, point.IsZero())
	require.False(t, point.X.IsZero())
	curve := crypto_tedwards.GetEdwardsCurve()
	var orderMultiple crypto_tedwards.PointAffine
	orderMultiple.ScalarMultiplication(&point, &curve.Order)
	require.False(t, orderMultiple.IsZero())
	return point
}

func TestDepositCircuitRejectsZeroActiveCommitment(t *testing.T) {
	assignment := buildValidDepositAssignment(t, big.NewInt(7), big.NewInt(11))
	assignment.Commitment = big.NewInt(0)

	assert := test.NewAssert(t)
	assert.ProverFailed(&DepositCircuit{}, assignment, test.WithCurves(ecc.BN254))
}

func buildValidDepositAssignment(t testing.TB, amount, assetID *big.Int) *DepositCircuit {
	t.Helper()

	randomness := big.NewInt(13)
	spendPubKey := scalarMulBase(big.NewInt(17))
	viewPubKey := scalarMulBase(big.NewInt(19))
	spendPubKeyX, spendPubKeyY := pointBigInts(spendPubKey)
	viewPubKeyX, viewPubKeyY := pointBigInts(viewPubKey)

	commitment := privacytypes.ComputeNoteCommitmentV1(
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
