package circuit

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/consensys/gnark-crypto/ecc"
	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	"github.com/consensys/gnark/std/signature/eddsa"
	"github.com/consensys/gnark/test"
	"github.com/stretchr/testify/require"

	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestJoinSplitCircuitValidProof(t *testing.T) {
	assignment := buildValidJoinSplitAssignment(t)

	ccs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &JoinSplitCircuit{})
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

	tampered := *assignment
	tampered.AuditDisclosureDigest = new(big.Int).Add(tampered.AuditDisclosureDigest.(*big.Int), big.NewInt(1))

	tamperedPublicWitness, err := frontend.NewWitness(&tampered, ecc.BN254.ScalarField(), frontend.PublicOnly())
	require.NoError(t, err)
	require.Error(t, groth16.Verify(proof, vk, tamperedPublicWitness))
}

func TestJoinSplitCircuitRejectsOutputAmountOutsideRange(t *testing.T) {
	maxAmount := privacytypes.MaxShieldedAmount()
	assignment := buildJoinSplitAssignmentWithAmounts(
		t,
		[NumInputs]*big.Int{maxAmount, big.NewInt(1)},
		[NumOutputs]*big.Int{new(big.Int).Add(maxAmount, big.NewInt(1)), big.NewInt(0)},
	)

	assert := test.NewAssert(t)
	assert.ProverFailed(&JoinSplitCircuit{}, assignment, test.WithCurves(ecc.BN254))
}

func TestJoinSplitCircuitRejectsMalformedInputSpendPubKey(t *testing.T) {
	assignment := buildValidJoinSplitAssignment(t)
	x, y := invalidEdwardsPointForTest(t)
	assignment.InputSpendPubKeys[0].A.X = x
	assignment.InputSpendPubKeys[0].A.Y = y

	assert := test.NewAssert(t)
	assert.ProverFailed(&JoinSplitCircuit{}, assignment, test.WithCurves(ecc.BN254))
}

func TestJoinSplitCircuitRejectsMalformedInputSignaturePoint(t *testing.T) {
	assignment := buildValidJoinSplitAssignment(t)
	x, y := invalidEdwardsPointForTest(t)
	assignment.InputSignatures[0].R.X = x
	assignment.InputSignatures[0].R.Y = y

	assert := test.NewAssert(t)
	assert.ProverFailed(&JoinSplitCircuit{}, assignment, test.WithCurves(ecc.BN254))
}

func TestJoinSplitCircuitRejectsInputSignatureScalarAboveOrder(t *testing.T) {
	assignment := buildValidJoinSplitAssignment(t)
	assignment.InputSignatures[0].S = signatureScalarAboveOrderForTest()

	assert := test.NewAssert(t)
	assert.ProverFailed(&JoinSplitCircuit{}, assignment, test.WithCurves(ecc.BN254))
}

func TestJoinSplitCircuitRejectsMalformedOutputViewPubKey(t *testing.T) {
	assignment := buildValidJoinSplitAssignment(t)
	x, y := invalidEdwardsPointForTest(t)
	assignment.OutputViewPubKeys[0].A.X = x
	assignment.OutputViewPubKeys[0].A.Y = y

	assert := test.NewAssert(t)
	assert.ProverFailed(&JoinSplitCircuit{}, assignment, test.WithCurves(ecc.BN254))
}

func buildValidJoinSplitAssignment(t testing.TB) *JoinSplitCircuit {
	return buildJoinSplitAssignmentWithAmounts(
		t,
		[NumInputs]*big.Int{big.NewInt(5), big.NewInt(8)},
		[NumOutputs]*big.Int{big.NewInt(6), big.NewInt(7)},
	)
}

func buildJoinSplitAssignmentWithAmounts(
	t testing.TB,
	inputAmounts [NumInputs]*big.Int,
	outputAmounts [NumOutputs]*big.Int,
) *JoinSplitCircuit {
	t.Helper()

	assetID := big.NewInt(21)
	inputRandomness := [NumInputs]*big.Int{big.NewInt(31), big.NewInt(37)}
	outputRandomness := [NumOutputs]*big.Int{big.NewInt(41), big.NewInt(43)}

	inputSpendScalar := big.NewInt(17)
	inputViewScalar := big.NewInt(19)
	outputSpendScalar := big.NewInt(23)
	outputViewScalar := big.NewInt(29)

	inputSpendPubKey := scalarMulBase(inputSpendScalar)
	inputViewPubKey := scalarMulBase(inputViewScalar)
	outputSpendPubKey := scalarMulBase(outputSpendScalar)
	outputViewPubKey := scalarMulBase(outputViewScalar)

	inputSpendPubX, inputSpendPubY := pointBigInts(inputSpendPubKey)
	inputViewPubX, inputViewPubY := pointBigInts(inputViewPubKey)
	outputSpendPubX, outputSpendPubY := pointBigInts(outputSpendPubKey)
	outputViewPubX, outputViewPubY := pointBigInts(outputViewPubKey)

	inputCommitments := [NumInputs]*big.Int{}
	for i := 0; i < NumInputs; i++ {
		inputCommitments[i] = privacycrypto.MimcHash(
			inputSpendPubX,
			inputSpendPubY,
			inputViewPubX,
			inputViewPubY,
			inputAmounts[i],
			assetID,
			inputRandomness[i],
		)
	}

	root := joinsplitRootFromLeaves(inputCommitments[0], inputCommitments[1])
	outputCommitment0 := privacycrypto.MimcHash(outputSpendPubX, outputSpendPubY, outputViewPubX, outputViewPubY, outputAmounts[0], assetID, outputRandomness[0])
	outputCommitment1 := privacycrypto.MimcHash(inputSpendPubX, inputSpendPubY, inputViewPubX, inputViewPubY, outputAmounts[1], assetID, outputRandomness[1])

	userDigest, err := privacytypes.ComputeTransferDisclosureDigestBytes(
		privacytypes.TransferPrivacyPolicyDiscloseAmountToFrom,
		privacytypes.TransferDisclosureRecipientOutputIndex,
		mustCanonicalFieldBytesFromBigIntForTest(t, outputCommitment0),
		outputAmounts[0],
		assetID,
		inputSpendPubX,
		inputSpendPubY,
		inputViewPubX,
		inputViewPubY,
		outputSpendPubX,
		outputSpendPubY,
		outputViewPubX,
		outputViewPubY,
	)
	require.NoError(t, err)

	auditDigest, err := privacytypes.ComputeAuditTransferDisclosureDigestBytes(
		privacytypes.TransferDisclosureRecipientOutputIndex,
		mustCanonicalFieldBytesFromBigIntForTest(t, outputCommitment0),
		outputAmounts[0],
		assetID,
		inputSpendPubX,
		inputSpendPubY,
		inputViewPubX,
		inputViewPubY,
		outputSpendPubX,
		outputSpendPubY,
		outputViewPubX,
		outputViewPubY,
	)
	require.NoError(t, err)

	assignment := &JoinSplitCircuit{
		MerkleRoot:            root,
		UserPrivacyPolicy:     big.NewInt(int64(privacytypes.TransferPrivacyPolicyDiscloseAmountToFrom)),
		UserDisclosureDigest:  new(big.Int).SetBytes(userDigest),
		AuditDisclosureDigest: new(big.Int).SetBytes(auditDigest),
		AssetID:               assetID,
	}

	assignPubKey(&assignment.InputSpendPubKeys[0], inputSpendPubKey)
	assignPubKey(&assignment.InputSpendPubKeys[1], inputSpendPubKey)
	assignPubKey(&assignment.InputViewPubKeys[0], inputViewPubKey)
	assignPubKey(&assignment.InputViewPubKeys[1], inputViewPubKey)
	assignPubKey(&assignment.OutputSpendPubKeys[0], outputSpendPubKey)
	assignPubKey(&assignment.OutputSpendPubKeys[1], inputSpendPubKey)
	assignPubKey(&assignment.OutputViewPubKeys[0], outputViewPubKey)
	assignPubKey(&assignment.OutputViewPubKeys[1], inputViewPubKey)

	for i := 0; i < NumInputs; i++ {
		assignment.InputAmounts[i] = inputAmounts[i]
		assignment.InputRandomness[i] = inputRandomness[i]
		assignment.Nullifiers[i] = privacycrypto.MimcHash(inputRandomness[i], inputSpendPubX, inputSpendPubY)

		msg := privacycrypto.MimcHash(inputAmounts[i], assetID, inputRandomness[i])
		assignment.InputSignatures[i] = signSpendMessage(t, msg, inputSpendScalar, inputSpendPubKey)
	}

	assignJoinSplitPath(&assignment.InputPaths[0], &assignment.InputPathHelpers[0], inputCommitments[1], 0)
	assignJoinSplitPath(&assignment.InputPaths[1], &assignment.InputPathHelpers[1], inputCommitments[0], 1)

	assignment.OutputAmounts[0] = outputAmounts[0]
	assignment.OutputRandomness[0] = outputRandomness[0]
	assignment.Commitments[0] = outputCommitment0

	assignment.OutputAmounts[1] = outputAmounts[1]
	assignment.OutputRandomness[1] = outputRandomness[1]
	assignment.Commitments[1] = outputCommitment1

	return assignment
}

func joinsplitRootFromLeaves(leftLeaf, rightLeaf *big.Int) *big.Int {
	current := privacycrypto.MimcHash(leftLeaf, rightLeaf)
	zero := big.NewInt(0)

	for i := 1; i < MerkleDepth; i++ {
		current = privacycrypto.MimcHash(current, zero)
	}

	return current
}

func assignJoinSplitPath(path *[MerkleDepth]frontend.Variable, helpers *[MerkleDepth]frontend.Variable, sibling *big.Int, helper int) {
	path[0] = sibling
	helpers[0] = helper

	for i := 1; i < MerkleDepth; i++ {
		path[i] = big.NewInt(0)
		helpers[i] = 0
	}
}

func assignPubKey(target *eddsa.PublicKey, source crypto_tedwards.PointAffine) {
	ax, ay := pointBigInts(source)
	target.A.X = ax
	target.A.Y = ay
}

func mustCanonicalFieldBytesFromBigIntForTest(t testing.TB, value *big.Int) []byte {
	t.Helper()

	bz, err := canonicalFieldBytesFromBigIntForTest(value)
	require.NoError(t, err)
	return bz
}

func canonicalFieldBytesFromBigIntForTest(value *big.Int) ([]byte, error) {
	if value == nil {
		return nil, fmt.Errorf("value is nil")
	}
	bz := value.Bytes()
	if len(bz) > 32 {
		return nil, fmt.Errorf("value exceeds 32 bytes")
	}
	out := make([]byte, 32)
	copy(out[32-len(bz):], bz)
	return out, nil
}
