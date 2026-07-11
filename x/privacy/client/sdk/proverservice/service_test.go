package proverservice

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	gnarklogger "github.com/consensys/gnark/logger"
	"github.com/stretchr/testify/require"

	privacyprovertransport "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/provertransport"
)

type stubTransferProver struct{}

type stubBatchTransferProver struct{}

func (stubTransferProver) ProveTransfer(request privacyprovertransport.TransferProofRequest, _ time.Time) (*privacyprovertransport.TransferProofResponse, error) {
	return nil, fmt.Errorf("unexpected proof request: %s", request.Version)
}

func (stubBatchTransferProver) ProveBatchTransfer(request privacyprovertransport.BatchTransferProofRequest, _ time.Time) (*privacyprovertransport.BatchTransferProofResponse, error) {
	return nil, fmt.Errorf("unexpected batch proof request: %s", request.Version)
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

func TestDefaultRuntimeInfoIncludesMetricsRoute(t *testing.T) {
	info := DefaultRuntimeInfo()
	require.Contains(t, info.Routes, MetricsPath)
	require.Contains(t, info.Routes, privacyprovertransport.BatchTransferProofPath)
	require.Equal(t, "prover_r1cs_pk", info.ReadinessRole)
	require.Contains(t, info.Circuits, privacyprovertransport.BatchTransferProofCircuitID)
}

func TestHandlerMetricsRoute(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, DefaultRuntimeInfo(), "", DefaultMaxRequestBz)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, MetricsPath, nil)
	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)

	var response MetricsResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, StatusVersion, response.Version)
	require.Equal(t, ServiceName, response.ServiceName)
	require.NotEmpty(t, response.Timestamp)
	require.Positive(t, response.Goroutines)
	require.Positive(t, response.SysBytes)
	require.Positive(t, response.RSSBytes)
	require.NotEmpty(t, response.RSSSource)
	require.Contains(t, response.Admission.Circuits, privacyprovertransport.TransferProofCircuitID)
	require.Contains(t, response.Admission.Circuits, privacyprovertransport.WithdrawProofCircuitID)
	require.Contains(t, response.Admission.Circuits, privacyprovertransport.BatchTransferProofCircuitID)
}

func TestHandlerMetricsRouteRejectsNonGET(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, DefaultRuntimeInfo(), "", DefaultMaxRequestBz)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, MetricsPath, nil)
	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusMethodNotAllowed, recorder.Code)

	errorResponse, err := privacyprovertransport.DecodeErrorResponseJSON(recorder.Body.Bytes())
	require.NoError(t, err)
	require.Equal(t, privacyprovertransport.ErrorCodeMethodNotAllowed, errorResponse.Code)
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

	require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)

	errorResponse, err := privacyprovertransport.DecodeErrorResponseJSON(recorder.Body.Bytes())
	require.NoError(t, err)
	require.Equal(t, privacyprovertransport.ErrorCodeInvalidRequest, errorResponse.Code)
	require.Contains(t, errorResponse.Message, "request body too large")
}

func TestHandlerLimitsDecompressedProofRequestBody(t *testing.T) {
	var compressed bytes.Buffer
	zw := gzip.NewWriter(&compressed)
	_, err := zw.Write([]byte(`{"padding":"` + string(bytes.Repeat([]byte("a"), 256)) + `"}`))
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	require.Less(t, compressed.Len(), 128)

	handler := NewHandler(stubTransferProver{}, nil, nil, nil, DefaultRuntimeInfo(), "", 128)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, privacyprovertransport.TransferProofPath, bytes.NewReader(compressed.Bytes()))
	request.Header.Set("Content-Encoding", "gzip")
	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
	errorResponse, err := privacyprovertransport.DecodeErrorResponseJSON(recorder.Body.Bytes())
	require.NoError(t, err)
	require.Equal(t, privacyprovertransport.ErrorCodeInvalidRequest, errorResponse.Code)
	require.NotContains(t, errorResponse.Message, "aaaa")
}

func TestHandlerRejectsUnsupportedProofRequestContentEncoding(t *testing.T) {
	handler := NewHandler(stubTransferProver{}, nil, nil, nil, DefaultRuntimeInfo(), "", DefaultMaxRequestBz)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, privacyprovertransport.TransferProofPath, bytes.NewBufferString("{}"))
	request.Header.Set("Content-Encoding", "br")
	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	errorResponse, err := privacyprovertransport.DecodeErrorResponseJSON(recorder.Body.Bytes())
	require.NoError(t, err)
	require.Equal(t, privacyprovertransport.ErrorCodeInvalidRequest, errorResponse.Code)
}

func TestHandlerReturnsRetryable429WhenCircuitAdmissionIsFull(t *testing.T) {
	admission, err := NewAdmissionController(AdmissionConfig{Circuits: map[string]CircuitAdmissionConfig{
		privacyprovertransport.TransferProofCircuitID: {MaxInFlight: 1, MaxQueued: 0},
	}})
	require.NoError(t, err)
	occupied, err := admission.Acquire(context.Background(), privacyprovertransport.TransferProofCircuitID)
	require.NoError(t, err)
	defer occupied.Release()

	handler := NewHandlerWithAdmission(stubTransferProver{}, nil, nil, nil, DefaultRuntimeInfo(), "", DefaultMaxRequestBz, admission)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, privacyprovertransport.TransferProofPath, bytes.NewBufferString(`{"version":"v2"}`))
	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusTooManyRequests, recorder.Code)
	errorResponse, err := privacyprovertransport.DecodeErrorResponseJSON(recorder.Body.Bytes())
	require.NoError(t, err)
	require.Equal(t, privacyprovertransport.ErrorCodeBusy, errorResponse.Code)
	require.True(t, errorResponse.Retryable)
	require.NotContains(t, errorResponse.Message, "payload")
}

func TestHandlerReturnsRetryable429WhenBatchAdmissionIsFull(t *testing.T) {
	admission, err := NewAdmissionController(AdmissionConfig{Circuits: map[string]CircuitAdmissionConfig{
		privacyprovertransport.BatchTransferProofCircuitID: {MaxInFlight: 1, MaxQueued: 0},
	}})
	require.NoError(t, err)
	occupied, err := admission.Acquire(context.Background(), privacyprovertransport.BatchTransferProofCircuitID)
	require.NoError(t, err)
	defer occupied.Release()

	handler := NewHandlerWithBatchAdmission(nil, nil, stubBatchTransferProver{}, nil, nil, DefaultRuntimeInfo(), "", DefaultMaxRequestBz, admission)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, privacyprovertransport.BatchTransferProofPath, bytes.NewBufferString(`{"version":"v1"}`))
	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusTooManyRequests, recorder.Code)
	errorResponse, err := privacyprovertransport.DecodeErrorResponseJSON(recorder.Body.Bytes())
	require.NoError(t, err)
	require.Equal(t, privacyprovertransport.ErrorCodeBusy, errorResponse.Code)
	require.True(t, errorResponse.Retryable)
	require.NotContains(t, errorResponse.Message, "payload")
}

func TestDefaultServerConfigBuildsHTTPServer(t *testing.T) {
	server, err := DefaultServerConfig().HTTPServer(http.NewServeMux())
	require.NoError(t, err)
	require.Equal(t, DefaultListenAddress, server.Addr)
}

func TestServerConfigRequiresHardBodyLimit(t *testing.T) {
	config := DefaultServerConfig()
	config.MaxRequestBytes = 0
	require.ErrorContains(t, config.Validate(), "must be positive")
	config.MaxRequestBytes = -1
	require.ErrorContains(t, config.Validate(), "must be positive")
}

func TestServerConfigRequiresReadAndIdleTimeoutsAndBoundsHeaders(t *testing.T) {
	config := DefaultServerConfig()
	config.ReadHeaderTimeout = 0
	require.ErrorContains(t, config.Validate(), "read header timeout must be positive")
	config = DefaultServerConfig()
	config.ReadTimeout = 0
	require.ErrorContains(t, config.Validate(), "read timeout must be positive")
	config = DefaultServerConfig()
	config.IdleTimeout = 0
	require.ErrorContains(t, config.Validate(), "idle timeout must be positive")

	server, err := DefaultServerConfig().HTTPServer(http.NewServeMux())
	require.NoError(t, err)
	require.Equal(t, DefaultMaxHeaderBz, server.MaxHeaderBytes)
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

func TestGnarkSolverOutputNeverUsesOperatorLogWriter(t *testing.T) {
	const sensitiveDerivedValue = "witness-derived-field-value"
	var operatorLog bytes.Buffer
	_, err := withGnarkLoggerOutput(&operatorLog, func() (struct{}, error) {
		logger := gnarklogger.Logger()
		logger.Error().Msg(sensitiveDerivedValue)
		return struct{}{}, fmt.Errorf("unsatisfied constraint")
	})
	require.Error(t, err)
	require.NotContains(t, operatorLog.String(), sensitiveDerivedValue)
	require.Empty(t, operatorLog.String())
}

func TestGnarkLogSuppressionDoesNotSerializeProofFunctions(t *testing.T) {
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	done := make(chan struct{}, 2)
	for range 2 {
		go func() {
			_, _ = withGnarkLoggerOutput(io.Discard, func() (struct{}, error) {
				started <- struct{}{}
				<-release
				return struct{}{}, nil
			})
			done <- struct{}{}
		}()
	}
	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("proof functions were serialized by logger suppression")
		}
	}
	close(release)
	<-done
	<-done
}
