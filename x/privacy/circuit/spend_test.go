package circuit

import (
	"math/big"
	"testing"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
	fr_mimc "github.com/consensys/gnark-crypto/ecc/bn254/fr/mimc"
	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	"github.com/consensys/gnark/std/signature/eddsa"
	"github.com/stretchr/testify/require"

	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
)

func TestSpendCircuitBindsRecipient(t *testing.T) {
	assertion := buildValidSpendAssignment(t, big.NewInt(424242))

	ccs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &SpendCircuit{})
	require.NoError(t, err)

	pk, vk, err := groth16.Setup(ccs)
	require.NoError(t, err)

	witness, err := frontend.NewWitness(assertion, ecc.BN254.ScalarField())
	require.NoError(t, err)

	proof, err := groth16.Prove(ccs, pk, witness)
	require.NoError(t, err)

	publicWitness, err := frontend.NewWitness(assertion, ecc.BN254.ScalarField(), frontend.PublicOnly())
	require.NoError(t, err)
	require.NoError(t, groth16.Verify(proof, vk, publicWitness))

	tampered := *assertion
	tampered.Recipient = big.NewInt(424243)

	tamperedPublicWitness, err := frontend.NewWitness(&tampered, ecc.BN254.ScalarField(), frontend.PublicOnly())
	require.NoError(t, err)
	require.Error(t, groth16.Verify(proof, vk, tamperedPublicWitness))
}

func TestSpendCircuitBindsAssetID(t *testing.T) {
	assertion := buildValidSpendAssignment(t, big.NewInt(424242))

	ccs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &SpendCircuit{})
	require.NoError(t, err)

	pk, vk, err := groth16.Setup(ccs)
	require.NoError(t, err)

	witness, err := frontend.NewWitness(assertion, ecc.BN254.ScalarField())
	require.NoError(t, err)

	proof, err := groth16.Prove(ccs, pk, witness)
	require.NoError(t, err)

	publicWitness, err := frontend.NewWitness(assertion, ecc.BN254.ScalarField(), frontend.PublicOnly())
	require.NoError(t, err)
	require.NoError(t, groth16.Verify(proof, vk, publicWitness))

	tampered := *assertion
	tampered.AssetID = big.NewInt(12)

	tamperedPublicWitness, err := frontend.NewWitness(&tampered, ecc.BN254.ScalarField(), frontend.PublicOnly())
	require.NoError(t, err)
	require.Error(t, groth16.Verify(proof, vk, tamperedPublicWitness))
}

func buildValidSpendAssignment(t *testing.T, recipient *big.Int) *SpendCircuit {
	t.Helper()

	amount := big.NewInt(7)
	assetID := big.NewInt(11)
	randomness := big.NewInt(13)
	spendScalar := big.NewInt(17)
	viewScalar := big.NewInt(19)

	spendPubKey := scalarMulBase(spendScalar)
	viewPubKey := scalarMulBase(viewScalar)
	spendPubKeyX, spendPubKeyY := pointBigInts(spendPubKey)
	viewPubKeyX, viewPubKeyY := pointBigInts(viewPubKey)

	msg := privacycrypto.MimcHash(amount, assetID, randomness, recipient)
	sig := signSpendMessage(t, msg, spendScalar, spendPubKey)

	commitment := privacycrypto.MimcHash(
		spendPubKeyX,
		spendPubKeyY,
		viewPubKeyX,
		viewPubKeyY,
		amount,
		assetID,
		randomness,
	)
	root := merkleRootFromLeaf(commitment)

	assignment := &SpendCircuit{
		MerkleRoot: root,
		Nullifier:  privacycrypto.MimcHash(randomness, spendPubKeyX, spendPubKeyY),
		Amount:     amount,
		Recipient:  recipient,
		AssetID:    assetID,
		Randomness: randomness,
	}

	for i := 0; i < MerkleDepth; i++ {
		assignment.Path[i] = big.NewInt(0)
		assignment.PathHelper[i] = 0
	}

	assignment.ReceiverSpendPubKey.A.X = spendPubKeyX
	assignment.ReceiverSpendPubKey.A.Y = spendPubKeyY
	assignment.ReceiverViewPubKey.A.X = viewPubKeyX
	assignment.ReceiverViewPubKey.A.Y = viewPubKeyY
	assignment.Signature = sig

	return assignment
}

func merkleRootFromLeaf(leaf *big.Int) *big.Int {
	current := new(big.Int).Set(leaf)
	zero := big.NewInt(0)

	for i := 0; i < MerkleDepth; i++ {
		current = privacycrypto.MimcHash(current, zero)
	}

	return current
}

func scalarMulBase(scalar *big.Int) crypto_tedwards.PointAffine {
	curve := crypto_tedwards.GetEdwardsCurve()

	var base crypto_tedwards.PointAffine
	base.X.Set(&curve.Base.X)
	base.Y.Set(&curve.Base.Y)

	var pubKey crypto_tedwards.PointAffine
	pubKey.ScalarMultiplication(&base, scalar)

	return pubKey
}

func pointBigInts(point crypto_tedwards.PointAffine) (*big.Int, *big.Int) {
	x := new(big.Int)
	y := new(big.Int)
	point.X.BigInt(x)
	point.Y.BigInt(y)
	return x, y
}

func signSpendMessage(t *testing.T, msg, scalar *big.Int, pubKey crypto_tedwards.PointAffine) eddsa.Signature {
	t.Helper()

	curve := crypto_tedwards.GetEdwardsCurve()
	nonce := big.NewInt(19)

	var base crypto_tedwards.PointAffine
	base.X.Set(&curve.Base.X)
	base.Y.Set(&curve.Base.Y)

	var pointR crypto_tedwards.PointAffine
	pointR.ScalarMultiplication(&base, nonce)

	rx, ry := pointBigInts(pointR)
	ax, ay := pointBigInts(pubKey)

	hFunc := fr_mimc.NewMiMC()
	writePaddedTest(hFunc, rx)
	writePaddedTest(hFunc, ry)
	writePaddedTest(hFunc, ax)
	writePaddedTest(hFunc, ay)
	writePaddedTest(hFunc, msg)

	hRAM := new(big.Int).SetBytes(hFunc.Sum(nil))

	sig := eddsa.Signature{}
	sig.R.X = rx
	sig.R.Y = ry

	s := new(big.Int).Mul(hRAM, scalar)
	s.Add(s, nonce)
	s.Mod(s, &curve.Order)
	sig.S = s

	return sig
}

func writePaddedTest(h interface{ Write([]byte) (int, error) }, v *big.Int) {
	var elem fr.Element
	elem.SetBigInt(v)
	b := elem.Bytes()
	h.Write(b[:])
}
