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
	"github.com/consensys/gnark/test"
	"github.com/stretchr/testify/require"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestSpendCircuitBindsRecipientDigest(t *testing.T) {
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
	require.Len(t, publicWitness.Vector().(fr.Vector), 9)
	require.NoError(t, groth16.Verify(proof, vk, publicWitness))

	for _, tc := range []struct {
		name   string
		mutate func(*SpendCircuit)
	}{
		{name: "recipient", mutate: func(tampered *SpendCircuit) { tampered.RecipientDigestHi = big.NewInt(424243) }},
		{name: "chain", mutate: func(tampered *SpendCircuit) { tampered.ChainDomainLo = big.NewInt(104) }},
		{name: "expiry", mutate: func(tampered *SpendCircuit) { tampered.ExpiresAtUnix = big.NewInt(2_000_000_001) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tampered := *assertion
			tc.mutate(&tampered)
			tamperedPublicWitness, err := frontend.NewWitness(&tampered, ecc.BN254.ScalarField(), frontend.PublicOnly())
			require.NoError(t, err)
			require.Error(t, groth16.Verify(proof, vk, tamperedPublicWitness))
		})
	}
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

func TestSpendCircuitRejectsAmountOutsideRange(t *testing.T) {
	tooLarge := new(big.Int).Add(privacytypes.MaxShieldedAmount(), big.NewInt(1))
	assignment := buildValidSpendAssignmentWithAmount(t, big.NewInt(424242), tooLarge)

	assert := test.NewAssert(t)
	assert.ProverFailed(&SpendCircuit{}, assignment, test.WithCurves(ecc.BN254))
}

func TestSpendCircuitRejectsMalformedSpendPubKey(t *testing.T) {
	assignment := buildValidSpendAssignment(t, big.NewInt(424242))
	x, y := invalidEdwardsPointForTest(t)
	assignment.ReceiverSpendPubKey.A.X = x
	assignment.ReceiverSpendPubKey.A.Y = y

	assert := test.NewAssert(t)
	assert.ProverFailed(&SpendCircuit{}, assignment, test.WithCurves(ecc.BN254))
}

func TestSpendCircuitRejectsZeroActiveNullifier(t *testing.T) {
	assignment := buildValidSpendAssignment(t, big.NewInt(424242))
	assignment.Nullifier = big.NewInt(0)

	assert := test.NewAssert(t)
	assert.ProverFailed(&SpendCircuit{}, assignment, test.WithCurves(ecc.BN254))
}

func TestSpendCircuitRejectsLiteralZeroForHigherEmptySibling(t *testing.T) {
	assignment := buildValidSpendAssignment(t, big.NewInt(424242))
	assignment.Path[1] = big.NewInt(0)

	assert := test.NewAssert(t)
	assert.ProverFailed(&SpendCircuit{}, assignment, test.WithCurves(ecc.BN254))
}

func TestSpendCircuitRejectsMalformedSignaturePoint(t *testing.T) {
	assignment := buildValidSpendAssignment(t, big.NewInt(424242))
	x, y := invalidEdwardsPointForTest(t)
	assignment.Signature.R.X = x
	assignment.Signature.R.Y = y

	assert := test.NewAssert(t)
	assert.ProverFailed(&SpendCircuit{}, assignment, test.WithCurves(ecc.BN254))
}

func TestSpendCircuitRejectsSignatureScalarAboveOrder(t *testing.T) {
	assignment := buildValidSpendAssignment(t, big.NewInt(424242))
	assignment.Signature.S = signatureScalarAboveOrderForTest()

	assert := test.NewAssert(t)
	assert.ProverFailed(&SpendCircuit{}, assignment, test.WithCurves(ecc.BN254))
}

func buildValidSpendAssignment(t testing.TB, recipient *big.Int) *SpendCircuit {
	return buildValidSpendAssignmentWithAmount(t, recipient, big.NewInt(7))
}

func buildValidSpendAssignmentWithAmount(t testing.TB, recipient, amount *big.Int) *SpendCircuit {
	t.Helper()

	assetID := big.NewInt(11)
	randomness := big.NewInt(13)
	spendScalar := big.NewInt(17)
	viewScalar := big.NewInt(19)

	spendPubKey := scalarMulBase(spendScalar)
	viewPubKey := scalarMulBase(viewScalar)
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
	root := merkleRootFromLeaf(commitment)
	nullifier := privacytypes.ComputeNoteNullifierV1(commitment, randomness, spendPubKeyX, spendPubKeyY)
	chainDomainHi := big.NewInt(101)
	chainDomainLo := big.NewInt(103)
	recipientDigestLo := big.NewInt(107)
	expiresAtUnix := int64(2_000_000_000)
	intent, err := privacytypes.ComputeSpendIntentV2(privacytypes.SpendIntentV2Input{
		ChainDomainHi:     chainDomainHi,
		ChainDomainLo:     chainDomainLo,
		MerkleRoot:        root,
		Nullifier:         nullifier,
		Amount:            amount,
		AssetID:           assetID,
		RecipientDigestHi: recipient,
		RecipientDigestLo: recipientDigestLo,
		ExpiresAtUnix:     expiresAtUnix,
	})
	require.NoError(t, err)
	sig := signSpendMessage(t, intent, spendScalar, spendPubKey)

	assignment := &SpendCircuit{
		MerkleRoot:        root,
		ChainDomainHi:     chainDomainHi,
		ChainDomainLo:     chainDomainLo,
		ExpiresAtUnix:     big.NewInt(expiresAtUnix),
		Nullifier:         nullifier,
		Amount:            amount,
		RecipientDigestHi: recipient,
		RecipientDigestLo: recipientDigestLo,
		AssetID:           assetID,
		Randomness:        randomness,
	}

	for i := 0; i < MerkleDepth; i++ {
		assignment.Path[i] = privacytypes.EmptyNoteTreeRootV1(uint32(i))
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
	for i := 0; i < MerkleDepth; i++ {
		current = privacytypes.ComputeNoteTreeNodeV1(uint32(i), current, privacytypes.EmptyNoteTreeRootV1(uint32(i)))
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

func signSpendMessage(t testing.TB, msg, scalar *big.Int, pubKey crypto_tedwards.PointAffine) eddsa.Signature {
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

func invalidEdwardsPointForTest(t testing.TB) (*big.Int, *big.Int) {
	t.Helper()

	for x := int64(0); x < 16; x++ {
		for y := int64(0); y < 16; y++ {
			var point crypto_tedwards.PointAffine
			point.X.SetBigInt(big.NewInt(x))
			point.Y.SetBigInt(big.NewInt(y))
			if !point.IsOnCurve() {
				return big.NewInt(x), big.NewInt(y)
			}
		}
	}

	t.Fatal("failed to find a small off-curve Edwards point")
	return nil, nil
}

func signatureScalarAboveOrderForTest() *big.Int {
	curve := crypto_tedwards.GetEdwardsCurve()
	return new(big.Int).Add(&curve.Order, big.NewInt(1))
}

func writePaddedTest(h interface{ Write([]byte) (int, error) }, v *big.Int) {
	var elem fr.Element
	elem.SetBigInt(v)
	b := elem.Bytes()
	h.Write(b[:])
}
