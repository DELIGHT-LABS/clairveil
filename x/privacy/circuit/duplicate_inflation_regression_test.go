package circuit

import (
	"fmt"
	"math/big"
	"testing"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/test"
	"github.com/stretchr/testify/require"
)

// These test-only circuits run the production Define methods while relaxing
// exactly one failing distinctness assertion. A passing relaxed control proves
// that membership, commitment/nullifier recomputation, owner signature,
// disclosure, roots, and conservation all remain valid for the exploit-shaped
// witness. Production circuit source and R1CS are not changed.
type joinSplitDuplicateInflationControl JoinSplitCircuit

func (c *joinSplitDuplicateInflationControl) Define(api frontend.API) error {
	relaxed := &relaxOneFailedAssertIsDifferentAPI{API: api}
	if err := (*JoinSplitCircuit)(c).Define(relaxed); err != nil {
		return err
	}
	if relaxed.skipped != 1 {
		return fmt.Errorf("expected to relax exactly one failed distinctness assertion; got %d", relaxed.skipped)
	}
	return nil
}

type batchDuplicateInflationControl BatchJoinSplit16x32

func (c *batchDuplicateInflationControl) Define(api frontend.API) error {
	relaxed := &relaxOneFailedAssertIsEqualAPI{API: api}
	if err := (*BatchJoinSplit16x32)(c).Define(relaxed); err != nil {
		return err
	}
	if relaxed.skipped != 1 {
		return fmt.Errorf("expected to relax exactly one failed pairwise-distinctness assertion; got %d", relaxed.skipped)
	}
	return nil
}

type relaxOneFailedAssertIsDifferentAPI struct {
	frontend.API
	skipped int
}

func (a *relaxOneFailedAssertIsDifferentAPI) AssertIsDifferent(i1, i2 frontend.Variable) {
	if a.skipped == 0 {
		if comparison, ok := duplicateRegressionCompare(i1, i2); ok && comparison == 0 {
			a.skipped++
			return
		}
	}
	a.API.AssertIsDifferent(i1, i2)
}

type relaxOneFailedAssertIsEqualAPI struct {
	frontend.API
	skipped int
}

func (a *relaxOneFailedAssertIsEqualAPI) AssertIsEqual(i1, i2 frontend.Variable) {
	if a.skipped == 0 {
		if comparison, ok := duplicateRegressionCompare(i1, i2); ok && comparison != 0 {
			a.skipped++
			return
		}
	}
	a.API.AssertIsEqual(i1, i2)
}

func duplicateRegressionCompare(i1, i2 frontend.Variable) (int, bool) {
	left, leftOK := duplicateRegressionBigInt(i1)
	right, rightOK := duplicateRegressionBigInt(i2)
	if !leftOK || !rightOK {
		return 0, false
	}
	return left.Cmp(right), true
}

func duplicateRegressionBigInt(value frontend.Variable) (*big.Int, bool) {
	switch value := value.(type) {
	case *big.Int:
		return value, true
	case big.Int:
		return new(big.Int).Set(&value), true
	case int:
		return big.NewInt(int64(value)), true
	case int8:
		return big.NewInt(int64(value)), true
	case int16:
		return big.NewInt(int64(value)), true
	case int32:
		return big.NewInt(int64(value)), true
	case int64:
		return big.NewInt(value), true
	case uint:
		return new(big.Int).SetUint64(uint64(value)), true
	case uint8:
		return new(big.Int).SetUint64(uint64(value)), true
	case uint16:
		return new(big.Int).SetUint64(uint64(value)), true
	case uint32:
		return new(big.Int).SetUint64(uint64(value)), true
	case uint64:
		return new(big.Int).SetUint64(value), true
	default:
		return nil, false
	}
}

func TestJoinSplitCircuitRejectsExactDuplicateInputInflation(t *testing.T) {
	assignment := buildExactDuplicateInputInflationJoinSplit(t)
	assertExactDuplicateInputInflationJoinSplitShape(t, assignment)

	controlAssignment := joinSplitDuplicateInflationControl(*assignment)
	require.NoError(t, test.IsSolved(
		&joinSplitDuplicateInflationControl{},
		&controlAssignment,
		ecc.BN254.ScalarField(),
	), "the exact exploit witness must satisfy every production constraint except input distinctness")

	err := test.IsSolved(&JoinSplitCircuit{}, assignment, ecc.BN254.ScalarField())
	require.Error(t, err)
	require.Contains(t, err.Error(), "[assertIsDifferent]")
	require.Contains(t, err.Error(), "circuit.(*JoinSplitCircuit).defineBase")

	assert := test.NewAssert(t)
	assert.ProverFailed(&JoinSplitCircuit{}, assignment, test.WithCurves(ecc.BN254))
}

func TestBatchJoinSplit16x32RejectsExactDuplicateInputInflation(t *testing.T) {
	assignment := buildExactDuplicateInputInflationBatch(t)
	assertExactDuplicateInputInflationBatchShape(t, assignment)

	controlAssignment := batchDuplicateInflationControl(*assignment)
	require.NoError(t, test.IsSolved(
		&batchDuplicateInflationControl{},
		&controlAssignment,
		ecc.BN254.ScalarField(),
	), "the exact exploit witness must satisfy every production constraint except active input distinctness")

	err := test.IsSolved(&BatchJoinSplit16x32{}, assignment, ecc.BN254.ScalarField())
	require.Error(t, err)
	require.Contains(t, err.Error(), "[assertIsEqual] 1 == 0")
	require.Contains(t, err.Error(), "batch_joinsplit_16x32.go:150")

	assertBatchProductionSolve(t, compiledBatchProductionCCS(t), assignment, false)
}

func buildExactDuplicateInputInflationJoinSplit(t testing.TB) *JoinSplitCircuit {
	t.Helper()
	assignment := buildJoinSplitAssignmentWithAmounts(
		t,
		[NumInputs]*big.Int{big.NewInt(5), big.NewInt(5)},
		[NumOutputs]*big.Int{big.NewInt(6), big.NewInt(4)},
	)

	inputSpendX := assignment.InputSpendPubKeys[0].A.X.(*big.Int)
	inputSpendY := assignment.InputSpendPubKeys[0].A.Y.(*big.Int)
	inputViewX := assignment.InputViewPubKeys[0].A.X.(*big.Int)
	inputViewY := assignment.InputViewPubKeys[0].A.Y.(*big.Int)
	noteCommitment := privacytypes.ComputeNoteCommitmentV1(
		inputSpendX,
		inputSpendY,
		inputViewX,
		inputViewY,
		assignment.InputAmounts[0].(*big.Int),
		assignment.AssetID.(*big.Int),
		assignment.InputRandomness[0].(*big.Int),
	)
	nullifier := privacytypes.ComputeNoteNullifierV1(
		noteCommitment,
		assignment.InputRandomness[0].(*big.Int),
		inputSpendX,
		inputSpendY,
	)

	assignment.MerkleRoot = merkleRootFromLeaf(noteCommitment)
	for i := 0; i < NumInputs; i++ {
		assignment.InputAmounts[i] = new(big.Int).Set(assignment.InputAmounts[0].(*big.Int))
		assignment.InputRandomness[i] = new(big.Int).Set(assignment.InputRandomness[0].(*big.Int))
		assignment.InputSpendPubKeys[i] = assignment.InputSpendPubKeys[0]
		assignment.InputViewPubKeys[i] = assignment.InputViewPubKeys[0]
		assignment.Nullifiers[i] = new(big.Int).Set(nullifier)
		assignJoinSplitPath(
			&assignment.InputPaths[i],
			&assignment.InputPathHelpers[i],
			privacytypes.EmptyNoteTreeRootV1(0),
			0,
		)
	}

	intent := duplicateRegressionJoinSplitIntent(t, assignment)
	ownerScalar := big.NewInt(17)
	assignment.OwnerSignature = signSpendMessage(t, intent, ownerScalar, scalarMulBase(ownerScalar))
	return assignment
}

func buildExactDuplicateInputInflationBatch(t testing.TB) *BatchJoinSplit16x32 {
	t.Helper()
	assignment := buildBatchFeasibilityAssignment(t, 2, 2)

	assignment.InputAmounts[1] = new(big.Int).Set(assignment.InputAmounts[0].(*big.Int))
	assignment.InputRandomness[1] = new(big.Int).Set(assignment.InputRandomness[0].(*big.Int))
	assignment.InputSpendPubKeys[1] = assignment.InputSpendPubKeys[0]
	assignment.InputViewPubKeys[1] = assignment.InputViewPubKeys[0]

	noteCommitment := duplicateRegressionBatchInputCommitment(assignment, 0)
	root, paths, helpers := batchFeasibilityTreeAndPaths([]*big.Int{noteCommitment})
	assignment.MerkleRoot = root
	for i := 0; i < 2; i++ {
		for level := 0; level < MerkleDepth; level++ {
			assignment.InputPaths[i][level] = new(big.Int).Set(paths[0][level])
			assignment.InputPathHelpers[i][level] = new(big.Int).Set(helpers[0][level])
		}
	}

	nullifier := privacytypes.ComputeNoteNullifierV1(
		noteCommitment,
		assignment.InputRandomness[0].(*big.Int),
		assignment.InputSpendPubKeys[0].A.X.(*big.Int),
		assignment.InputSpendPubKeys[0].A.Y.(*big.Int),
	)
	nullifiers := zeroBigIntVector(MaxBatchJoinSplitInputs)
	nullifiers[0] = new(big.Int).Set(nullifier)
	nullifiers[1] = new(big.Int).Set(nullifier)
	var err error
	assignment.NullifierRoot, err = privacytypes.ComputeBatchVectorRootV1(
		privacytypes.BatchVectorNullifierV1,
		2,
		nullifiers,
	)
	require.NoError(t, err)
	resignBatchFixture(t, assignment)
	return assignment
}

func duplicateRegressionJoinSplitIntent(t testing.TB, assignment *JoinSplitCircuit) *big.Int {
	t.Helper()
	intent, err := privacytypes.ComputeTransferIntentV2(privacytypes.TransferIntentV2Input{
		ChainDomainHi:        assignment.ChainDomainHi.(*big.Int),
		ChainDomainLo:        assignment.ChainDomainLo.(*big.Int),
		MerkleRoot:           assignment.MerkleRoot.(*big.Int),
		AssetID:              assignment.AssetID.(*big.Int),
		Nullifiers:           [2]*big.Int{assignment.Nullifiers[0].(*big.Int), assignment.Nullifiers[1].(*big.Int)},
		Commitments:          [2]*big.Int{assignment.Commitments[0].(*big.Int), assignment.Commitments[1].(*big.Int)},
		UserDisclosureDigest: assignment.UserDisclosureDigest.(*big.Int),
		FullDisclosureDigest: assignment.FullDisclosureDigest.(*big.Int),
		PayloadDigestHi:      assignment.PayloadDigestHi.(*big.Int),
		PayloadDigestLo:      assignment.PayloadDigestLo.(*big.Int),
		ExpiresAtUnix:        assignment.ExpiresAtUnix.(*big.Int).Int64(),
	})
	require.NoError(t, err)
	return intent
}

func assertExactDuplicateInputInflationJoinSplitShape(t testing.TB, assignment *JoinSplitCircuit) {
	t.Helper()
	inputCommitments := [NumInputs]*big.Int{}
	for i := 0; i < NumInputs; i++ {
		require.Equal(t, assignment.InputAmounts[0], assignment.InputAmounts[i])
		require.Equal(t, assignment.InputRandomness[0], assignment.InputRandomness[i])
		require.Equal(t, assignment.InputSpendPubKeys[0], assignment.InputSpendPubKeys[i])
		require.Equal(t, assignment.InputViewPubKeys[0], assignment.InputViewPubKeys[i])
		require.Equal(t, assignment.InputPaths[0], assignment.InputPaths[i])
		require.Equal(t, assignment.InputPathHelpers[0], assignment.InputPathHelpers[i])
		require.Equal(t, assignment.Nullifiers[0], assignment.Nullifiers[i])

		inputCommitments[i] = privacytypes.ComputeNoteCommitmentV1(
			assignment.InputSpendPubKeys[i].A.X.(*big.Int),
			assignment.InputSpendPubKeys[i].A.Y.(*big.Int),
			assignment.InputViewPubKeys[i].A.X.(*big.Int),
			assignment.InputViewPubKeys[i].A.Y.(*big.Int),
			assignment.InputAmounts[i].(*big.Int),
			assignment.AssetID.(*big.Int),
			assignment.InputRandomness[i].(*big.Int),
		)
		require.Equal(t, assignment.MerkleRoot, duplicateRegressionFoldPath(
			t,
			inputCommitments[i],
			assignment.InputPaths[i],
			assignment.InputPathHelpers[i],
		))
		require.Equal(t, assignment.Nullifiers[i], privacytypes.ComputeNoteNullifierV1(
			inputCommitments[i],
			assignment.InputRandomness[i].(*big.Int),
			assignment.InputSpendPubKeys[i].A.X.(*big.Int),
			assignment.InputSpendPubKeys[i].A.Y.(*big.Int),
		))
	}
	require.Equal(t, inputCommitments[0], inputCommitments[1])

	inputTotal := new(big.Int).Add(
		assignment.InputAmounts[0].(*big.Int),
		assignment.InputAmounts[1].(*big.Int),
	)
	outputTotal := new(big.Int)
	outputCommitments := [NumOutputs]*big.Int{}
	for i := 0; i < NumOutputs; i++ {
		outputCommitments[i] = privacytypes.ComputeNoteCommitmentV1(
			assignment.OutputSpendPubKeys[i].A.X.(*big.Int),
			assignment.OutputSpendPubKeys[i].A.Y.(*big.Int),
			assignment.OutputViewPubKeys[i].A.X.(*big.Int),
			assignment.OutputViewPubKeys[i].A.Y.(*big.Int),
			assignment.OutputAmounts[i].(*big.Int),
			assignment.AssetID.(*big.Int),
			assignment.OutputRandomness[i].(*big.Int),
		)
		require.Equal(t, assignment.Commitments[i], outputCommitments[i])
		outputTotal.Add(outputTotal, assignment.OutputAmounts[i].(*big.Int))
	}
	require.NotEqual(t, outputCommitments[0], outputCommitments[1])
	require.Equal(t, inputTotal, outputTotal)
	require.Equal(t, new(big.Int).Mul(assignment.InputAmounts[0].(*big.Int), big.NewInt(2)), outputTotal)
}

func assertExactDuplicateInputInflationBatchShape(t testing.TB, assignment *BatchJoinSplit16x32) {
	t.Helper()
	require.Equal(t, int64(2), assignment.InputCount.(*big.Int).Int64())
	require.Equal(t, int64(2), assignment.OutputCount.(*big.Int).Int64())

	inputCommitments := [2]*big.Int{}
	nullifierValues := zeroBigIntVector(MaxBatchJoinSplitInputs)
	inputTotal := new(big.Int)
	for i := 0; i < 2; i++ {
		require.Equal(t, assignment.InputAmounts[0], assignment.InputAmounts[i])
		require.Equal(t, assignment.InputRandomness[0], assignment.InputRandomness[i])
		require.Equal(t, assignment.InputSpendPubKeys[0], assignment.InputSpendPubKeys[i])
		require.Equal(t, assignment.InputViewPubKeys[0], assignment.InputViewPubKeys[i])
		require.Equal(t, assignment.InputPaths[0], assignment.InputPaths[i])
		require.Equal(t, assignment.InputPathHelpers[0], assignment.InputPathHelpers[i])

		inputCommitments[i] = duplicateRegressionBatchInputCommitment(assignment, i)
		require.Equal(t, assignment.MerkleRoot, duplicateRegressionFoldPath(
			t,
			inputCommitments[i],
			assignment.InputPaths[i],
			assignment.InputPathHelpers[i],
		))
		nullifierValues[i] = privacytypes.ComputeNoteNullifierV1(
			inputCommitments[i],
			assignment.InputRandomness[i].(*big.Int),
			assignment.InputSpendPubKeys[i].A.X.(*big.Int),
			assignment.InputSpendPubKeys[i].A.Y.(*big.Int),
		)
		inputTotal.Add(inputTotal, assignment.InputAmounts[i].(*big.Int))
	}
	require.Equal(t, inputCommitments[0], inputCommitments[1])
	require.Equal(t, nullifierValues[0], nullifierValues[1])
	expectedNullifierRoot, err := privacytypes.ComputeBatchVectorRootV1(
		privacytypes.BatchVectorNullifierV1,
		2,
		nullifierValues,
	)
	require.NoError(t, err)
	require.Equal(t, assignment.NullifierRoot, expectedNullifierRoot)

	outputTotal := new(big.Int)
	outputCommitments := zeroBigIntVector(MaxBatchJoinSplitOutputs)
	for i := 0; i < 2; i++ {
		outputCommitments[i] = privacytypes.ComputeNoteCommitmentV1(
			assignment.OutputSpendPubKeys[i].A.X.(*big.Int),
			assignment.OutputSpendPubKeys[i].A.Y.(*big.Int),
			assignment.OutputViewPubKeys[i].A.X.(*big.Int),
			assignment.OutputViewPubKeys[i].A.Y.(*big.Int),
			assignment.OutputAmounts[i].(*big.Int),
			assignment.AssetID.(*big.Int),
			assignment.OutputRandomness[i].(*big.Int),
		)
		outputTotal.Add(outputTotal, assignment.OutputAmounts[i].(*big.Int))
	}
	require.NotEqual(t, outputCommitments[0], outputCommitments[1])
	expectedCommitmentRoot, err := privacytypes.ComputeBatchVectorRootV1(
		privacytypes.BatchVectorCommitmentV1,
		2,
		outputCommitments,
	)
	require.NoError(t, err)
	require.Equal(t, assignment.CommitmentRoot, expectedCommitmentRoot)
	require.Equal(t, inputTotal, outputTotal)
	require.Equal(t, new(big.Int).Mul(assignment.InputAmounts[0].(*big.Int), big.NewInt(2)), outputTotal)
}

func duplicateRegressionBatchInputCommitment(assignment *BatchJoinSplit16x32, index int) *big.Int {
	return privacytypes.ComputeNoteCommitmentV1(
		assignment.InputSpendPubKeys[index].A.X.(*big.Int),
		assignment.InputSpendPubKeys[index].A.Y.(*big.Int),
		assignment.InputViewPubKeys[index].A.X.(*big.Int),
		assignment.InputViewPubKeys[index].A.Y.(*big.Int),
		assignment.InputAmounts[index].(*big.Int),
		assignment.AssetID.(*big.Int),
		assignment.InputRandomness[index].(*big.Int),
	)
}

func duplicateRegressionFoldPath(
	t testing.TB,
	commitment *big.Int,
	path [MerkleDepth]frontend.Variable,
	helpers [MerkleDepth]frontend.Variable,
) *big.Int {
	t.Helper()
	current := new(big.Int).Set(commitment)
	for level := 0; level < MerkleDepth; level++ {
		helper, ok := duplicateRegressionBigInt(helpers[level])
		require.True(t, ok)
		require.True(t, helper.IsUint64())
		require.LessOrEqual(t, helper.Uint64(), uint64(1))
		sibling, ok := duplicateRegressionBigInt(path[level])
		require.True(t, ok)
		if helper.Sign() == 0 {
			current = privacytypes.ComputeNoteTreeNodeV1(uint32(level), current, sibling)
		} else {
			current = privacytypes.ComputeNoteTreeNodeV1(uint32(level), sibling, current)
		}
	}
	return current
}
