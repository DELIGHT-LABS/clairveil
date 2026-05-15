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
	TransferProofPath    = "/v1/prover/transfer"
	WithdrawProofPath    = "/v1/prover/withdraw"
	ErrorResponseVersion = "v1"
)

const (
	ErrorCodeInvalidRequest   = "invalid_request"
	ErrorCodeMethodNotAllowed = "method_not_allowed"
	ErrorCodeNotFound         = "not_found"
	ErrorCodeUnauthorized     = "unauthorized"
	ErrorCodeUnavailable      = "unavailable"
	ErrorCodeProofFailed      = "proof_failed"
)

type ErrorResponse struct {
	Version string `json:"version"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type TransferProver interface {
	ProveTransfer(request TransferProofRequest) (*TransferProofResponse, error)
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
}

type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type HTTPProverClient struct {
	BaseURL string
	Client  HTTPDoer
	Now     func() time.Time
}

func (p ReferenceTransferProver) ProveTransfer(request TransferProofRequest) (*TransferProofResponse, error) {
	return BuildTransferProofResponse(request, p.Artifacts, p.Runner)
}

func (p ReferenceWithdrawProver) ProveWithdraw(request WithdrawProofRequest, now time.Time) (*WithdrawProofResponse, error) {
	return BuildWithdrawProofResponse(request, p.Artifacts, p.Runner, now)
}

func NewHTTPHandler(transferProver TransferProver, withdrawProver WithdrawProver, now func() time.Time) *HTTPHandler {
	if now == nil {
		now = time.Now
	}
	return &HTTPHandler{
		TransferProver: transferProver,
		WithdrawProver: withdrawProver,
		Now:            now,
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
	request, err := DecodeTransferProofRequestJSON(requestBytes)
	if err != nil {
		writeErrorResponse(w, http.StatusBadRequest, ErrorCodeInvalidRequest, err.Error())
		return
	}

	response, err := h.TransferProver.ProveTransfer(*request)
	if err != nil {
		writeErrorResponse(w, http.StatusBadRequest, ErrorCodeProofFailed, err.Error())
		return
	}
	if err := ValidateTransferProofResponse(*request, *response); err != nil {
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
	request, err := DecodeWithdrawProofRequestJSON(requestBytes)
	if err != nil {
		writeErrorResponse(w, http.StatusBadRequest, ErrorCodeInvalidRequest, err.Error())
		return
	}

	response, err := h.WithdrawProver.ProveWithdraw(*request, h.Now())
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

func (c HTTPProverClient) ProveTransfer(ctx context.Context, request TransferProofRequest) (*TransferProofResponse, error) {
	if err := ValidateTransferProofRequest(request); err != nil {
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
	if err := ValidateTransferProofResponse(request, *response); err != nil {
		return nil, err
	}
	return response, nil
}

func (c HTTPProverClient) ProveWithdraw(ctx context.Context, request WithdrawProofRequest) (*WithdrawProofResponse, error) {
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	currentTime := now()

	if err := ValidateWithdrawProofRequest(request, currentTime); err != nil {
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
	if err := ValidateWithdrawProofResponse(request, *response, currentTime); err != nil {
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

	responseBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		if errorResponse, decodeErr := DecodeErrorResponseJSON(responseBytes); decodeErr == nil {
			return nil, fmt.Errorf("prover transport request failed (%s): %s", errorResponse.Code, errorResponse.Message)
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
	return &response, nil
}

func writeErrorResponse(w http.ResponseWriter, status int, code string, message string) {
	writeJSON(w, status, ErrorResponse{
		Version: ErrorResponseVersion,
		Code:    code,
		Message: message,
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
