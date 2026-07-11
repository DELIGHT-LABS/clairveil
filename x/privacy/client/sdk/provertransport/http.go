package provertransport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	privacytransfer "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/transfer"
	privacywithdraw "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/withdraw"
)

const (
	TransferProofPath             = "/v1/prover/transfer"
	WithdrawProofPath             = "/v1/prover/withdraw"
	TransferProofCircuitID        = "joinsplit"
	WithdrawProofCircuitID        = "spend"
	ErrorResponseVersion          = "v1"
	DefaultMaxResponseBytes int64 = 1 << 20
)

const (
	ErrorCodeInvalidRequest   = "invalid_request"
	ErrorCodeMethodNotAllowed = "method_not_allowed"
	ErrorCodeNotFound         = "not_found"
	ErrorCodeUnauthorized     = "unauthorized"
	ErrorCodeUnavailable      = "unavailable"
	ErrorCodeProofFailed      = "proof_failed"
	ErrorCodeBusy             = "busy"
)

type ErrorResponse struct {
	Version   string `json:"version"`
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable,omitempty"`
}

// HTTPError preserves retryability without performing any automatic retry or
// failover. Callers remain responsible for an explicit privacy-aware policy.
type HTTPError struct {
	StatusCode int
	Response   ErrorResponse
}

func (e *HTTPError) Error() string {
	if e == nil {
		return "prover transport request failed"
	}
	return fmt.Sprintf("prover transport request failed (%s): %s", e.Response.Code, e.Response.Message)
}

func (e *HTTPError) Retryable() bool {
	return e != nil && e.Response.Retryable
}

// ProofPermit is acquired after cheap body/framing checks and before semantic
// or cryptographic validation. Release must not be called until an actual
// prover invocation has returned.
type ProofPermit interface {
	StartProve()
	Release()
}

type ProofAdmission interface {
	Acquire(ctx context.Context, circuitID string) (ProofPermit, error)
}

type TransferProver interface {
	ProveTransfer(request TransferProofRequest, now time.Time) (*TransferProofResponse, error)
}

type WithdrawProver interface {
	ProveWithdraw(request WithdrawProofRequest, now time.Time) (*WithdrawProofResponse, error)
}

type ReferenceTransferProver struct {
	Artifacts privacytransfer.JoinSplitArtifactProvider
	Runner    privacytransfer.JoinSplitProofRunner
}

type ReferenceWithdrawProver struct {
	Artifacts privacywithdraw.SpendArtifactProvider
	Runner    privacywithdraw.SpendProofRunner
}

type HTTPHandler struct {
	TransferProver TransferProver
	WithdrawProver WithdrawProver
	Now            func() time.Time
	Admission      ProofAdmission
}

type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type HTTPProverClient struct {
	BaseURL          string
	Client           HTTPDoer
	Now              func() time.Time
	MaxResponseBytes int64
}

func (p ReferenceTransferProver) ProveTransfer(request TransferProofRequest, now time.Time) (*TransferProofResponse, error) {
	return BuildTransferProofResponseAt(request, p.Artifacts, p.Runner, now)
}

func (p ReferenceWithdrawProver) ProveWithdraw(request WithdrawProofRequest, now time.Time) (*WithdrawProofResponse, error) {
	return BuildWithdrawProofResponse(request, p.Artifacts, p.Runner, now)
}

func NewHTTPHandler(transferProver TransferProver, withdrawProver WithdrawProver, now func() time.Time) *HTTPHandler {
	return NewHTTPHandlerWithAdmission(transferProver, withdrawProver, now, nil)
}

func NewHTTPHandlerWithAdmission(transferProver TransferProver, withdrawProver WithdrawProver, now func() time.Time, admission ProofAdmission) *HTTPHandler {
	if now == nil {
		now = time.Now
	}
	return &HTTPHandler{
		TransferProver: transferProver,
		WithdrawProver: withdrawProver,
		Now:            now,
		Admission:      admission,
	}
}

func (h *HTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h == nil {
		writeErrorResponse(w, http.StatusServiceUnavailable, ErrorCodeUnavailable, "prover transport handler is unavailable")
		return
	}

	switch r.URL.Path {
	case TransferProofPath:
		h.serveTransferProof(w, r)
	case WithdrawProofPath:
		h.serveWithdrawProof(w, r)
	default:
		writeErrorResponse(w, http.StatusNotFound, ErrorCodeNotFound, "prover transport route not found")
	}
}

func (h *HTTPHandler) serveTransferProof(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorResponse(w, http.StatusMethodNotAllowed, ErrorCodeMethodNotAllowed, "transfer proof route requires POST")
		return
	}
	if h.TransferProver == nil {
		writeErrorResponse(w, http.StatusServiceUnavailable, ErrorCodeUnavailable, "transfer prover is unavailable")
		return
	}

	requestBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeErrorResponse(w, http.StatusBadRequest, ErrorCodeInvalidRequest, fmt.Sprintf("failed to read transfer proof request body: %v", err))
		return
	}
	if !json.Valid(requestBytes) {
		writeErrorResponse(w, http.StatusBadRequest, ErrorCodeInvalidRequest, "invalid transfer proof request JSON framing")
		return
	}
	permit, ok := h.acquirePermit(w, r, TransferProofCircuitID)
	if !ok {
		return
	}
	permitReleased := false
	defer func() {
		if permit != nil && !permitReleased {
			permit.Release()
		}
	}()
	request, err := DecodeTransferProofRequestJSON(requestBytes)
	if err != nil {
		writeErrorResponse(w, http.StatusBadRequest, ErrorCodeInvalidRequest, err.Error())
		return
	}
	currentTime := h.Now()
	if err := ValidateTransferProofRequestAt(*request, currentTime); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, ErrorCodeInvalidRequest, err.Error())
		return
	}

	if permit != nil {
		permit.StartProve()
	}
	response, err := h.TransferProver.ProveTransfer(*request, currentTime)
	if permit != nil {
		permit.Release()
		permitReleased = true
	}
	if err != nil {
		writeErrorResponse(w, http.StatusBadRequest, ErrorCodeProofFailed, err.Error())
		return
	}
	if err := ValidateTransferProofResponseAt(*request, *response, h.Now()); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, ErrorCodeProofFailed, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *HTTPHandler) serveWithdrawProof(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorResponse(w, http.StatusMethodNotAllowed, ErrorCodeMethodNotAllowed, "withdraw proof route requires POST")
		return
	}
	if h.WithdrawProver == nil {
		writeErrorResponse(w, http.StatusServiceUnavailable, ErrorCodeUnavailable, "withdraw prover is unavailable")
		return
	}

	requestBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeErrorResponse(w, http.StatusBadRequest, ErrorCodeInvalidRequest, fmt.Sprintf("failed to read withdraw proof request body: %v", err))
		return
	}
	if !json.Valid(requestBytes) {
		writeErrorResponse(w, http.StatusBadRequest, ErrorCodeInvalidRequest, "invalid withdraw proof request JSON framing")
		return
	}
	permit, ok := h.acquirePermit(w, r, WithdrawProofCircuitID)
	if !ok {
		return
	}
	permitReleased := false
	defer func() {
		if permit != nil && !permitReleased {
			permit.Release()
		}
	}()
	request, err := DecodeWithdrawProofRequestJSON(requestBytes)
	if err != nil {
		writeErrorResponse(w, http.StatusBadRequest, ErrorCodeInvalidRequest, err.Error())
		return
	}
	if err := ValidateWithdrawProofRequest(*request, h.Now()); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, ErrorCodeInvalidRequest, err.Error())
		return
	}

	if permit != nil {
		permit.StartProve()
	}
	response, err := h.WithdrawProver.ProveWithdraw(*request, h.Now())
	if permit != nil {
		permit.Release()
		permitReleased = true
	}
	if err != nil {
		writeErrorResponse(w, http.StatusBadRequest, ErrorCodeProofFailed, err.Error())
		return
	}
	if err := ValidateWithdrawProofResponse(*request, *response, h.Now()); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, ErrorCodeProofFailed, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *HTTPHandler) acquirePermit(w http.ResponseWriter, r *http.Request, circuitID string) (ProofPermit, bool) {
	if h.Admission == nil {
		return nil, true
	}
	permit, err := h.Admission.Acquire(r.Context(), circuitID)
	if err != nil {
		writeRetryableErrorResponse(w, http.StatusTooManyRequests, ErrorCodeBusy, "prover capacity is temporarily unavailable")
		return nil, false
	}
	if permit == nil {
		writeRetryableErrorResponse(w, http.StatusTooManyRequests, ErrorCodeBusy, "prover capacity is temporarily unavailable")
		return nil, false
	}
	return permit, true
}

func (c HTTPProverClient) ProveTransfer(ctx context.Context, request TransferProofRequest) (*TransferProofResponse, error) {
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	if err := ValidateTransferProofRequestAt(request, now()); err != nil {
		return nil, err
	}
	responseBytes, err := c.doJSONRequest(ctx, TransferProofPath, request)
	if err != nil {
		return nil, err
	}
	response, err := DecodeTransferProofResponseJSON(responseBytes)
	if err != nil {
		return nil, err
	}
	if err := ValidateTransferProofResponseAt(request, *response, now()); err != nil {
		return nil, err
	}
	return response, nil
}

func (c HTTPProverClient) ProveWithdraw(ctx context.Context, request WithdrawProofRequest) (*WithdrawProofResponse, error) {
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	if err := ValidateWithdrawProofRequest(request, now()); err != nil {
		return nil, err
	}
	responseBytes, err := c.doJSONRequest(ctx, WithdrawProofPath, request)
	if err != nil {
		return nil, err
	}
	response, err := DecodeWithdrawProofResponseJSON(responseBytes)
	if err != nil {
		return nil, err
	}
	if err := ValidateWithdrawProofResponse(request, *response, now()); err != nil {
		return nil, err
	}
	return response, nil
}

func (c HTTPProverClient) doJSONRequest(ctx context.Context, path string, body interface{}) ([]byte, error) {
	if strings.TrimSpace(c.BaseURL) == "" {
		return nil, fmt.Errorf("prover transport client base URL is required")
	}
	if c.Client == nil {
		return nil, fmt.Errorf("prover transport client HTTP client is required")
	}

	requestBytes, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+path, bytes.NewReader(requestBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	responseLimit := c.MaxResponseBytes
	if responseLimit <= 0 {
		responseLimit = DefaultMaxResponseBytes
	}
	responseBytes, err := io.ReadAll(io.LimitReader(resp.Body, responseLimit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(responseBytes)) > responseLimit {
		return nil, fmt.Errorf("prover transport response exceeds %d bytes", responseLimit)
	}
	if resp.StatusCode != http.StatusOK {
		if errorResponse, decodeErr := DecodeErrorResponseJSON(responseBytes); decodeErr == nil {
			return nil, &HTTPError{StatusCode: resp.StatusCode, Response: *errorResponse}
		}
		return nil, fmt.Errorf("prover transport request failed with status %d", resp.StatusCode)
	}
	return responseBytes, nil
}

func DecodeErrorResponseJSON(payloadBytes []byte) (*ErrorResponse, error) {
	var response ErrorResponse
	if err := json.Unmarshal(payloadBytes, &response); err != nil {
		return nil, fmt.Errorf("invalid prover transport error JSON: %w", err)
	}
	if err := ValidateErrorResponse(response); err != nil {
		return nil, err
	}
	return &response, nil
}

func ValidateErrorResponse(response ErrorResponse) error {
	if response.Version != ErrorResponseVersion {
		return fmt.Errorf("invalid prover transport error version %q", response.Version)
	}
	switch response.Code {
	case ErrorCodeInvalidRequest,
		ErrorCodeMethodNotAllowed,
		ErrorCodeNotFound,
		ErrorCodeUnauthorized,
		ErrorCodeUnavailable,
		ErrorCodeProofFailed,
		ErrorCodeBusy:
	default:
		return fmt.Errorf("invalid prover transport error code %q", response.Code)
	}
	if strings.TrimSpace(response.Message) == "" {
		return fmt.Errorf("prover transport error message is required")
	}
	if response.Retryable && response.Code != ErrorCodeBusy {
		return fmt.Errorf("only %s prover transport errors may be retryable", ErrorCodeBusy)
	}
	if response.Code == ErrorCodeBusy && !response.Retryable {
		return fmt.Errorf("%s prover transport errors must be retryable", ErrorCodeBusy)
	}
	return nil
}

func writeErrorResponse(w http.ResponseWriter, status int, code string, message string) {
	writeJSON(w, status, ErrorResponse{
		Version: ErrorResponseVersion,
		Code:    code,
		Message: message,
	})
}

func writeRetryableErrorResponse(w http.ResponseWriter, status int, code string, message string) {
	writeJSON(w, status, ErrorResponse{
		Version:   ErrorResponseVersion,
		Code:      code,
		Message:   message,
		Retryable: true,
	})
}

func writeJSON(w http.ResponseWriter, status int, body interface{}) {
	responseBytes, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(responseBytes)
}
