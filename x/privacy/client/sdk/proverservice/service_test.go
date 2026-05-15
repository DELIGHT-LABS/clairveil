package proverservice

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	privacyprovertransport "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/provertransport"
)

type stubTransferProver struct{}

func (stubTransferProver) ProveTransfer(request privacyprovertransport.TransferProofRequest) (*privacyprovertransport.TransferProofResponse, error) {
	return nil, fmt.Errorf("unexpected proof request: %s", request.Version)
}

func TestHandlerHealthRoute(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, RuntimeInfo{
		ServiceName:   ServiceName,
		ArtifactDir:   "/tmp/privacy-artifacts",
		PreflightMode: "warn",
		AuthEnabled:   false,
		Routes:        []string{HealthPath, ReadinessPath},
	}, "", DefaultMaxRequestBz)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, HealthPath, nil)
	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)

	var response StatusResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, StatusVersion, response.Version)
	require.Equal(t, "ok", response.Status)
	require.Equal(t, ServiceName, response.ServiceName)
	require.False(t, response.AuthEnabled)
}

func TestHandlerReadinessRouteFailsWhenCheckerFails(t *testing.T) {
	handler := NewHandler(nil, nil, nil, func() error {
		return fmt.Errorf("artifacts missing")
	}, RuntimeInfo{
		ServiceName:   ServiceName,
		ArtifactDir:   "/tmp/privacy-artifacts",
		PreflightMode: "strict",
		AuthEnabled:   false,
		Routes:        []string{HealthPath, ReadinessPath},
	}, "", DefaultMaxRequestBz)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, ReadinessPath, nil)
	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)

	var response StatusResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, "unavailable", response.Status)
	require.Contains(t, response.Error, "artifacts missing")
}

func TestHandlerDelegatesProofRouteMethodValidation(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, DefaultRuntimeInfo(), "", DefaultMaxRequestBz)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, privacyprovertransport.TransferProofPath, nil)
	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusMethodNotAllowed, recorder.Code)

	errorResponse, err := privacyprovertransport.DecodeErrorResponseJSON(recorder.Body.Bytes())
	require.NoError(t, err)
	require.Equal(t, privacyprovertransport.ErrorCodeMethodNotAllowed, errorResponse.Code)
}

func TestHandlerLimitsProofRequestBody(t *testing.T) {
	handler := NewHandler(stubTransferProver{}, nil, nil, nil, DefaultRuntimeInfo(), "", 1)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, privacyprovertransport.TransferProofPath, bytes.NewBufferString("{}"))
	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusBadRequest, recorder.Code)

	errorResponse, err := privacyprovertransport.DecodeErrorResponseJSON(recorder.Body.Bytes())
	require.NoError(t, err)
	require.Equal(t, privacyprovertransport.ErrorCodeInvalidRequest, errorResponse.Code)
	require.Contains(t, errorResponse.Message, "request body too large")
}

func TestDefaultServerConfigBuildsHTTPServer(t *testing.T) {
	server, err := DefaultServerConfig().HTTPServer(http.NewServeMux())
	require.NoError(t, err)
	require.Equal(t, DefaultListenAddress, server.Addr)
}

func TestHandlerRejectsUnauthorizedProofRoute(t *testing.T) {
	info := DefaultRuntimeInfo()
	info.AuthEnabled = true

	handler := NewHandler(stubTransferProver{}, nil, nil, nil, info, "secret-token", DefaultMaxRequestBz)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, privacyprovertransport.TransferProofPath, bytes.NewBufferString("{}"))
	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)

	errorResponse, err := privacyprovertransport.DecodeErrorResponseJSON(recorder.Body.Bytes())
	require.NoError(t, err)
	require.Equal(t, privacyprovertransport.ErrorCodeUnauthorized, errorResponse.Code)
}
