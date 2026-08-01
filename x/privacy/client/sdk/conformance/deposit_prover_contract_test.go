package conformance_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	privacydeposit "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/deposit"
	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
	privacyprovertransport "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/provertransport"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	privacyzk "github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
)

type depositProverContractFixture struct {
	SchemaVersion string `json:"schema_version"`
	Transport     struct {
		Method          string `json:"method"`
		Path            string `json:"path"`
		ContentType     string `json:"content_type"`
		RequestVersion  string `json:"request_version"`
		PayloadVersion  string `json:"payload_version"`
		ResponseVersion string `json:"response_version"`
		ProofVersion    string `json:"proof_version"`
	} `json:"transport"`
	Encoding struct {
		Field struct {
			Encoding   string `json:"encoding"`
			ByteLength int    `json:"byte_length"`
			HexLength  int    `json:"hex_length"`
			ByteOrder  string `json:"byte_order"`
		} `json:"field"`
		PublicKey struct {
			Encoding   string `json:"encoding"`
			ByteLength int    `json:"byte_length"`
			HexLength  int    `json:"hex_length"`
			Format     string `json:"format"`
		} `json:"public_key"`
		Amount struct {
			Encoding string `json:"encoding"`
			Minimum  string `json:"minimum"`
			Maximum  string `json:"maximum"`
		} `json:"amount"`
	} `json:"encoding"`
	CanonicalPositiveRequest  privacyprovertransport.DepositProofRequest `json:"canonical_positive_request"`
	ExpectedNoteCommitment    string                                     `json:"expected_note_commitment_hex"`
	ResponseCommitmentBinding struct {
		ResponseField string   `json:"response_field"`
		MustEqual     []string `json:"must_equal"`
	} `json:"response_commitment_binding"`
	ProofWire struct {
		ByteLength             int   `json:"byte_length"`
		HexLength              int   `json:"hex_length"`
		CompressedPointOffsets []int `json:"compressed_point_offsets"`
		CommitmentCount        struct {
			Offset        int    `json:"offset"`
			ByteLength    int    `json:"byte_length"`
			ByteOrder     string `json:"byte_order"`
			RequiredValue uint32 `json:"required_value"`
		} `json:"commitment_count"`
		CanonicalRoundTrip string `json:"canonical_round_trip"`
		StaticRealProof    string `json:"static_real_proof"`
	} `json:"proof_wire"`
	NegativeVectors struct {
		RawJSONStrictDecoder struct {
			RawJSON               string `json:"raw_json"`
			ExpectedErrorContains string `json:"expected_error_contains"`
		} `json:"raw_json_strict_decoder"`
		FieldMutationCommitmentMismatch struct {
			Field                 string `json:"field"`
			MutatedValue          string `json:"mutated_value"`
			ExpectedErrorContains string `json:"expected_error_contains"`
		} `json:"field_mutation_commitment_mismatch"`
	} `json:"negative_vectors"`
	CommonErrorPolicy struct {
		ErrorResponseVersion string                      `json:"error_response_version"`
		Mappings             []errorStatusMappingFixture `json:"mappings"`
	} `json:"common_error_policy"`
	RouteFailureStatus map[string]routeFailureStatusFixture `json:"route_failure_status"`
	RetryPolicy        struct {
		Timeout           string `json:"timeout"`
		SameEndpointRetry string `json:"same_endpoint_retry"`
		AutomaticFailover bool   `json:"automatic_failover"`
	} `json:"retry_policy"`
}

type errorStatusMappingFixture struct {
	Code        string `json:"code"`
	StatusCodes []int  `json:"status_codes"`
	Retryable   bool   `json:"retryable"`
}

type routeFailureStatusFixture struct {
	RequestFailure int `json:"request_failure"`
	ProverFailure  int `json:"prover_failure"`
}

func TestDepositProverContractFixture(t *testing.T) {
	fixture := loadDepositProverContractFixture(t)

	require.Equal(t, "clairveil.proverd.deposit-api.contract.v1", fixture.SchemaVersion)
	require.Equal(t, http.MethodPost, fixture.Transport.Method)
	require.Equal(t, privacyprovertransport.DepositProofPath, fixture.Transport.Path)
	require.Equal(t, "application/json", fixture.Transport.ContentType)
	require.Equal(t, privacyprovertransport.DepositProofRequestVersion, fixture.Transport.RequestVersion)
	require.Equal(t, privacydeposit.PreparedDepositProverPayloadVersion, fixture.Transport.PayloadVersion)
	require.Equal(t, privacyprovertransport.DepositProofResponseVersion, fixture.Transport.ResponseVersion)
	require.Equal(t, privacydeposit.PreparedDepositProofVersion, fixture.Transport.ProofVersion)

	require.Equal(t, "lowercase_hex", fixture.Encoding.Field.Encoding)
	require.Equal(t, privacyfield.ByteSize, fixture.Encoding.Field.ByteLength)
	require.Equal(t, privacyfield.ByteSize*2, fixture.Encoding.Field.HexLength)
	require.Equal(t, "unsigned_big_endian", fixture.Encoding.Field.ByteOrder)
	require.Equal(t, "lowercase_hex", fixture.Encoding.PublicKey.Encoding)
	require.Equal(t, privacycrypto.CanonicalPointSize, fixture.Encoding.PublicKey.ByteLength)
	require.Equal(t, privacycrypto.CanonicalPointSize*2, fixture.Encoding.PublicKey.HexLength)
	require.Equal(t, "canonical_compressed_bn254_twisted_edwards", fixture.Encoding.PublicKey.Format)
	require.Equal(t, "canonical_non_negative_decimal", fixture.Encoding.Amount.Encoding)
	require.Equal(t, "0", fixture.Encoding.Amount.Minimum)
	require.Equal(t, privacytypes.MaxShieldedAmount().String(), fixture.Encoding.Amount.Maximum)

	requestJSON, err := json.Marshal(fixture.CanonicalPositiveRequest)
	require.NoError(t, err)
	request, err := privacyprovertransport.DecodeDepositProofRequestJSON(requestJSON)
	require.NoError(t, err)
	require.NoError(t, privacyprovertransport.ValidateDepositProofRequest(*request))
	require.Equal(t, fixture.ExpectedNoteCommitment, request.Payload.NoteCommitmentHex)
	require.Len(t, request.Payload.AssetIDHex, fixture.Encoding.Field.HexLength)
	require.Len(t, request.Payload.RandomnessHex, fixture.Encoding.Field.HexLength)
	require.Len(t, request.Payload.NoteCommitmentHex, fixture.Encoding.Field.HexLength)
	require.Len(t, request.Payload.ReceiverSpendPubKeyHex, fixture.Encoding.PublicKey.HexLength)
	require.Len(t, request.Payload.ReceiverViewPubKeyHex, fixture.Encoding.PublicKey.HexLength)

	require.Equal(t, "proof.note_commitment_hex", fixture.ResponseCommitmentBinding.ResponseField)
	require.Equal(t, []string{"request.payload.note_commitment_hex", "recomputed_note_commitment"}, fixture.ResponseCommitmentBinding.MustEqual)
	response := privacyprovertransport.DepositProofResponse{
		Version: privacyprovertransport.DepositProofResponseVersion,
		Proof: privacydeposit.PreparedDepositProof{
			Version:           privacydeposit.PreparedDepositProofVersion,
			NoteCommitmentHex: fixture.ExpectedNoteCommitment,
		},
	}
	require.Equal(t, request.Payload.NoteCommitmentHex, response.Proof.NoteCommitmentHex)
	response.Proof.NoteCommitmentHex = fixture.ExpectedNoteCommitment[:len(fixture.ExpectedNoteCommitment)-1] + "0"
	require.ErrorContains(t, privacyprovertransport.ValidateDepositProofResponse(*request, response), "note commitment mismatch")

	require.Equal(t, privacyzk.CanonicalBN254Groth16ProofSize, fixture.ProofWire.ByteLength)
	require.Equal(t, privacyzk.CanonicalBN254Groth16ProofSize*2, fixture.ProofWire.HexLength)
	require.Equal(t, []int{0, 32, 96, 132}, fixture.ProofWire.CompressedPointOffsets)
	require.Equal(t, 128, fixture.ProofWire.CommitmentCount.Offset)
	require.Equal(t, 4, fixture.ProofWire.CommitmentCount.ByteLength)
	require.Equal(t, "unsigned_big_endian", fixture.ProofWire.CommitmentCount.ByteOrder)
	require.Zero(t, fixture.ProofWire.CommitmentCount.RequiredValue)
	require.Equal(t, "decode_exact_frame_then_encode_must_preserve_every_byte", fixture.ProofWire.CanonicalRoundTrip)
	require.Equal(t, "not_included", fixture.ProofWire.StaticRealProof)

	_, err = privacyprovertransport.DecodeDepositProofRequestJSON([]byte(fixture.NegativeVectors.RawJSONStrictDecoder.RawJSON))
	require.ErrorContains(t, err, fixture.NegativeVectors.RawJSONStrictDecoder.ExpectedErrorContains)
	require.Equal(t, "payload.amount", fixture.NegativeVectors.FieldMutationCommitmentMismatch.Field)
	mutatedRequest := *request
	mutatedRequest.Payload.Amount = fixture.NegativeVectors.FieldMutationCommitmentMismatch.MutatedValue
	require.ErrorContains(t, privacyprovertransport.ValidateDepositProofRequest(mutatedRequest), fixture.NegativeVectors.FieldMutationCommitmentMismatch.ExpectedErrorContains)

	require.Equal(t, privacyprovertransport.ErrorResponseVersion, fixture.CommonErrorPolicy.ErrorResponseVersion)
	require.Equal(t, expectedProverErrorStatusMappings(), fixture.CommonErrorPolicy.Mappings)
	for _, route := range []string{"deposit", "transfer", "withdraw", "batch_transfer"} {
		status, ok := fixture.RouteFailureStatus[route]
		require.Truef(t, ok, "missing %s route failure status", route)
		require.Equal(t, http.StatusBadRequest, status.RequestFailure)
		require.Equal(t, http.StatusInternalServerError, status.ProverFailure)
	}
	require.Equal(t, "caller_context_deadline", fixture.RetryPolicy.Timeout)
	require.Equal(t, "caller_controlled", fixture.RetryPolicy.SameEndpointRetry)
	require.False(t, fixture.RetryPolicy.AutomaticFailover)
}

func expectedProverErrorStatusMappings() []errorStatusMappingFixture {
	return []errorStatusMappingFixture{
		{Code: privacyprovertransport.ErrorCodeInvalidRequest, StatusCodes: []int{http.StatusBadRequest, http.StatusRequestEntityTooLarge, http.StatusUnsupportedMediaType}},
		{Code: privacyprovertransport.ErrorCodeMethodNotAllowed, StatusCodes: []int{http.StatusMethodNotAllowed}},
		{Code: privacyprovertransport.ErrorCodeNotFound, StatusCodes: []int{http.StatusNotFound}},
		{Code: privacyprovertransport.ErrorCodeUnauthorized, StatusCodes: []int{http.StatusUnauthorized}},
		{Code: privacyprovertransport.ErrorCodeUnavailable, StatusCodes: []int{http.StatusServiceUnavailable}},
		{Code: privacyprovertransport.ErrorCodeProofFailed, StatusCodes: []int{http.StatusInternalServerError}},
		{Code: privacyprovertransport.ErrorCodeBusy, StatusCodes: []int{http.StatusTooManyRequests}, Retryable: true},
	}
}

func loadDepositProverContractFixture(t *testing.T) depositProverContractFixture {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)
	bz, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "testdata", "privacy_deposit_prover_contract.json"))
	require.NoError(t, err)

	var fixture depositProverContractFixture
	require.NoError(t, json.Unmarshal(bz, &fixture))
	return fixture
}
