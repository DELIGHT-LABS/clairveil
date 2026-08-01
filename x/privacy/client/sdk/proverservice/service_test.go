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
	privacyzk "github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
)

type stubTransferProver struct{}

type stubWithdrawProver struct{}

type stubBatchTransferProver struct{}

type stubDepositProver struct{}

func newServiceRequest(method, path string, body io.Reader) *http.Request {
	request := httptest.NewRequest(method, path, body)
	if method == http.MethodPost {
		request.Header.Set("Content-Type", "application/json")
	}
	return request
}

func (stubTransferProver) ProveTransfer(request privacyprovertransport.TransferProofRequest, _ time.Time) (*privacyprovertransport.TransferProofResponse, error) {
	return nil, fmt.Errorf("unexpected proof request: %s", request.Version)
}

func (stubWithdrawProver) ProveWithdraw(request privacyprovertransport.WithdrawProofRequest, _ time.Time) (*privacyprovertransport.WithdrawProofResponse, error) {
	return nil, fmt.Errorf("unexpected withdraw proof request: %s", request.Version)
}

func (stubBatchTransferProver) ProveBatchTransfer(request privacyprovertransport.BatchTransferProofRequest, _ time.Time) (*privacyprovertransport.BatchTransferProofResponse, error) {
	return nil, fmt.Errorf("unexpected batch proof request: %s", request.Version)
}

func (stubDepositProver) ProveDeposit(request privacyprovertransport.DepositProofRequest) (*privacyprovertransport.DepositProofResponse, error) {
	return nil, fmt.Errorf("unexpected deposit proof request: %s", request.Version)
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
	request := newServiceRequest(http.MethodGet, HealthPath, nil)
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
	request := newServiceRequest(http.MethodGet, ReadinessPath, nil)
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
	require.Contains(t, info.Routes, privacyprovertransport.DepositProofPath)
	require.Equal(t, "prover_r1cs_pk", info.ReadinessRole)
	require.Contains(t, info.Circuits, privacyprovertransport.BatchTransferProofCircuitID)
	require.Equal(t, circuitIDStrings(privacyzk.RequiredCircuitIDs()), info.Circuits)
}

func TestHandlerRuntimeInventoryMatchesConfiguredProvers(t *testing.T) {
	t.Run("legacy transfer-only constructor", func(t *testing.T) {
		handler := NewHandler(stubTransferProver{}, nil, nil, nil, RuntimeInfo{}, "secret-token", DefaultMaxRequestBz)

		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, newServiceRequest(http.MethodGet, ReadinessPath, nil))
		require.Equal(t, http.StatusOK, recorder.Code)

		var response StatusResponse
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
		require.Equal(t, []string{HealthPath, ReadinessPath, MetricsPath, privacyprovertransport.TransferProofPath}, response.Routes)
		require.Equal(t, []string{privacyprovertransport.TransferProofCircuitID}, response.Circuits)
		require.True(t, response.AuthEnabled)
		require.NotContains(t, response.Routes, privacyprovertransport.DepositProofPath)
		require.NotContains(t, response.Circuits, privacyprovertransport.DepositProofCircuitID)

		unavailableRecorder := httptest.NewRecorder()
		unavailableRequest := newServiceRequest(http.MethodPost, privacyprovertransport.DepositProofPath, bytes.NewBufferString(`{}`))
		unavailableRequest.Header.Set("Authorization", "Bearer secret-token")
		handler.ServeHTTP(unavailableRecorder, unavailableRequest)
		require.Equal(t, http.StatusServiceUnavailable, unavailableRecorder.Code)
	})

	t.Run("full prover set", func(t *testing.T) {
		handler := NewHandlerWithProverSet(
			privacyprovertransport.ProverSet{
				Deposit:       stubDepositProver{},
				Transfer:      stubTransferProver{},
				Withdraw:      stubWithdrawProver{},
				BatchTransfer: stubBatchTransferProver{},
			},
			nil,
			nil,
			RuntimeInfo{},
			"",
			DefaultMaxRequestBz,
			mustDefaultAdmissionController(),
		)
		require.Equal(t, DefaultRuntimeInfo().Routes, handler.info.Routes)
		require.Equal(t, circuitIDStrings(privacyzk.RequiredCircuitIDs()), handler.info.Circuits)
	})
}

func TestHandlerMetricsRoute(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, DefaultRuntimeInfo(), "", DefaultMaxRequestBz)

	recorder := httptest.NewRecorder()
	request := newServiceRequest(http.MethodGet, MetricsPath, nil)
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
	require.Contains(t, response.Admission.Circuits, privacyprovertransport.DepositProofCircuitID)
}

func TestHandlerMetricsRouteRejectsNonGET(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, DefaultRuntimeInfo(), "", DefaultMaxRequestBz)

	recorder := httptest.NewRecorder()
	request := newServiceRequest(http.MethodPost, MetricsPath, nil)
	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusMethodNotAllowed, recorder.Code)

	errorResponse, err := privacyprovertransport.DecodeErrorResponseJSON(recorder.Body.Bytes())
	require.NoError(t, err)
	require.Equal(t, privacyprovertransport.ErrorCodeMethodNotAllowed, errorResponse.Code)
}

func TestHandlerDelegatesProofRouteMethodValidation(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, DefaultRuntimeInfo(), "", DefaultMaxRequestBz)

	recorder := httptest.NewRecorder()
	request := newServiceRequest(http.MethodGet, privacyprovertransport.TransferProofPath, nil)
	request.Header.Set("Content-Encoding", "br")
	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusMethodNotAllowed, recorder.Code)
	require.Equal(t, http.MethodPost, recorder.Header().Get("Allow"))

	errorResponse, err := privacyprovertransport.DecodeErrorResponseJSON(recorder.Body.Bytes())
	require.NoError(t, err)
	require.Equal(t, privacyprovertransport.ErrorCodeMethodNotAllowed, errorResponse.Code)
}

func TestHandlerLimitsProofRequestBody(t *testing.T) {
	handler := NewHandler(stubTransferProver{}, nil, nil, nil, DefaultRuntimeInfo(), "", 1)

	recorder := httptest.NewRecorder()
	request := newServiceRequest(http.MethodPost, privacyprovertransport.TransferProofPath, bytes.NewBufferString("{}"))
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
	request := newServiceRequest(http.MethodPost, privacyprovertransport.TransferProofPath, bytes.NewReader(compressed.Bytes()))
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
	request := newServiceRequest(http.MethodPost, privacyprovertransport.TransferProofPath, bytes.NewBufferString("{}"))
	request.Header.Set("Content-Encoding", "br")
	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	errorResponse, err := privacyprovertransport.DecodeErrorResponseJSON(recorder.Body.Bytes())
	require.NoError(t, err)
	require.Equal(t, privacyprovertransport.ErrorCodeInvalidRequest, errorResponse.Code)
}

func TestHandlerRejectsAmbiguousProofRequestContentEncoding(t *testing.T) {
	tests := []struct {
		name   string
		values []string
	}{
		{name: "unsupported repeated value", values: []string{"identity", "br"}},
		{name: "comma-separated values", values: []string{"identity, br"}},
		{name: "multiple supported values", values: []string{"identity", "gzip"}},
		{name: "repeated gzip", values: []string{"gzip", "gzip"}},
		{name: "empty value", values: []string{""}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := NewHandlerWithProverSet(
				privacyprovertransport.ProverSet{Deposit: stubDepositProver{}},
				nil,
				nil,
				RuntimeInfo{},
				"",
				DefaultMaxRequestBz,
				mustDefaultAdmissionController(),
			)
			recorder := httptest.NewRecorder()
			request := newServiceRequest(http.MethodPost, privacyprovertransport.DepositProofPath, bytes.NewBufferString(`{}`))
			request.Header["Content-Encoding"] = append([]string(nil), test.values...)
			handler.ServeHTTP(recorder, request)

			require.Equal(t, http.StatusBadRequest, recorder.Code)
			errorResponse, err := privacyprovertransport.DecodeErrorResponseJSON(recorder.Body.Bytes())
			require.NoError(t, err)
			require.Equal(t, privacyprovertransport.ErrorCodeInvalidRequest, errorResponse.Code)
			require.Equal(t, "unsupported proof request content encoding", errorResponse.Message)
		})
	}
}

func TestHandlerRejectsMediaTypeBeforeReadingGzipBody(t *testing.T) {
	handler := NewHandler(stubTransferProver{}, nil, nil, nil, DefaultRuntimeInfo(), "", DefaultMaxRequestBz)
	for _, contentType := range []string{"text/plain"} {
		t.Run(contentType, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, privacyprovertransport.TransferProofPath, bytes.NewBufferString("not-a-gzip-frame"))
			request.Header.Set("Content-Type", contentType)
			request.Header.Set("Content-Encoding", "gzip")
			handler.ServeHTTP(recorder, request)

			require.Equal(t, http.StatusUnsupportedMediaType, recorder.Code)
			require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
			errorResponse, err := privacyprovertransport.DecodeErrorResponseJSON(recorder.Body.Bytes())
			require.NoError(t, err)
			require.Equal(t, privacyprovertransport.ErrorCodeInvalidRequest, errorResponse.Code)
			require.Equal(t, "proof route requires application/json content type", errorResponse.Message)
		})
	}
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
	request := newServiceRequest(http.MethodPost, privacyprovertransport.TransferProofPath, bytes.NewBufferString(`{"version":"v2"}`))
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
	request := newServiceRequest(http.MethodPost, privacyprovertransport.BatchTransferProofPath, bytes.NewBufferString(`{"version":"v1"}`))
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
	request := newServiceRequest(http.MethodPost, privacyprovertransport.TransferProofPath, bytes.NewBufferString("{}"))
	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)

	errorResponse, err := privacyprovertransport.DecodeErrorResponseJSON(recorder.Body.Bytes())
	require.NoError(t, err)
	require.Equal(t, privacyprovertransport.ErrorCodeUnauthorized, errorResponse.Code)
	require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
}

func TestHandlerAppliesAuthAndEncodingBoundaryToDepositRoute(t *testing.T) {
	info := DefaultRuntimeInfo()
	info.AuthEnabled = true
	handler := NewHandlerWithProverSet(
		privacyprovertransport.ProverSet{Deposit: stubDepositProver{}},
		nil,
		nil,
		info,
		"secret-token",
		DefaultMaxRequestBz,
		mustDefaultAdmissionController(),
	)

	t.Run("unauthorized", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		request := newServiceRequest(http.MethodPost, privacyprovertransport.DepositProofPath, bytes.NewBufferString(`{}`))
		handler.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusUnauthorized, recorder.Code)
		require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
	})

	t.Run("unsupported encoding", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		request := newServiceRequest(http.MethodPost, privacyprovertransport.DepositProofPath, bytes.NewBufferString(`{}`))
		request.Header.Set("Authorization", "Bearer secret-token")
		request.Header.Set("Content-Encoding", "br")
		handler.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusBadRequest, recorder.Code)
		require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
	})
}

func TestReferenceHandlerWiresDepositProverAndRequiredCircuitInventory(t *testing.T) {
	handler := NewReferenceHandler(nil, io.Discard, DefaultMaxRequestBz, "")
	require.NotNil(t, handler)
	require.NotNil(t, handler.proverHandler)
	require.NotNil(t, handler.proverHandler.DepositProver)
	require.Equal(t, circuitIDStrings(privacyzk.RequiredCircuitIDs()), handler.info.Circuits)
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
