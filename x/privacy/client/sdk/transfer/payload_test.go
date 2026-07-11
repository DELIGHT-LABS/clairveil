package transfer

import (
	"bytes"
	"context"
	"encoding/hex"
	"math/big"
	"testing"
	"time"

	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/circuit"
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
	require.Len(t, payload.ViewTagHexes, 2)
	require.NotEmpty(t, payload.SelfViewDisclosureDigestHex)
	require.NotEmpty(t, payload.SelfViewDisclosurePayloadHex)
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
	require.NotEmpty(t, msg.SelfViewDisclosureDigest)
	require.NotEmpty(t, msg.SelfViewDisclosurePayload)
	require.Len(t, msg.ViewTags, 2)
}

func TestValidatePreparedTransferProofRejectsEmptyProof(t *testing.T) {
	input, merkleProvider, signer, artifacts, runner := testBuildTransferMessageDeps(t)
	payload, err := BuildPreparedTransferPayload(context.Background(), merkleProvider, signer, input)
	require.NoError(t, err)
	proof, err := BuildPreparedTransferProof(*payload, artifacts, runner)
	require.NoError(t, err)

	proof.ProofHex = ""
	err = ValidatePreparedTransferProof(*payload, *proof)
	require.ErrorContains(t, err, "proof must be exactly")
}

func TestBuildPreparedTransferProofRejectsExpiredPayloadBeforeProving(t *testing.T) {
	input, merkleProvider, signer, artifacts, runner := testBuildTransferMessageDeps(t)
	payload, err := BuildPreparedTransferPayload(context.Background(), merkleProvider, signer, input)
	require.NoError(t, err)
	now := time.Unix(payload.ExpiresAtUnix, 0)

	_, err = BuildPreparedTransferProofAt(*payload, artifacts, runner, now)
	require.ErrorContains(t, err, "expired")
}

func TestBuildPreparedTransferProofRejectsPayloadThatExpiresDuringProving(t *testing.T) {
	input, merkleProvider, signer, artifacts, runner := testBuildTransferMessageDeps(t)
	payload, err := BuildPreparedTransferPayload(context.Background(), merkleProvider, signer, input)
	require.NoError(t, err)
	clockCalls := 0
	now := func() time.Time {
		clockCalls++
		if clockCalls == 1 {
			return time.Unix(payload.ExpiresAtUnix-1, 0)
		}
		return time.Unix(payload.ExpiresAtUnix, 0)
	}

	_, err = buildPreparedTransferProofWithClock(*payload, artifacts, runner, now)
	require.ErrorContains(t, err, "expired")
	require.Equal(t, 2, clockCalls)
}

func TestPreparedTransferPayloadToMsgRejectsExpiredProof(t *testing.T) {
	input, merkleProvider, signer, artifacts, runner := testBuildTransferMessageDeps(t)
	payload, err := BuildPreparedTransferPayload(context.Background(), merkleProvider, signer, input)
	require.NoError(t, err)
	proof, err := BuildPreparedTransferProofAt(*payload, artifacts, runner, time.Unix(payload.ExpiresAtUnix-1, 0))
	require.NoError(t, err)

	_, err = payload.ToMsgAt(*proof, time.Unix(payload.ExpiresAtUnix, 0))
	require.ErrorContains(t, err, "expired")
}

func TestValidatePreparedTransferPayloadMetadataRejectsHashMismatch(t *testing.T) {
	input, merkleProvider, signer, _, _ := testBuildTransferMessageDeps(t)

	payload, err := BuildPreparedTransferPayload(context.Background(), merkleProvider, signer, input)
	require.NoError(t, err)

	payload.Creator = sdk.AccAddress(bytes.Repeat([]byte{0x2}, 20)).String()
	err = ValidatePreparedTransferPayloadMetadata(*payload)
	require.ErrorContains(t, err, "hash mismatch")
}

func TestValidatePreparedTransferPayloadMetadataBindsViewTagsToHash(t *testing.T) {
	input, merkleProvider, signer, _, _ := testBuildTransferMessageDeps(t)

	payload, err := BuildPreparedTransferPayload(context.Background(), merkleProvider, signer, input)
	require.NoError(t, err)
	require.Len(t, payload.ViewTagHexes, 2)

	payload.ViewTagHexes[0] = "ffff"
	err = ValidatePreparedTransferPayloadMetadata(*payload)
	require.ErrorContains(t, err, "hash mismatch")
}

func TestPreparedTransferPayloadRejectsNonCanonicalPublicKeys(t *testing.T) {
	input, merkleProvider, signer, _, _ := testBuildTransferMessageDeps(t)
	base, err := BuildPreparedTransferPayload(context.Background(), merkleProvider, signer, input)
	require.NoError(t, err)

	identity := crypto_tedwards.PointAffine{}
	identity.Y.SetOne()
	identityBytes := identity.Bytes()

	orderTwo := crypto_tedwards.PointAffine{}
	orderTwo.Y.SetOne()
	orderTwo.Y.Neg(&orderTwo.Y)
	require.True(t, orderTwo.IsOnCurve())
	require.False(t, orderTwo.IsZero())
	orderTwoBytes := orderTwo.Bytes()

	invalidEncodings := []struct {
		name   string
		mutate func(string) string
		want   string
	}{
		{
			name: "non-canonical",
			mutate: func(value string) string {
				return nonCanonicalPreparedTransferPointHex(t, value)
			},
			want: "non-canonical compressed encoding",
		},
		{
			name:   "identity",
			mutate: func(string) string { return hex.EncodeToString(identityBytes[:]) },
			want:   "identity point is not allowed",
		},
		{
			name:   "non-subgroup",
			mutate: func(string) string { return hex.EncodeToString(orderTwoBytes[:]) },
			want:   "point is not in the prime-order subgroup",
		},
		{
			name:   "oversized",
			mutate: func(value string) string { return value + "00" },
			want:   "expected exactly 32 bytes",
		},
	}

	keyLocations := []struct {
		name string
		get  func(*PreparedTransferPayload) string
		set  func(*PreparedTransferPayload, string)
	}{
		{
			name: "input-spend",
			get:  func(payload *PreparedTransferPayload) string { return payload.Inputs[0].SpendPubKeyHex },
			set:  func(payload *PreparedTransferPayload, value string) { payload.Inputs[0].SpendPubKeyHex = value },
		},
		{
			name: "input-view",
			get:  func(payload *PreparedTransferPayload) string { return payload.Inputs[0].ViewPubKeyHex },
			set:  func(payload *PreparedTransferPayload, value string) { payload.Inputs[0].ViewPubKeyHex = value },
		},
		{
			name: "output-spend",
			get:  func(payload *PreparedTransferPayload) string { return payload.Outputs[0].SpendPubKeyHex },
			set:  func(payload *PreparedTransferPayload, value string) { payload.Outputs[0].SpendPubKeyHex = value },
		},
		{
			name: "output-view",
			get:  func(payload *PreparedTransferPayload) string { return payload.Outputs[0].ViewPubKeyHex },
			set:  func(payload *PreparedTransferPayload, value string) { payload.Outputs[0].ViewPubKeyHex = value },
		},
	}

	for _, location := range keyLocations {
		for _, invalid := range invalidEncodings {
			t.Run(location.name+"/"+invalid.name, func(t *testing.T) {
				payload := clonePreparedTransferPayloadForTest(*base)
				location.set(&payload, invalid.mutate(location.get(&payload)))
				payload.PayloadHash = ComputePreparedTransferPayloadHash(payload)

				err := ValidatePreparedTransferPayloadMetadata(payload)
				require.ErrorContains(t, err, invalid.want)
				_, err = buildJoinSplitAssignmentFromPreparedTransferPayload(payload)
				require.ErrorContains(t, err, invalid.want)
			})
		}
	}
}

func TestBuildPreparedTransferPayloadCanDisableSelfViewDisclosure(t *testing.T) {
	input, merkleProvider, signer, _, _ := testBuildTransferMessageDeps(t)
	input.DisableSelfViewDisclosure = true
	input.SelfViewDisclosureTargetPubKey = nil

	payload, err := BuildPreparedTransferPayload(context.Background(), merkleProvider, signer, input)
	require.NoError(t, err)
	require.Empty(t, payload.SelfViewDisclosureDigestHex)
	require.Empty(t, payload.SelfViewDisclosurePayloadHex)
	require.NoError(t, ValidatePreparedTransferPayloadMetadata(*payload))
}

func clonePreparedTransferPayloadForTest(payload PreparedTransferPayload) PreparedTransferPayload {
	payload.Inputs = append([]PreparedTransferInput(nil), payload.Inputs...)
	payload.Outputs = append([]PreparedTransferOutput(nil), payload.Outputs...)
	return payload
}

func nonCanonicalPreparedTransferPointHex(t *testing.T, canonicalHex string) string {
	t.Helper()
	canonical, err := hex.DecodeString(canonicalHex)
	require.NoError(t, err)

	var point crypto_tedwards.PointAffine
	_, err = point.SetBytes(canonical)
	require.NoError(t, err)
	y := point.Y.BigInt(new(big.Int))
	y.Add(y, fr.Modulus())
	require.Less(t, y.BitLen(), 256)

	encoded := make([]byte, fr.Bytes)
	y.FillBytes(encoded)
	for i, j := 0, len(encoded)-1; i < j; i, j = i+1, j-1 {
		encoded[i], encoded[j] = encoded[j], encoded[i]
	}
	if point.X.LexicographicallyLargest() {
		encoded[len(encoded)-1] |= 0x80
	}
	return hex.EncodeToString(encoded)
}

func TestValidatePreparedTransferPayloadMetadataRejectsLegacyV1WithoutViewTags(t *testing.T) {
	input, merkleProvider, signer, _, _ := testBuildTransferMessageDeps(t)

	payload, err := BuildPreparedTransferPayload(context.Background(), merkleProvider, signer, input)
	require.NoError(t, err)

	payload.Version = legacyPreparedTransferPayloadVersionV1
	payload.SelfViewDisclosureDigestHex = ""
	payload.SelfViewDisclosurePayloadHex = ""
	payload.ViewTagHexes = nil
	payload.PayloadHash = ComputePreparedTransferPayloadHash(*payload)
	err = ValidatePreparedTransferPayloadMetadata(*payload)
	require.ErrorContains(t, err, "legacy transfer payload version")
	require.ErrorContains(t, err, "regenerate it with transfer payload version")
}

func TestValidatePreparedTransferPayloadMetadataRejectsLegacyV1WithSelfView(t *testing.T) {
	input, merkleProvider, signer, _, _ := testBuildTransferMessageDeps(t)

	payload, err := BuildPreparedTransferPayload(context.Background(), merkleProvider, signer, input)
	require.NoError(t, err)

	payload.Version = legacyPreparedTransferPayloadVersionV1
	payload.PayloadHash = ComputePreparedTransferPayloadHash(*payload)

	err = ValidatePreparedTransferPayloadMetadata(*payload)
	require.ErrorContains(t, err, "legacy transfer payload version")
	require.ErrorContains(t, err, "regenerate it with transfer payload version")
}

func TestValidatePreparedTransferPayloadMetadataRejectsLegacyV2WithoutViewTags(t *testing.T) {
	input, merkleProvider, signer, _, _ := testBuildTransferMessageDeps(t)

	payload, err := BuildPreparedTransferPayload(context.Background(), merkleProvider, signer, input)
	require.NoError(t, err)

	payload.Version = legacyPreparedTransferPayloadVersionV2
	payload.ViewTagHexes = nil
	payload.PayloadHash = ComputePreparedTransferPayloadHash(*payload)

	err = ValidatePreparedTransferPayloadMetadata(*payload)
	require.ErrorContains(t, err, "legacy transfer payload version")
	require.ErrorContains(t, err, "regenerate it with transfer payload version")
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

func TestPreparedTransferOwnerIntentBindsFinalPayloadAndExcludesCreator(t *testing.T) {
	input, merkleProvider, signer, _, _ := testBuildTransferMessageDeps(t)
	payload, err := BuildPreparedTransferPayload(context.Background(), merkleProvider, signer, input)
	require.NoError(t, err)
	require.Len(t, signer.hashes, 1)

	assignment, err := buildJoinSplitAssignmentFromPreparedTransferPayload(*payload)
	require.NoError(t, err)
	originalIntent, err := transferIntentFromAssignment(assignment)
	require.NoError(t, err)
	require.Equal(t, 0, signer.hashes[0].Cmp(originalIntent))

	originalMessage, err := payload.transferEffectMessage(nil)
	require.NoError(t, err)
	originalDigest, err := privacytypes.ComputeTransferPayloadDigestV1(originalMessage)
	require.NoError(t, err)

	payload.Creator = sdk.AccAddress(bytes.Repeat([]byte{0x3}, 20)).String()
	payload.PayloadHash = ComputePreparedTransferPayloadHash(*payload)
	relayerMessage, err := payload.transferEffectMessage(nil)
	require.NoError(t, err)
	relayerDigest, err := privacytypes.ComputeTransferPayloadDigestV1(relayerMessage)
	require.NoError(t, err)
	require.Equal(t, originalDigest, relayerDigest)

	last := len(payload.CipherTextHexes[0]) - 1
	replacement := "0"
	if payload.CipherTextHexes[0][last] == '0' {
		replacement = "1"
	}
	payload.CipherTextHexes[0] = payload.CipherTextHexes[0][:last] + replacement
	payload.PayloadHash = ComputePreparedTransferPayloadHash(*payload)
	mutatedAssignment, err := buildJoinSplitAssignmentFromPreparedTransferPayload(*payload)
	require.NoError(t, err)
	mutatedIntent, err := transferIntentFromAssignment(mutatedAssignment)
	require.NoError(t, err)
	require.NotEqual(t, 0, signer.hashes[0].Cmp(mutatedIntent))
}

func TestPreparedTransferRejectsMalformedOwnerSignature(t *testing.T) {
	input, merkleProvider, signer, _, _ := testBuildTransferMessageDeps(t)
	payload, err := BuildPreparedTransferPayload(context.Background(), merkleProvider, signer, input)
	require.NoError(t, err)

	payload.OwnerSignatureHex = payload.OwnerSignatureHex[:len(payload.OwnerSignatureHex)-2]
	payload.PayloadHash = ComputePreparedTransferPayloadHash(*payload)
	err = ValidatePreparedTransferPayloadMetadata(*payload)
	require.ErrorContains(t, err, "owner intent signature")
}

func transferIntentFromAssignment(assignment *circuit.JoinSplitCircuit) (*big.Int, error) {
	return privacytypes.ComputeTransferIntentV2(privacytypes.TransferIntentV2Input{
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
