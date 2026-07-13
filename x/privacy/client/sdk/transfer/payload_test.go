package transfer

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
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
	require.Len(t, signer.requests, 1)
	require.NoError(t, ValidateJoinSplitOwnerIntentSigningRequestV1(signer.requests[0]))
	require.Equal(t, signer.hashes[0], signer.requests[0].Intent)

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

func TestJoinSplitStructuredSigningBoundaryRejectsDisclosureReuseBeforeRelease(t *testing.T) {
	input, merkleProvider, signer, _, _ := testBuildTransferMessageDeps(t)
	_, err := BuildPreparedTransferPayload(context.Background(), merkleProvider, signer, input)
	require.NoError(t, err)
	require.Len(t, signer.requests, 1)
	base := signer.requests[0]

	tests := []struct {
		name string
		code privacytypes.DisclosureBlindingErrorCodeV1
		set  func(*JoinSplitOwnerIntentSigningRequestV1)
	}{
		{
			name: "DBS-01",
			code: privacytypes.DisclosureBlindingErrorUserRandomnessReuseV1,
			set: func(request *JoinSplitOwnerIntentSigningRequestV1) {
				request.UserDisclosureBlinding = new(big.Int).Set(request.RecipientOutputRandomness)
			},
		},
		{
			name: "DBS-02",
			code: privacytypes.DisclosureBlindingErrorFullRandomnessReuseV1,
			set: func(request *JoinSplitOwnerIntentSigningRequestV1) {
				request.FullDisclosureBlinding = new(big.Int).Set(request.RecipientOutputRandomness)
			},
		},
		{
			name: "DBS-03",
			code: privacytypes.DisclosureBlindingErrorUserFullBlindingReuseV1,
			set: func(request *JoinSplitOwnerIntentSigningRequestV1) {
				request.FullDisclosureBlinding = new(big.Int).Set(request.UserDisclosureBlinding)
			},
		},
		{
			name: "all-private user sentinel",
			code: privacytypes.DisclosureBlindingErrorAllPrivateUserSentinelV1,
			set: func(request *JoinSplitOwnerIntentSigningRequestV1) {
				request.UserPrivacyPolicy = privacytypes.TransferPrivacyPolicyAllPrivate
				request.Effect.UserPrivacyPolicy = privacytypes.TransferPrivacyPolicyAllPrivate
				request.UserDisclosureBlinding = big.NewInt(1)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			request := cloneJoinSplitOwnerIntentSigningRequestV1(base)
			tc.set(&request)
			signatureReleases := 0
			_, err := SignValidatedJoinSplitOwnerIntentV1(request, func(*big.Int) ([]byte, error) {
				signatureReleases++
				return testSignatureBytes(t), nil
			})
			require.Error(t, err)
			require.Zero(t, signatureReleases)
			var invariantErr *privacytypes.DisclosureBlindingErrorV1
			require.True(t, errors.As(err, &invariantErr))
			require.Equal(t, tc.code, invariantErr.Code)
			require.Equal(t, privacytypes.TransferDisclosureRecipientOutputIndex, invariantErr.OutputIndex)
		})
	}
}

func TestJoinSplitStructuredSigningBoundaryBindsFinalEffectBeforeRelease(t *testing.T) {
	input, merkleProvider, signer, _, _ := testBuildTransferMessageDeps(t)
	_, err := BuildPreparedTransferPayload(context.Background(), merkleProvider, signer, input)
	require.NoError(t, err)
	require.Len(t, signer.requests, 1)

	request := cloneJoinSplitOwnerIntentSigningRequestV1(signer.requests[0])
	request.Intent = new(big.Int).Add(request.Intent, big.NewInt(1))
	signatureReleases := 0
	_, err = SignValidatedJoinSplitOwnerIntentV1(request, func(*big.Int) ([]byte, error) {
		signatureReleases++
		return testSignatureBytes(t), nil
	})
	require.ErrorContains(t, err, "does not match the final effect")
	require.Zero(t, signatureReleases)
}

func TestJoinSplitStructuredSigningBoundaryRejectsDecoupledPrivateProjectionBeforeRelease(t *testing.T) {
	input, merkleProvider, signer, _, _ := testBuildTransferMessageDeps(t)
	_, err := BuildPreparedTransferPayload(context.Background(), merkleProvider, signer, input)
	require.NoError(t, err)
	require.Len(t, signer.requests, 1)
	base := signer.requests[0]

	tests := []struct {
		name string
		set  func(*JoinSplitOwnerIntentSigningRequestV1)
		want string
	}{
		{
			name: "decoy recipient randomness",
			set: func(request *JoinSplitOwnerIntentSigningRequestV1) {
				request.RecipientOutputRandomness = new(big.Int).Add(request.RecipientOutputRandomness, big.NewInt(1))
				request.UserDisclosureBlinding = new(big.Int).Add(request.UserDisclosureBlinding, big.NewInt(2))
				request.FullDisclosureBlinding = new(big.Int).Add(request.FullDisclosureBlinding, big.NewInt(3))
			},
			want: "randomness does not match",
		},
		{
			name: "decoy recipient note",
			set: func(request *JoinSplitOwnerIntentSigningRequestV1) {
				request.RecipientOutputNote.Randomness = new(big.Int).Add(request.RecipientOutputNote.Randomness, big.NewInt(4))
				request.RecipientOutputRandomness = new(big.Int).Set(request.RecipientOutputNote.Randomness)
				request.UserDisclosureBlinding = new(big.Int).Add(request.UserDisclosureBlinding, big.NewInt(5))
				request.FullDisclosureBlinding = new(big.Int).Add(request.FullDisclosureBlinding, big.NewInt(6))
			},
			want: "does not match the final effect commitment",
		},
		{
			name: "decoy user blinding",
			set: func(request *JoinSplitOwnerIntentSigningRequestV1) {
				request.UserDisclosureBlinding = new(big.Int).Add(request.UserDisclosureBlinding, big.NewInt(7))
			},
			want: "user disclosure preimage does not match",
		},
		{
			name: "decoy full blinding",
			set: func(request *JoinSplitOwnerIntentSigningRequestV1) {
				request.FullDisclosureBlinding = new(big.Int).Add(request.FullDisclosureBlinding, big.NewInt(8))
			},
			want: "audit disclosure preimage does not match",
		},
		{
			name: "redirected change output",
			set: func(request *JoinSplitOwnerIntentSigningRequestV1) {
				request.ChangeOutputNote.ReceiverSpendPubKeyX = new(big.Int).Set(request.RecipientOutputNote.ReceiverSpendPubKeyX)
				request.ChangeOutputNote.ReceiverSpendPubKeyY = new(big.Int).Set(request.RecipientOutputNote.ReceiverSpendPubKeyY)
				request.ChangeOutputNote.ReceiverViewPubKeyX = new(big.Int).Set(request.RecipientOutputNote.ReceiverViewPubKeyX)
				request.ChangeOutputNote.ReceiverViewPubKeyY = new(big.Int).Set(request.RecipientOutputNote.ReceiverViewPubKeyY)
				rebuildSigningRequestEffectAndIntent(t, request)
			},
			want: "change output does not return to the expected owner",
		},
		{
			name: "non-conserving change amount",
			set: func(request *JoinSplitOwnerIntentSigningRequestV1) {
				request.ChangeOutputNote.Amount = new(big.Int).Add(request.ChangeOutputNote.Amount, big.NewInt(1))
				rebuildSigningRequestEffectAndIntent(t, request)
			},
			want: "amounts are not conserved",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			request := cloneJoinSplitOwnerIntentSigningRequestV1(base)
			tc.set(&request)
			signatureReleases := 0
			_, err := SignValidatedJoinSplitOwnerIntentV1(request, func(*big.Int) ([]byte, error) {
				signatureReleases++
				return testSignatureBytes(t), nil
			})
			require.ErrorContains(t, err, tc.want)
			require.Zero(t, signatureReleases)
		})
	}
}

func rebuildSigningRequestEffectAndIntent(t *testing.T, request *JoinSplitOwnerIntentSigningRequestV1) {
	t.Helper()
	request.Effect.NewCommitments[0] = request.RecipientOutputNote.ComputeCommitment().FillBytes(make([]byte, 32))
	request.Effect.NewCommitments[1] = request.ChangeOutputNote.ComputeCommitment().FillBytes(make([]byte, 32))
	payloadDigest, err := privacytypes.ComputeTransferPayloadDigestV1(request.Effect)
	require.NoError(t, err)
	chainDomain, err := privacytypes.ComputeChainDomainV1(request.ChainID, privacytypes.ActiveCircuitSetID)
	require.NoError(t, err)
	request.Intent, err = privacytypes.ComputeTransferIntentV2(privacytypes.TransferIntentV2Input{
		ChainDomainHi:        chainDomain.Hi,
		ChainDomainLo:        chainDomain.Lo,
		MerkleRoot:           new(big.Int).SetBytes(request.Effect.Root),
		AssetID:              request.AssetID,
		Nullifiers:           [2]*big.Int{new(big.Int).SetBytes(request.Effect.Nullifiers[0]), new(big.Int).SetBytes(request.Effect.Nullifiers[1])},
		Commitments:          [2]*big.Int{new(big.Int).SetBytes(request.Effect.NewCommitments[0]), new(big.Int).SetBytes(request.Effect.NewCommitments[1])},
		UserDisclosureDigest: new(big.Int).SetBytes(request.Effect.UserDisclosureDigest),
		FullDisclosureDigest: new(big.Int).SetBytes(request.Effect.AuditDisclosureDigest),
		PayloadDigestHi:      payloadDigest.Hi,
		PayloadDigestLo:      payloadDigest.Lo,
		ExpiresAtUnix:        request.Effect.ExpiresAtUnix,
	})
	require.NoError(t, err)
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

func TestGenerateTransferDisclosureBlindingsV1RetriesExactReuse(t *testing.T) {
	outputRandomness := big.NewInt(11)
	generated := []*big.Int{
		big.NewInt(0),
		big.NewInt(11),
		big.NewInt(13),
		big.NewInt(13),
		big.NewInt(17),
	}
	generate := func() (*big.Int, error) {
		require.NotEmpty(t, generated)
		value := generated[0]
		generated = generated[1:]
		return value, nil
	}

	userBlinding, fullBlinding, err := generateTransferDisclosureBlindingsV1(
		outputRandomness,
		privacytypes.TransferPrivacyPolicyDiscloseAmount,
		generate,
	)
	require.NoError(t, err)
	require.Equal(t, int64(17), userBlinding.Int64())
	require.Equal(t, int64(13), fullBlinding.Int64())
	require.Empty(t, generated)
}

func TestValidatePreparedTransferPayloadMetadataRejectsDisclosureBlindingReuse(t *testing.T) {
	input, merkleProvider, signer, _, _ := testBuildTransferMessageDeps(t)
	base, err := BuildPreparedTransferPayload(context.Background(), merkleProvider, signer, input)
	require.NoError(t, err)

	tests := []struct {
		name string
		code privacytypes.DisclosureBlindingErrorCodeV1
		set  func(*PreparedTransferPayload)
	}{
		{
			name: "recipient output randomness is non-canonical",
			code: privacytypes.DisclosureBlindingErrorNonCanonicalFieldV1,
			set: func(payload *PreparedTransferPayload) {
				payload.Outputs[privacytypes.TransferDisclosureRecipientOutputIndex].RandomnessHex = hex.EncodeToString(fr.Modulus().Bytes())
			},
		},
		{
			name: "enabled user blinding is missing",
			code: privacytypes.DisclosureBlindingErrorUserBlindingRequiredV1,
			set: func(payload *PreparedTransferPayload) {
				payload.UserDisclosureBlindingHex = ""
			},
		},
		{
			name: "full blinding is missing",
			code: privacytypes.DisclosureBlindingErrorFullBlindingRequiredV1,
			set: func(payload *PreparedTransferPayload) {
				payload.FullDisclosureBlindingHex = ""
			},
		},
		{
			name: "user equals recipient output randomness",
			code: privacytypes.DisclosureBlindingErrorUserRandomnessReuseV1,
			set: func(payload *PreparedTransferPayload) {
				payload.UserDisclosureBlindingHex = payload.Outputs[privacytypes.TransferDisclosureRecipientOutputIndex].RandomnessHex
			},
		},
		{
			name: "full equals recipient output randomness",
			code: privacytypes.DisclosureBlindingErrorFullRandomnessReuseV1,
			set: func(payload *PreparedTransferPayload) {
				payload.FullDisclosureBlindingHex = payload.Outputs[privacytypes.TransferDisclosureRecipientOutputIndex].RandomnessHex
			},
		},
		{
			name: "full equals user",
			code: privacytypes.DisclosureBlindingErrorUserFullBlindingReuseV1,
			set: func(payload *PreparedTransferPayload) {
				payload.FullDisclosureBlindingHex = payload.UserDisclosureBlindingHex
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			payload := clonePreparedTransferPayloadForTest(*base)
			tc.set(&payload)
			payload.PayloadHash = ComputePreparedTransferPayloadHash(payload)

			err := ValidatePreparedTransferPayloadMetadata(payload)
			require.Error(t, err)
			var invariantErr *privacytypes.DisclosureBlindingErrorV1
			require.True(t, errors.As(err, &invariantErr))
			require.Equal(t, tc.code, invariantErr.Code)
		})
	}
}

func TestValidatePreparedTransferPayloadMetadataCanonicalizesAllPrivateUserBlinding(t *testing.T) {
	input, merkleProvider, signer, _, _ := testBuildTransferMessageDeps(t)
	input.UserPrivacyPolicy = privacytypes.TransferPrivacyPolicyAllPrivate
	input.UserDisclosureMode = privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_NONE
	input.UserDisclosureTargetPubKey = nil
	input.UserDisclosureTargetPubKeyBz = nil

	payload, err := BuildPreparedTransferPayload(context.Background(), merkleProvider, signer, input)
	require.NoError(t, err)
	require.Empty(t, payload.UserDisclosureBlindingHex)
	require.NoError(t, ValidatePreparedTransferPayloadMetadata(*payload))
	require.Len(t, signer.requests, 1)
	require.Zero(t, signer.requests[0].UserDisclosureBlinding.Sign())
	require.NoError(t, ValidateJoinSplitOwnerIntentSigningRequestV1(signer.requests[0]))

	payload.UserDisclosureBlindingHex = payload.FullDisclosureBlindingHex
	payload.PayloadHash = ComputePreparedTransferPayloadHash(*payload)
	err = ValidatePreparedTransferPayloadMetadata(*payload)
	require.Error(t, err)
	var invariantErr *privacytypes.DisclosureBlindingErrorV1
	require.True(t, errors.As(err, &invariantErr))
	require.Equal(t, privacytypes.DisclosureBlindingErrorAllPrivateUserSentinelV1, invariantErr.Code)
}

func cloneJoinSplitOwnerIntentSigningRequestV1(
	request JoinSplitOwnerIntentSigningRequestV1,
) JoinSplitOwnerIntentSigningRequestV1 {
	request.Intent = new(big.Int).Set(request.Intent)
	request.AssetID = new(big.Int).Set(request.AssetID)
	request.RecipientOutputRandomness = new(big.Int).Set(request.RecipientOutputRandomness)
	request.UserDisclosureBlinding = new(big.Int).Set(request.UserDisclosureBlinding)
	request.FullDisclosureBlinding = new(big.Int).Set(request.FullDisclosureBlinding)
	for i, note := range request.InputNotes {
		request.InputNotes[i] = cloneSigningRequestNote(note)
	}
	if request.RecipientOutputNote != nil {
		request.RecipientOutputNote = cloneSigningRequestNote(request.RecipientOutputNote)
	}
	if request.ChangeOutputNote != nil {
		request.ChangeOutputNote = cloneSigningRequestNote(request.ChangeOutputNote)
	}
	request.SenderSpendPubKeyX = new(big.Int).Set(request.SenderSpendPubKeyX)
	request.SenderSpendPubKeyY = new(big.Int).Set(request.SenderSpendPubKeyY)
	request.SenderViewPubKeyX = new(big.Int).Set(request.SenderViewPubKeyX)
	request.SenderViewPubKeyY = new(big.Int).Set(request.SenderViewPubKeyY)
	effect := *request.Effect
	effect.Root = append([]byte(nil), effect.Root...)
	effect.Nullifiers = cloneByteSlicesForSigningTest(effect.Nullifiers)
	effect.NewCommitments = cloneByteSlicesForSigningTest(effect.NewCommitments)
	request.Effect = &effect
	return request
}

func cloneSigningRequestNote(note *privacytypes.Note) *privacytypes.Note {
	if note == nil {
		return nil
	}
	clone := *note
	clone.ReceiverSpendPubKeyX = new(big.Int).Set(note.ReceiverSpendPubKeyX)
	clone.ReceiverSpendPubKeyY = new(big.Int).Set(note.ReceiverSpendPubKeyY)
	clone.ReceiverViewPubKeyX = new(big.Int).Set(note.ReceiverViewPubKeyX)
	clone.ReceiverViewPubKeyY = new(big.Int).Set(note.ReceiverViewPubKeyY)
	clone.Amount = new(big.Int).Set(note.Amount)
	clone.AssetID = new(big.Int).Set(note.AssetID)
	clone.Randomness = new(big.Int).Set(note.Randomness)
	return &clone
}

func cloneByteSlicesForSigningTest(values [][]byte) [][]byte {
	clones := make([][]byte, len(values))
	for i := range values {
		clones[i] = append([]byte(nil), values[i]...)
	}
	return clones
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
