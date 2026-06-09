package transfer

import (
	"context"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestBuildPreparedTransferPayloadAndProofRoundTrip(t *testing.T) {
	input, merkleProvider, signer, artifacts, runner := testBuildTransferMessageDeps(t)

	payload, err := BuildPreparedTransferPayload(context.Background(), merkleProvider, signer, input)
	require.NoError(t, err)
	require.NotEmpty(t, payload.PayloadHash)
	require.Len(t, payload.Inputs, 2)
	require.Len(t, payload.Outputs, 2)
	require.Len(t, payload.CipherTextHexes, 2)
	require.NoError(t, ValidatePreparedTransferPayloadMetadata(*payload))

	proof, err := BuildPreparedTransferProof(*payload, artifacts, runner)
	require.NoError(t, err)
	require.NoError(t, ValidatePreparedTransferProof(*payload, *proof))
	require.Equal(t, payload.PayloadHash, proof.PayloadHash)
	require.NotEmpty(t, proof.ProofHex)

	msg, err := payload.ToMsg(*proof)
	require.NoError(t, err)
	require.NoError(t, msg.ValidateBasic())
	require.Equal(t, payload.Creator, msg.Creator)
	require.Equal(t, payload.UserPrivacyPolicy, msg.UserPrivacyPolicy)
	require.Equal(t, int32(msg.UserDisclosureMode), payload.UserDisclosureMode)
}

func TestValidatePreparedTransferPayloadMetadataRejectsHashMismatch(t *testing.T) {
	input, merkleProvider, signer, _, _ := testBuildTransferMessageDeps(t)

	payload, err := BuildPreparedTransferPayload(context.Background(), merkleProvider, signer, input)
	require.NoError(t, err)

	payload.Creator = "clair1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq08l9p7"
	err = ValidatePreparedTransferPayloadMetadata(*payload)
	require.ErrorContains(t, err, "hash mismatch")
}

func TestProvePreparedTransferPayloadRejectsMismatchedCommitment(t *testing.T) {
	input, merkleProvider, signer, artifacts, runner := testBuildTransferMessageDeps(t)

	payload, err := BuildPreparedTransferPayload(context.Background(), merkleProvider, signer, input)
	require.NoError(t, err)

	payload.Outputs[0].CommitmentHex = payload.RootHex
	payload.PayloadHash = ComputePreparedTransferPayloadHash(*payload)

	_, err = ProvePreparedTransferPayload(*payload, artifacts, runner)
	require.ErrorContains(t, err, "output commitment 0 does not match payload witness")
}

func TestParseDecimalFieldRequiresCanonicalShieldedAmount(t *testing.T) {
	maxAmount := privacytypes.MaxShieldedAmount()
	maxPlusOne := new(big.Int).Add(maxAmount, big.NewInt(1))

	for _, value := range []string{"0", "1", maxAmount.String()} {
		parsed, err := parseDecimalField(value, "input amount")
		require.NoError(t, err)
		require.Equal(t, value, parsed.String())
	}

	for _, value := range []string{"", "01", "+1", " 1", "1 ", "-1", maxPlusOne.String()} {
		_, err := parseDecimalField(value, "input amount")
		require.Error(t, err, value)
	}
}

func TestPreparedTransferPayloadAndProofJSONRoundTrip(t *testing.T) {
	input, merkleProvider, signer, artifacts, runner := testBuildTransferMessageDeps(t)

	payload, err := BuildPreparedTransferPayload(context.Background(), merkleProvider, signer, input)
	require.NoError(t, err)
	proof, err := BuildPreparedTransferProof(*payload, artifacts, runner)
	require.NoError(t, err)

	payloadJSON, err := payload.MarshalIndentedJSON()
	require.NoError(t, err)
	decodedPayload, err := DecodePreparedTransferPayloadJSON(payloadJSON)
	require.NoError(t, err)
	require.Equal(t, payload.PayloadHash, decodedPayload.PayloadHash)

	proofJSON, err := proof.MarshalIndentedJSON()
	require.NoError(t, err)
	decodedProof, err := DecodePreparedTransferProofJSON(proofJSON)
	require.NoError(t, err)
	require.Equal(t, proof.PayloadHash, decodedProof.PayloadHash)

	msg, err := decodedPayload.ToMsg(*decodedProof)
	require.NoError(t, err)
	require.NoError(t, msg.ValidateBasic())
}
