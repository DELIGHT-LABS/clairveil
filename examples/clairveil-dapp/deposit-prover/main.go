package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"cosmossdk.io/log/v2"

	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/backend/witness"
	"github.com/consensys/gnark/constraint"

	clairveiltypes "github.com/DELIGHT-LABS/clairveil/types"
	privacydeposit "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/deposit"
	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	privacyzk "github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
)

const (
	depositProofPath       = "/v1/prove"
	healthPath             = "/healthz"
	defaultListenAddress   = "127.0.0.1:8090"
	defaultMaxRequestBytes = int64(64 << 10)
	maxHeaderBytes         = 1 << 20
)

type depositProofRequest struct {
	NoteJSON          string `json:"note_json"`
	NoteCommitmentHex string `json:"note_commitment_hex"`
}

type depositProofResponse struct {
	Version           string `json:"version"`
	ProofHex          string `json:"proof_hex"`
	NoteCommitmentHex string `json:"note_commitment_hex"`
}

type errorResponse struct {
	Version string `json:"version"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type noteProver interface {
	ProveDeposit(privacytypes.Note) ([]byte, error)
}

type referenceNoteProver struct{}

type depositArtifactProvider struct{}

type depositProofRunner struct{}

type depositProofHandler struct {
	prover          noteProver
	maxRequestBytes int64
	proofSlot       chan struct{}
}

func configureSDK() {
	clairveiltypes.SetConfig()
}

func (referenceNoteProver) ProveDeposit(note privacytypes.Note) ([]byte, error) {
	return privacydeposit.BuildDepositProof(note, depositArtifactProvider{}, depositProofRunner{})
}

func (depositArtifactProvider) DepositR1CS() (constraint.ConstraintSystem, error) {
	return privacyzk.GetDepositR1CS()
}

func (depositArtifactProvider) DepositProvingKey() (groth16.ProvingKey, error) {
	return privacyzk.GetDepositProvingKey()
}

func (depositProofRunner) ProveDeposit(r1cs constraint.ConstraintSystem, provingKey groth16.ProvingKey, depositWitness witness.Witness) (groth16.Proof, error) {
	return groth16.Prove(r1cs, provingKey, depositWitness)
}

func newDepositProofHandler(prover noteProver, maxRequestBytes int64) (http.Handler, error) {
	if prover == nil {
		return nil, fmt.Errorf("deposit prover is required")
	}
	if maxRequestBytes <= 0 {
		return nil, fmt.Errorf("max request bytes must be positive")
	}

	handler := &depositProofHandler{
		prover:          prover,
		maxRequestBytes: maxRequestBytes,
		proofSlot:       make(chan struct{}, 1),
	}
	mux := http.NewServeMux()
	mux.HandleFunc(healthPath, handler.handleHealth)
	mux.HandleFunc(depositProofPath, handler.handleProof)
	return mux, nil
}

func (h *depositProofHandler) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "health endpoint requires GET")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"version": "v1", "status": "ok"})
}

func (h *depositProofHandler) handleProof(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "deposit proof endpoint requires POST")
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "invalid_request", "Content-Type must be application/json")
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, h.maxRequestBytes))
	if err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeError(w, http.StatusRequestEntityTooLarge, "invalid_request", "deposit proof request is too large")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_request", "failed to read deposit proof request")
		return
	}

	var request depositProofRequest
	if err := decodeStrictJSON(body, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid deposit proof request JSON")
		return
	}
	if strings.TrimSpace(request.NoteJSON) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "note_json is required")
		return
	}

	requestCommitment, err := normalizeCommitmentHex(request.NoteCommitmentHex)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	var note privacytypes.Note
	if err := decodeStrictJSON([]byte(request.NoteJSON), &note); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "note_json must contain a valid NoteV1")
		return
	}
	if err := note.ValidateV1(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "note_json must contain a valid NoteV1")
		return
	}
	computedCommitment, err := privacyfield.CanonicalHexFromBigInt(note.ComputeCommitment())
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "note commitment is invalid")
		return
	}
	if requestCommitment != computedCommitment {
		writeError(w, http.StatusBadRequest, "invalid_request", "note_commitment_hex does not match note_json")
		return
	}

	select {
	case h.proofSlot <- struct{}{}:
		defer func() { <-h.proofSlot }()
	case <-r.Context().Done():
		return
	}

	proof, err := h.prover.ProveDeposit(note)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "proof_failed", "deposit proof generation failed")
		return
	}
	writeJSON(w, http.StatusOK, depositProofResponse{
		Version:           "v1",
		ProofHex:          hex.EncodeToString(proof),
		NoteCommitmentHex: computedCommitment,
	})
}

func decodeStrictJSON(payload []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values are not allowed")
		}
		return err
	}
	return nil
}

func normalizeCommitmentHex(value string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	decoded, err := hex.DecodeString(normalized)
	if err != nil || len(decoded) != privacyfield.ByteSize {
		return "", fmt.Errorf("note_commitment_hex must be a 32-byte hex string")
	}
	if err := privacyfield.ValidateCanonicalBytes32(decoded); err != nil {
		return "", fmt.Errorf("note_commitment_hex must be a canonical field element")
	}
	return normalized, nil
}

func writeError(w http.ResponseWriter, statusCode int, code, message string) {
	writeJSON(w, statusCode, errorResponse{Version: "v1", Code: code, Message: message})
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}

func validateListenAddress(address string) error {
	host, _, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil {
		return fmt.Errorf("invalid listen address: %w", err)
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("deposit prover must listen on a loopback address")
	}
	return nil
}

func main() {
	configureSDK()

	listenAddress := defaultListenAddress
	maxRequestBytes := defaultMaxRequestBytes
	flag.StringVar(&listenAddress, "listen", listenAddress, "loopback listen address for the local deposit prover")
	flag.Int64Var(&maxRequestBytes, "max-request-bytes", maxRequestBytes, "maximum accepted JSON request body size in bytes")
	flag.Parse()
	if err := validateListenAddress(listenAddress); err != nil {
		fmt.Fprintf(os.Stderr, "invalid local deposit prover configuration: %v\n", err)
		os.Exit(1)
	}

	logger := log.NewLogger(os.Stderr)
	if err := privacyzk.RunProverPreflight(logger, []privacyzk.CircuitID{privacyzk.CircuitDeposit}); err != nil {
		fmt.Fprintf(os.Stderr, "local deposit prover preflight failed: %v\n", err)
		os.Exit(1)
	}
	handler, err := newDepositProofHandler(referenceNoteProver{}, maxRequestBytes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to build local deposit prover: %v\n", err)
		os.Exit(1)
	}
	server := &http.Server{
		Addr:              listenAddress,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      0,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    maxHeaderBytes,
	}

	fmt.Fprintf(os.Stderr, "local deposit prover listening on %s\n", listenAddress)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintf(os.Stderr, "local deposit prover stopped with error: %v\n", err)
		os.Exit(1)
	}
}
