package main

import (
	"bytes"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
	privacyidentity "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/identity"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

type recordingNoteProver struct {
	calls int
	note  privacytypes.Note
	proof []byte
	err   error
}

func (p *recordingNoteProver) ProveDeposit(note privacytypes.Note) ([]byte, error) {
	p.calls++
	p.note = note
	return p.proof, p.err
}

func TestDepositProofHandlerReturnsClairveilJSContract(t *testing.T) {
	note := validTestNote(t)
	commitment, err := privacyfield.CanonicalHexFromBigInt(note.ComputeCommitment())
	require.NoError(t, err)
	noteJSON, err := json.Marshal(note)
	require.NoError(t, err)
	requestBody, err := json.Marshal(depositProofRequest{
		NoteJSON:          string(noteJSON),
		NoteCommitmentHex: strings.ToUpper(commitment),
	})
	require.NoError(t, err)

	prover := &recordingNoteProver{proof: []byte{0xaa, 0xbb}}
	handler, err := newDepositProofHandler(prover, defaultMaxRequestBytes)
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, depositProofPath, bytes.NewReader(requestBody))
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, 1, prover.calls)
	require.Zero(t, prover.note.Amount.Cmp(note.Amount))
	var response map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, map[string]any{
		"version":             "v1",
		"proof_hex":           "aabb",
		"note_commitment_hex": commitment,
	}, response)
}

func TestDepositProofHandlerRejectsMismatchedCommitmentBeforeProving(t *testing.T) {
	note := validTestNote(t)
	noteJSON, err := json.Marshal(note)
	require.NoError(t, err)
	requestBody, err := json.Marshal(depositProofRequest{
		NoteJSON:          string(noteJSON),
		NoteCommitmentHex: strings.Repeat("00", privacyfield.ByteSize),
	})
	require.NoError(t, err)

	prover := &recordingNoteProver{proof: []byte{0xaa}}
	handler, err := newDepositProofHandler(prover, defaultMaxRequestBytes)
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, depositProofPath, bytes.NewReader(requestBody))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Zero(t, prover.calls)
	require.Contains(t, recorder.Body.String(), "does not match note_json")
}

func TestDepositProofHandlerRejectsInvalidFraming(t *testing.T) {
	prover := &recordingNoteProver{proof: []byte{0xaa}}
	handler, err := newDepositProofHandler(prover, 8)
	require.NoError(t, err)

	tests := []struct {
		name        string
		method      string
		contentType string
		body        string
		status      int
	}{
		{name: "method", method: http.MethodGet, contentType: "application/json", status: http.StatusMethodNotAllowed},
		{name: "content type", method: http.MethodPost, contentType: "text/plain", body: `{}`, status: http.StatusUnsupportedMediaType},
		{name: "oversized", method: http.MethodPost, contentType: "application/json", body: `{"long":true}`, status: http.StatusRequestEntityTooLarge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(test.method, depositProofPath, strings.NewReader(test.body))
			request.Header.Set("Content-Type", test.contentType)
			handler.ServeHTTP(recorder, request)
			require.Equal(t, test.status, recorder.Code)
		})
	}
	require.Zero(t, prover.calls)
}

func TestValidateListenAddressRequiresLoopback(t *testing.T) {
	require.NoError(t, validateListenAddress("127.0.0.1:8090"))
	require.NoError(t, validateListenAddress("[::1]:8090"))
	require.NoError(t, validateListenAddress("localhost:8090"))
	require.ErrorContains(t, validateListenAddress("0.0.0.0:8090"), "loopback")
}

func TestReferenceNoteProverGeneratesDepositProof(t *testing.T) {
	if os.Getenv("CLAIRVEIL_DEPOSIT_PROVER_INTEGRATION") != "1" {
		t.Skip("set CLAIRVEIL_DEPOSIT_PROVER_INTEGRATION=1 with generated ZK artifacts")
	}
	proof, err := (referenceNoteProver{}).ProveDeposit(validTestNote(t))
	require.NoError(t, err)
	require.NotEmpty(t, proof)
}

func validTestNote(t *testing.T) privacytypes.Note {
	t.Helper()
	rootSeed := bytes.Repeat([]byte{0x42}, privacyidentity.RootSeedLength)
	_, spendPubKey, _ := privacyidentity.DeriveSpendKeys(rootSeed)
	_, viewPubKey, _ := privacyidentity.DeriveViewKeys(rootSeed)
	return privacytypes.Note{
		ReceiverSpendPubKeyX: spendPubKey.X.BigInt(new(big.Int)),
		ReceiverSpendPubKeyY: spendPubKey.Y.BigInt(new(big.Int)),
		ReceiverViewPubKeyX:  viewPubKey.X.BigInt(new(big.Int)),
		ReceiverViewPubKeyY:  viewPubKey.Y.BigInt(new(big.Int)),
		Amount:               big.NewInt(7),
		AssetID:              privacytypes.ComputeAssetIDV1("uclair"),
		Randomness:           big.NewInt(13),
		Memo:                 "local deposit",
	}
}
