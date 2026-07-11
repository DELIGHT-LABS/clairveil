package circuit

import (
	"math/big"
	"testing"

	privacyscan "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/scan"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	"github.com/stretchr/testify/require"
)

func TestNoteV1OneVectorAcrossDepositSpendJoinSplitBatchAndScanner(t *testing.T) {
	deposit := buildValidDepositAssignment(t, big.NewInt(7), big.NewInt(11))
	note := privacytypes.Note{
		ReceiverSpendPubKeyX: deposit.ReceiverSpendPubKey.X.(*big.Int),
		ReceiverSpendPubKeyY: deposit.ReceiverSpendPubKey.Y.(*big.Int),
		ReceiverViewPubKeyX:  deposit.ReceiverViewPubKey.X.(*big.Int),
		ReceiverViewPubKeyY:  deposit.ReceiverViewPubKey.Y.(*big.Int),
		Amount:               big.NewInt(7),
		AssetID:              big.NewInt(11),
		Randomness:           big.NewInt(13),
		Memo:                 "cross-circuit-v1",
	}
	require.NoError(t, note.ValidateV1())
	require.Zero(t, note.ComputeCommitment().Cmp(deposit.Commitment.(*big.Int)))

	spend := buildValidSpendAssignmentWithAmount(t, big.NewInt(424242), big.NewInt(7))
	require.Zero(t, note.ComputeNullifier().Cmp(spend.Nullifier.(*big.Int)))

	joinSplit := buildJoinSplitAssignmentWithNoteParameters(
		t,
		[NumInputs]*big.Int{big.NewInt(7), big.NewInt(5)},
		[NumOutputs]*big.Int{big.NewInt(6), big.NewInt(6)},
		big.NewInt(11),
		[NumInputs]*big.Int{big.NewInt(13), big.NewInt(37)},
	)
	require.Zero(t, note.ComputeNullifier().Cmp(joinSplit.Nullifiers[0].(*big.Int)))

	batch := buildBatchFeasibilityAssignment(t, 1, 1)
	batchCommitment := privacytypes.ComputeNoteCommitmentV1(
		batch.InputSpendPubKeys[0].A.X.(*big.Int), batch.InputSpendPubKeys[0].A.Y.(*big.Int),
		batch.InputViewPubKeys[0].A.X.(*big.Int), batch.InputViewPubKeys[0].A.Y.(*big.Int),
		batch.InputAmounts[0].(*big.Int), batch.AssetID.(*big.Int), batch.InputRandomness[0].(*big.Int),
	)
	batchNullifier := privacytypes.ComputeNoteNullifierV1(
		batchCommitment, batch.InputRandomness[0].(*big.Int),
		batch.InputSpendPubKeys[0].A.X.(*big.Int), batch.InputSpendPubKeys[0].A.Y.(*big.Int),
	)
	require.Zero(t, note.ComputeCommitment().Cmp(batchCommitment))
	require.Zero(t, note.ComputeNullifier().Cmp(batchNullifier))
	ccs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &BatchJoinSplit16x32FeasibilityCircuit{})
	require.NoError(t, err)
	witness, err := frontend.NewWitness(batch, ecc.BN254.ScalarField())
	require.NoError(t, err)
	_, err = ccs.Solve(witness)
	require.NoError(t, err)

	noteBytes, err := privacytypes.MarshalNotePlaintextV1(&note)
	require.NoError(t, err)
	parsed, err := privacyscan.ParseNoteBytes(noteBytes)
	require.NoError(t, err)
	require.Zero(t, note.ComputeCommitment().Cmp(parsed.ComputeCommitment()))
	require.Zero(t, note.ComputeNullifier().Cmp(parsed.ComputeNullifier()))
}

func TestNoteV1NativeAssignmentsMatchDepositSpendAndJoinSplit(t *testing.T) {
	t.Run("deposit", func(t *testing.T) {
		assignment := buildValidDepositAssignment(t, big.NewInt(7), big.NewInt(11))
		note := privacytypes.Note{
			ReceiverSpendPubKeyX: assignment.ReceiverSpendPubKey.X.(*big.Int),
			ReceiverSpendPubKeyY: assignment.ReceiverSpendPubKey.Y.(*big.Int),
			ReceiverViewPubKeyX:  assignment.ReceiverViewPubKey.X.(*big.Int),
			ReceiverViewPubKeyY:  assignment.ReceiverViewPubKey.Y.(*big.Int),
			Amount:               assignment.Amount.(*big.Int),
			AssetID:              assignment.AssetID.(*big.Int),
			Randomness:           assignment.Randomness.(*big.Int),
		}
		require.NoError(t, note.ValidateV1())
		require.Zero(t, note.ComputeCommitment().Cmp(assignment.Commitment.(*big.Int)))
	})

	t.Run("spend", func(t *testing.T) {
		assignment := buildValidSpendAssignment(t, big.NewInt(424242))
		note := privacytypes.Note{
			ReceiverSpendPubKeyX: assignment.ReceiverSpendPubKey.A.X.(*big.Int),
			ReceiverSpendPubKeyY: assignment.ReceiverSpendPubKey.A.Y.(*big.Int),
			ReceiverViewPubKeyX:  assignment.ReceiverViewPubKey.A.X.(*big.Int),
			ReceiverViewPubKeyY:  assignment.ReceiverViewPubKey.A.Y.(*big.Int),
			Amount:               assignment.Amount.(*big.Int),
			AssetID:              assignment.AssetID.(*big.Int),
			Randomness:           assignment.Randomness.(*big.Int),
		}
		require.NoError(t, note.ValidateV1())
		require.Zero(t, note.ComputeNullifier().Cmp(assignment.Nullifier.(*big.Int)))
	})

	t.Run("joinsplit input and output", func(t *testing.T) {
		assignment := buildValidJoinSplitAssignment(t)
		input := privacytypes.Note{
			ReceiverSpendPubKeyX: assignment.InputSpendPubKeys[0].A.X.(*big.Int),
			ReceiverSpendPubKeyY: assignment.InputSpendPubKeys[0].A.Y.(*big.Int),
			ReceiverViewPubKeyX:  assignment.InputViewPubKeys[0].A.X.(*big.Int),
			ReceiverViewPubKeyY:  assignment.InputViewPubKeys[0].A.Y.(*big.Int),
			Amount:               assignment.InputAmounts[0].(*big.Int),
			AssetID:              assignment.AssetID.(*big.Int),
			Randomness:           assignment.InputRandomness[0].(*big.Int),
		}
		output := privacytypes.Note{
			ReceiverSpendPubKeyX: assignment.OutputSpendPubKeys[0].A.X.(*big.Int),
			ReceiverSpendPubKeyY: assignment.OutputSpendPubKeys[0].A.Y.(*big.Int),
			ReceiverViewPubKeyX:  assignment.OutputViewPubKeys[0].A.X.(*big.Int),
			ReceiverViewPubKeyY:  assignment.OutputViewPubKeys[0].A.Y.(*big.Int),
			Amount:               assignment.OutputAmounts[0].(*big.Int),
			AssetID:              assignment.AssetID.(*big.Int),
			Randomness:           assignment.OutputRandomness[0].(*big.Int),
		}
		require.NoError(t, input.ValidateV1())
		require.NoError(t, output.ValidateV1())
		require.Zero(t, input.ComputeNullifier().Cmp(assignment.Nullifiers[0].(*big.Int)))
		require.Zero(t, output.ComputeCommitment().Cmp(assignment.Commitments[0].(*big.Int)))
	})
}
