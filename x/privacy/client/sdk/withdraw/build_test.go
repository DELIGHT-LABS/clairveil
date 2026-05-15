package withdraw

import (
	"context"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
	privacyscan "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/scan"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestBuildWithdrawPayloadBuildsPreparedPayload(t *testing.T) {
	selectedNote := testBuildWithdrawFoundNote(10, "uclair", 701)
	rootBytes, commitmentHex := testBuildWithdrawMerklePath(t, selectedNote)
	recipient, err := sdk.AccAddressFromBech32(testBech32Address())
	require.NoError(t, err)

	source := &stubExactMatchNoteSource{
		responses: [][]privacyscan.FoundNote{{selectedNote}},
	}
	planner := &stubExactMatchAutoPlanner{}
	merklePaths := &stubMerklePathProvider{
		paths: map[string]*MerklePathResult{
			commitmentHex: {
				Root:       rootBytes,
				Path:       []string{"01", "02"},
				PathHelper: []uint32{0, 1},
			},
		},
	}
	signer := &stubSpendNoteHashSigner{signature: testSignatureBytes()}
	artifacts := &stubSpendArtifactProvider{
		r1cs:       groth16.NewCS(ecc.BN254),
		provingKey: groth16.NewProvingKey(ecc.BN254),
	}
	runner := &stubSpendProofRunner{
		proof: groth16.NewProof(ecc.BN254),
	}

	result, err := BuildWithdrawPayload(
		context.Background(),
		source,
		planner,
		merklePaths,
		signer,
		artifacts,
		runner,
		BuildWithdrawPayloadInput{
			TargetCoin: sdk.NewInt64Coin("uclair", 10),
			Recipient:  recipient,
			ChainID:    "clairveil-local-1",
			ExpiresAt:  time.Now().Add(time.Hour),
			AutoPlan:   false,
		},
	)
	require.NoError(t, err)
	require.Equal(t, int64(10), result.SelectedNote.Note.Amount.Int64())
	require.NotNil(t, result.Payload)
	require.Equal(t, "10uclair", result.Payload.Amount)
	require.Equal(t, recipient.String(), result.Payload.Recipient)
	require.Equal(t, "clairveil-local-1", result.Payload.ChainID)
	require.Len(t, source.calls, 1)
	require.Len(t, planner.calls, 0)
	require.Len(t, merklePaths.requests, 1)
	require.True(t, artifacts.r1csCalled)
	require.True(t, artifacts.provingKeyCalled)
	require.NotNil(t, runner.witness)
}

func TestBuildWithdrawPayloadAutoPlansAndRescans(t *testing.T) {
	initialNote := testBuildWithdrawFoundNote(7, "uclair", 701)
	selectedNote := testBuildWithdrawFoundNote(10, "uclair", 703)
	rootBytes, commitmentHex := testBuildWithdrawMerklePath(t, selectedNote)
	recipient, err := sdk.AccAddressFromBech32(testBech32Address())
	require.NoError(t, err)

	source := &stubExactMatchNoteSource{
		responses: [][]privacyscan.FoundNote{
			{initialNote},
			{selectedNote},
		},
	}
	planner := &stubExactMatchAutoPlanner{}
	merklePaths := &stubMerklePathProvider{
		paths: map[string]*MerklePathResult{
			commitmentHex: {
				Root:       rootBytes,
				Path:       []string{"01", "02"},
				PathHelper: []uint32{0, 1},
			},
		},
	}

	result, err := BuildWithdrawPayload(
		context.Background(),
		source,
		planner,
		merklePaths,
		&stubSpendNoteHashSigner{signature: testSignatureBytes()},
		&stubSpendArtifactProvider{
			r1cs:       groth16.NewCS(ecc.BN254),
			provingKey: groth16.NewProvingKey(ecc.BN254),
		},
		&stubSpendProofRunner{
			proof: groth16.NewProof(ecc.BN254),
		},
		BuildWithdrawPayloadInput{
			TargetCoin: sdk.NewInt64Coin("uclair", 10),
			Recipient:  recipient,
			ChainID:    "clairveil-local-1",
			ExpiresAt:  time.Now().Add(time.Hour),
			AutoPlan:   true,
		},
	)
	require.NoError(t, err)
	require.Equal(t, int64(10), result.SelectedNote.Note.Amount.Int64())
	require.Len(t, source.calls, 2)
	require.Len(t, planner.calls, 1)
}

func TestBuildWithdrawPayloadRejectsMissingRecipient(t *testing.T) {
	_, err := BuildWithdrawPayload(
		context.Background(),
		&stubExactMatchNoteSource{},
		&stubExactMatchAutoPlanner{},
		&stubMerklePathProvider{},
		&stubSpendNoteHashSigner{signature: testSignatureBytes()},
		&stubSpendArtifactProvider{
			r1cs:       groth16.NewCS(ecc.BN254),
			provingKey: groth16.NewProvingKey(ecc.BN254),
		},
		&stubSpendProofRunner{
			proof: groth16.NewProof(ecc.BN254),
		},
		BuildWithdrawPayloadInput{
			TargetCoin: sdk.NewInt64Coin("uclair", 10),
			ChainID:    "clairveil-local-1",
			ExpiresAt:  time.Now().Add(time.Hour),
		},
	)
	require.ErrorContains(t, err, "recipient address is required to build a withdraw payload")
}

func TestBuildWithdrawPayloadPropagatesProofError(t *testing.T) {
	selectedNote := testBuildWithdrawFoundNote(10, "uclair", 705)
	rootBytes, commitmentHex := testBuildWithdrawMerklePath(t, selectedNote)
	recipient, err := sdk.AccAddressFromBech32(testBech32Address())
	require.NoError(t, err)

	_, err = BuildWithdrawPayload(
		context.Background(),
		&stubExactMatchNoteSource{
			responses: [][]privacyscan.FoundNote{{selectedNote}},
		},
		&stubExactMatchAutoPlanner{},
		&stubMerklePathProvider{
			paths: map[string]*MerklePathResult{
				commitmentHex: {
					Root:       rootBytes,
					Path:       []string{"01", "02"},
					PathHelper: []uint32{0, 1},
				},
			},
		},
		&stubSpendNoteHashSigner{signature: testSignatureBytes()},
		&stubSpendArtifactProvider{
			r1cs:       groth16.NewCS(ecc.BN254),
			provingKey: groth16.NewProvingKey(ecc.BN254),
		},
		&stubSpendProofRunner{err: fmt.Errorf("boom")},
		BuildWithdrawPayloadInput{
			TargetCoin: sdk.NewInt64Coin("uclair", 10),
			Recipient:  recipient,
			ChainID:    "clairveil-local-1",
			ExpiresAt:  time.Now().Add(time.Hour),
			AutoPlan:   false,
		},
	)
	require.ErrorContains(t, err, "spend proof generation failed")
	require.ErrorContains(t, err, "boom")
}

func testBuildWithdrawFoundNote(amount int64, denom string, randomness int64) privacyscan.FoundNote {
	spendPubKey := testPubKey(31)
	viewPubKey := testPubKey(37)
	return privacyscan.FoundNote{
		Note: privacytypes.Note{
			ReceiverSpendPubKeyX: pointCoordinate(spendPubKey, true),
			ReceiverSpendPubKeyY: pointCoordinate(spendPubKey, false),
			ReceiverViewPubKeyX:  pointCoordinate(viewPubKey, true),
			ReceiverViewPubKeyY:  pointCoordinate(viewPubKey, false),
			Amount:               big.NewInt(amount),
			AssetID:              privacycrypto.HashString(denom),
			Randomness:           big.NewInt(randomness),
		},
	}
}

func testBuildWithdrawMerklePath(t *testing.T, note privacyscan.FoundNote) ([]byte, string) {
	t.Helper()

	rootBytes, err := privacyfield.CanonicalBytesFromBigInt(big.NewInt(909))
	require.NoError(t, err)
	commitmentHex, err := privacyfield.CanonicalHexFromBigInt(note.Note.ComputeCommitment())
	require.NoError(t, err)
	return rootBytes, commitmentHex
}
