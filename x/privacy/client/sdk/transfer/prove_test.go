package transfer

import (
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/backend/witness"
	"github.com/consensys/gnark/constraint"
	"github.com/stretchr/testify/require"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/circuit"
	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
	privacyscan "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/scan"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestProveJoinSplitAssignmentUsesArtifactsAndRunner(t *testing.T) {
	assignment := testJoinSplitAssignment(t)
	artifacts := &stubJoinSplitArtifactProvider{
		r1cs:       groth16.NewCS(ecc.BN254),
		provingKey: groth16.NewProvingKey(ecc.BN254),
	}
	runner := &stubJoinSplitProofRunner{
		proof: groth16.NewProof(ecc.BN254),
	}

	proofBytes, err := ProveJoinSplitAssignment(assignment, artifacts, runner)
	require.NoError(t, err)
	require.NotEmpty(t, proofBytes)
	require.True(t, artifacts.r1csCalled)
	require.True(t, artifacts.provingKeyCalled)
	require.NotNil(t, runner.witness)
}

func TestProveJoinSplitAssignmentPropagatesRunnerError(t *testing.T) {
	assignment := testJoinSplitAssignment(t)
	artifacts := &stubJoinSplitArtifactProvider{
		r1cs:       groth16.NewCS(ecc.BN254),
		provingKey: groth16.NewProvingKey(ecc.BN254),
	}
	runner := &stubJoinSplitProofRunner{
		err: errors.New("runner boom"),
	}

	_, err := ProveJoinSplitAssignment(assignment, artifacts, runner)
	require.ErrorContains(t, err, "proof generation failed")
	require.ErrorContains(t, err, "runner boom")
}

func testJoinSplitAssignment(t *testing.T) *circuit.JoinSplitCircuit {
	t.Helper()

	senderSpendScalar, senderSpendPubKey := testScalarAndPubKey(109)
	senderViewScalar, senderViewPubKey := testScalarAndPubKey(113)
	recipientSpendScalar, recipientSpendPubKey := testScalarAndPubKey(127)
	recipientViewScalar, recipientViewPubKey := testScalarAndPubKey(131)

	inputs := [2]privacyscan.FoundNote{
		{
			Note: privacytypes.Note{
				ReceiverSpendPubKeyX: pointCoordinate(senderSpendPubKey, true),
				ReceiverSpendPubKeyY: pointCoordinate(senderSpendPubKey, false),
				ReceiverViewPubKeyX:  pointCoordinate(senderViewPubKey, true),
				ReceiverViewPubKeyY:  pointCoordinate(senderViewPubKey, false),
				Amount:               big.NewInt(8),
				AssetID:              privacycrypto.HashString("uclair"),
				Randomness:           big.NewInt(901),
			},
		},
		{
			Note: privacytypes.Note{
				ReceiverSpendPubKeyX: pointCoordinate(senderSpendPubKey, true),
				ReceiverSpendPubKeyY: pointCoordinate(senderSpendPubKey, false),
				ReceiverViewPubKeyX:  pointCoordinate(senderViewPubKey, true),
				ReceiverViewPubKeyY:  pointCoordinate(senderViewPubKey, false),
				Amount:               big.NewInt(4),
				AssetID:              privacycrypto.HashString("uclair"),
				Randomness:           big.NewInt(902),
			},
		},
	}

	rootBytes, err := privacyfield.CanonicalBytesFromBigInt(big.NewInt(1201))
	require.NoError(t, err)

	merkleProvider := &stubMerklePathProvider{paths: map[string]*MerklePathResult{}}
	for _, input := range inputs {
		commitmentHex, err := privacyfield.CanonicalHexFromBigInt(input.Note.ComputeCommitment())
		require.NoError(t, err)
		merkleProvider.paths[commitmentHex] = &MerklePathResult{
			Root:       rootBytes,
			Path:       []string{"01", "02"},
			PathHelper: []uint32{0, 1},
		}
	}

	prepared, err := PrepareJoinSplitTransfer(
		context.Background(),
		merkleProvider,
		&stubNoteHashSigner{signature: testSignatureBytes(t)},
		PrepareJoinSplitInput{
			Inputs:               inputs,
			RecipientSpendPubKey: recipientSpendPubKey,
			RecipientViewPubKey:  recipientViewPubKey,
			TransferAmount:       big.NewInt(8),
			SenderSpendPubKey:    senderSpendPubKey,
			SenderViewPubKey:     senderViewPubKey,
		},
	)
	require.NoError(t, err)

	require.NotNil(t, senderSpendScalar)
	require.NotNil(t, senderViewScalar)
	require.NotNil(t, recipientSpendScalar)
	require.NotNil(t, recipientViewScalar)

	prepared.Assignment.UserPrivacyPolicy = big.NewInt(0)
	prepared.Assignment.UserDisclosureDigest = big.NewInt(0)
	prepared.Assignment.AuditDisclosureDigest = big.NewInt(0)

	return &prepared.Assignment
}

type stubJoinSplitArtifactProvider struct {
	r1cs             constraint.ConstraintSystem
	provingKey       groth16.ProvingKey
	r1csCalled       bool
	provingKeyCalled bool
}

func (s *stubJoinSplitArtifactProvider) JoinSplitR1CS() (constraint.ConstraintSystem, error) {
	s.r1csCalled = true
	return s.r1cs, nil
}

func (s *stubJoinSplitArtifactProvider) JoinSplitProvingKey() (groth16.ProvingKey, error) {
	s.provingKeyCalled = true
	return s.provingKey, nil
}

type stubJoinSplitProofRunner struct {
	proof   groth16.Proof
	err     error
	witness witness.Witness
}

func (s *stubJoinSplitProofRunner) ProveJoinSplit(_ constraint.ConstraintSystem, _ groth16.ProvingKey, witness witness.Witness) (groth16.Proof, error) {
	s.witness = witness
	if s.err != nil {
		return nil, s.err
	}
	return s.proof, nil
}
