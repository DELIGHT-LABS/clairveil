package withdraw

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

func TestProveSpendWithdrawAssignmentUsesArtifactsAndRunner(t *testing.T) {
	assignment := testSpendAssignment(t)
	artifacts := &stubSpendArtifactProvider{
		r1cs:       groth16.NewCS(ecc.BN254),
		provingKey: groth16.NewProvingKey(ecc.BN254),
	}
	runner := &stubSpendProofRunner{
		proof: groth16.NewProof(ecc.BN254),
	}

	proofBytes, err := ProveSpendWithdrawAssignment(assignment, artifacts, runner)
	require.NoError(t, err)
	require.NotEmpty(t, proofBytes)
	require.True(t, artifacts.r1csCalled)
	require.True(t, artifacts.provingKeyCalled)
	require.NotNil(t, runner.witness)
}

func TestProveSpendWithdrawAssignmentPropagatesRunnerError(t *testing.T) {
	assignment := testSpendAssignment(t)
	artifacts := &stubSpendArtifactProvider{
		r1cs:       groth16.NewCS(ecc.BN254),
		provingKey: groth16.NewProvingKey(ecc.BN254),
	}
	runner := &stubSpendProofRunner{
		err: errors.New("runner boom"),
	}

	_, err := ProveSpendWithdrawAssignment(assignment, artifacts, runner)
	require.ErrorContains(t, err, "proof generation failed")
	require.ErrorContains(t, err, "runner boom")
}

func testSpendAssignment(t *testing.T) *circuit.SpendCircuit {
	t.Helper()

	spendPubKey := testPubKey(11)
	viewPubKey := testPubKey(13)
	note := privacyscan.FoundNote{
		Note: privacytypes.Note{
			ReceiverSpendPubKeyX: pointCoordinate(spendPubKey, true),
			ReceiverSpendPubKeyY: pointCoordinate(spendPubKey, false),
			ReceiverViewPubKeyX:  pointCoordinate(viewPubKey, true),
			ReceiverViewPubKeyY:  pointCoordinate(viewPubKey, false),
			Amount:               big.NewInt(7),
			AssetID:              privacycrypto.HashString("uclair"),
			Randomness:           big.NewInt(701),
		},
	}

	rootBytes, err := privacyfield.CanonicalBytesFromBigInt(big.NewInt(909))
	require.NoError(t, err)
	commitmentHex, err := privacyfield.CanonicalHexFromBigInt(note.Note.ComputeCommitment())
	require.NoError(t, err)

	provider := &stubMerklePathProvider{
		paths: map[string]*MerklePathResult{
			commitmentHex: {
				Root:       rootBytes,
				Path:       []string{"01", "02"},
				PathHelper: []uint32{0, 1},
			},
		},
	}

	prepared, err := PrepareSpendWithdraw(
		context.Background(),
		provider,
		&stubSpendNoteHashSigner{signature: testSignatureBytes()},
		PrepareSpendWithdrawInput{
			Note:           note,
			RecipientBytes: []byte{0x01, 0x02, 0x03},
		},
	)
	require.NoError(t, err)

	return &prepared.Assignment
}

type stubSpendArtifactProvider struct {
	r1cs             constraint.ConstraintSystem
	provingKey       groth16.ProvingKey
	r1csCalled       bool
	provingKeyCalled bool
}

func (s *stubSpendArtifactProvider) SpendR1CS() (constraint.ConstraintSystem, error) {
	s.r1csCalled = true
	return s.r1cs, nil
}

func (s *stubSpendArtifactProvider) SpendProvingKey() (groth16.ProvingKey, error) {
	s.provingKeyCalled = true
	return s.provingKey, nil
}

type stubSpendProofRunner struct {
	proof   groth16.Proof
	err     error
	witness witness.Witness
}

func (s *stubSpendProofRunner) ProveSpend(_ constraint.ConstraintSystem, _ groth16.ProvingKey, witness witness.Witness) (groth16.Proof, error) {
	s.witness = witness
	if s.err != nil {
		return nil, s.err
	}
	return s.proof, nil
}
