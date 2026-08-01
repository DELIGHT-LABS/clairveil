package provertransport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	privacybatchtransfer "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/batchtransfer"
	privacydeposit "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/deposit"
	privacytransfer "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/transfer"
	privacywithdraw "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/withdraw"
)

const (
	TransferProofPath                 = "/v1/prover/transfer"
	WithdrawProofPath                 = "/v1/prover/withdraw"
	BatchTransferProofPath            = "/v1/proofs/batch-transfer"
	DepositProofPath                  = "/v1/prover/deposit"
	TransferProofCircuitID            = "joinsplit"
	WithdrawProofCircuitID            = "spend"
	BatchTransferProofCircuitID       = "batch-joinsplit-16x32-v1"
	DepositProofCircuitID             = "deposit"
	BearerTokenEnv                    = "CLAIRVEIL_PRIVACY_PROVER_BEARER_TOKEN"
	ErrorResponseVersion              = "v1"
	DefaultMaxResponseBytes     int64 = 1 << 20
	DefaultMaxRequestBytes      int64 = 8 << 20
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

type BatchTransferProver interface {
	ProveBatchTransfer(request BatchTransferProofRequest, now time.Time) (*BatchTransferProofResponse, error)
}

type DepositProver interface {
	ProveDeposit(request DepositProofRequest) (*DepositProofResponse, error)
}

type ReferenceDepositProver struct {
	Artifacts privacydeposit.DepositArtifactProvider
	Runner    privacydeposit.DepositProofRunner
}

type ReferenceTransferProver struct {
	Artifacts privacytransfer.JoinSplitArtifactProvider
	Runner    privacytransfer.JoinSplitProofRunner
}

type ReferenceWithdrawProver struct {
	Artifacts privacywithdraw.SpendArtifactProvider
	Runner    privacywithdraw.SpendProofRunner
}

type ReferenceBatchTransferProver struct {
	Artifacts privacybatchtransfer.BatchJoinSplitArtifactProvider
	Runner    privacybatchtransfer.BatchJoinSplitProofRunner
}

type HTTPHandler struct {
	DepositProver       DepositProver
	TransferProver      TransferProver
	WithdrawProver      WithdrawProver
	BatchTransferProver BatchTransferProver
	Now                 func() time.Time
	Admission           ProofAdmission
	MaxRequestBytes     int64
}

type ProverSet struct {
	Deposit       DepositProver
	Transfer      TransferProver
	Withdraw      WithdrawProver
	BatchTransfer BatchTransferProver
}

type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// RedirectSafeHTTPDoer is the opt-in contract for custom HTTPDoer wrappers.
// Returning true asserts that the implementation never follows redirects.
// A plain *http.Client does not need this marker because the prover client
// clones it with a fail-closed CheckRedirect policy.
type RedirectSafeHTTPDoer interface {
	HTTPDoer
	ProverTransportRedirectsDisabled() bool
}

type HTTPProverClient struct {
	BaseURL          string
	Client           HTTPDoer
	BearerToken      string
	Now              func() time.Time
	MaxResponseBytes int64
}

func (p ReferenceTransferProver) ProveTransfer(request TransferProofRequest, now time.Time) (*TransferProofResponse, error) {
	return BuildTransferProofResponseAt(request, p.Artifacts, p.Runner, now)
}

func (p ReferenceWithdrawProver) ProveWithdraw(request WithdrawProofRequest, now time.Time) (*WithdrawProofResponse, error) {
	return BuildWithdrawProofResponse(request, p.Artifacts, p.Runner, now)
}

func (p ReferenceBatchTransferProver) ProveBatchTransfer(request BatchTransferProofRequest, now time.Time) (*BatchTransferProofResponse, error) {
	return BuildBatchTransferProofResponseAt(request, p.Artifacts, p.Runner, now)
}

func (p ReferenceDepositProver) ProveDeposit(request DepositProofRequest) (*DepositProofResponse, error) {
	return BuildDepositProofResponse(request, p.Artifacts, p.Runner)
}

func NewHTTPHandler(transferProver TransferProver, withdrawProver WithdrawProver, now func() time.Time) *HTTPHandler {
	return NewHTTPHandlerWithAdmission(transferProver, withdrawProver, now, nil)
}

func NewHTTPHandlerWithAdmission(transferProver TransferProver, withdrawProver WithdrawProver, now func() time.Time, admission ProofAdmission) *HTTPHandler {
	return NewHTTPHandlerWithBatchAdmission(transferProver, withdrawProver, nil, now, admission)
}

func NewHTTPHandlerWithBatchAdmission(transferProver TransferProver, withdrawProver WithdrawProver, batchTransferProver BatchTransferProver, now func() time.Time, admission ProofAdmission) *HTTPHandler {
	return NewHTTPHandlerWithProverSet(ProverSet{
		Transfer:      transferProver,
		Withdraw:      withdrawProver,
		BatchTransfer: batchTransferProver,
	}, now, admission)
}

func NewHTTPHandlerWithProverSet(provers ProverSet, now func() time.Time, admission ProofAdmission) *HTTPHandler {
	if now == nil {
		now = time.Now
	}
	return &HTTPHandler{
		DepositProver:       provers.Deposit,
		TransferProver:      provers.Transfer,
		WithdrawProver:      provers.Withdraw,
		BatchTransferProver: provers.BatchTransfer,
		Now:                 now,
		Admission:           admission,
		MaxRequestBytes:     DefaultMaxRequestBytes,
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
	case BatchTransferProofPath:
		h.serveBatchTransferProof(w, r)
	case DepositProofPath:
		h.serveDepositProof(w, r)
	default:
		writeErrorResponse(w, http.StatusNotFound, ErrorCodeNotFound, "prover transport route not found")
	}
}

func (h *HTTPHandler) serveBatchTransferProof(w http.ResponseWriter, r *http.Request) {
	if !beginProofRequest(w, r, "batch transfer") {
		return
	}
	if h.BatchTransferProver == nil {
		writeErrorResponse(w, http.StatusServiceUnavailable, ErrorCodeUnavailable, "batch transfer prover is unavailable")
		return
	}

	requestBytes, ok := h.readProofRequestBody(w, r, "batch transfer")
	if !ok {
		return
	}
	if !json.Valid(requestBytes) {
		writeErrorResponse(w, http.StatusBadRequest, ErrorCodeInvalidRequest, "invalid batch transfer proof request JSON framing")
		return
	}
	permit, ok := h.acquirePermit(w, r, BatchTransferProofCircuitID)
	if !ok {
		return
	}
	permitReleased := false
	defer func() {
		if permit != nil && !permitReleased {
			permit.Release()
		}
	}()

	request, err := DecodeBatchTransferProofRequestJSON(requestBytes)
	if err != nil {
		writeErrorResponse(w, http.StatusBadRequest, ErrorCodeInvalidRequest, "invalid batch transfer proof request")
		return
	}
	currentTime := h.Now()
	if err := ValidateBatchTransferProofRequestAt(*request, currentTime); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, ErrorCodeInvalidRequest, "batch transfer proof request validation failed")
		return
	}

	if permit != nil {
		permit.StartProve()
	}
	response, err := h.BatchTransferProver.ProveBatchTransfer(*request, currentTime)
	if permit != nil {
		permit.Release()
		permitReleased = true
	}
	if err != nil {
		writeErrorResponse(w, http.StatusInternalServerError, ErrorCodeProofFailed, "batch transfer proof generation failed")
		return
	}
	if response == nil {
		writeErrorResponse(w, http.StatusInternalServerError, ErrorCodeProofFailed, "batch transfer proof generation failed")
		return
	}
	if err := ValidateBatchTransferProofResponseAt(*request, *response, h.Now()); err != nil {
		writeErrorResponse(w, http.StatusInternalServerError, ErrorCodeProofFailed, "batch transfer proof response validation failed")
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *HTTPHandler) serveTransferProof(w http.ResponseWriter, r *http.Request) {
	if !beginProofRequest(w, r, "transfer") {
		return
	}
	if h.TransferProver == nil {
		writeErrorResponse(w, http.StatusServiceUnavailable, ErrorCodeUnavailable, "transfer prover is unavailable")
		return
	}

	requestBytes, ok := h.readProofRequestBody(w, r, "transfer")
	if !ok {
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
		writeErrorResponse(w, http.StatusBadRequest, ErrorCodeInvalidRequest, "invalid transfer proof request")
		return
	}
	currentTime := h.Now()
	if err := ValidateTransferProofRequestAt(*request, currentTime); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, ErrorCodeInvalidRequest, "transfer proof request validation failed")
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
		writeErrorResponse(w, http.StatusInternalServerError, ErrorCodeProofFailed, "transfer proof generation failed")
		return
	}
	if response == nil {
		writeErrorResponse(w, http.StatusInternalServerError, ErrorCodeProofFailed, "transfer proof generation failed")
		return
	}
	if err := ValidateTransferProofResponseAt(*request, *response, h.Now()); err != nil {
		writeErrorResponse(w, http.StatusInternalServerError, ErrorCodeProofFailed, "transfer proof response validation failed")
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *HTTPHandler) serveWithdrawProof(w http.ResponseWriter, r *http.Request) {
	if !beginProofRequest(w, r, "withdraw") {
		return
	}
	if h.WithdrawProver == nil {
		writeErrorResponse(w, http.StatusServiceUnavailable, ErrorCodeUnavailable, "withdraw prover is unavailable")
		return
	}

	requestBytes, ok := h.readProofRequestBody(w, r, "withdraw")
	if !ok {
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
		writeErrorResponse(w, http.StatusBadRequest, ErrorCodeInvalidRequest, "invalid withdraw proof request")
		return
	}
	if err := ValidateWithdrawProofRequest(*request, h.Now()); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, ErrorCodeInvalidRequest, "withdraw proof request validation failed")
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
		writeErrorResponse(w, http.StatusInternalServerError, ErrorCodeProofFailed, "withdraw proof generation failed")
		return
	}
	if response == nil {
		writeErrorResponse(w, http.StatusInternalServerError, ErrorCodeProofFailed, "withdraw proof generation failed")
		return
	}
	if err := ValidateWithdrawProofResponse(*request, *response, h.Now()); err != nil {
		writeErrorResponse(w, http.StatusInternalServerError, ErrorCodeProofFailed, "withdraw proof response validation failed")
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *HTTPHandler) serveDepositProof(w http.ResponseWriter, r *http.Request) {
	if !beginProofRequest(w, r, "deposit") {
		return
	}
	if h.DepositProver == nil {
		writeErrorResponse(w, http.StatusServiceUnavailable, ErrorCodeUnavailable, "deposit prover is unavailable")
		return
	}

	requestBytes, ok := h.readProofRequestBody(w, r, "deposit")
	if !ok {
		return
	}
	if !json.Valid(requestBytes) {
		writeErrorResponse(w, http.StatusBadRequest, ErrorCodeInvalidRequest, "invalid deposit proof request JSON framing")
		return
	}
	permit, ok := h.acquirePermit(w, r, DepositProofCircuitID)
	if !ok {
		return
	}
	permitReleased := false
	defer func() {
		if permit != nil && !permitReleased {
			permit.Release()
		}
	}()

	request, err := DecodeDepositProofRequestJSON(requestBytes)
	if err != nil {
		writeErrorResponse(w, http.StatusBadRequest, ErrorCodeInvalidRequest, "invalid deposit proof request")
		return
	}
	if err := ValidateDepositProofRequest(*request); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, ErrorCodeInvalidRequest, "deposit proof request validation failed")
		return
	}

	if permit != nil {
		permit.StartProve()
	}
	response, err := h.DepositProver.ProveDeposit(*request)
	if permit != nil {
		permit.Release()
		permitReleased = true
	}
	if err != nil {
		writeErrorResponse(w, http.StatusInternalServerError, ErrorCodeProofFailed, "deposit proof generation failed")
		return
	}
	if response == nil {
		writeErrorResponse(w, http.StatusInternalServerError, ErrorCodeProofFailed, "deposit proof generation failed")
		return
	}
	if err := ValidateDepositProofResponse(*request, *response); err != nil {
		writeErrorResponse(w, http.StatusInternalServerError, ErrorCodeProofFailed, "deposit proof response validation failed")
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func beginProofRequest(w http.ResponseWriter, r *http.Request, route string) bool {
	SetProofResponseHeaders(w)
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeErrorResponse(w, http.StatusMethodNotAllowed, ErrorCodeMethodNotAllowed, route+" proof route requires POST")
		return false
	}
	if err := ValidateProofRequestMediaType(r.Header.Get("Content-Type")); err != nil {
		writeErrorResponse(w, http.StatusUnsupportedMediaType, ErrorCodeInvalidRequest, "proof route requires application/json content type")
		return false
	}
	return true
}

// ValidateProofRequestMediaType applies the common JSON media-type contract
// before a proof-route body is read.
func ValidateProofRequestMediaType(value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("content type is required")
	}
	mediaType, params, err := mime.ParseMediaType(value)
	if err != nil || !strings.EqualFold(mediaType, "application/json") {
		return fmt.Errorf("unsupported content type")
	}
	if len(params) == 0 {
		return nil
	}
	if len(params) != 1 {
		return fmt.Errorf("unsupported content type parameters")
	}
	for name, parameter := range params {
		if !strings.EqualFold(name, "charset") || !strings.EqualFold(parameter, "utf-8") {
			return fmt.Errorf("unsupported content type parameters")
		}
	}
	return nil
}

func SetProofResponseHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
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

func (h *HTTPHandler) readProofRequestBody(w http.ResponseWriter, r *http.Request, route string) ([]byte, bool) {
	requestLimit := h.MaxRequestBytes
	if requestLimit <= 0 {
		requestLimit = DefaultMaxRequestBytes
	}
	requestBytes, err := io.ReadAll(io.LimitReader(r.Body, requestLimit+1))
	if err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeErrorResponse(w, http.StatusRequestEntityTooLarge, ErrorCodeInvalidRequest, route+" proof request body too large")
			return nil, false
		}
		writeErrorResponse(w, http.StatusBadRequest, ErrorCodeInvalidRequest, "failed to read "+route+" proof request body")
		return nil, false
	}
	if int64(len(requestBytes)) > requestLimit {
		writeErrorResponse(w, http.StatusRequestEntityTooLarge, ErrorCodeInvalidRequest, route+" proof request body too large")
		return nil, false
	}
	return requestBytes, true
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

func (c HTTPProverClient) ProveBatchTransfer(ctx context.Context, request BatchTransferProofRequest) (*BatchTransferProofResponse, error) {
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	if err := ValidateBatchTransferProofRequestAt(request, now()); err != nil {
		return nil, err
	}
	responseBytes, err := c.doJSONRequest(ctx, BatchTransferProofPath, request)
	if err != nil {
		return nil, err
	}
	response, err := DecodeBatchTransferProofResponseJSON(responseBytes)
	if err != nil {
		return nil, err
	}
	if err := ValidateBatchTransferProofResponseAt(request, *response, now()); err != nil {
		return nil, err
	}
	return response, nil
}

func (c HTTPProverClient) ProveDeposit(ctx context.Context, request DepositProofRequest) (*DepositProofResponse, error) {
	if err := ValidateDepositProofRequest(request); err != nil {
		return nil, err
	}
	responseBytes, err := c.doJSONRequest(ctx, DepositProofPath, request)
	if err != nil {
		return nil, err
	}
	response, err := DecodeDepositProofResponseJSON(responseBytes)
	if err != nil {
		return nil, err
	}
	if err := ValidateDepositProofResponse(request, *response); err != nil {
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
	if err := validateProverRequestURL(req.URL); err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	bearerToken := strings.TrimSpace(c.BearerToken)
	if strings.ContainsAny(bearerToken, "\r\n") {
		return nil, fmt.Errorf("prover transport bearer token contains invalid characters")
	}
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}

	doer, err := proverHTTPDoerWithoutRedirects(c.Client)
	if err != nil {
		return nil, err
	}
	resp, err := doer.Do(req)
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

func validateProverRequestURL(endpoint *url.URL) error {
	if endpoint == nil || strings.TrimSpace(endpoint.Host) == "" {
		return fmt.Errorf("prover transport client requires an absolute HTTP(S) base URL")
	}
	switch strings.ToLower(endpoint.Scheme) {
	case "https":
		return nil
	case "http":
		host := endpoint.Hostname()
		if strings.EqualFold(host, "localhost") {
			return nil
		}
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			return nil
		}
		return fmt.Errorf("prover transport requires HTTPS for non-loopback endpoint %q", endpoint.Host)
	default:
		return fmt.Errorf("prover transport client base URL must use HTTP or HTTPS")
	}
}

func proverHTTPDoerWithoutRedirects(doer HTTPDoer) (HTTPDoer, error) {
	if client, ok := doer.(*http.Client); ok {
		if client == nil {
			return nil, fmt.Errorf("prover transport client HTTP client is required")
		}
		copy := *client
		copy.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		}
		return &copy, nil
	}
	redirectSafe, ok := doer.(RedirectSafeHTTPDoer)
	if !ok || !redirectSafe.ProverTransportRedirectsDisabled() {
		return nil, fmt.Errorf("custom prover transport HTTP doer must implement RedirectSafeHTTPDoer and disable redirects")
	}
	return redirectSafe, nil
}

func DecodeErrorResponseJSON(payloadBytes []byte) (*ErrorResponse, error) {
	var response ErrorResponse
	if err := decodeStrictJSON(payloadBytes, &response); err != nil {
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
	SetProofResponseHeaders(w)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(responseBytes)
}
