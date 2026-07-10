package provertransport

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	privacytransfer "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/transfer"
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

func TestHTTPHandlerRejectsExpiredTransferBeforeProving(t *testing.T) {
	now := time.Now()
	payload, _, _ := testPreparedTransferPayload(t)
	payload.ExpiresAtUnix = now.Add(-time.Minute).Unix()
	payload.PayloadHash = privacytransfer.ComputePreparedTransferPayloadHash(payload)
	request := TransferProofRequest{Version: TransferProofRequestVersion, Payload: payload}
	requestBody, err := request.MarshalIndentedJSON()
	require.NoError(t, err)
	prover := &countingTransferProver{}
	handler := NewHTTPHandler(prover, nil, func() time.Time { return now })

	recorder := httptest.NewRecorder()
	httpRequest := httptest.NewRequest(http.MethodPost, TransferProofPath, bytesReader(requestBody))
	handler.ServeHTTP(recorder, httpRequest)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Zero(t, prover.calls)
	errorResponse, err := DecodeErrorResponseJSON(recorder.Body.Bytes())
	require.NoError(t, err)
	require.Equal(t, ErrorCodeInvalidRequest, errorResponse.Code)
	require.Contains(t, errorResponse.Message, "expired")
}

func TestHTTPHandlerPassesInjectedClockToTransferProver(t *testing.T) {
	now := time.Unix(1_900_000_000, 0)
	payload, _, _ := testPreparedTransferPayload(t)
	request, err := NewTransferProofRequestAt(payload, now)
	require.NoError(t, err)
	requestBody, err := request.MarshalIndentedJSON()
	require.NoError(t, err)
	prover := &clockCapturingTransferProver{}
	handler := NewHTTPHandler(prover, nil, func() time.Time { return now })

	recorder := httptest.NewRecorder()
	httpRequest := httptest.NewRequest(http.MethodPost, TransferProofPath, bytesReader(requestBody))
	handler.ServeHTTP(recorder, httpRequest)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Equal(t, now, prover.now)
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

func TestHTTPProverClientRejectsWithdrawResponseExpiredInFlight(t *testing.T) {
	start := time.Unix(1_900_000_000, 0)
	payload, artifacts, runner := testPreparedWithdrawProverPayload(t, start)
	request, err := NewWithdrawProofRequest(payload, start)
	require.NoError(t, err)
	response, err := BuildWithdrawProofResponse(*request, artifacts, runner, start)
	require.NoError(t, err)
	responseBody, err := response.MarshalIndentedJSON()
	require.NoError(t, err)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(responseBody)
	}))
	defer server.Close()

	clockCalls := 0
	client := HTTPProverClient{
		BaseURL: server.URL,
		Client:  server.Client(),
		Now: func() time.Time {
			clockCalls++
			if clockCalls == 1 {
				return start
			}
			return time.Unix(payload.ExpiresAtUnix, 0)
		},
	}

	_, err = client.ProveWithdraw(context.Background(), *request)
	require.ErrorContains(t, err, "expired")
}

func TestHTTPProverClientRejectsOversizedResponse(t *testing.T) {
	const responseLimit = int64(64)
	payload, _, _ := testPreparedTransferPayload(t)
	request, err := NewTransferProofRequest(payload)
	require.NoError(t, err)

	for _, status := range []int{http.StatusOK, http.StatusBadRequest} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(strings.Repeat("x", int(responseLimit)+1)))
			}))
			defer server.Close()

			client := HTTPProverClient{
				BaseURL:          server.URL,
				Client:           server.Client(),
				MaxResponseBytes: responseLimit,
			}
			_, err := client.ProveTransfer(context.Background(), *request)
			require.ErrorContains(t, err, "response exceeds 64 bytes")
		})
	}
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

type countingTransferProver struct {
	calls int
}

func (p *countingTransferProver) ProveTransfer(TransferProofRequest, time.Time) (*TransferProofResponse, error) {
	p.calls++
	return nil, fmt.Errorf("unexpected proof request")
}

type clockCapturingTransferProver struct {
	now time.Time
}

func (p *clockCapturingTransferProver) ProveTransfer(_ TransferProofRequest, now time.Time) (*TransferProofResponse, error) {
	p.now = now
	return nil, fmt.Errorf("stop after capturing clock")
}
