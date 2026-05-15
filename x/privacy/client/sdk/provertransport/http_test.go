package provertransport

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHTTPHandlerTransferProofRoute(t *testing.T) {
	payload, artifacts, runner := testPreparedTransferPayload(t)
	request, err := NewTransferProofRequest(payload)
	require.NoError(t, err)
	requestBody, err := request.MarshalIndentedJSON()
	require.NoError(t, err)

	handler := NewHTTPHandler(
		ReferenceTransferProver{Artifacts: artifacts, Runner: runner},
		nil,
		nil,
	)

	recorder := httptest.NewRecorder()
	httpRequest := httptest.NewRequest(http.MethodPost, TransferProofPath, bytesReader(requestBody))
	handler.ServeHTTP(recorder, httpRequest)

	require.Equal(t, http.StatusOK, recorder.Code)

	response, err := DecodeTransferProofResponseJSON(recorder.Body.Bytes())
	require.NoError(t, err)
	require.NoError(t, ValidateTransferProofResponse(*request, *response))
}

func TestHTTPHandlerWithdrawProofRoute(t *testing.T) {
	now := time.Now()
	payload, artifacts, runner := testPreparedWithdrawProverPayload(t, now)
	request, err := NewWithdrawProofRequest(payload, now)
	require.NoError(t, err)
	requestBody, err := request.MarshalIndentedJSON()
	require.NoError(t, err)

	handler := NewHTTPHandler(
		nil,
		ReferenceWithdrawProver{Artifacts: artifacts, Runner: runner},
		func() time.Time { return now },
	)

	recorder := httptest.NewRecorder()
	httpRequest := httptest.NewRequest(http.MethodPost, WithdrawProofPath, bytesReader(requestBody))
	handler.ServeHTTP(recorder, httpRequest)

	require.Equal(t, http.StatusOK, recorder.Code)

	response, err := DecodeWithdrawProofResponseJSON(recorder.Body.Bytes())
	require.NoError(t, err)
	require.NoError(t, ValidateWithdrawProofResponse(*request, *response, now))
}

func TestHTTPHandlerRejectsMethod(t *testing.T) {
	handler := NewHTTPHandler(nil, nil, nil)

	recorder := httptest.NewRecorder()
	httpRequest := httptest.NewRequest(http.MethodGet, TransferProofPath, nil)
	handler.ServeHTTP(recorder, httpRequest)

	require.Equal(t, http.StatusMethodNotAllowed, recorder.Code)
	errorResponse, err := DecodeErrorResponseJSON(recorder.Body.Bytes())
	require.NoError(t, err)
	require.Equal(t, ErrorCodeMethodNotAllowed, errorResponse.Code)
}

func TestHTTPProverClientTransferRoundTrip(t *testing.T) {
	payload, artifacts, runner := testPreparedTransferPayload(t)
	request, err := NewTransferProofRequest(payload)
	require.NoError(t, err)

	server := httptest.NewServer(NewHTTPHandler(
		ReferenceTransferProver{Artifacts: artifacts, Runner: runner},
		nil,
		nil,
	))
	defer server.Close()

	client := HTTPProverClient{
		BaseURL: server.URL,
		Client:  server.Client(),
	}

	response, err := client.ProveTransfer(context.Background(), *request)
	require.NoError(t, err)
	require.NoError(t, ValidateTransferProofResponse(*request, *response))
}

func TestHTTPProverClientWithdrawRoundTrip(t *testing.T) {
	now := time.Now()
	payload, artifacts, runner := testPreparedWithdrawProverPayload(t, now)
	request, err := NewWithdrawProofRequest(payload, now)
	require.NoError(t, err)

	server := httptest.NewServer(NewHTTPHandler(
		nil,
		ReferenceWithdrawProver{Artifacts: artifacts, Runner: runner},
		func() time.Time { return now },
	))
	defer server.Close()

	client := HTTPProverClient{
		BaseURL: server.URL,
		Client:  server.Client(),
		Now:     func() time.Time { return now },
	}

	response, err := client.ProveWithdraw(context.Background(), *request)
	require.NoError(t, err)
	require.NoError(t, ValidateWithdrawProofResponse(*request, *response, now))
}

func TestHTTPProverClientPropagatesErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeErrorResponse(w, http.StatusBadRequest, ErrorCodeInvalidRequest, "bad request body")
	}))
	defer server.Close()

	payload, _, _ := testPreparedTransferPayload(t)
	request, err := NewTransferProofRequest(payload)
	require.NoError(t, err)

	client := HTTPProverClient{
		BaseURL: server.URL,
		Client:  server.Client(),
	}

	_, err = client.ProveTransfer(context.Background(), *request)
	require.ErrorContains(t, err, ErrorCodeInvalidRequest)
	require.ErrorContains(t, err, "bad request body")
}

func bytesReader(bz []byte) *bytes.Reader {
	return bytes.NewReader(bz)
}
