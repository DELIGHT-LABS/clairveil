package proverservice

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"cosmossdk.io/log/v2"

	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/backend/witness"
	"github.com/consensys/gnark/constraint"
	gnarklogger "github.com/consensys/gnark/logger"

	privacyprovertransport "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/provertransport"
	privacytransfer "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/transfer"
	privacywithdraw "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/withdraw"
	privacyzk "github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
)

const (
	ServiceName          = "clairveil-proverd"
	StatusVersion        = "v1"
	HealthPath           = "/healthz"
	ReadinessPath        = "/readyz"
	MetricsPath          = "/debug/vars"
	DefaultListenAddress = "127.0.0.1:8080"
	DefaultMaxRequestBz  = int64(8 << 20)
	DefaultMaxHeaderBz   = 1 << 20
	BearerTokenEnv       = "CLAIRVEIL_PRIVACY_PROVER_BEARER_TOKEN"
)

var gnarkLoggerDisableOnce sync.Once

type ServerConfig struct {
	ListenAddress     string
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	MaxRequestBytes   int64
}

type RuntimeInfo struct {
	ServiceName   string   `json:"service"`
	ArtifactDir   string   `json:"artifact_dir"`
	PreflightMode string   `json:"preflight_mode"`
	AuthEnabled   bool     `json:"auth_enabled"`
	Routes        []string `json:"routes"`
}

type StatusResponse struct {
	Version       string   `json:"version"`
	Status        string   `json:"status"`
	ServiceName   string   `json:"service"`
	ArtifactDir   string   `json:"artifact_dir,omitempty"`
	PreflightMode string   `json:"preflight_mode,omitempty"`
	AuthEnabled   bool     `json:"auth_enabled,omitempty"`
	Routes        []string `json:"routes,omitempty"`
	Error         string   `json:"error,omitempty"`
}

type MetricsResponse struct {
	Version           string                   `json:"version"`
	ServiceName       string                   `json:"service"`
	Timestamp         string                   `json:"timestamp"`
	Goroutines        int                      `json:"goroutines"`
	HeapAllocBytes    uint64                   `json:"heap_alloc_bytes"`
	HeapSysBytes      uint64                   `json:"heap_sys_bytes"`
	StackInUseBytes   uint64                   `json:"stack_inuse_bytes"`
	SysBytes          uint64                   `json:"sys_bytes"`
	RSSBytes          uint64                   `json:"rss_bytes"`
	MaxRSSBytes       uint64                   `json:"max_rss_bytes"`
	RSSSource         string                   `json:"rss_source"`
	ProcessCPUSeconds float64                  `json:"process_cpu_seconds"`
	Admission         AdmissionMetricsSnapshot `json:"admission"`
}

type ReadinessChecker func() error

type Handler struct {
	proverHandler   *privacyprovertransport.HTTPHandler
	admission       *AdmissionController
	readiness       ReadinessChecker
	info            RuntimeInfo
	bearerToken     string
	maxRequestBytes int64
}

type referenceJoinSplitArtifactProvider struct {
	registry *privacyzk.ArtifactRegistry
	err      error
}

type referenceSpendArtifactProvider struct {
	registry *privacyzk.ArtifactRegistry
	err      error
}

type referenceJoinSplitProofRunner struct {
	logWriter io.Writer
}

type referenceSpendProofRunner struct {
	logWriter io.Writer
}

func DefaultServerConfig() ServerConfig {
	return ServerConfig{
		ListenAddress:     DefaultListenAddress,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      0,
		IdleTimeout:       2 * time.Minute,
		MaxRequestBytes:   DefaultMaxRequestBz,
	}
}

func (c ServerConfig) Validate() error {
	if strings.TrimSpace(c.ListenAddress) == "" {
		return fmt.Errorf("listen address is required")
	}
	if c.ReadHeaderTimeout <= 0 {
		return fmt.Errorf("read header timeout must be positive")
	}
	if c.ReadTimeout <= 0 {
		return fmt.Errorf("read timeout must be positive")
	}
	if c.WriteTimeout < 0 {
		return fmt.Errorf("write timeout cannot be negative")
	}
	if c.IdleTimeout <= 0 {
		return fmt.Errorf("idle timeout must be positive")
	}
	if c.MaxRequestBytes <= 0 {
		return fmt.Errorf("max request bytes must be positive")
	}
	return nil
}

func (c ServerConfig) HTTPServer(handler http.Handler) (*http.Server, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if handler == nil {
		return nil, fmt.Errorf("handler is required")
	}

	return &http.Server{
		Addr:              c.ListenAddress,
		Handler:           handler,
		ReadHeaderTimeout: c.ReadHeaderTimeout,
		ReadTimeout:       c.ReadTimeout,
		WriteTimeout:      c.WriteTimeout,
		IdleTimeout:       c.IdleTimeout,
		MaxHeaderBytes:    DefaultMaxHeaderBz,
	}, nil
}

func DefaultRuntimeInfo() RuntimeInfo {
	artifactDir := strings.TrimSpace(os.Getenv(privacyzk.ZKArtifactDirEnv))
	if artifactDir == "" {
		artifactDir = "."
	}

	preflightMode := string(privacyzk.ZKPreflightWarn)
	if mode, err := privacyzk.ParseZKPreflightMode(os.Getenv(privacyzk.ZKPreflightModeEnv)); err == nil {
		preflightMode = string(mode)
	}

	return RuntimeInfo{
		ServiceName:   ServiceName,
		ArtifactDir:   artifactDir,
		PreflightMode: preflightMode,
		Routes: []string{
			HealthPath,
			ReadinessPath,
			MetricsPath,
			privacyprovertransport.TransferProofPath,
			privacyprovertransport.WithdrawProofPath,
		},
	}
}

func NewReferenceHandler(now func() time.Time, logWriter io.Writer, maxRequestBytes int64, bearerToken string) *Handler {
	info := DefaultRuntimeInfo()
	info.AuthEnabled = strings.TrimSpace(bearerToken) != ""
	registry, registryErr := privacyzk.DefaultArtifactRegistry()

	return newHandlerWithAdmission(
		privacyprovertransport.ReferenceTransferProver{
			Artifacts: referenceJoinSplitArtifactProvider{registry: registry, err: registryErr},
			Runner:    referenceJoinSplitProofRunner{logWriter: logWriter},
		},
		privacyprovertransport.ReferenceWithdrawProver{
			Artifacts: referenceSpendArtifactProvider{registry: registry, err: registryErr},
			Runner:    referenceSpendProofRunner{logWriter: logWriter},
		},
		now,
		func() error {
			if registryErr != nil {
				return registryErr
			}
			return registry.CheckReadiness(privacyzk.ArtifactRoleProver, []privacyzk.CircuitID{
				privacyzk.CircuitJoinSplit,
				privacyzk.CircuitSpend,
			}, nil)
		},
		info,
		bearerToken,
		maxRequestBytes,
		mustDefaultAdmissionController(),
	)
}

func NewHandler(
	transferProver privacyprovertransport.TransferProver,
	withdrawProver privacyprovertransport.WithdrawProver,
	now func() time.Time,
	readiness ReadinessChecker,
	info RuntimeInfo,
	bearerToken string,
	maxRequestBytes int64,
) *Handler {
	return newHandlerWithAdmission(transferProver, withdrawProver, now, readiness, info, bearerToken, maxRequestBytes, mustDefaultAdmissionController())
}

func NewHandlerWithAdmission(
	transferProver privacyprovertransport.TransferProver,
	withdrawProver privacyprovertransport.WithdrawProver,
	now func() time.Time,
	readiness ReadinessChecker,
	info RuntimeInfo,
	bearerToken string,
	maxRequestBytes int64,
	admission *AdmissionController,
) *Handler {
	return newHandlerWithAdmission(transferProver, withdrawProver, now, readiness, info, bearerToken, maxRequestBytes, admission)
}

func newHandlerWithAdmission(
	transferProver privacyprovertransport.TransferProver,
	withdrawProver privacyprovertransport.WithdrawProver,
	now func() time.Time,
	readiness ReadinessChecker,
	info RuntimeInfo,
	bearerToken string,
	maxRequestBytes int64,
	admission *AdmissionController,
) *Handler {
	if now == nil {
		now = time.Now
	}

	if strings.TrimSpace(info.ServiceName) == "" {
		info = DefaultRuntimeInfo()
	}
	if maxRequestBytes <= 0 {
		maxRequestBytes = DefaultMaxRequestBz
	}
	if admission == nil {
		admission = mustDefaultAdmissionController()
	}

	return &Handler{
		proverHandler:   privacyprovertransport.NewHTTPHandlerWithAdmission(transferProver, withdrawProver, now, admission),
		admission:       admission,
		readiness:       readiness,
		info:            info,
		bearerToken:     strings.TrimSpace(bearerToken),
		maxRequestBytes: maxRequestBytes,
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h == nil {
		writeStatusJSON(w, http.StatusServiceUnavailable, StatusResponse{
			Version: StatusVersion,
			Status:  "unavailable",
			Error:   "prover service handler is unavailable",
		})
		return
	}

	switch r.URL.Path {
	case HealthPath:
		h.serveHealth(w, r)
	case ReadinessPath:
		h.serveReadiness(w, r)
	case MetricsPath:
		h.serveMetrics(w, r)
	default:
		if isProofRoute(r.URL.Path) && h.bearerToken != "" && !authorized(r, h.bearerToken) {
			writeErrorResponse(w, http.StatusUnauthorized, privacyprovertransport.ErrorCodeUnauthorized, "missing or invalid bearer token")
			return
		}
		if h.maxRequestBytes > 0 && r.Body != nil && isProofRoute(r.URL.Path) {
			r.Body = http.MaxBytesReader(w, r.Body, h.maxRequestBytes)
		}
		h.proverHandler.ServeHTTP(w, r)
	}
}

func mustDefaultAdmissionController() *AdmissionController {
	controller, err := NewAdmissionController(DefaultAdmissionConfig())
	if err != nil {
		panic(err)
	}
	return controller
}

func (p referenceJoinSplitArtifactProvider) JoinSplitR1CS() (constraint.ConstraintSystem, error) {
	if p.err != nil {
		return nil, p.err
	}
	if p.registry == nil {
		return nil, fmt.Errorf("zk artifact registry is unavailable")
	}
	return p.registry.R1CS(privacyzk.CircuitJoinSplit)
}

func (p referenceJoinSplitArtifactProvider) JoinSplitProvingKey() (groth16.ProvingKey, error) {
	if p.err != nil {
		return nil, p.err
	}
	if p.registry == nil {
		return nil, fmt.Errorf("zk artifact registry is unavailable")
	}
	return p.registry.ProvingKey(privacyzk.CircuitJoinSplit)
}

func (p referenceSpendArtifactProvider) SpendR1CS() (constraint.ConstraintSystem, error) {
	if p.err != nil {
		return nil, p.err
	}
	if p.registry == nil {
		return nil, fmt.Errorf("zk artifact registry is unavailable")
	}
	return p.registry.R1CS(privacyzk.CircuitSpend)
}

func (p referenceSpendArtifactProvider) SpendProvingKey() (groth16.ProvingKey, error) {
	if p.err != nil {
		return nil, p.err
	}
	if p.registry == nil {
		return nil, fmt.Errorf("zk artifact registry is unavailable")
	}
	return p.registry.ProvingKey(privacyzk.CircuitSpend)
}

func (r referenceJoinSplitProofRunner) ProveJoinSplit(
	r1cs constraint.ConstraintSystem,
	provingKey groth16.ProvingKey,
	joinSplitWitness witness.Witness,
) (groth16.Proof, error) {
	return withGnarkLoggerOutput(r.logWriter, func() (groth16.Proof, error) {
		return groth16.Prove(r1cs, provingKey, joinSplitWitness)
	})
}

func (r referenceSpendProofRunner) ProveSpend(
	r1cs constraint.ConstraintSystem,
	provingKey groth16.ProvingKey,
	spendWitness witness.Witness,
) (groth16.Proof, error) {
	return withGnarkLoggerOutput(r.logWriter, func() (groth16.Proof, error) {
		return groth16.Prove(r1cs, provingKey, spendWitness)
	})
}

func RunPreflight(logger log.Logger) error {
	return privacyzk.RunProverPreflight(logger, []privacyzk.CircuitID{privacyzk.CircuitJoinSplit, privacyzk.CircuitSpend})
}

// withGnarkLoggerOutput suppresses gnark solver output process-wide on first
// use. Solver errors can contain field values derived from the privacy-sensitive
// witness, so even an operator-supplied stderr writer is not a safe logging
// sink. The one-time logger update does not serialize proof execution.
func withGnarkLoggerOutput[T any](_ io.Writer, fn func() (T, error)) (T, error) {
	gnarkLoggerDisableOnce.Do(func() {
		logger := gnarklogger.Logger()
		gnarklogger.Set(logger.Output(io.Discard))
	})
	return fn()
}

func (h *Handler) serveHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorResponse(w, http.StatusMethodNotAllowed, privacyprovertransport.ErrorCodeMethodNotAllowed, "health route requires GET")
		return
	}

	writeStatusJSON(w, http.StatusOK, StatusResponse{
		Version:       StatusVersion,
		Status:        "ok",
		ServiceName:   h.info.ServiceName,
		ArtifactDir:   h.info.ArtifactDir,
		PreflightMode: h.info.PreflightMode,
		AuthEnabled:   h.info.AuthEnabled,
		Routes:        h.info.Routes,
	})
}

func (h *Handler) serveReadiness(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorResponse(w, http.StatusMethodNotAllowed, privacyprovertransport.ErrorCodeMethodNotAllowed, "readiness route requires GET")
		return
	}

	if h.readiness != nil {
		if err := h.readiness(); err != nil {
			writeStatusJSON(w, http.StatusServiceUnavailable, StatusResponse{
				Version:       StatusVersion,
				Status:        "unavailable",
				ServiceName:   h.info.ServiceName,
				ArtifactDir:   h.info.ArtifactDir,
				PreflightMode: h.info.PreflightMode,
				AuthEnabled:   h.info.AuthEnabled,
				Routes:        h.info.Routes,
				Error:         err.Error(),
			})
			return
		}
	}

	writeStatusJSON(w, http.StatusOK, StatusResponse{
		Version:       StatusVersion,
		Status:        "ready",
		ServiceName:   h.info.ServiceName,
		ArtifactDir:   h.info.ArtifactDir,
		PreflightMode: h.info.PreflightMode,
		AuthEnabled:   h.info.AuthEnabled,
		Routes:        h.info.Routes,
	})
}

func (h *Handler) serveMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorResponse(w, http.StatusMethodNotAllowed, privacyprovertransport.ErrorCodeMethodNotAllowed, "metrics route requires GET")
		return
	}

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	rssBytes, rssSource := currentRSSBytes(mem)
	maxRSSBytes := rssBytes
	if value, ok := processMaxRSSBytes(); ok && value > maxRSSBytes {
		maxRSSBytes = value
	}

	admission := AdmissionMetricsSnapshot{Circuits: map[string]CircuitAdmissionMetrics{}}
	if h.admission != nil {
		admission = h.admission.Snapshot()
	}

	writeJSON(w, http.StatusOK, MetricsResponse{
		Version:           StatusVersion,
		ServiceName:       h.info.ServiceName,
		Timestamp:         time.Now().UTC().Format(time.RFC3339Nano),
		Goroutines:        runtime.NumGoroutine(),
		HeapAllocBytes:    mem.HeapAlloc,
		HeapSysBytes:      mem.HeapSys,
		StackInUseBytes:   mem.StackInuse,
		SysBytes:          mem.Sys,
		RSSBytes:          rssBytes,
		MaxRSSBytes:       maxRSSBytes,
		RSSSource:         rssSource,
		ProcessCPUSeconds: processCPUSeconds(),
		Admission:         admission,
	})
}

func currentRSSBytes(mem runtime.MemStats) (uint64, string) {
	if runtime.GOOS == "linux" {
		if bz, err := os.ReadFile("/proc/self/statm"); err == nil {
			fields := strings.Fields(string(bz))
			if len(fields) >= 2 {
				if residentPages, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
					return residentPages * uint64(os.Getpagesize()), "procfs_statm"
				}
			}
		}
	}

	return mem.Sys, "runtime_memstats_sys"
}

func processMaxRSSBytes() (uint64, bool) {
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil || usage.Maxrss <= 0 {
		return 0, false
	}
	if runtime.GOOS == "darwin" || runtime.GOOS == "ios" {
		return uint64(usage.Maxrss), true
	}
	return uint64(usage.Maxrss) * 1024, true
}

func processCPUSeconds() float64 {
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		return 0
	}
	return timevalSeconds(usage.Utime) + timevalSeconds(usage.Stime)
}

func timevalSeconds(value syscall.Timeval) float64 {
	return float64(value.Sec) + float64(value.Usec)/1_000_000
}

func isProofRoute(path string) bool {
	return path == privacyprovertransport.TransferProofPath || path == privacyprovertransport.WithdrawProofPath
}

func authorized(r *http.Request, bearerToken string) bool {
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	if authHeader == "" || bearerToken == "" {
		return false
	}

	const prefix = "Bearer "
	if !strings.HasPrefix(authHeader, prefix) {
		return false
	}

	return strings.TrimSpace(strings.TrimPrefix(authHeader, prefix)) == bearerToken
}

func writeStatusJSON(w http.ResponseWriter, statusCode int, payload StatusResponse) {
	writeJSON(w, statusCode, payload)
}

func writeErrorResponse(w http.ResponseWriter, statusCode int, code, message string) {
	writeJSON(w, statusCode, privacyprovertransport.ErrorResponse{
		Version: privacyprovertransport.ErrorResponseVersion,
		Code:    code,
		Message: message,
	})
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

var (
	_ privacytransfer.JoinSplitArtifactProvider = referenceJoinSplitArtifactProvider{}
	_ privacytransfer.JoinSplitProofRunner      = referenceJoinSplitProofRunner{}
	_ privacywithdraw.SpendArtifactProvider     = referenceSpendArtifactProvider{}
	_ privacywithdraw.SpendProofRunner          = referenceSpendProofRunner{}
)
