package withdraw

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	privacyscan "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/scan"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestBuildPreparedWithdrawProverPayloadAndProofRoundTrip(t *testing.T) {
	input, source, planner, merklePaths, signer, artifacts, runner := testBuildPreparedWithdrawProverPayloadDeps(t)

	result, err := BuildPreparedWithdrawProverPayload(context.Background(), source, planner, merklePaths, signer, input)
	require.NoError(t, err)
	require.NotNil(t, result.Payload)
	require.NoError(t, ValidatePreparedWithdrawProverPayloadMetadata(*result.Payload, time.Now()))
	require.Equal(t, input.TargetCoin.Amount.String(), result.Payload.Amount)
	require.Equal(t, input.TargetCoin.Denom, result.Payload.AssetDenom)
	require.Equal(t, input.Recipient.String(), result.Payload.Recipient)
	require.Equal(t, input.ChainID, result.Payload.ChainID)

	proof, err := BuildPreparedWithdrawProof(*result.Payload, artifacts, runner)
	require.NoError(t, err)
	require.NoError(t, ValidatePreparedWithdrawProof(*result.Payload, *proof, time.Now()))
	require.Equal(t, result.Payload.PayloadHash, proof.PayloadHash)

	finalPayload, err := result.Payload.ToPreparedWithdrawPayload(*proof, time.Now())
	require.NoError(t, err)
	require.NoError(t, ValidatePreparedWithdrawPayloadMetadata(*finalPayload, time.Now()))
	require.Equal(t, input.TargetCoin.String(), finalPayload.Amount)
	require.Equal(t, input.Recipient.String(), finalPayload.Recipient)
}

func TestValidatePreparedWithdrawProverPayloadMetadataRejectsHashMismatch(t *testing.T) {
	input, source, planner, merklePaths, signer, _, _ := testBuildPreparedWithdrawProverPayloadDeps(t)

	result, err := BuildPreparedWithdrawProverPayload(context.Background(), source, planner, merklePaths, signer, input)
	require.NoError(t, err)

	result.Payload.Recipient = testBech32AddressWithByte(0x3)
	err = ValidatePreparedWithdrawProverPayloadMetadata(*result.Payload, time.Now())
	require.ErrorContains(t, err, "hash mismatch")
}

func TestProvePreparedWithdrawPayloadRejectsMismatchedNullifier(t *testing.T) {
	input, source, planner, merklePaths, signer, artifacts, runner := testBuildPreparedWithdrawProverPayloadDeps(t)

	result, err := BuildPreparedWithdrawProverPayload(context.Background(), source, planner, merklePaths, signer, input)
	require.NoError(t, err)

	result.Payload.NullifierHex = result.Payload.RootHex
	result.Payload.PayloadHash = ComputePreparedWithdrawProverPayloadHash(*result.Payload)

	_, err = ProvePreparedWithdrawPayload(*result.Payload, artifacts, runner)
	require.ErrorContains(t, err, "nullifier does not match payload witness")
}

func TestParseWithdrawAmountRequiresPositiveCanonicalShieldedAmount(t *testing.T) {
	maxAmount := privacytypes.MaxShieldedAmount()
	maxPlusOne := new(big.Int).Add(maxAmount, big.NewInt(1))

	for _, value := range []string{"1", maxAmount.String()} {
		parsed, err := parseWithdrawAmount(value)
		require.NoError(t, err)
		require.Equal(t, value, parsed.String())
	}

	for _, value := range []string{"", "0", "01", "+1", " 1", "1 ", "-1", maxPlusOne.String()} {
		_, err := parseWithdrawAmount(value)
		require.Error(t, err, value)
	}
}

func TestPreparedWithdrawProverPayloadAndProofJSONRoundTrip(t *testing.T) {
	input, source, planner, merklePaths, signer, artifacts, runner := testBuildPreparedWithdrawProverPayloadDeps(t)

	result, err := BuildPreparedWithdrawProverPayload(context.Background(), source, planner, merklePaths, signer, input)
	require.NoError(t, err)
	proof, err := BuildPreparedWithdrawProof(*result.Payload, artifacts, runner)
	require.NoError(t, err)

	payloadJSON, err := result.Payload.MarshalIndentedJSON()
	require.NoError(t, err)
	decodedPayload, err := DecodePreparedWithdrawProverPayloadJSON(payloadJSON)
	require.NoError(t, err)
	require.Equal(t, result.Payload.PayloadHash, decodedPayload.PayloadHash)

	proofJSON, err := proof.MarshalIndentedJSON()
	require.NoError(t, err)
	decodedProof, err := DecodePreparedWithdrawProofJSON(proofJSON)
	require.NoError(t, err)
	require.Equal(t, proof.PayloadHash, decodedProof.PayloadHash)

	finalPayload, err := decodedPayload.ToPreparedWithdrawPayload(*decodedProof, time.Now())
	require.NoError(t, err)
	require.NoError(t, ValidatePreparedWithdrawPayloadMetadata(*finalPayload, time.Now()))
}

func testBuildPreparedWithdrawProverPayloadDeps(
	t *testing.T,
) (
	BuildWithdrawPayloadInput,
	*stubExactMatchNoteSource,
	*stubExactMatchAutoPlanner,
	*stubMerklePathProvider,
	*stubSpendNoteHashSigner,
	*stubSpendArtifactProvider,
	*stubSpendProofRunner,
) {
	t.Helper()

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

	return BuildWithdrawPayloadInput{
			TargetCoin: sdk.NewInt64Coin("uclair", 10),
			Recipient:  recipient,
			ChainID:    "clairveil-local-1",
			ExpiresAt:  time.Now().Add(time.Hour),
			AutoPlan:   false,
		},
		source,
		planner,
		merklePaths,
		signer,
		artifacts,
		runner
}
