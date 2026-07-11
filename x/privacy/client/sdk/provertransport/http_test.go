package provertransport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	privacytransfer "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/transfer"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
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

func TestHTTPProverClientBatchTransferRoundTrip(t *testing.T) {
	const bearerToken = "batch-prover-test-token"
	payload, artifacts, runner := testPreparedBatchTransferPayload(t)
	request, err := NewBatchTransferProofRequest(payload)
	require.NoError(t, err)
	admission := &recordingAdmission{}
	handler := NewHTTPHandlerWithBatchAdmission(
		nil,
		nil,
		ReferenceBatchTransferProver{Artifacts: artifacts, Runner: runner},
		nil,
		admission,
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer "+bearerToken, r.Header.Get("Authorization"))
		handler.ServeHTTP(w, r)
	}))
	defer server.Close()

	client := HTTPProverClient{BaseURL: server.URL, Client: server.Client(), BearerToken: bearerToken}
	response, err := client.ProveBatchTransfer(context.Background(), *request)
	require.NoError(t, err)
	require.NoError(t, ValidateBatchTransferProofResponse(*request, *response))
	require.Equal(t, BatchTransferProofCircuitID, admission.circuitID)
	require.Equal(t, 1, admission.permit.started)
	require.Equal(t, 1, admission.permit.released)
}

func TestHTTPProverClientRejectsNonLoopbackPlainHTTPBeforeSend(t *testing.T) {
	transport := &countingRoundTripper{}
	client := HTTPProverClient{BaseURL: "http://192.0.2.10:8080", Client: &http.Client{Transport: transport}}

	_, err := client.doJSONRequest(context.Background(), BatchTransferProofPath, struct{}{})
	require.ErrorContains(t, err, "requires HTTPS for non-loopback endpoint")
	require.Zero(t, transport.calls)
}

func TestHTTPProverClientDoesNotFollowProofRequestRedirects(t *testing.T) {
	destinationCalls := 0
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		destinationCalls++
		w.WriteHeader(http.StatusOK)
	}))
	defer destination.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()

	client := HTTPProverClient{BaseURL: source.URL, Client: source.Client()}
	_, err := client.doJSONRequest(context.Background(), BatchTransferProofPath, struct{}{})
	require.ErrorContains(t, err, "status 307")
	require.Zero(t, destinationCalls)
}

func TestHTTPProverClientRejectsUnmarkedCustomDoerBeforeSend(t *testing.T) {
	doer := &countingHTTPDoer{}
	client := HTTPProverClient{BaseURL: "https://prover.example", Client: doer}

	_, err := client.doJSONRequest(context.Background(), BatchTransferProofPath, struct{}{})
	require.ErrorContains(t, err, "must implement RedirectSafeHTTPDoer")
	require.Zero(t, doer.calls)
}

func TestHTTPHandlerRejectsNullBatchMessageOutputWithoutPanicking(t *testing.T) {
	payload, _, _ := testPreparedBatchTransferPayload(t)
	payload.MessageOutputs = append([]*privacytypes.BatchTransferOutput(nil), payload.MessageOutputs...)
	payload.MessageOutputs[0] = nil
	request := BatchTransferProofRequest{Version: BatchTransferProofRequestVersion, Payload: payload}
	requestBody, err := request.MarshalIndentedJSON()
	require.NoError(t, err)
	prover := &countingBatchTransferProver{}
	handler := NewHTTPHandlerWithBatchAdmission(nil, nil, prover, nil, &recordingAdmission{})

	recorder := httptest.NewRecorder()
	httpRequest := httptest.NewRequest(http.MethodPost, BatchTransferProofPath, bytesReader(requestBody))
	handler.ServeHTTP(recorder, httpRequest)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Zero(t, prover.calls)
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

func TestHTTPHandlerAcquiresAfterFramingBeforeSemanticValidation(t *testing.T) {
	admission := &recordingAdmission{}
	handler := NewHTTPHandlerWithAdmission(&countingTransferProver{}, nil, nil, admission)
	recorder := httptest.NewRecorder()
	httpRequest := httptest.NewRequest(http.MethodPost, TransferProofPath, strings.NewReader(`{"version":"v2"}`))
	handler.ServeHTTP(recorder, httpRequest)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Equal(t, 1, admission.acquired)
	require.Equal(t, TransferProofCircuitID, admission.circuitID)
	require.Equal(t, 0, admission.permit.started)
	require.Equal(t, 1, admission.permit.released)

	// Malformed JSON is rejected before admission and cannot consume a permit.
	recorder = httptest.NewRecorder()
	httpRequest = httptest.NewRequest(http.MethodPost, TransferProofPath, strings.NewReader(`{"version":`))
	handler.ServeHTTP(recorder, httpRequest)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Equal(t, 1, admission.acquired)
}

func TestHTTPHandlerUsesSpendAdmissionForWithdraw(t *testing.T) {
	admission := &recordingAdmission{}
	handler := NewHTTPHandlerWithAdmission(nil, &countingWithdrawProver{}, nil, admission)
	recorder := httptest.NewRecorder()
	httpRequest := httptest.NewRequest(http.MethodPost, WithdrawProofPath, strings.NewReader(`{"version":"v2"}`))
	handler.ServeHTTP(recorder, httpRequest)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Equal(t, 1, admission.acquired)
	require.Equal(t, WithdrawProofCircuitID, admission.circuitID)
	require.Equal(t, 0, admission.permit.started)
	require.Equal(t, 1, admission.permit.released)
}

func TestHTTPHandlerUsesBatchSpecificAdmissionAfterFraming(t *testing.T) {
	admission := &recordingAdmission{}
	handler := NewHTTPHandlerWithBatchAdmission(nil, nil, &countingBatchTransferProver{}, nil, admission)
	recorder := httptest.NewRecorder()
	httpRequest := httptest.NewRequest(http.MethodPost, BatchTransferProofPath, strings.NewReader(`{"version":"v1"}`))
	handler.ServeHTTP(recorder, httpRequest)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Equal(t, 1, admission.acquired)
	require.Equal(t, BatchTransferProofCircuitID, admission.circuitID)
	require.Equal(t, 0, admission.permit.started)
	require.Equal(t, 1, admission.permit.released)
	errorResponse, err := DecodeErrorResponseJSON(recorder.Body.Bytes())
	require.NoError(t, err)
	require.Equal(t, ErrorCodeInvalidRequest, errorResponse.Code)
	require.NotContains(t, errorResponse.Message, "payload")

	recorder = httptest.NewRecorder()
	httpRequest = httptest.NewRequest(http.MethodPost, BatchTransferProofPath, strings.NewReader(`{"version":`))
	handler.ServeHTTP(recorder, httpRequest)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Equal(t, 1, admission.acquired)
}

func TestHTTPHandlerRejectsOversizedBatchBeforeAdmission(t *testing.T) {
	admission := &recordingAdmission{}
	handler := NewHTTPHandlerWithBatchAdmission(nil, nil, &countingBatchTransferProver{}, nil, admission)
	handler.MaxRequestBytes = 8
	recorder := httptest.NewRecorder()
	httpRequest := httptest.NewRequest(http.MethodPost, BatchTransferProofPath, strings.NewReader(`{"version":"v1"}`))
	handler.ServeHTTP(recorder, httpRequest)

	require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
	require.Zero(t, admission.acquired)
	errorResponse, err := DecodeErrorResponseJSON(recorder.Body.Bytes())
	require.NoError(t, err)
	require.Equal(t, ErrorCodeInvalidRequest, errorResponse.Code)
}

func TestHTTPHandlerRejectsOversizedTransferAndWithdrawBeforeAdmission(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		handler func(ProofAdmission) *HTTPHandler
	}{
		{
			name: "transfer",
			path: TransferProofPath,
			handler: func(admission ProofAdmission) *HTTPHandler {
				return NewHTTPHandlerWithAdmission(&countingTransferProver{}, nil, nil, admission)
			},
		},
		{
			name: "withdraw",
			path: WithdrawProofPath,
			handler: func(admission ProofAdmission) *HTTPHandler {
				return NewHTTPHandlerWithAdmission(nil, &countingWithdrawProver{}, nil, admission)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			admission := &recordingAdmission{}
			handler := tt.handler(admission)
			handler.MaxRequestBytes = 8
			recorder := httptest.NewRecorder()
			httpRequest := httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(`{"version":"v1"}`))
			handler.ServeHTTP(recorder, httpRequest)

			require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
			require.Zero(t, admission.acquired)
			errorResponse, err := DecodeErrorResponseJSON(recorder.Body.Bytes())
			require.NoError(t, err)
			require.Equal(t, ErrorCodeInvalidRequest, errorResponse.Code)
			require.NotContains(t, errorResponse.Message, "version")
		})
	}
}

func TestHTTPHandlerBatchErrorsDoNotEchoPayload(t *testing.T) {
	const canary = "secret-batch-witness-canary"
	handler := NewHTTPHandlerWithBatchAdmission(nil, nil, &countingBatchTransferProver{}, nil, &recordingAdmission{})
	recorder := httptest.NewRecorder()
	httpRequest := httptest.NewRequest(http.MethodPost, BatchTransferProofPath, strings.NewReader(`{"version":"v1","payload":{},"secret":"`+canary+`"}`))
	handler.ServeHTTP(recorder, httpRequest)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.NotContains(t, recorder.Body.String(), canary)
	errorResponse, err := DecodeErrorResponseJSON(recorder.Body.Bytes())
	require.NoError(t, err)
	require.Equal(t, ErrorCodeInvalidRequest, errorResponse.Code)
}

func TestHTTPHandlerAdmissionRejectsBeforeTypedDecode(t *testing.T) {
	handler := NewHTTPHandlerWithAdmission(&countingTransferProver{}, nil, nil, rejectingAdmission{})
	recorder := httptest.NewRecorder()
	// This is valid JSON framing but cannot decode into the typed payload. A
	// 429 proves admission runs before dynamic typed materialization.
	httpRequest := httptest.NewRequest(http.MethodPost, TransferProofPath, strings.NewReader(`{"version":"v2","payload":{"inputs":"not-an-array"}}`))
	handler.ServeHTTP(recorder, httpRequest)
	require.Equal(t, http.StatusTooManyRequests, recorder.Code)
	errorResponse, err := DecodeErrorResponseJSON(recorder.Body.Bytes())
	require.NoError(t, err)
	require.Equal(t, ErrorCodeBusy, errorResponse.Code)
	require.True(t, errorResponse.Retryable)
}

func TestHTTPHandlerDoesNotReleasePermitWhenContextCancelsDuringProve(t *testing.T) {
	payload, _, _ := testPreparedTransferPayload(t)
	request, err := NewTransferProofRequest(payload)
	require.NoError(t, err)
	requestBody, err := request.MarshalIndentedJSON()
	require.NoError(t, err)

	prover := &blockingTransferProver{started: make(chan struct{}), finish: make(chan struct{})}
	admission := &recordingAdmission{permit: &recordingPermit{releasedCh: make(chan struct{}, 1)}}
	handler := NewHTTPHandlerWithAdmission(prover, nil, nil, admission)
	ctx, cancel := context.WithCancel(context.Background())
	httpRequest := httptest.NewRequest(http.MethodPost, TransferProofPath, bytesReader(requestBody)).WithContext(ctx)
	recorder := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.ServeHTTP(recorder, httpRequest)
	}()
	<-prover.started
	cancel()
	select {
	case <-admission.permit.releasedCh:
		t.Fatal("permit released while prove was still running")
	case <-time.After(20 * time.Millisecond):
	}
	close(prover.finish)
	<-done
	require.Equal(t, 1, admission.permit.started)
	require.Equal(t, 1, admission.permit.released)
}

func TestBatchHTTPHandlerHoldsPermitUntilProveActuallyReturns(t *testing.T) {
	payload, _, _ := testPreparedBatchTransferPayload(t)
	request, err := NewBatchTransferProofRequest(payload)
	require.NoError(t, err)
	requestBody, err := request.MarshalIndentedJSON()
	require.NoError(t, err)

	prover := &blockingBatchTransferProver{started: make(chan struct{}), finish: make(chan struct{})}
	admission := &recordingAdmission{permit: &recordingPermit{releasedCh: make(chan struct{}, 1)}}
	handler := NewHTTPHandlerWithBatchAdmission(nil, nil, prover, nil, admission)
	ctx, cancel := context.WithCancel(context.Background())
	httpRequest := httptest.NewRequest(http.MethodPost, BatchTransferProofPath, bytesReader(requestBody)).WithContext(ctx)
	recorder := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.ServeHTTP(recorder, httpRequest)
	}()
	<-prover.started
	cancel()
	select {
	case <-admission.permit.releasedCh:
		t.Fatal("batch permit released while prove was still running")
	case <-time.After(20 * time.Millisecond):
	}
	close(prover.finish)
	<-done
	require.Equal(t, 1, admission.permit.started)
	require.Equal(t, 1, admission.permit.released)
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

func TestHTTPProverClientPreservesRetryableErrorWithoutFailover(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeRetryableErrorResponse(w, http.StatusTooManyRequests, ErrorCodeBusy, "capacity unavailable")
	}))
	defer server.Close()

	payload, _, _ := testPreparedTransferPayload(t)
	request, err := NewTransferProofRequest(payload)
	require.NoError(t, err)
	client := HTTPProverClient{BaseURL: server.URL, Client: server.Client()}
	_, err = client.ProveTransfer(context.Background(), *request)
	require.Error(t, err)
	var httpErr *HTTPError
	require.True(t, errors.As(err, &httpErr))
	require.Equal(t, http.StatusTooManyRequests, httpErr.StatusCode)
	require.Equal(t, ErrorCodeBusy, httpErr.Response.Code)
	require.True(t, httpErr.Retryable())
}

func TestDecodeErrorResponseRejectsInvalidRetryability(t *testing.T) {
	_, err := DecodeErrorResponseJSON([]byte(`{"version":"v1","code":"busy","message":"busy"}`))
	require.ErrorContains(t, err, "must be retryable")
	_, err = DecodeErrorResponseJSON([]byte(`{"version":"v1","code":"invalid_request","message":"bad","retryable":true}`))
	require.ErrorContains(t, err, "only busy")
}

func bytesReader(bz []byte) *bytes.Reader {
	return bytes.NewReader(bz)
}

type countingTransferProver struct {
	calls int
}

type countingRoundTripper struct {
	calls int
}

func (t *countingRoundTripper) RoundTrip(_ *http.Request) (*http.Response, error) {
	t.calls++
	return nil, errors.New("unexpected HTTP request")
}

type countingHTTPDoer struct {
	calls int
}

func (d *countingHTTPDoer) Do(_ *http.Request) (*http.Response, error) {
	d.calls++
	return nil, errors.New("unexpected HTTP request")
}

type countingWithdrawProver struct{}

type countingBatchTransferProver struct{ calls int }

func (*countingWithdrawProver) ProveWithdraw(WithdrawProofRequest, time.Time) (*WithdrawProofResponse, error) {
	return nil, fmt.Errorf("unexpected withdraw proof request")
}

func (p *countingBatchTransferProver) ProveBatchTransfer(BatchTransferProofRequest, time.Time) (*BatchTransferProofResponse, error) {
	p.calls++
	return nil, fmt.Errorf("unexpected batch transfer proof request")
}

func (p *countingTransferProver) ProveTransfer(TransferProofRequest, time.Time) (*TransferProofResponse, error) {
	p.calls++
	return nil, fmt.Errorf("unexpected proof request")
}

type clockCapturingTransferProver struct {
	now time.Time
}

type recordingAdmission struct {
	acquired  int
	circuitID string
	permit    *recordingPermit
}

type rejectingAdmission struct{}

func (rejectingAdmission) Acquire(context.Context, string) (ProofPermit, error) {
	return nil, fmt.Errorf("queue full")
}

func (a *recordingAdmission) Acquire(_ context.Context, circuitID string) (ProofPermit, error) {
	a.acquired++
	a.circuitID = circuitID
	if a.permit == nil {
		a.permit = &recordingPermit{}
	}
	return a.permit, nil
}

type recordingPermit struct {
	started    int
	released   int
	releasedCh chan struct{}
	once       sync.Once
}

func (p *recordingPermit) StartProve() { p.started++ }

func (p *recordingPermit) Release() {
	p.once.Do(func() {
		p.released++
		if p.releasedCh != nil {
			p.releasedCh <- struct{}{}
		}
	})
}

type blockingTransferProver struct {
	started chan struct{}
	finish  chan struct{}
}

type blockingBatchTransferProver struct {
	started chan struct{}
	finish  chan struct{}
}

func (p *blockingBatchTransferProver) ProveBatchTransfer(BatchTransferProofRequest, time.Time) (*BatchTransferProofResponse, error) {
	close(p.started)
	<-p.finish
	return nil, fmt.Errorf("batch proof stopped after test release")
}

func (p *blockingTransferProver) ProveTransfer(TransferProofRequest, time.Time) (*TransferProofResponse, error) {
	close(p.started)
	<-p.finish
	return nil, fmt.Errorf("proof stopped after test release")
}

func (p *clockCapturingTransferProver) ProveTransfer(_ TransferProofRequest, now time.Time) (*TransferProofResponse, error) {
	p.now = now
	return nil, fmt.Errorf("stop after capturing clock")
}
