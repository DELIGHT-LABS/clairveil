package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	privacyzk "github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
)

const reportSchemaVersion = "v1"

var benchmarkNamePattern = regexp.MustCompile(`^(Benchmark\S+)-\d+$`)

type report struct {
	SchemaVersion       string                   `json:"schema_version"`
	GeneratedAt         string                   `json:"generated_at"`
	ResultFamily        string                   `json:"result_family,omitempty"`
	SourceFiles         []string                 `json:"source_files,omitempty"`
	SourceFileSHA256    map[string]string        `json:"source_file_sha256,omitempty"`
	SourceFileIssues    []string                 `json:"source_file_issues,omitempty"`
	RunStartedAt        string                   `json:"run_started_at,omitempty"`
	RunEndedAt          string                   `json:"run_ended_at,omitempty"`
	Commit              string                   `json:"commit"`
	Dirty               bool                     `json:"dirty"`
	ActiveSetID         string                   `json:"active_set_id"`
	ClaimProfile        claimProfile             `json:"claim_profile"`
	ClaimEvidence       claimEvidence            `json:"claim_evidence"`
	ClaimEvidenceByType map[string]claimEvidence `json:"claim_evidence_by_type,omitempty"`
	Environment         environment              `json:"environment"`
	ArtifactSet         artifactSet              `json:"artifact_set"`
	GoVersion           string                   `json:"go_version"`
	GnarkVersion        string                   `json:"gnark_version"`
	GnarkCrypto         string                   `json:"gnark_crypto_version"`
	OS                  string                   `json:"os"`
	Arch                string                   `json:"arch"`
	CPU                 string                   `json:"cpu"`
	ManifestPath        string                   `json:"manifest_path,omitempty"`
	ManifestChecksum    string                   `json:"manifest_sha256,omitempty"`
	FeeModel            feeModel                 `json:"fee_model"`
	ComponentReports    []componentReport        `json:"component_reports,omitempty"`
	Benchmarks          []benchmarkSummary       `json:"benchmarks,omitempty"`
	Fees                []feeSummary             `json:"fees,omitempty"`
}

type claimProfile struct {
	RunProfile      string   `json:"run_profile"`
	ClaimTypes      []string `json:"claim_types,omitempty"`
	Eligible        bool     `json:"eligible"`
	BlockingReasons []string `json:"blocking_reasons,omitempty"`
}

type claimEvidence struct {
	SteadyStateSeconds          int     `json:"steady_state_seconds,omitempty"`
	LoadProfile                 string  `json:"load_profile,omitempty"`
	PreflightMode               string  `json:"preflight_mode,omitempty"`
	AuthEnabled                 string  `json:"auth_enabled,omitempty"`
	InstanceProfile             string  `json:"instance_profile,omitempty"`
	ProverConfigFile            string  `json:"prover_config_file,omitempty"`
	ProverConfigSHA256          string  `json:"prover_config_sha256,omitempty"`
	ChainConfig                 string  `json:"chain_config,omitempty"`
	ChainConfigFile             string  `json:"chain_config_file,omitempty"`
	ChainConfigSHA256           string  `json:"chain_config_sha256,omitempty"`
	ReserveInvariant            string  `json:"reserve_invariant,omitempty"`
	LatencyP99SLOMS             float64 `json:"latency_p99_slo_ms,omitempty"`
	InclusionP95SLOMS           float64 `json:"inclusion_p95_slo_ms,omitempty"`
	RSSStable                   string  `json:"rss_stable,omitempty"`
	SaturationProfile           string  `json:"saturation_profile,omitempty"`
	SaturationProfileFile       string  `json:"saturation_profile_file,omitempty"`
	SaturationProfileSHA256     string  `json:"saturation_profile_sha256,omitempty"`
	ThroughputWindowSeconds     int     `json:"throughput_window_seconds,omitempty"`
	ReserveSnapshotBeforeFile   string  `json:"reserve_snapshot_before_file,omitempty"`
	ReserveSnapshotBeforeSHA256 string  `json:"reserve_snapshot_before_sha256,omitempty"`
	ReserveSnapshotAfterFile    string  `json:"reserve_snapshot_after_file,omitempty"`
	ReserveSnapshotAfterSHA256  string  `json:"reserve_snapshot_after_sha256,omitempty"`
	LatencyMode                 string  `json:"latency_mode,omitempty"`
	ColdWarmSeparated           string  `json:"cold_warm_separated,omitempty"`
	BrowserMatrix               string  `json:"browser_matrix,omitempty"`
	BrowserAdapterReady         string  `json:"browser_adapter_ready,omitempty"`
	BrowserAdapterVersion       string  `json:"browser_adapter_version,omitempty"`
	BrowserAdapterFile          string  `json:"browser_adapter_file,omitempty"`
	BrowserAdapterSHA256        string  `json:"browser_adapter_sha256,omitempty"`
	RemoteTopology              string  `json:"remote_topology,omitempty"`
	LinkedProverReportFile      string  `json:"linked_prover_report_file,omitempty"`
	LinkedProverReportSHA256    string  `json:"linked_prover_report_sha256,omitempty"`
}

type environment struct {
	MachineProfile string `json:"machine_profile,omitempty"`
	CPUGovernor    string `json:"cpu_governor,omitempty"`
	MemoryGiB      string `json:"memory_gib,omitempty"`
	OS             string `json:"os"`
	Arch           string `json:"arch"`
	CPU            string `json:"cpu,omitempty"`
}

type artifactSet struct {
	ActiveSetID           string                     `json:"active_set_id"`
	ManifestActiveSetID   string                     `json:"manifest_active_set_id,omitempty"`
	ManifestSHA256        string                     `json:"manifest_sha256,omitempty"`
	DescriptorComplete    bool                       `json:"descriptor_complete,omitempty"`
	DescriptorIssues      []string                   `json:"descriptor_issues,omitempty"`
	ArtifactFilesVerified bool                       `json:"artifact_files_verified,omitempty"`
	ArtifactFileIssues    []string                   `json:"artifact_file_issues,omitempty"`
	ArtifactDescriptors   []artifactDescriptorReport `json:"artifact_descriptors,omitempty"`
	ArtifactSHA256ByFile  map[string]string          `json:"artifact_sha256_by_file,omitempty"`
}

type artifactDescriptorReport struct {
	CircuitID    string `json:"circuit_id"`
	ArtifactType string `json:"artifact_type"`
	Filename     string `json:"filename"`
	SHA256       string `json:"sha256"`
}

type feeModel struct {
	FeeDenom      string `json:"fee_denom,omitempty"`
	MinGasPrice   string `json:"min_gas_price,omitempty"`
	GasAdjustment string `json:"gas_adjustment,omitempty"`
}

type componentReport struct {
	Path            string   `json:"path"`
	SHA256          string   `json:"sha256"`
	ResultFamily    string   `json:"result_family,omitempty"`
	RunProfile      string   `json:"run_profile,omitempty"`
	ClaimTypes      []string `json:"claim_types,omitempty"`
	Eligible        bool     `json:"eligible"`
	BlockingReasons []string `json:"blocking_reasons,omitempty"`
	Commit          string   `json:"commit,omitempty"`
	Dirty           bool     `json:"dirty,omitempty"`
	ActiveSetID     string   `json:"active_set_id,omitempty"`
	ManifestSHA256  string   `json:"manifest_sha256,omitempty"`
	RunStartedAt    string   `json:"run_started_at,omitempty"`
	RunEndedAt      string   `json:"run_ended_at,omitempty"`
	MachineProfile  string   `json:"machine_profile,omitempty"`
}

type benchmarkSample struct {
	Name     string
	NSOp     float64
	BytesOp  float64
	AllocsOp float64
	Metrics  map[string]float64
}

type benchmarkSummary struct {
	Name            string                   `json:"name"`
	Samples         int                      `json:"samples"`
	NSOpMean        float64                  `json:"ns_per_op_mean"`
	NSOpP50         float64                  `json:"ns_per_op_p50"`
	NSOpP95         float64                  `json:"ns_per_op_p95"`
	NSOpP99         float64                  `json:"ns_per_op_p99"`
	NSOpMin         float64                  `json:"ns_per_op_min"`
	NSOpMax         float64                  `json:"ns_per_op_max"`
	OpsPerSec       float64                  `json:"ops_per_sec_mean"`
	BytesOp         float64                  `json:"bytes_per_op_mean,omitempty"`
	AllocsOp        float64                  `json:"allocs_per_op_mean,omitempty"`
	MetricKind      string                   `json:"metric_kind"`
	ClaimType       string                   `json:"claim_type,omitempty"`
	LoadProfile     string                   `json:"load_profile,omitempty"`
	FlowProfile     string                   `json:"flow_profile,omitempty"`
	LatencyMode     string                   `json:"latency_mode,omitempty"`
	ColdWarm        string                   `json:"cold_warm,omitempty"`
	Route           string                   `json:"route,omitempty"`
	Concurrency     int                      `json:"concurrency,omitempty"`
	WarmupSeconds   int                      `json:"warmup_seconds,omitempty"`
	DurationSeconds int                      `json:"duration_seconds,omitempty"`
	TargetTxPerSec  float64                  `json:"target_tx_per_sec,omitempty"`
	Metrics         map[string]metricSummary `json:"metric_summaries,omitempty"`
}

type metricSummary struct {
	Mean float64 `json:"mean"`
	P50  float64 `json:"p50"`
	P95  float64 `json:"p95"`
	P99  float64 `json:"p99"`
	Min  float64 `json:"min"`
	Max  float64 `json:"max"`
}

type namedMetricSummary struct {
	BenchmarkName string
	MetricName    string
	Metric        metricSummary
}

type txMetric struct {
	TxType    string `json:"tx_type"`
	GasUsed   uint64 `json:"gas_used"`
	GasWanted uint64 `json:"gas_wanted,omitempty"`
	Success   *bool  `json:"success,omitempty"`
}

type txMetricEnvelope struct {
	Transactions []txMetric `json:"transactions"`
}

type benchmarkSummaryEnvelope struct {
	Benchmarks []benchmarkSummary `json:"benchmarks"`
}

type feeSummary struct {
	TxType          string `json:"tx_type"`
	Samples         int    `json:"samples"`
	FailedSamples   int    `json:"failed_samples,omitempty"`
	GasUsedMean     uint64 `json:"gas_used_mean"`
	GasUsedP50      uint64 `json:"gas_used_p50"`
	GasUsedP95      uint64 `json:"gas_used_p95"`
	GasUsedMax      uint64 `json:"gas_used_max"`
	GasWantedMax    uint64 `json:"gas_wanted_max,omitempty"`
	GasAdjustment   string `json:"gas_adjustment"`
	MinGasPrice     string `json:"min_gas_price"`
	FeeDenom        string `json:"fee_denom"`
	EstimatedFeeP50 string `json:"estimated_fee_p50"`
	EstimatedFeeP95 string `json:"estimated_fee_p95"`
}

type reserveSnapshot struct {
	Denom                 string `json:"denom"`
	ModuleBalance         string `json:"module_balance"`
	TotalDeposited        string `json:"total_deposited"`
	TotalWithdrawn        string `json:"total_withdrawn"`
	ExpectedModuleBalance string `json:"expected_module_balance"`
	InvariantHolds        bool   `json:"invariant_holds"`
}

type humanSummaryComponent struct {
	Path   string
	Report report
}

func main() {
	var inputPath string
	var mergeReports string
	var humanSummaryOut string
	var humanSummaryReports string
	var outDir string
	var activeSetID string
	var manifestPath string
	var feeDenom string
	var minGasPrice string
	var gasAdjustment string
	var txMetricsPath string
	var benchmarkSummariesPath string
	var commitOverride string
	var dirtyOverride string
	var resultFamily string
	var sourceFiles string
	var runStartedAt string
	var runEndedAt string
	var runProfile string
	var claimTypes string
	var machineProfile string
	var cpuGovernor string
	var memoryGiB string
	var steadyStateSeconds int
	var loadProfile string
	var preflightMode string
	var authEnabled string
	var instanceProfile string
	var proverConfigFile string
	var proverConfigSHA256 string
	var chainConfig string
	var chainConfigFile string
	var chainConfigSHA256 string
	var reserveInvariant string
	var latencyP99SLOMS float64
	var inclusionP95SLOMS float64
	var rssStable string
	var saturationProfile string
	var saturationProfileFile string
	var saturationProfileSHA256 string
	var throughputWindowSeconds int
	var reserveSnapshotBeforeFile string
	var reserveSnapshotBeforeSHA256 string
	var reserveSnapshotAfterFile string
	var reserveSnapshotAfterSHA256 string
	var latencyMode string
	var coldWarmSeparated string
	var browserMatrix string
	var browserAdapterReady string
	var browserAdapterVersion string
	var browserAdapterFile string
	var browserAdapterSHA256 string
	var remoteTopology string
	var linkedProverReportFile string
	var linkedProverReportSHA256 string
	flag.StringVar(&inputPath, "input", "", "raw go test -bench output")
	flag.StringVar(&mergeReports, "merge-reports", "", "comma-separated benchmark report JSON files to aggregate into a public-capacity report")
	flag.StringVar(&humanSummaryOut, "human-summary-out", "", "write a Korean all-in-one human benchmark summary Markdown report to this path")
	flag.StringVar(&humanSummaryReports, "human-summary-reports", "", "comma-separated benchmark report JSON files for -human-summary-out; defaults to existing standard latest.json files")
	flag.StringVar(&outDir, "out", "benchmarks/privacy-circuits", "output directory for benchmark reports")
	flag.StringVar(&activeSetID, "active-set-id", privacyzk.ActiveCircuitSetID, "active circuit set id")
	flag.StringVar(&manifestPath, "manifest", "", "optional privacy_zk_manifest.json path")
	flag.StringVar(&feeDenom, "fee-denom", "", "fee denom used for expected fee calculations")
	flag.StringVar(&minGasPrice, "min-gas-price", "", "minimum gas price used for expected fee calculations")
	flag.StringVar(&gasAdjustment, "gas-adjustment", "", "gas adjustment used for expected fee calculations")
	flag.StringVar(&txMetricsPath, "tx-metrics", "", "optional JSON file with observed tx gas metrics")
	flag.StringVar(&benchmarkSummariesPath, "benchmark-summaries", "", "optional structured JSON file with benchmark summaries")
	flag.StringVar(&commitOverride, "commit", "", "source commit hash override captured before benchmark output files are written")
	flag.StringVar(&dirtyOverride, "dirty", "", "source dirty state override captured before benchmark output files are written")
	flag.StringVar(&resultFamily, "result-family", "", "benchmark result family, for example privacy-circuits or privacy-proverd-load")
	flag.StringVar(&sourceFiles, "source-files", "", "comma-separated source files used to build this report")
	flag.StringVar(&runStartedAt, "run-started-at", "", "RFC3339 timestamp captured when the benchmark run started")
	flag.StringVar(&runEndedAt, "run-ended-at", "", "RFC3339 timestamp captured when the benchmark run ended")
	flag.StringVar(&runProfile, "run-profile", "smoke", "benchmark run profile: smoke, reference, production_like, or public_claim")
	flag.StringVar(&claimTypes, "claim-types", "", "comma-separated capacity claim types: chain_tps, prover_rps, user_latency")
	flag.StringVar(&machineProfile, "machine-profile", "", "optional machine profile label for benchmark reports")
	flag.StringVar(&cpuGovernor, "cpu-governor", "", "optional CPU governor or power mode")
	flag.StringVar(&memoryGiB, "memory-gib", "", "optional memory size in GiB")
	flag.IntVar(&steadyStateSeconds, "claim-steady-state-seconds", 0, "steady-state measurement duration in seconds for public claim evidence")
	flag.StringVar(&loadProfile, "claim-load-profile", "", "load or flow profile used for public claim evidence")
	flag.StringVar(&preflightMode, "claim-preflight-mode", "", "zk preflight mode used for public claim evidence")
	flag.StringVar(&authEnabled, "claim-auth-enabled", "", "whether prover auth was enabled for public claim evidence")
	flag.StringVar(&instanceProfile, "claim-instance-profile", "", "prover or machine instance profile for public claim evidence")
	flag.StringVar(&proverConfigFile, "claim-prover-config-file", "", "prover config file used for public prover claim evidence")
	flag.StringVar(&proverConfigSHA256, "claim-prover-config-sha256", "", "SHA-256 of prover config used for public prover claim evidence")
	flag.StringVar(&chainConfig, "claim-chain-config", "", "chain config identifier for public chain TPS evidence")
	flag.StringVar(&chainConfigFile, "claim-chain-config-file", "", "chain config file used for public chain TPS evidence")
	flag.StringVar(&chainConfigSHA256, "claim-chain-config-sha256", "", "SHA-256 of chain config used for public chain TPS evidence")
	flag.StringVar(&reserveInvariant, "claim-reserve-invariant", "", "whether reserve invariant held for public chain TPS evidence")
	flag.Float64Var(&latencyP99SLOMS, "claim-latency-p99-slo-ms", 0, "p99 latency SLO in milliseconds for public prover/user latency claims")
	flag.Float64Var(&inclusionP95SLOMS, "claim-inclusion-p95-slo-ms", 0, "p95 inclusion latency SLO in milliseconds for public chain TPS claims")
	flag.StringVar(&rssStable, "claim-rss-stable", "", "whether RSS was stable during steady-state public prover claim")
	flag.StringVar(&saturationProfile, "claim-saturation-profile", "", "saturation profile identifier for public prover claim")
	flag.StringVar(&saturationProfileFile, "claim-saturation-profile-file", "", "saturation profile evidence file for public prover or chain capacity claims")
	flag.StringVar(&saturationProfileSHA256, "claim-saturation-profile-sha256", "", "SHA-256 of the saturation profile evidence file")
	flag.IntVar(&throughputWindowSeconds, "claim-throughput-window-seconds", 0, "window size in seconds used for sustained throughput metrics")
	flag.StringVar(&reserveSnapshotBeforeFile, "claim-reserve-snapshot-before-file", "", "reserve snapshot captured before a public chain TPS run")
	flag.StringVar(&reserveSnapshotBeforeSHA256, "claim-reserve-snapshot-before-sha256", "", "SHA-256 of the before-run reserve snapshot")
	flag.StringVar(&reserveSnapshotAfterFile, "claim-reserve-snapshot-after-file", "", "reserve snapshot captured after a public chain TPS run")
	flag.StringVar(&reserveSnapshotAfterSHA256, "claim-reserve-snapshot-after-sha256", "", "SHA-256 of the after-run reserve snapshot")
	flag.StringVar(&latencyMode, "claim-latency-mode", "", "user latency mode: native, remote, or browser")
	flag.StringVar(&coldWarmSeparated, "claim-cold-warm-separated", "", "whether cold and warm user latency samples were separated")
	flag.StringVar(&browserMatrix, "claim-browser-matrix", "", "browser/device matrix identifier for browser user latency evidence")
	flag.StringVar(&browserAdapterReady, "claim-browser-adapter-ready", "", "whether the browser/WASM prover adapter was ready for public user latency evidence")
	flag.StringVar(&browserAdapterVersion, "claim-browser-adapter-version", "", "browser/WASM prover adapter version used for public user latency evidence")
	flag.StringVar(&browserAdapterFile, "claim-browser-adapter-file", "", "browser/WASM prover adapter artifact or manifest file used for public user latency evidence")
	flag.StringVar(&browserAdapterSHA256, "claim-browser-adapter-sha256", "", "SHA-256 of the browser/WASM prover adapter artifact or manifest")
	flag.StringVar(&remoteTopology, "claim-remote-topology", "", "remote prover topology identifier for remote user latency evidence")
	flag.StringVar(&linkedProverReportFile, "claim-linked-prover-report-file", "", "public prover RPS report file linked to remote user latency evidence")
	flag.StringVar(&linkedProverReportSHA256, "claim-linked-prover-report-sha256", "", "SHA-256 of the linked public prover RPS report")
	flag.Parse()

	if strings.TrimSpace(humanSummaryOut) != "" {
		paths := parseCSV(humanSummaryReports)
		if len(paths) == 0 {
			paths = existingStandardReportPaths()
		}
		components, err := readHumanSummaryComponents(paths)
		if err != nil {
			fatalf("read human summary reports: %v", err)
		}
		markdown := renderHumanSummaryMarkdownKR(components, time.Now().UTC().Format(time.RFC3339))
		if err := os.MkdirAll(filepath.Dir(humanSummaryOut), 0o755); err != nil {
			fatalf("create human summary output directory: %v", err)
		}
		if err := os.WriteFile(humanSummaryOut, []byte(markdown), 0o644); err != nil {
			fatalf("write human summary report: %v", err)
		}
		fmt.Printf("human benchmark summary written to %s\n", humanSummaryOut)
		return
	}

	if strings.TrimSpace(mergeReports) != "" {
		sourceCommit, sourceDirty, err := sourceMetadata(commitOverride, dirtyOverride)
		if err != nil {
			fatalf("source metadata: %v", err)
		}
		parsedRunProfile, err := parseRunProfile(runProfile)
		if err != nil {
			fatalf("run profile: %v", err)
		}
		rep, err := buildAggregateReport(
			parseCSV(mergeReports),
			time.Now().UTC().Format(time.RFC3339),
			sourceCommit,
			sourceDirty,
			parsedRunProfile,
			environment{
				MachineProfile: strings.TrimSpace(machineProfile),
				CPUGovernor:    strings.TrimSpace(cpuGovernor),
				MemoryGiB:      strings.TrimSpace(memoryGiB),
				OS:             runtime.GOOS,
				Arch:           runtime.GOARCH,
			},
		)
		if err != nil {
			fatalf("build aggregate report: %v", err)
		}
		writeReportFiles(outDir, rep)
		fmt.Printf("benchmark report written to %s\n", outDir)
		return
	}

	var samples []benchmarkSample
	var cpu string
	if strings.TrimSpace(inputPath) != "" {
		raw, err := os.ReadFile(inputPath)
		if err != nil {
			fatalf("read benchmark input: %v", err)
		}
		samples, cpu = parseBenchmarkOutput(string(raw))
		if len(samples) == 0 {
			fatalf("no benchmark samples found in %s", inputPath)
		}
	}
	if strings.TrimSpace(inputPath) == "" && strings.TrimSpace(txMetricsPath) == "" && strings.TrimSpace(benchmarkSummariesPath) == "" {
		fatalf("-input, -benchmark-summaries, or -tx-metrics is required")
	}
	sourceCommit, sourceDirty, err := sourceMetadata(commitOverride, dirtyOverride)
	if err != nil {
		fatalf("source metadata: %v", err)
	}

	parsedRunProfile, err := parseRunProfile(runProfile)
	if err != nil {
		fatalf("run profile: %v", err)
	}
	resolvedProverConfigSHA256, err := resolveConfigSHA256("prover", proverConfigSHA256, proverConfigFile)
	if err != nil {
		fatalf("prover config evidence: %v", err)
	}
	resolvedChainConfigSHA256, err := resolveConfigSHA256("chain", chainConfigSHA256, chainConfigFile)
	if err != nil {
		fatalf("chain config evidence: %v", err)
	}
	resolvedBrowserAdapterSHA256, err := resolveConfigSHA256("browser adapter", browserAdapterSHA256, browserAdapterFile)
	if err != nil {
		fatalf("browser adapter evidence: %v", err)
	}
	resolvedLinkedProverReportSHA256, err := resolveConfigSHA256("linked prover report", linkedProverReportSHA256, linkedProverReportFile)
	if err != nil {
		fatalf("linked prover report evidence: %v", err)
	}
	resolvedSaturationProfileSHA256, err := resolveConfigSHA256("saturation profile", saturationProfileSHA256, saturationProfileFile)
	if err != nil {
		fatalf("saturation profile evidence: %v", err)
	}
	resolvedReserveSnapshotBeforeSHA256, err := resolveConfigSHA256("reserve snapshot before", reserveSnapshotBeforeSHA256, reserveSnapshotBeforeFile)
	if err != nil {
		fatalf("reserve snapshot before evidence: %v", err)
	}
	resolvedReserveSnapshotAfterSHA256, err := resolveConfigSHA256("reserve snapshot after", reserveSnapshotAfterSHA256, reserveSnapshotAfterFile)
	if err != nil {
		fatalf("reserve snapshot after evidence: %v", err)
	}

	rep := report{
		SchemaVersion: reportSchemaVersion,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		ResultFamily:  strings.TrimSpace(resultFamily),
		SourceFiles: reportSourceFiles(
			sourceFiles,
			inputPath,
			benchmarkSummariesPath,
			txMetricsPath,
			proverConfigFile,
			chainConfigFile,
			saturationProfileFile,
			reserveSnapshotBeforeFile,
			reserveSnapshotAfterFile,
			browserAdapterFile,
			linkedProverReportFile,
		),
		RunStartedAt: strings.TrimSpace(runStartedAt),
		RunEndedAt:   strings.TrimSpace(runEndedAt),
		Commit:       sourceCommit,
		Dirty:        sourceDirty,
		ActiveSetID:  activeSetID,
		ClaimProfile: claimProfile{
			RunProfile: parsedRunProfile,
			ClaimTypes: parseCSV(claimTypes),
		},
		ClaimEvidence: claimEvidence{
			SteadyStateSeconds:          steadyStateSeconds,
			LoadProfile:                 strings.TrimSpace(loadProfile),
			PreflightMode:               strings.TrimSpace(preflightMode),
			AuthEnabled:                 strings.TrimSpace(authEnabled),
			InstanceProfile:             strings.TrimSpace(instanceProfile),
			ProverConfigFile:            strings.TrimSpace(proverConfigFile),
			ProverConfigSHA256:          resolvedProverConfigSHA256,
			ChainConfig:                 strings.TrimSpace(chainConfig),
			ChainConfigFile:             strings.TrimSpace(chainConfigFile),
			ChainConfigSHA256:           resolvedChainConfigSHA256,
			ReserveInvariant:            strings.TrimSpace(reserveInvariant),
			LatencyP99SLOMS:             latencyP99SLOMS,
			InclusionP95SLOMS:           inclusionP95SLOMS,
			RSSStable:                   strings.TrimSpace(rssStable),
			SaturationProfile:           strings.TrimSpace(saturationProfile),
			SaturationProfileFile:       strings.TrimSpace(saturationProfileFile),
			SaturationProfileSHA256:     resolvedSaturationProfileSHA256,
			ThroughputWindowSeconds:     throughputWindowSeconds,
			ReserveSnapshotBeforeFile:   strings.TrimSpace(reserveSnapshotBeforeFile),
			ReserveSnapshotBeforeSHA256: resolvedReserveSnapshotBeforeSHA256,
			ReserveSnapshotAfterFile:    strings.TrimSpace(reserveSnapshotAfterFile),
			ReserveSnapshotAfterSHA256:  resolvedReserveSnapshotAfterSHA256,
			LatencyMode:                 strings.TrimSpace(latencyMode),
			ColdWarmSeparated:           strings.TrimSpace(coldWarmSeparated),
			BrowserMatrix:               strings.TrimSpace(browserMatrix),
			BrowserAdapterReady:         strings.TrimSpace(browserAdapterReady),
			BrowserAdapterVersion:       strings.TrimSpace(browserAdapterVersion),
			BrowserAdapterFile:          strings.TrimSpace(browserAdapterFile),
			BrowserAdapterSHA256:        resolvedBrowserAdapterSHA256,
			RemoteTopology:              strings.TrimSpace(remoteTopology),
			LinkedProverReportFile:      strings.TrimSpace(linkedProverReportFile),
			LinkedProverReportSHA256:    resolvedLinkedProverReportSHA256,
		},
		Environment: environment{
			MachineProfile: strings.TrimSpace(machineProfile),
			CPUGovernor:    strings.TrimSpace(cpuGovernor),
			MemoryGiB:      strings.TrimSpace(memoryGiB),
			OS:             runtime.GOOS,
			Arch:           runtime.GOARCH,
			CPU:            cpu,
		},
		ArtifactSet: artifactSet{
			ActiveSetID: activeSetID,
		},
		GoVersion:    runtime.Version(),
		GnarkVersion: moduleVersion("github.com/consensys/gnark"),
		GnarkCrypto:  moduleVersion("github.com/consensys/gnark-crypto"),
		OS:           runtime.GOOS,
		Arch:         runtime.GOARCH,
		CPU:          cpu,
		FeeModel: feeModel{
			FeeDenom:      strings.TrimSpace(feeDenom),
			MinGasPrice:   strings.TrimSpace(minGasPrice),
			GasAdjustment: strings.TrimSpace(gasAdjustment),
		},
		Benchmarks: summarizeBenchmarks(samples),
	}

	if strings.TrimSpace(benchmarkSummariesPath) != "" {
		summaries, err := readBenchmarkSummaries(benchmarkSummariesPath)
		if err != nil {
			fatalf("read benchmark summaries: %v", err)
		}
		rep.Benchmarks = append(rep.Benchmarks, summaries...)
	}

	if strings.TrimSpace(txMetricsPath) != "" {
		metrics, err := readTxMetrics(txMetricsPath)
		if err != nil {
			fatalf("read tx metrics: %v", err)
		}
		feeSummaries, err := summarizeFees(metrics, rep.FeeModel)
		if err != nil {
			fatalf("summarize fees: %v", err)
		}
		rep.Fees = feeSummaries
	}
	rep.SourceFileSHA256, rep.SourceFileIssues = hashSourceFiles(rep.SourceFiles)

	resolvedManifest := resolveManifestPath(manifestPath)
	if resolvedManifest != "" {
		checksum, err := fileSHA256(resolvedManifest)
		if err != nil {
			fatalf("hash manifest %s: %v", resolvedManifest, err)
		}
		rep.ManifestPath = resolvedManifest
		rep.ManifestChecksum = checksum
		artifactSet, err := loadArtifactSet(resolvedManifest, activeSetID, checksum)
		if err != nil {
			fatalf("load manifest descriptors %s: %v", resolvedManifest, err)
		}
		rep.ArtifactSet = artifactSet
	}
	rep.ClaimProfile = evaluateClaimProfile(rep)

	writeReportFiles(outDir, rep)

	fmt.Printf("benchmark report written to %s\n", outDir)
}

func writeReportFiles(outDir string, rep report) {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		fatalf("create output directory: %v", err)
	}

	stamp := time.Now().UTC().Format("20060102T150405Z")
	shortCommit := rep.Commit
	if len(shortCommit) > 12 {
		shortCommit = shortCommit[:12]
	}
	if shortCommit == "" {
		shortCommit = "unknown"
	}
	base := fmt.Sprintf("%s-%s", stamp, shortCommit)
	if err := writeJSON(filepath.Join(outDir, base+".json"), rep); err != nil {
		fatalf("write JSON report: %v", err)
	}
	if err := writeJSON(filepath.Join(outDir, "latest.json"), rep); err != nil {
		fatalf("write latest JSON report: %v", err)
	}
	markdown := renderMarkdown(rep)
	if err := os.WriteFile(filepath.Join(outDir, base+".md"), []byte(markdown), 0o644); err != nil {
		fatalf("write Markdown report: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "latest.md"), []byte(markdown), 0o644); err != nil {
		fatalf("write latest Markdown report: %v", err)
	}
}

func parseBenchmarkOutput(raw string) ([]benchmarkSample, string) {
	var samples []benchmarkSample
	cpu := ""
	scanner := bufio.NewScanner(strings.NewReader(raw))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "cpu:") {
			cpu = strings.TrimSpace(strings.TrimPrefix(line, "cpu:"))
			continue
		}
		sample, ok := parseBenchmarkLine(line)
		if !ok {
			continue
		}
		samples = append(samples, sample)
	}
	return samples, cpu
}

func parseBenchmarkLine(line string) (benchmarkSample, bool) {
	fields := strings.Fields(line)
	if len(fields) < 4 {
		return benchmarkSample{}, false
	}
	match := benchmarkNamePattern.FindStringSubmatch(fields[0])
	if match == nil {
		return benchmarkSample{}, false
	}
	if _, err := strconv.ParseUint(fields[1], 10, 64); err != nil {
		return benchmarkSample{}, false
	}

	sample := benchmarkSample{
		Name:    match[1],
		Metrics: make(map[string]float64),
	}
	for i := 2; i+1 < len(fields); i += 2 {
		value, err := strconv.ParseFloat(fields[i], 64)
		if err != nil {
			continue
		}
		unit := fields[i+1]
		sample.Metrics[unit] = value
		switch unit {
		case "ns/op":
			sample.NSOp = value
		case "B/op":
			sample.BytesOp = value
		case "allocs/op":
			sample.AllocsOp = value
		}
	}
	if sample.NSOp == 0 {
		return benchmarkSample{}, false
	}
	return sample, true
}

func summarizeBenchmarks(samples []benchmarkSample) []benchmarkSummary {
	grouped := make(map[string][]benchmarkSample)
	for _, sample := range samples {
		grouped[sample.Name] = append(grouped[sample.Name], sample)
	}

	names := make([]string, 0, len(grouped))
	for name := range grouped {
		names = append(names, name)
	}
	sort.Strings(names)

	summaries := make([]benchmarkSummary, 0, len(names))
	for _, name := range names {
		group := grouped[name]
		nsValues := make([]float64, 0, len(group))
		bytesValues := make([]float64, 0, len(group))
		allocValues := make([]float64, 0, len(group))
		metricValues := make(map[string][]float64)
		for _, sample := range group {
			nsValues = append(nsValues, sample.NSOp)
			bytesValues = append(bytesValues, sample.BytesOp)
			allocValues = append(allocValues, sample.AllocsOp)
			for unit, value := range sample.Metrics {
				metricValues[unit] = append(metricValues[unit], value)
			}
		}
		meanNS := mean(nsValues)
		summary := benchmarkSummary{
			Name:       name,
			Samples:    len(group),
			NSOpMean:   meanNS,
			NSOpP50:    percentile(nsValues, 50),
			NSOpP95:    percentile(nsValues, 95),
			NSOpP99:    percentile(nsValues, 99),
			NSOpMin:    min(nsValues),
			NSOpMax:    max(nsValues),
			OpsPerSec:  1e9 / meanNS,
			BytesOp:    mean(bytesValues),
			AllocsOp:   mean(allocValues),
			MetricKind: benchmarkMetricKind(name),
			Metrics:    summarizeMetricValues(metricValues),
		}
		summaries = append(summaries, summary)
	}
	return summaries
}

func summarizeMetricValues(metricValues map[string][]float64) map[string]metricSummary {
	if len(metricValues) == 0 {
		return nil
	}
	result := make(map[string]metricSummary, len(metricValues))
	for unit, values := range metricValues {
		result[unit] = metricSummary{
			Mean: mean(values),
			P50:  percentile(values, 50),
			P95:  percentile(values, 95),
			P99:  percentile(values, 99),
			Min:  min(values),
			Max:  max(values),
		}
	}
	return result
}

func benchmarkMetricKind(name string) string {
	switch {
	case strings.Contains(name, "HTTPProverClient"):
		return "prover_http_client_roundtrip"
	case strings.HasSuffix(name, "Prove"):
		return "native_proving"
	case strings.HasSuffix(name, "Verify"):
		return "native_verification"
	case strings.HasSuffix(name, "PublicWitness"):
		return "public_witness"
	case strings.HasSuffix(name, "Compile"):
		return "compile"
	case strings.HasSuffix(name, "Setup"):
		return "groth16_setup"
	case strings.HasSuffix(name, "ArtifactWrite"):
		return "artifact_write"
	default:
		return "unknown"
	}
}

func customMetricRows(benchmarks []benchmarkSummary) []namedMetricSummary {
	var rows []namedMetricSummary
	for _, bench := range benchmarks {
		for metricName, metric := range bench.Metrics {
			switch metricName {
			case "ns/op", "B/op", "allocs/op":
				continue
			}
			rows = append(rows, namedMetricSummary{
				BenchmarkName: bench.Name,
				MetricName:    metricName,
				Metric:        metric,
			})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].BenchmarkName == rows[j].BenchmarkName {
			return rows[i].MetricName < rows[j].MetricName
		}
		return rows[i].BenchmarkName < rows[j].BenchmarkName
	})
	return rows
}

func buildAggregateReport(paths []string, generatedAt, sourceCommit string, sourceDirty bool, runProfile string, env environment) (report, error) {
	paths = uniqueStrings(paths)
	if len(paths) == 0 {
		return report{}, fmt.Errorf("at least one report path is required")
	}

	var components []componentReport
	var benchmarks []benchmarkSummary
	var fees []feeSummary
	var componentReports []report
	var reasons []string
	for _, path := range paths {
		component, sha, err := readReportFile(path)
		if err != nil {
			return report{}, fmt.Errorf("read component report %s: %w", path, err)
		}
		componentReports = append(componentReports, component)
		components = append(components, componentReport{
			Path:            path,
			SHA256:          sha,
			ResultFamily:    component.ResultFamily,
			RunProfile:      component.ClaimProfile.RunProfile,
			ClaimTypes:      append([]string(nil), component.ClaimProfile.ClaimTypes...),
			Eligible:        component.ClaimProfile.Eligible,
			BlockingReasons: append([]string(nil), component.ClaimProfile.BlockingReasons...),
			Commit:          component.Commit,
			Dirty:           component.Dirty,
			ActiveSetID:     component.ActiveSetID,
			ManifestSHA256:  component.ArtifactSet.ManifestSHA256,
			RunStartedAt:    component.RunStartedAt,
			RunEndedAt:      component.RunEndedAt,
			MachineProfile:  component.Environment.MachineProfile,
		})
		if !component.ClaimProfile.Eligible {
			reasons = append(reasons, fmt.Sprintf("component report %s is not eligible", path))
		}
		benchmarks = append(benchmarks, component.Benchmarks...)
		fees = append(fees, component.Fees...)
	}

	claimTypes := aggregateClaimTypes(componentReports)
	runStartedAt, runEndedAt := aggregateRunWindow(componentReports)
	activeSetID, activeSetOK := commonStringValue(componentReports, func(rep report) string { return rep.ActiveSetID })
	manifestSHA256, manifestOK := commonStringValue(componentReports, func(rep report) string { return rep.ArtifactSet.ManifestSHA256 })
	commit, commitOK := commonStringValue(componentReports, func(rep report) string { return rep.Commit })
	if sourceCommit != "" {
		commit = sourceCommit
	}
	if !activeSetOK {
		reasons = append(reasons, "component reports use different active_set_id values")
	}
	if !manifestOK {
		reasons = append(reasons, "component reports use different artifact manifest checksums")
	}
	if !commitOK {
		reasons = append(reasons, "component reports use different source commits")
	}
	claimEvidenceByType, evidenceIssues := aggregateClaimEvidenceByType(componentReports)
	reasons = append(reasons, evidenceIssues...)
	artifactSet := aggregateArtifactSet(activeSetID, manifestSHA256, componentReports)

	sourceHashes, sourceIssues := hashSourceFiles(paths)
	rep := report{
		SchemaVersion:    reportSchemaVersion,
		GeneratedAt:      generatedAt,
		ResultFamily:     "public-capacity",
		SourceFiles:      paths,
		SourceFileSHA256: sourceHashes,
		SourceFileIssues: sourceIssues,
		RunStartedAt:     runStartedAt,
		RunEndedAt:       runEndedAt,
		Commit:           commit,
		Dirty:            sourceDirty || anyComponentDirty(componentReports),
		ActiveSetID:      activeSetID,
		ClaimProfile: claimProfile{
			RunProfile:      runProfile,
			ClaimTypes:      claimTypes,
			Eligible:        false,
			BlockingReasons: uniqueStrings(reasons),
		},
		ClaimEvidenceByType: claimEvidenceByType,
		Environment:         env,
		ArtifactSet:         artifactSet,
		GoVersion:           runtime.Version(),
		GnarkVersion:        moduleVersion("github.com/consensys/gnark"),
		GnarkCrypto:         moduleVersion("github.com/consensys/gnark-crypto"),
		OS:                  runtime.GOOS,
		Arch:                runtime.GOARCH,
		ComponentReports:    components,
		Benchmarks:          benchmarks,
		Fees:                fees,
	}
	rep.ClaimProfile = evaluateClaimProfile(rep)
	return rep, nil
}

func aggregateClaimEvidenceByType(reports []report) (map[string]claimEvidence, []string) {
	evidenceByType := make(map[string]claimEvidence)
	var issues []string
	for _, rep := range reports {
		for _, claimType := range rep.ClaimProfile.ClaimTypes {
			claimType = strings.TrimSpace(claimType)
			if claimType == "" {
				continue
			}
			evidence := evidenceForClaim(rep, claimType)
			if !hasClaimEvidence(evidence) {
				issues = append(issues, fmt.Sprintf("component report for %s has no claim evidence", claimType))
				continue
			}
			if existing, ok := evidenceByType[claimType]; ok && !reflect.DeepEqual(existing, evidence) {
				issues = append(issues, fmt.Sprintf("component reports have conflicting %s claim evidence", claimType))
				continue
			}
			evidenceByType[claimType] = evidence
		}
	}
	if len(evidenceByType) == 0 {
		return nil, uniqueStrings(issues)
	}
	return evidenceByType, uniqueStrings(issues)
}

func aggregateArtifactSet(activeSetID, manifestSHA256 string, reports []report) artifactSet {
	set := artifactSet{
		ActiveSetID:           activeSetID,
		ManifestActiveSetID:   activeSetID,
		ManifestSHA256:        manifestSHA256,
		DescriptorComplete:    len(reports) > 0,
		ArtifactFilesVerified: len(reports) > 0,
		ArtifactSHA256ByFile:  make(map[string]string),
	}
	var descriptorIssues []string
	var artifactFileIssues []string
	for _, rep := range reports {
		if !rep.ArtifactSet.DescriptorComplete {
			set.DescriptorComplete = false
		}
		if !rep.ArtifactSet.ArtifactFilesVerified {
			set.ArtifactFilesVerified = false
		}
		descriptorIssues = append(descriptorIssues, rep.ArtifactSet.DescriptorIssues...)
		artifactFileIssues = append(artifactFileIssues, rep.ArtifactSet.ArtifactFileIssues...)
		set.ArtifactDescriptors = append(set.ArtifactDescriptors, rep.ArtifactSet.ArtifactDescriptors...)
		for path, checksum := range rep.ArtifactSet.ArtifactSHA256ByFile {
			if existing, ok := set.ArtifactSHA256ByFile[path]; ok && !strings.EqualFold(existing, checksum) {
				set.ArtifactFilesVerified = false
				artifactFileIssues = append(artifactFileIssues, fmt.Sprintf("artifact file %s has conflicting checksums across component reports", path))
				continue
			}
			set.ArtifactSHA256ByFile[path] = checksum
		}
	}
	set.DescriptorIssues = uniqueStrings(descriptorIssues)
	set.ArtifactFileIssues = uniqueStrings(artifactFileIssues)
	if len(set.ArtifactSHA256ByFile) == 0 {
		set.ArtifactSHA256ByFile = nil
	}
	return set
}

func readReportFile(path string) (report, string, error) {
	bz, err := os.ReadFile(path)
	if err != nil {
		return report{}, "", err
	}
	sum := sha256.Sum256(bz)
	var rep report
	if err := json.Unmarshal(bz, &rep); err != nil {
		return report{}, "", err
	}
	return rep, hex.EncodeToString(sum[:]), nil
}

func standardReportPaths() []string {
	return []string{
		"benchmarks/privacy-circuits/latest.json",
		"benchmarks/privacy-proverd/latest.json",
		"benchmarks/privacy-proverd-load/latest.json",
		"benchmarks/privacy-localnet/latest.json",
		"benchmarks/privacy-localnet-tps/latest.json",
		"benchmarks/privacy-user-latency/latest.json",
		"benchmarks/public-capacity/latest.json",
	}
}

func existingStandardReportPaths() []string {
	var paths []string
	for _, path := range standardReportPaths() {
		if _, err := os.Stat(path); err == nil {
			paths = append(paths, path)
		}
	}
	return paths
}

func readHumanSummaryComponents(paths []string) ([]humanSummaryComponent, error) {
	paths = uniqueStrings(paths)
	if len(paths) == 0 {
		return nil, fmt.Errorf("at least one benchmark report path is required")
	}
	components := make([]humanSummaryComponent, 0, len(paths))
	for _, path := range paths {
		rep, _, err := readReportFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		components = append(components, humanSummaryComponent{
			Path:   path,
			Report: rep,
		})
	}
	sort.SliceStable(components, func(i, j int) bool {
		return humanSummaryFamilyOrder(components[i].Report.ResultFamily) < humanSummaryFamilyOrder(components[j].Report.ResultFamily)
	})
	return components, nil
}

func humanSummaryFamilyOrder(family string) int {
	switch family {
	case "privacy-circuits":
		return 10
	case "privacy-proverd":
		return 20
	case "privacy-proverd-load":
		return 30
	case "privacy-localnet":
		return 40
	case "privacy-localnet-tps":
		return 50
	case "privacy-user-latency":
		return 60
	case "public-capacity":
		return 70
	default:
		return 100
	}
}

func aggregateClaimTypes(reports []report) []string {
	var values []string
	for _, rep := range reports {
		values = append(values, rep.ClaimProfile.ClaimTypes...)
	}
	return uniqueStrings(values)
}

func aggregateRunWindow(reports []report) (string, string) {
	var starts []time.Time
	var ends []time.Time
	for _, rep := range reports {
		if start, err := time.Parse(time.RFC3339, strings.TrimSpace(rep.RunStartedAt)); err == nil {
			starts = append(starts, start)
		}
		if end, err := time.Parse(time.RFC3339, strings.TrimSpace(rep.RunEndedAt)); err == nil {
			ends = append(ends, end)
		}
	}
	start := ""
	if len(starts) > 0 {
		sort.Slice(starts, func(i, j int) bool { return starts[i].Before(starts[j]) })
		start = starts[0].UTC().Format(time.RFC3339)
	}
	end := ""
	if len(ends) > 0 {
		sort.Slice(ends, func(i, j int) bool { return ends[i].After(ends[j]) })
		end = ends[0].UTC().Format(time.RFC3339)
	}
	return start, end
}

func commonStringValue(reports []report, pick func(report) string) (string, bool) {
	value := ""
	for _, rep := range reports {
		next := strings.TrimSpace(pick(rep))
		if next == "" {
			continue
		}
		if value == "" {
			value = next
			continue
		}
		if !strings.EqualFold(value, next) {
			return value, false
		}
	}
	return value, true
}

func anyComponentDirty(reports []report) bool {
	for _, rep := range reports {
		if rep.Dirty {
			return true
		}
	}
	return false
}

func goBenchmarkRows(benchmarks []benchmarkSummary) []benchmarkSummary {
	rows := make([]benchmarkSummary, 0, len(benchmarks))
	for _, bench := range benchmarks {
		if benchmarkHasGoMetrics(bench) {
			rows = append(rows, bench)
		}
	}
	return rows
}

func benchmarkHasGoMetrics(bench benchmarkSummary) bool {
	return bench.NSOpMean > 0 ||
		bench.NSOpP50 > 0 ||
		bench.NSOpP95 > 0 ||
		bench.NSOpP99 > 0 ||
		bench.OpsPerSec > 0 ||
		bench.BytesOp > 0 ||
		bench.AllocsOp > 0
}

func sortedMapKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedMapKeysClaimEvidence(values map[string]claimEvidence) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func reportsFromComponents(components []humanSummaryComponent) []report {
	reports := make([]report, 0, len(components))
	for _, component := range components {
		reports = append(reports, component.Report)
	}
	return reports
}

func firstNonEmptyReportValue(reports []report, pick func(report) string) string {
	for _, rep := range reports {
		value := strings.TrimSpace(pick(rep))
		if value != "" {
			return value
		}
	}
	return ""
}

func findComponentByFamily(components []humanSummaryComponent, family string) (humanSummaryComponent, bool) {
	for _, component := range components {
		if component.Report.ResultFamily == family {
			return component, true
		}
	}
	return humanSummaryComponent{}, false
}

func findPreferredFeeComponent(components []humanSummaryComponent) (humanSummaryComponent, bool) {
	for _, family := range []string{"privacy-localnet-tps", "privacy-localnet"} {
		if component, ok := findComponentByFamily(components, family); ok && len(component.Report.Fees) > 0 {
			return component, true
		}
	}
	return humanSummaryComponent{}, false
}

func findBenchmark(benchmarks []benchmarkSummary, name string) (benchmarkSummary, bool) {
	for _, bench := range benchmarks {
		if bench.Name == name {
			return bench, true
		}
	}
	return benchmarkSummary{}, false
}

func firstBenchmarkByKind(benchmarks []benchmarkSummary, kind string) (benchmarkSummary, bool) {
	for _, bench := range benchmarks {
		if bench.MetricKind == kind {
			return bench, true
		}
	}
	return benchmarkSummary{}, false
}

func firstBenchmarkByFlow(benchmarks []benchmarkSummary, flow string) *benchmarkSummary {
	for _, bench := range benchmarks {
		if bench.FlowProfile == flow {
			found := bench
			return &found
		}
	}
	return nil
}

func bestMetricBenchmark(benchmarks []benchmarkSummary, metricName string) *benchmarkSummary {
	var best *benchmarkSummary
	bestValue := math.Inf(-1)
	for _, bench := range benchmarks {
		metric, ok := bench.Metrics[metricName]
		if !ok {
			continue
		}
		if metric.Mean > bestValue {
			bestValue = metric.Mean
			found := bench
			best = &found
		}
	}
	return best
}

func sortedBenchmarksByName(benchmarks []benchmarkSummary) []benchmarkSummary {
	rows := append([]benchmarkSummary(nil), benchmarks...)
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].Name < rows[j].Name
	})
	return rows
}

func sortedBenchmarksByFlow(benchmarks []benchmarkSummary) []benchmarkSummary {
	rows := append([]benchmarkSummary(nil), benchmarks...)
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].FlowProfile == rows[j].FlowProfile {
			return rows[i].Name < rows[j].Name
		}
		return rows[i].FlowProfile < rows[j].FlowProfile
	})
	return rows
}

func sortedFees(fees []feeSummary) []feeSummary {
	order := map[string]int{
		"deposit":        10,
		"dummy_deposit":  20,
		"transfer":       30,
		"withdraw":       40,
		"relay_withdraw": 50,
	}
	rows := append([]feeSummary(nil), fees...)
	sort.Slice(rows, func(i, j int) bool {
		left := order[rows[i].TxType]
		right := order[rows[j].TxType]
		if left == right {
			return rows[i].TxType < rows[j].TxType
		}
		return left < right
	})
	return rows
}

func metricMean(bench *benchmarkSummary, name string) float64 {
	if bench == nil {
		return 0
	}
	metric, ok := bench.Metrics[name]
	if !ok {
		return 0
	}
	return metric.Mean
}

func metricP95(bench *benchmarkSummary, name string) float64 {
	if bench == nil {
		return 0
	}
	metric, ok := bench.Metrics[name]
	if !ok {
		return 0
	}
	return metric.P95
}

func metricP99(bench *benchmarkSummary, name string) float64 {
	if bench == nil {
		return 0
	}
	metric, ok := bench.Metrics[name]
	if !ok {
		return 0
	}
	return metric.P99
}

func readReserveSnapshotFromReport(rep report) (reserveSnapshot, bool) {
	for _, path := range rep.SourceFiles {
		base := filepath.Base(path)
		if !strings.HasPrefix(base, "reserve-") || !strings.HasSuffix(base, ".json") {
			continue
		}
		bz, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var snapshot reserveSnapshot
		if err := json.Unmarshal(bz, &snapshot); err != nil {
			continue
		}
		return snapshot, true
	}
	return reserveSnapshot{}, false
}

func humanSummaryTargetForFamily(family string) string {
	switch family {
	case "privacy-circuits":
		return "make privacy-bench"
	case "privacy-proverd":
		return "make privacy-proverd-bench"
	case "privacy-proverd-load":
		return "make privacy-proverd-load-bench"
	case "privacy-localnet":
		return "make privacy-bench-localnet"
	case "privacy-localnet-tps":
		return "make privacy-localnet-tps-bench"
	case "privacy-user-latency":
		return "make privacy-user-latency-bench"
	case "public-capacity":
		return "make privacy-public-capacity-report"
	default:
		return family
	}
}

func humanSummaryFamilyNote(family string) string {
	switch family {
	case "privacy-circuits":
		return "Native circuit prove/verify/setup/compile/artifact write"
	case "privacy-proverd":
		return "In-process HTTP prover transport overhead"
	case "privacy-proverd-load":
		return "External clairveil-proverd load"
	case "privacy-localnet":
		return "CLI e2e, fee/gas, reserve snapshot"
	case "privacy-localnet-tps":
		return "Localnet smoke를 chain TPS schema로 변환"
	case "privacy-user-latency":
		return "Wallet flow latency trace"
	case "public-capacity":
		return "Public claim gate aggregate"
	default:
		return ""
	}
}

func humanSummaryResultLabel(rep report) string {
	if rep.ResultFamily == "public-capacity" && !rep.ClaimProfile.Eligible {
		return "성공, ineligible"
	}
	return "성공"
}

func humanSummaryWindow(start, end string) string {
	start = strings.TrimSpace(start)
	end = strings.TrimSpace(end)
	if start == "" && end == "" {
		return "-"
	}
	return valueOrDash(start) + " to " + valueOrDash(end)
}

func humanSummaryCircuitOperation(name string) string {
	switch name {
	case "BenchmarkDepositCircuitProve":
		return "Deposit prove"
	case "BenchmarkDepositCircuitVerify":
		return "Deposit verify"
	case "BenchmarkSpendCircuitProve":
		return "Spend prove"
	case "BenchmarkSpendCircuitVerify":
		return "Spend verify"
	case "BenchmarkJoinSplitCircuitProve":
		return "JoinSplit prove"
	case "BenchmarkJoinSplitCircuitVerify":
		return "JoinSplit verify"
	default:
		return "`" + name + "`"
	}
}

func valueOrDash(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	return value
}

func renderHumanSummaryMarkdownKR(components []humanSummaryComponent, generatedAt string) string {
	reports := reportsFromComponents(components)
	commit, commitOK := commonStringValue(reports, func(rep report) string { return rep.Commit })
	if !commitOK && commit != "" {
		commit += " (mixed)"
	}
	activeSetID, _ := commonStringValue(reports, func(rep report) string { return rep.ActiveSetID })
	goVersion, _ := commonStringValue(reports, func(rep report) string { return rep.GoVersion })
	gnarkVersion, _ := commonStringValue(reports, func(rep report) string { return rep.GnarkVersion })
	gnarkCryptoVersion, _ := commonStringValue(reports, func(rep report) string { return rep.GnarkCrypto })
	osName, _ := commonStringValue(reports, func(rep report) string { return rep.OS })
	arch, _ := commonStringValue(reports, func(rep report) string { return rep.Arch })
	cpu := firstNonEmptyReportValue(reports, func(rep report) string { return rep.CPU })
	runStartedAt, runEndedAt := aggregateRunWindow(reports)
	dirty := anyComponentDirty(reports)

	var b strings.Builder
	fmt.Fprintf(&b, "# Clairveil Privacy Benchmark 결과 리포트\n\n")
	fmt.Fprintf(&b, "> 이 파일은 `clairveil-benchreport -human-summary-out`로 자동 생성된다. 수동으로 편집하지 말고 benchmark 산출물을 다시 생성한 뒤 이 리포트를 재생성한다.\n\n")
	fmt.Fprintf(&b, "- report_generated_at: `%s`\n", generatedAt)
	if runStartedAt != "" || runEndedAt != "" {
		fmt.Fprintf(&b, "- benchmark_run_window: `%s` to `%s`\n", runStartedAt, runEndedAt)
	}
	fmt.Fprintf(&b, "- 기준 commit: `%s`\n", valueOrDash(commit))
	fmt.Fprintf(&b, "- working_tree_dirty: `%t`\n", dirty)
	fmt.Fprintf(&b, "- active_set_id: `%s`\n", valueOrDash(activeSetID))
	fmt.Fprintf(&b, "- 플랫폼: `%s/%s`\n", valueOrDash(osName), valueOrDash(arch))
	if cpu != "" {
		fmt.Fprintf(&b, "- CPU: `%s`\n", cpu)
	}
	fmt.Fprintf(&b, "- Go: `%s`\n", valueOrDash(goVersion))
	fmt.Fprintf(&b, "- gnark: `%s`\n", valueOrDash(gnarkVersion))
	fmt.Fprintf(&b, "- gnark-crypto: `%s`\n\n", valueOrDash(gnarkCryptoVersion))

	fmt.Fprintf(&b, "현재 결과는 개발용 benchmark report이다. Public capacity claim, 특히 \"TPS\", \"운영 prover 처리량\", \"사용자가 체감할 실제 지연\"의 공개 수치로 바로 쓰려면 `public-capacity` gate가 통과해야 한다.\n\n")

	renderHumanSummaryConclusionKR(&b, components)
	renderHumanSummaryExecutionKR(&b, components)
	renderHumanSummaryReportStatusKR(&b, components)
	component, ok := findComponentByFamily(components, "privacy-circuits")
	renderHumanSummaryNativeCircuitsKR(&b, component, ok)
	component, ok = findComponentByFamily(components, "privacy-proverd")
	renderHumanSummaryHTTPTransportKR(&b, component, ok)
	component, ok = findComponentByFamily(components, "privacy-proverd-load")
	renderHumanSummaryExternalProverdKR(&b, component, ok)
	component, ok = findPreferredFeeComponent(components)
	renderHumanSummaryLocalnetKR(&b, component, ok)
	component, ok = findComponentByFamily(components, "privacy-localnet-tps")
	renderHumanSummaryTPSKR(&b, component, ok)
	component, ok = findComponentByFamily(components, "privacy-user-latency")
	renderHumanSummaryUserLatencyKR(&b, component, ok)
	component, ok = findComponentByFamily(components, "public-capacity")
	renderHumanSummaryPublicCapacityKR(&b, component, ok)
	renderHumanSummaryArtifactsKR(&b, components)
	renderHumanSummaryLimitationsKR(&b)

	return b.String()
}

func renderHumanSummaryConclusionKR(b *strings.Builder, components []humanSummaryComponent) {
	fmt.Fprintf(b, "## 결론\n\n")
	for _, line := range humanSummaryHeadlineLines(components) {
		fmt.Fprintf(b, "- %s\n", line)
	}
	fmt.Fprintf(b, "\n")
}

func humanSummaryHeadlineLines(components []humanSummaryComponent) []string {
	var lines []string
	if component, ok := findComponentByFamily(components, "privacy-circuits"); ok {
		if bench, ok := findBenchmark(component.Report.Benchmarks, "BenchmarkJoinSplitCircuitProve"); ok {
			lines = append(lines, fmt.Sprintf("Native JoinSplit/transfer proof는 평균 %s, p95 %s였다.", formatDurationNS(bench.NSOpMean), formatDurationNS(bench.NSOpP95)))
		}
	}
	if component, ok := findComponentByFamily(components, "privacy-proverd-load"); ok {
		best := bestMetricBenchmark(component.Report.Benchmarks, "requests/sec")
		if best != nil {
			if latency, ok := best.Metrics["latency_ms"]; ok {
				lines = append(lines, fmt.Sprintf("External `clairveil-proverd` %s는 %.3f req/s, latency p95 %.3f ms로 측정됐다.", best.Name, best.Metrics["requests/sec"].Mean, latency.P95))
			}
		}
	}
	if component, ok := findComponentByFamily(components, "privacy-localnet-tps"); ok {
		if bench, ok := firstBenchmarkByKind(component.Report.Benchmarks, "chain_tps"); ok {
			if metric, ok := bench.Metrics["tx/sec"]; ok {
				lines = append(lines, fmt.Sprintf("Localnet smoke의 observed throughput은 %.6g tx/s지만, scripted e2e smoke이므로 capacity TPS로 해석하면 안 된다.", metric.Mean))
			}
		}
	}
	if component, ok := findComponentByFamily(components, "privacy-user-latency"); ok {
		transfer := firstBenchmarkByFlow(component.Report.Benchmarks, "transfer_all_private")
		deposit := firstBenchmarkByFlow(component.Report.Benchmarks, "deposit")
		if transfer != nil && deposit != nil {
			lines = append(lines, fmt.Sprintf("User latency smoke는 deposit 평균 %.3f ms, all-private transfer 평균 %.3f ms 수준으로 관측됐다.", metricMean(deposit, "total_latency_ms"), metricMean(transfer, "total_latency_ms")))
		}
	}
	if component, ok := findComponentByFamily(components, "public-capacity"); ok {
		lines = append(lines, fmt.Sprintf("Public capacity aggregate는 `eligible=%t`이다.", component.Report.ClaimProfile.Eligible))
	}
	if len(lines) == 0 {
		lines = append(lines, "입력 benchmark report가 비어 있어 요약할 수 있는 핵심 수치가 없다.")
	}
	return lines
}

func renderHumanSummaryExecutionKR(b *strings.Builder, components []humanSummaryComponent) {
	fmt.Fprintf(b, "## 실행 대상\n\n")
	fmt.Fprintf(b, "| Target | 결과 | 생성 family | 비고 |\n")
	fmt.Fprintf(b, "| --- | --- | --- | --- |\n")
	for _, family := range []string{
		"privacy-circuits",
		"privacy-proverd",
		"privacy-proverd-load",
		"privacy-localnet",
		"privacy-localnet-tps",
		"privacy-user-latency",
		"public-capacity",
	} {
		component, ok := findComponentByFamily(components, family)
		if !ok {
			continue
		}
		fmt.Fprintf(b, "| `%s` | %s | `%s` | %s |\n", humanSummaryTargetForFamily(family), humanSummaryResultLabel(component.Report), family, humanSummaryFamilyNote(family))
	}
	fmt.Fprintf(b, "\n")
}

func renderHumanSummaryReportStatusKR(b *strings.Builder, components []humanSummaryComponent) {
	fmt.Fprintf(b, "## Report 상태\n\n")
	fmt.Fprintf(b, "| Family | generated_at UTC | run window UTC | eligible | rows |\n")
	fmt.Fprintf(b, "| --- | --- | --- | ---: | ---: |\n")
	for _, component := range components {
		rep := component.Report
		fmt.Fprintf(
			b,
			"| `%s` | `%s` | `%s` | %t | %d |\n",
			valueOrDash(rep.ResultFamily),
			valueOrDash(rep.GeneratedAt),
			humanSummaryWindow(rep.RunStartedAt, rep.RunEndedAt),
			rep.ClaimProfile.Eligible,
			len(rep.Benchmarks),
		)
	}
	fmt.Fprintf(b, "\n")
}

func renderHumanSummaryNativeCircuitsKR(b *strings.Builder, component humanSummaryComponent, ok bool) {
	if !ok {
		return
	}
	fmt.Fprintf(b, "## Native Circuit\n\n")
	fmt.Fprintf(b, "| Operation | Samples | Mean | p95 | Mean ops/sec |\n")
	fmt.Fprintf(b, "| --- | ---: | ---: | ---: | ---: |\n")
	for _, name := range []string{
		"BenchmarkDepositCircuitProve",
		"BenchmarkDepositCircuitVerify",
		"BenchmarkSpendCircuitProve",
		"BenchmarkSpendCircuitVerify",
		"BenchmarkJoinSplitCircuitProve",
		"BenchmarkJoinSplitCircuitVerify",
	} {
		bench, found := findBenchmark(component.Report.Benchmarks, name)
		if !found {
			continue
		}
		fmt.Fprintf(
			b,
			"| %s | %d | %s | %s | %.2f |\n",
			humanSummaryCircuitOperation(name),
			bench.Samples,
			formatDurationNS(bench.NSOpMean),
			formatDurationNS(bench.NSOpP95),
			bench.OpsPerSec,
		)
	}
	fmt.Fprintf(b, "\n해석:\n\n")
	fmt.Fprintf(b, "- JoinSplit proving이 가장 큰 단일 CPU 비용이다.\n")
	fmt.Fprintf(b, "- Verification은 세 회로 모두 매우 작게 측정된다.\n")
	fmt.Fprintf(b, "- 이 표는 native microbenchmark이며, chain TPS나 운영 prover throughput으로 직접 환산하면 안 된다.\n\n")
}

func renderHumanSummaryHTTPTransportKR(b *strings.Builder, component humanSummaryComponent, ok bool) {
	if !ok {
		return
	}
	fmt.Fprintf(b, "## In-Process Prover HTTP Transport\n\n")
	fmt.Fprintf(b, "| Operation | Samples | Mean | p95 | Mean ops/sec |\n")
	fmt.Fprintf(b, "| --- | ---: | ---: | ---: | ---: |\n")
	for _, bench := range sortedBenchmarksByName(component.Report.Benchmarks) {
		if bench.MetricKind != "prover_http_client_roundtrip" {
			continue
		}
		fmt.Fprintf(
			b,
			"| `%s` | %d | %s | %s | %.2f |\n",
			bench.Name,
			bench.Samples,
			formatDurationNS(bench.NSOpMean),
			formatDurationNS(bench.NSOpP95),
			bench.OpsPerSec,
		)
	}
	fmt.Fprintf(b, "\n해석:\n\n")
	fmt.Fprintf(b, "- 이 결과는 HTTP client/server adapter와 JSON payload 처리 오버헤드다.\n")
	fmt.Fprintf(b, "- 실제 Groth16 proving은 포함하지 않는다.\n\n")
}

func renderHumanSummaryExternalProverdKR(b *strings.Builder, component humanSummaryComponent, ok bool) {
	if !ok {
		return
	}
	fmt.Fprintf(b, "## External Proverd Load\n\n")
	fmt.Fprintf(b, "| Bucket | Concurrency | Samples | Requests/sec | Latency mean | Latency p95 | Latency p99 | Error rate | Timeout rate | RSS p95 |\n")
	fmt.Fprintf(b, "| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	for _, bench := range sortedBenchmarksByName(component.Report.Benchmarks) {
		if bench.MetricKind != "prover_load" {
			continue
		}
		fmt.Fprintf(
			b,
			"| `%s` | %d | %d | %.3f | %.3f ms | %.3f ms | %.3f ms | %.6g | %.6g | %.0f B |\n",
			bench.Name,
			bench.Concurrency,
			bench.Samples,
			metricMean(&bench, "requests/sec"),
			metricMean(&bench, "latency_ms"),
			metricP95(&bench, "latency_ms"),
			metricP99(&bench, "latency_ms"),
			metricMean(&bench, "error_rate"),
			metricMean(&bench, "timeout_rate"),
			metricP95(&bench, "rss_bytes"),
		)
	}
	fmt.Fprintf(b, "\n해석:\n\n")
	fmt.Fprintf(b, "- 실제 `clairveil-proverd` 프로세스에 HTTP 요청을 넣은 결과다.\n")
	fmt.Fprintf(b, "- Smoke duration이 짧으므로 운영 capacity claim으로 쓰려면 더 긴 steady-state, saturation sweep, machine/config evidence가 필요하다.\n\n")
}

func renderHumanSummaryLocalnetKR(b *strings.Builder, component humanSummaryComponent, ok bool) {
	if !ok {
		return
	}
	rep := component.Report
	if len(rep.Fees) == 0 {
		return
	}
	fmt.Fprintf(b, "## Localnet Fee 및 Reserve\n\n")
	if rep.FeeModel.FeeDenom != "" || rep.FeeModel.MinGasPrice != "" || rep.FeeModel.GasAdjustment != "" {
		fmt.Fprintf(b, "Fee model: denom `%s`, min gas price `%s`, gas adjustment `%s`\n\n", rep.FeeModel.FeeDenom, rep.FeeModel.MinGasPrice, rep.FeeModel.GasAdjustment)
	}
	fmt.Fprintf(b, "| Tx type | Samples | Gas p50 | Gas p95 | Fee p50 | Fee p95 |\n")
	fmt.Fprintf(b, "| --- | ---: | ---: | ---: | ---: | ---: |\n")
	for _, fee := range sortedFees(rep.Fees) {
		fmt.Fprintf(
			b,
			"| `%s` | %d | %d | %d | `%s` | `%s` |\n",
			fee.TxType,
			fee.Samples,
			fee.GasUsedP50,
			fee.GasUsedP95,
			fee.EstimatedFeeP50,
			fee.EstimatedFeeP95,
		)
	}
	if reserve, ok := readReserveSnapshotFromReport(rep); ok {
		fmt.Fprintf(b, "\nReserve snapshot:\n\n")
		fmt.Fprintf(b, "| Field | Value |\n")
		fmt.Fprintf(b, "| --- | ---: |\n")
		fmt.Fprintf(b, "| denom | `%s` |\n", reserve.Denom)
		fmt.Fprintf(b, "| total deposited | %s |\n", reserve.TotalDeposited)
		fmt.Fprintf(b, "| total withdrawn | %s |\n", reserve.TotalWithdrawn)
		fmt.Fprintf(b, "| expected module balance | %s |\n", reserve.ExpectedModuleBalance)
		fmt.Fprintf(b, "| module balance | %s |\n", reserve.ModuleBalance)
		fmt.Fprintf(b, "| invariant holds | %t |\n", reserve.InvariantHolds)
	}
	fmt.Fprintf(b, "\n해석:\n\n")
	fmt.Fprintf(b, "- Fee는 observed `gas_used` 기반 추정치이며, prover infrastructure cost는 포함하지 않는다.\n")
	fmt.Fprintf(b, "- Reserve snapshot은 localnet smoke flow 기준 accounting invariant 확인용이다.\n\n")
}

func renderHumanSummaryTPSKR(b *strings.Builder, component humanSummaryComponent, ok bool) {
	if !ok {
		return
	}
	bench, found := firstBenchmarkByKind(component.Report.Benchmarks, "chain_tps")
	if !found {
		return
	}
	fmt.Fprintf(b, "## Localnet TPS Smoke\n\n")
	fmt.Fprintf(b, "| Metric | Value |\n")
	fmt.Fprintf(b, "| --- | ---: |\n")
	fmt.Fprintf(b, "| benchmark | `%s` |\n", bench.Name)
	fmt.Fprintf(b, "| samples | %d |\n", bench.Samples)
	fmt.Fprintf(b, "| duration | %d s |\n", bench.DurationSeconds)
	fmt.Fprintf(b, "| target tx/sec | %.6g |\n", bench.TargetTxPerSec)
	fmt.Fprintf(b, "| measured tx/sec | %.6g |\n", metricMean(&bench, "tx/sec"))
	fmt.Fprintf(b, "| failed tx rate | %.6g |\n", metricMean(&bench, "failed_tx_rate"))
	fmt.Fprintf(b, "| gas used p95 | %.6g |\n", metricP95(&bench, "gas_used"))
	if metric, ok := bench.Metrics["inclusion_latency_ms"]; ok {
		fmt.Fprintf(b, "| inclusion latency p95 | %.6g ms |\n", metric.P95)
	}
	fmt.Fprintf(b, "\n해석:\n\n")
	fmt.Fprintf(b, "- 이 값은 capacity TPS가 아니라 localnet scripted e2e smoke를 TPS schema로 변환한 결과다.\n")
	fmt.Fprintf(b, "- Public TPS claim에는 target tx/sec sweep과 positive inclusion latency 계측이 필요하다.\n\n")
}

func renderHumanSummaryUserLatencyKR(b *strings.Builder, component humanSummaryComponent, ok bool) {
	if !ok {
		return
	}
	fmt.Fprintf(b, "## User Latency Smoke\n\n")
	fmt.Fprintf(b, "| Flow | Samples | Total mean | Total p95 | Prepare mean | Proof mean | Submit mean | Error rate |\n")
	fmt.Fprintf(b, "| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	for _, bench := range sortedBenchmarksByFlow(component.Report.Benchmarks) {
		if bench.MetricKind != "user_latency" {
			continue
		}
		fmt.Fprintf(
			b,
			"| `%s` | %d | %.3f ms | %.3f ms | %.3f ms | %.3f ms | %.3f ms | %.6g |\n",
			bench.FlowProfile,
			bench.Samples,
			metricMean(&bench, "total_latency_ms"),
			metricP95(&bench, "total_latency_ms"),
			metricMean(&bench, "prepare_latency_ms"),
			metricMean(&bench, "proof_latency_ms"),
			metricMean(&bench, "time_to_submit_ms"),
			metricMean(&bench, "error_rate"),
		)
	}
	fmt.Fprintf(b, "\n해석:\n\n")
	fmt.Fprintf(b, "- Warm native smoke 기준 사용자 체감 latency를 flow별로 분해한 값이다.\n")
	fmt.Fprintf(b, "- Sample 수가 작으면 p95/p99 public claim에는 사용할 수 없다.\n\n")
}

func renderHumanSummaryPublicCapacityKR(b *strings.Builder, component humanSummaryComponent, ok bool) {
	if !ok {
		return
	}
	rep := component.Report
	fmt.Fprintf(b, "## Public Capacity 판정\n\n")
	fmt.Fprintf(b, "`public-capacity/latest.json`의 최종 판정은 `claim_eligible=%t`이다.\n\n", rep.ClaimProfile.Eligible)
	if len(rep.ClaimProfile.BlockingReasons) > 0 {
		fmt.Fprintf(b, "주요 blocker:\n\n")
		for _, reason := range rep.ClaimProfile.BlockingReasons {
			fmt.Fprintf(b, "- %s\n", reason)
		}
		fmt.Fprintf(b, "\n")
	}
	fmt.Fprintf(b, "해석:\n\n")
	fmt.Fprintf(b, "- Aggregate가 실패한 것은 benchmark 실행 실패가 아니다.\n")
	fmt.Fprintf(b, "- 현재 산출물이 공개 수치로 승격되기에는 evidence와 sample이 부족하다는 뜻이다.\n\n")
}

func renderHumanSummaryArtifactsKR(b *strings.Builder, components []humanSummaryComponent) {
	fmt.Fprintf(b, "## 산출물\n\n")
	fmt.Fprintf(b, "| Family | Markdown | JSON |\n")
	fmt.Fprintf(b, "| --- | --- | --- |\n")
	for _, component := range components {
		family := component.Report.ResultFamily
		if family == "" {
			family = "unknown"
		}
		mdPath := strings.TrimSuffix(component.Path, ".json") + ".md"
		if strings.HasSuffix(component.Path, "latest.json") {
			mdPath = filepath.Join(filepath.Dir(component.Path), "latest.md")
		}
		fmt.Fprintf(b, "| `%s` | `%s` | `%s` |\n", family, mdPath, component.Path)
	}
	fmt.Fprintf(b, "\n")
}

func renderHumanSummaryLimitationsKR(b *strings.Builder) {
	fmt.Fprintf(b, "## 현재 한계\n\n")
	fmt.Fprintf(b, "1. 이 문서는 자동 생성되지만, 입력이 되는 family별 `latest.json`들이 fresh하다는 전제에 의존한다.\n")
	fmt.Fprintf(b, "2. Public claim을 만들려면 smoke가 아니라 `public_claim` profile로 긴 시간 실행해야 한다.\n")
	fmt.Fprintf(b, "3. User latency와 chain TPS는 sample/evidence가 부족하면 공개 p95/p99 claim으로 사용할 수 없다.\n")
	fmt.Fprintf(b, "4. External proverd load가 특정 profile만 측정했다면 다른 profile의 capacity는 별도로 측정해야 한다.\n")
}

func renderMarkdown(rep report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Clairveil Privacy Benchmark Report\n\n")
	fmt.Fprintf(&b, "- generated_at: `%s`\n", rep.GeneratedAt)
	fmt.Fprintf(&b, "- commit: `%s`\n", rep.Commit)
	fmt.Fprintf(&b, "- dirty: `%t`\n", rep.Dirty)
	if rep.ResultFamily != "" {
		fmt.Fprintf(&b, "- result_family: `%s`\n", rep.ResultFamily)
	}
	if rep.RunStartedAt != "" || rep.RunEndedAt != "" {
		fmt.Fprintf(&b, "- run_window: `%s` to `%s`\n", rep.RunStartedAt, rep.RunEndedAt)
	}
	fmt.Fprintf(&b, "- run_profile: `%s`\n", rep.ClaimProfile.RunProfile)
	fmt.Fprintf(&b, "- claim_eligible: `%t`\n", rep.ClaimProfile.Eligible)
	fmt.Fprintf(&b, "- active_set_id: `%s`\n", rep.ActiveSetID)
	fmt.Fprintf(&b, "- go_version: `%s`\n", rep.GoVersion)
	fmt.Fprintf(&b, "- gnark_version: `%s`\n", rep.GnarkVersion)
	fmt.Fprintf(&b, "- gnark_crypto_version: `%s`\n", rep.GnarkCrypto)
	fmt.Fprintf(&b, "- platform: `%s/%s`\n", rep.OS, rep.Arch)
	if rep.CPU != "" {
		fmt.Fprintf(&b, "- cpu: `%s`\n", rep.CPU)
	}
	if rep.ManifestChecksum != "" {
		fmt.Fprintf(&b, "- manifest_sha256: `%s`\n", rep.ManifestChecksum)
	}
	fmt.Fprintf(&b, "- artifact_descriptors_complete: `%t`\n", rep.ArtifactSet.DescriptorComplete)
	fmt.Fprintf(&b, "- artifact_files_verified: `%t`\n", rep.ArtifactSet.ArtifactFilesVerified)
	if rep.FeeModel.FeeDenom != "" || rep.FeeModel.MinGasPrice != "" || rep.FeeModel.GasAdjustment != "" {
		fmt.Fprintf(&b, "- fee_model: denom=`%s`, min_gas_price=`%s`, gas_adjustment=`%s`\n", rep.FeeModel.FeeDenom, rep.FeeModel.MinGasPrice, rep.FeeModel.GasAdjustment)
	}
	if len(rep.ClaimProfile.BlockingReasons) > 0 {
		fmt.Fprintf(&b, "- claim_blocking_reasons: `%s`\n", strings.Join(rep.ClaimProfile.BlockingReasons, "; "))
	}
	if len(rep.SourceFiles) > 0 {
		fmt.Fprintf(&b, "- source_files: `%s`\n", strings.Join(rep.SourceFiles, "`, `"))
	}
	if len(rep.SourceFileIssues) > 0 {
		fmt.Fprintf(&b, "- source_file_issues: `%s`\n", strings.Join(rep.SourceFileIssues, "; "))
	}

	fmt.Fprintf(&b, "\n")
	if rep.Dirty {
		fmt.Fprintf(&b, "> Note: this benchmark was generated from a dirty worktree and should be treated as a development result.\n\n")
	}
	fmt.Fprintf(&b, "These numbers describe the measured benchmark scope only. Do not infer chain TPS from native proving or verification rows.\n\n")

	if hasClaimEvidence(rep.ClaimEvidence) || (len(rep.ClaimEvidenceByType) == 0 && (rep.ClaimProfile.RunProfile == "public_claim" || len(rep.ClaimProfile.ClaimTypes) > 0)) {
		fmt.Fprintf(&b, "## Claim Evidence\n\n")
		fmt.Fprintf(&b, "| Field | Value |\n")
		fmt.Fprintf(&b, "| --- | --- |\n")
		fmt.Fprintf(&b, "| claim types | `%s` |\n", strings.Join(rep.ClaimProfile.ClaimTypes, ","))
		fmt.Fprintf(&b, "| steady state seconds | `%d` |\n", rep.ClaimEvidence.SteadyStateSeconds)
		fmt.Fprintf(&b, "| load profile | `%s` |\n", rep.ClaimEvidence.LoadProfile)
		fmt.Fprintf(&b, "| preflight mode | `%s` |\n", rep.ClaimEvidence.PreflightMode)
		fmt.Fprintf(&b, "| auth enabled | `%s` |\n", rep.ClaimEvidence.AuthEnabled)
		fmt.Fprintf(&b, "| instance profile | `%s` |\n", rep.ClaimEvidence.InstanceProfile)
		fmt.Fprintf(&b, "| prover config file | `%s` |\n", rep.ClaimEvidence.ProverConfigFile)
		fmt.Fprintf(&b, "| prover config SHA-256 | `%s` |\n", rep.ClaimEvidence.ProverConfigSHA256)
		fmt.Fprintf(&b, "| chain config | `%s` |\n", rep.ClaimEvidence.ChainConfig)
		fmt.Fprintf(&b, "| chain config file | `%s` |\n", rep.ClaimEvidence.ChainConfigFile)
		fmt.Fprintf(&b, "| chain config SHA-256 | `%s` |\n", rep.ClaimEvidence.ChainConfigSHA256)
		fmt.Fprintf(&b, "| reserve invariant | `%s` |\n", rep.ClaimEvidence.ReserveInvariant)
		fmt.Fprintf(&b, "| latency p99 SLO ms | `%.3f` |\n", rep.ClaimEvidence.LatencyP99SLOMS)
		fmt.Fprintf(&b, "| inclusion p95 SLO ms | `%.3f` |\n", rep.ClaimEvidence.InclusionP95SLOMS)
		fmt.Fprintf(&b, "| RSS stable | `%s` |\n", rep.ClaimEvidence.RSSStable)
		fmt.Fprintf(&b, "| saturation profile | `%s` |\n", rep.ClaimEvidence.SaturationProfile)
		fmt.Fprintf(&b, "| saturation profile file | `%s` |\n", rep.ClaimEvidence.SaturationProfileFile)
		fmt.Fprintf(&b, "| saturation profile SHA-256 | `%s` |\n", rep.ClaimEvidence.SaturationProfileSHA256)
		fmt.Fprintf(&b, "| throughput window seconds | `%d` |\n", rep.ClaimEvidence.ThroughputWindowSeconds)
		fmt.Fprintf(&b, "| reserve snapshot before file | `%s` |\n", rep.ClaimEvidence.ReserveSnapshotBeforeFile)
		fmt.Fprintf(&b, "| reserve snapshot before SHA-256 | `%s` |\n", rep.ClaimEvidence.ReserveSnapshotBeforeSHA256)
		fmt.Fprintf(&b, "| reserve snapshot after file | `%s` |\n", rep.ClaimEvidence.ReserveSnapshotAfterFile)
		fmt.Fprintf(&b, "| reserve snapshot after SHA-256 | `%s` |\n", rep.ClaimEvidence.ReserveSnapshotAfterSHA256)
		fmt.Fprintf(&b, "| latency mode | `%s` |\n", rep.ClaimEvidence.LatencyMode)
		fmt.Fprintf(&b, "| cold/warm separated | `%s` |\n", rep.ClaimEvidence.ColdWarmSeparated)
		fmt.Fprintf(&b, "| browser matrix | `%s` |\n", rep.ClaimEvidence.BrowserMatrix)
		fmt.Fprintf(&b, "| browser adapter ready | `%s` |\n", rep.ClaimEvidence.BrowserAdapterReady)
		fmt.Fprintf(&b, "| browser adapter version | `%s` |\n", rep.ClaimEvidence.BrowserAdapterVersion)
		fmt.Fprintf(&b, "| browser adapter file | `%s` |\n", rep.ClaimEvidence.BrowserAdapterFile)
		fmt.Fprintf(&b, "| browser adapter SHA-256 | `%s` |\n", rep.ClaimEvidence.BrowserAdapterSHA256)
		fmt.Fprintf(&b, "| remote topology | `%s` |\n", rep.ClaimEvidence.RemoteTopology)
		fmt.Fprintf(&b, "| linked prover report file | `%s` |\n", rep.ClaimEvidence.LinkedProverReportFile)
		fmt.Fprintf(&b, "| linked prover report SHA-256 | `%s` |\n", rep.ClaimEvidence.LinkedProverReportSHA256)
		fmt.Fprintf(&b, "| machine profile | `%s` |\n", rep.Environment.MachineProfile)
		fmt.Fprintf(&b, "| cpu governor | `%s` |\n", rep.Environment.CPUGovernor)
		fmt.Fprintf(&b, "| memory GiB | `%s` |\n", rep.Environment.MemoryGiB)
		fmt.Fprintf(&b, "\n")
	}

	if len(rep.ClaimEvidenceByType) > 0 {
		fmt.Fprintf(&b, "## Claim Evidence By Type\n\n")
		fmt.Fprintf(&b, "| Claim | Load profile | Mode | Instance | Prover config SHA-256 | Chain config SHA-256 | Saturation SHA-256 | Linked prover report SHA-256 |\n")
		fmt.Fprintf(&b, "| --- | --- | --- | --- | --- | --- | --- | --- |\n")
		claimTypes := sortedMapKeysClaimEvidence(rep.ClaimEvidenceByType)
		for _, claimType := range claimTypes {
			evidence := rep.ClaimEvidenceByType[claimType]
			fmt.Fprintf(
				&b,
				"| `%s` | `%s` | `%s` | `%s` | `%s` | `%s` | `%s` | `%s` |\n",
				claimType,
				evidence.LoadProfile,
				evidence.LatencyMode,
				evidence.InstanceProfile,
				evidence.ProverConfigSHA256,
				evidence.ChainConfigSHA256,
				evidence.SaturationProfileSHA256,
				evidence.LinkedProverReportSHA256,
			)
		}
		fmt.Fprintf(&b, "\n")
	}

	if len(rep.SourceFileSHA256) > 0 {
		fmt.Fprintf(&b, "## Source Files\n\n")
		fmt.Fprintf(&b, "| File | SHA-256 |\n")
		fmt.Fprintf(&b, "| --- | --- |\n")
		for _, path := range sortedMapKeys(rep.SourceFileSHA256) {
			fmt.Fprintf(&b, "| `%s` | `%s` |\n", path, rep.SourceFileSHA256[path])
		}
		fmt.Fprintf(&b, "\n")
	}

	if len(rep.ComponentReports) > 0 {
		fmt.Fprintf(&b, "## Component Reports\n\n")
		fmt.Fprintf(&b, "| Report | Family | Claims | Eligible | Commit | Active set | Manifest SHA-256 |\n")
		fmt.Fprintf(&b, "| --- | --- | --- | ---: | --- | --- | --- |\n")
		for _, component := range rep.ComponentReports {
			fmt.Fprintf(
				&b,
				"| `%s` | `%s` | `%s` | `%t` | `%s` | `%s` | `%s` |\n",
				component.Path,
				component.ResultFamily,
				strings.Join(component.ClaimTypes, ","),
				component.Eligible,
				component.Commit,
				component.ActiveSetID,
				component.ManifestSHA256,
			)
		}
		fmt.Fprintf(&b, "\n")
	}

	if blocked := blockedComponentReports(rep.ComponentReports); len(blocked) > 0 {
		fmt.Fprintf(&b, "## Blocked Components\n\n")
		fmt.Fprintf(&b, "The component reports below are not public capacity evidence. Treat their metric rows as diagnostic output, not as zero-capacity claims.\n\n")
		fmt.Fprintf(&b, "| Report | Claims | Blocking reasons |\n")
		fmt.Fprintf(&b, "| --- | --- | --- |\n")
		for _, component := range blocked {
			fmt.Fprintf(
				&b,
				"| `%s` | `%s` | `%s` |\n",
				component.Path,
				strings.Join(component.ClaimTypes, ","),
				strings.Join(component.BlockingReasons, "; "),
			)
		}
		fmt.Fprintf(&b, "\n")
	}

	if len(rep.ArtifactSet.DescriptorIssues) > 0 || len(rep.ArtifactSet.ArtifactFileIssues) > 0 {
		fmt.Fprintf(&b, "## Artifact Issues\n\n")
		for _, issue := range append(rep.ArtifactSet.DescriptorIssues, rep.ArtifactSet.ArtifactFileIssues...) {
			fmt.Fprintf(&b, "- %s\n", issue)
		}
		fmt.Fprintf(&b, "\n")
	}

	if hasBenchmarkBucketMetadata(rep.Benchmarks) {
		fmt.Fprintf(&b, "## Benchmark Buckets\n\n")
		fmt.Fprintf(&b, "| Benchmark | Claim | Load profile | Flow profile | Latency mode | Cold/warm | Route | Concurrency | Warmup seconds | Duration seconds | Target tx/sec |\n")
		fmt.Fprintf(&b, "| --- | --- | --- | --- | --- | --- | --- | ---: | ---: | ---: | ---: |\n")
		for _, bench := range rep.Benchmarks {
			if !benchmarkHasBucketMetadata(bench) {
				continue
			}
			fmt.Fprintf(
				&b,
				"| `%s` | `%s` | `%s` | `%s` | `%s` | `%s` | `%s` | %d | %d | %d | %.6g |\n",
				bench.Name,
				bench.ClaimType,
				bench.LoadProfile,
				bench.FlowProfile,
				bench.LatencyMode,
				bench.ColdWarm,
				bench.Route,
				bench.Concurrency,
				bench.WarmupSeconds,
				bench.DurationSeconds,
				bench.TargetTxPerSec,
			)
		}
		fmt.Fprintf(&b, "\n")
	}

	goRows := goBenchmarkRows(rep.Benchmarks)
	if len(goRows) > 0 {
		fmt.Fprintf(&b, "| Benchmark | Kind | Samples | Mean | p50 | p95 | p99 | ops/sec | B/op | allocs/op |\n")
		fmt.Fprintf(&b, "| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
		for _, bench := range goRows {
			fmt.Fprintf(
				&b,
				"| `%s` | `%s` | %d | %s | %s | %s | %s | %.2f | %.0f | %.0f |\n",
				bench.Name,
				bench.MetricKind,
				bench.Samples,
				formatDurationNS(bench.NSOpMean),
				formatDurationNS(bench.NSOpP50),
				formatDurationNS(bench.NSOpP95),
				formatDurationNS(bench.NSOpP99),
				bench.OpsPerSec,
				bench.BytesOp,
				bench.AllocsOp,
			)
		}
		fmt.Fprintf(&b, "\n")
	}

	customMetrics := customMetricRows(rep.Benchmarks)
	if len(customMetrics) > 0 {
		fmt.Fprintf(&b, "## Custom Metrics\n\n")
		fmt.Fprintf(&b, "| Benchmark | Metric | Mean | p50 | p95 | p99 | min | max |\n")
		fmt.Fprintf(&b, "| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |\n")
		for _, row := range customMetrics {
			metric := row.Metric
			fmt.Fprintf(
				&b,
				"| `%s` | `%s` | %.6g | %.6g | %.6g | %.6g | %.6g | %.6g |\n",
				row.BenchmarkName,
				row.MetricName,
				metric.Mean,
				metric.P50,
				metric.P95,
				metric.P99,
				metric.Min,
				metric.Max,
			)
		}
		fmt.Fprintf(&b, "\n")
	}

	if len(rep.Fees) > 0 {
		fmt.Fprintf(&b, "## Expected Fees\n\n")
		fmt.Fprintf(&b, "Fee estimates are derived from observed `gas_used` only. They do not include prover infrastructure cost.\n\n")
		fmt.Fprintf(&b, "| Tx type | Samples | Failed | Gas mean | Gas p50 | Gas p95 | Gas max | Estimated fee p50 | Estimated fee p95 |\n")
		fmt.Fprintf(&b, "| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
		for _, fee := range rep.Fees {
			fmt.Fprintf(
				&b,
				"| `%s` | %d | %d | %d | %d | %d | %d | `%s` | `%s` |\n",
				fee.TxType,
				fee.Samples,
				fee.FailedSamples,
				fee.GasUsedMean,
				fee.GasUsedP50,
				fee.GasUsedP95,
				fee.GasUsedMax,
				fee.EstimatedFeeP50,
				fee.EstimatedFeeP95,
			)
		}
	}
	return b.String()
}

func hasBenchmarkBucketMetadata(benchmarks []benchmarkSummary) bool {
	for _, bench := range benchmarks {
		if benchmarkHasBucketMetadata(bench) {
			return true
		}
	}
	return false
}

func benchmarkHasBucketMetadata(bench benchmarkSummary) bool {
	return bench.ClaimType != "" ||
		bench.LoadProfile != "" ||
		bench.FlowProfile != "" ||
		bench.LatencyMode != "" ||
		bench.ColdWarm != "" ||
		bench.Route != "" ||
		bench.Concurrency != 0 ||
		bench.WarmupSeconds != 0 ||
		bench.DurationSeconds != 0 ||
		bench.TargetTxPerSec != 0
}

func blockedComponentReports(components []componentReport) []componentReport {
	var blocked []componentReport
	for _, component := range components {
		if component.Eligible {
			continue
		}
		blocked = append(blocked, component)
	}
	return blocked
}

func readTxMetrics(path string) ([]txMetric, error) {
	bz, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var direct []txMetric
	if err := json.Unmarshal(bz, &direct); err == nil {
		return validateTxMetrics(direct)
	}

	var envelope txMetricEnvelope
	if err := json.Unmarshal(bz, &envelope); err != nil {
		return nil, err
	}
	return validateTxMetrics(envelope.Transactions)
}

func validateTxMetrics(metrics []txMetric) ([]txMetric, error) {
	if len(metrics) == 0 {
		return nil, fmt.Errorf("tx metrics are empty")
	}
	return metrics, nil
}

func readBenchmarkSummaries(path string) ([]benchmarkSummary, error) {
	bz, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var direct []benchmarkSummary
	if err := json.Unmarshal(bz, &direct); err == nil {
		if err := validateBenchmarkSummaryJSON(bz); err != nil {
			return nil, err
		}
		return validateBenchmarkSummaries(direct)
	}

	var envelope benchmarkSummaryEnvelope
	if err := json.Unmarshal(bz, &envelope); err != nil {
		return nil, err
	}
	var rawEnvelope struct {
		Benchmarks json.RawMessage `json:"benchmarks"`
	}
	if err := json.Unmarshal(bz, &rawEnvelope); err != nil {
		return nil, err
	}
	if len(rawEnvelope.Benchmarks) == 0 {
		return nil, fmt.Errorf("benchmark summaries envelope is missing benchmarks")
	}
	if err := validateBenchmarkSummaryJSON(rawEnvelope.Benchmarks); err != nil {
		return nil, err
	}
	return validateBenchmarkSummaries(envelope.Benchmarks)
}

func validateBenchmarkSummaries(summaries []benchmarkSummary) ([]benchmarkSummary, error) {
	if len(summaries) == 0 {
		return nil, fmt.Errorf("benchmark summaries are empty")
	}
	for i, summary := range summaries {
		if strings.TrimSpace(summary.Name) == "" {
			return nil, fmt.Errorf("benchmark summary %d is missing name", i)
		}
		if strings.TrimSpace(summary.MetricKind) == "" {
			return nil, fmt.Errorf("benchmark summary %q is missing metric_kind", summary.Name)
		}
		if len(summary.Metrics) == 0 {
			return nil, fmt.Errorf("benchmark summary %q is missing metric_summaries", summary.Name)
		}
		if summary.Samples <= 0 {
			return nil, fmt.Errorf("benchmark summary %q has non-positive samples", summary.Name)
		}
	}
	return summaries, nil
}

func validateBenchmarkSummaryJSON(raw []byte) error {
	var summaries []struct {
		Name    string                                `json:"name"`
		Metrics map[string]map[string]json.RawMessage `json:"metric_summaries"`
	}
	if err := json.Unmarshal(raw, &summaries); err != nil {
		return err
	}
	requiredStats := []string{"mean", "p50", "p95", "p99", "min", "max"}
	for i, summary := range summaries {
		name := strings.TrimSpace(summary.Name)
		if name == "" {
			name = fmt.Sprintf("#%d", i)
		}
		for metricName, stats := range summary.Metrics {
			for _, stat := range requiredStats {
				rawValue, ok := stats[stat]
				if !ok {
					return fmt.Errorf("benchmark summary %q metric %q is missing %s", name, metricName, stat)
				}
				var numeric *float64
				if err := json.Unmarshal(rawValue, &numeric); err != nil {
					return fmt.Errorf("benchmark summary %q metric %q has non-numeric %s", name, metricName, stat)
				}
				if numeric == nil || math.IsNaN(*numeric) || math.IsInf(*numeric, 0) {
					return fmt.Errorf("benchmark summary %q metric %q has non-numeric %s", name, metricName, stat)
				}
			}
		}
	}
	return nil
}

func hashSourceFiles(paths []string) (map[string]string, []string) {
	if len(paths) == 0 {
		return nil, nil
	}
	hashes := make(map[string]string, len(paths))
	var issues []string
	for _, path := range paths {
		cleanPath := strings.TrimSpace(path)
		if cleanPath == "" {
			continue
		}
		sum, err := fileSHA256(cleanPath)
		if err != nil {
			issues = append(issues, fmt.Sprintf("cannot hash %s: %v", cleanPath, err))
			continue
		}
		hashes[cleanPath] = sum
	}
	sort.Strings(issues)
	return hashes, issues
}

func sourceMetadata(commitOverride string, dirtyOverride string) (string, bool, error) {
	commit := strings.TrimSpace(commitOverride)
	if commit == "" {
		commit = commandOutput("git", "rev-parse", "HEAD")
	}

	dirty := sourceWorktreeDirty()
	if strings.TrimSpace(dirtyOverride) != "" {
		parsed, err := strconv.ParseBool(strings.TrimSpace(dirtyOverride))
		if err != nil {
			return "", false, fmt.Errorf("dirty override must be true or false: %w", err)
		}
		dirty = parsed
	}

	return commit, dirty, nil
}

func sourceWorktreeDirty() bool {
	return sourceStatusDirty(commandOutput("git", "status", "--short", "--untracked-files=all", "--", "."))
}

func sourceStatusDirty(status string) bool {
	for _, line := range strings.Split(status, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if generatedBenchmarkStatusLine(line) {
			continue
		}
		return true
	}
	return false
}

func generatedBenchmarkStatusLine(line string) bool {
	if len(line) < 4 {
		return false
	}
	if line[:2] != "??" {
		return false
	}
	path := strings.TrimSpace(line[3:])
	for _, prefix := range generatedBenchmarkPrefixes() {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	for _, name := range generatedRootBuildArtifacts() {
		if path == name {
			return true
		}
	}
	return false
}

func generatedBenchmarkPrefixes() []string {
	return []string{
		"benchmarks/privacy-circuits/",
		"benchmarks/privacy-proverd/",
		"benchmarks/privacy-localnet/",
		"benchmarks/privacy-proverd-load/",
		"benchmarks/privacy-localnet-tps/",
		"benchmarks/privacy-user-latency/",
		"benchmarks/public-capacity/",
	}
}

func generatedRootBuildArtifacts() []string {
	return []string{
		"clairveild",
		"clairveil-setup",
		"clairveil-verify",
		"clairveil-proverd",
		"clairveil-benchreport",
		"clairveil-proverload",
		"clairveil-localnetload",
		"clairveil-userlatency",
	}
}

func parseRunProfile(value string) (string, error) {
	switch strings.TrimSpace(value) {
	case "", "smoke":
		return "smoke", nil
	case "reference", "production_like", "public_claim":
		return strings.TrimSpace(value), nil
	default:
		return "", fmt.Errorf("invalid run profile %q", value)
	}
}

func parseCSV(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func reportSourceFiles(explicitFiles string, inputPaths ...string) []string {
	files := parseCSV(explicitFiles)
	for _, path := range inputPaths {
		path = strings.TrimSpace(path)
		if path != "" {
			files = append(files, path)
		}
	}
	return uniqueStrings(files)
}

func resolveConfigSHA256(name, explicitSHA256, configFile string) (string, error) {
	explicitSHA256 = strings.ToLower(strings.TrimSpace(explicitSHA256))
	configFile = strings.TrimSpace(configFile)
	if explicitSHA256 != "" && !isSHA256Hex(explicitSHA256) {
		return "", fmt.Errorf("%s config sha256 must be 64 lowercase or uppercase hex characters", name)
	}
	if configFile == "" {
		return explicitSHA256, nil
	}
	computedSHA256, err := fileSHA256(configFile)
	if err != nil {
		return "", fmt.Errorf("hash %s config file %s: %w", name, configFile, err)
	}
	if explicitSHA256 != "" && !strings.EqualFold(explicitSHA256, computedSHA256) {
		return "", fmt.Errorf("%s config sha256 %s does not match %s", name, explicitSHA256, computedSHA256)
	}
	return computedSHA256, nil
}

func isSHA256Hex(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func evaluateClaimProfile(rep report) claimProfile {
	profile := rep.ClaimProfile
	reasons := append([]string(nil), profile.BlockingReasons...)
	switch profile.RunProfile {
	case "smoke":
		reasons = append(reasons, "run_profile is smoke")
	case "public_claim":
	default:
		reasons = append(reasons, "run_profile is not public_claim")
	}
	if rep.Dirty {
		reasons = append(reasons, "source worktree is dirty")
	}
	if rep.ActiveSetID == "" {
		reasons = append(reasons, "active_set_id is missing")
	}
	if rep.ArtifactSet.ManifestSHA256 == "" {
		reasons = append(reasons, "artifact manifest checksum is missing")
	} else if !isSHA256Hex(rep.ArtifactSet.ManifestSHA256) {
		reasons = append(reasons, "artifact manifest checksum must be 64hex")
	}
	if rep.ManifestChecksum != "" && rep.ArtifactSet.ManifestSHA256 != "" && !strings.EqualFold(rep.ManifestChecksum, rep.ArtifactSet.ManifestSHA256) {
		reasons = append(reasons, "artifact manifest checksum does not match top-level manifest_sha256")
	}
	if rep.ArtifactSet.ManifestActiveSetID != "" && rep.ArtifactSet.ManifestActiveSetID != rep.ActiveSetID {
		reasons = append(reasons, fmt.Sprintf("artifact manifest active_set_id %q does not match report active_set_id %q", rep.ArtifactSet.ManifestActiveSetID, rep.ActiveSetID))
	}
	if rep.ArtifactSet.ActiveSetID != "" && rep.ArtifactSet.ActiveSetID != rep.ActiveSetID {
		reasons = append(reasons, fmt.Sprintf("artifact set active_set_id %q does not match report active_set_id %q", rep.ArtifactSet.ActiveSetID, rep.ActiveSetID))
	}
	if profile.RunProfile == "public_claim" {
		if rep.ResultFamily == "" {
			reasons = append(reasons, "result_family is required for public_claim")
		}
		if len(rep.SourceFiles) == 0 {
			reasons = append(reasons, "source_files is required for public_claim")
		}
		if len(rep.SourceFileIssues) > 0 {
			reasons = append(reasons, "source files are not verified: "+strings.Join(rep.SourceFileIssues, "; "))
		}
		if issues := sourceFileHashIssues(rep); len(issues) > 0 {
			reasons = append(reasons, "source file hashes invalid: "+strings.Join(issues, ", "))
		}
		if issues := componentReportIssues(rep); len(issues) > 0 {
			reasons = append(reasons, "component reports invalid: "+strings.Join(issues, ", "))
		}
		if missing := runWindowIssues(rep); len(missing) > 0 {
			reasons = append(reasons, missing...)
		}
		if !rep.ArtifactSet.DescriptorComplete {
			if len(rep.ArtifactSet.DescriptorIssues) == 0 {
				reasons = append(reasons, "artifact descriptor set is incomplete")
			} else {
				reasons = append(reasons, "artifact descriptor set is incomplete: "+strings.Join(rep.ArtifactSet.DescriptorIssues, "; "))
			}
		}
		if !rep.ArtifactSet.ArtifactFilesVerified {
			if len(rep.ArtifactSet.ArtifactFileIssues) == 0 {
				reasons = append(reasons, "artifact files are not verified")
			} else {
				reasons = append(reasons, "artifact files are not verified: "+strings.Join(rep.ArtifactSet.ArtifactFileIssues, "; "))
			}
		}
		if len(profile.ClaimTypes) == 0 {
			reasons = append(reasons, "claim_types is required for public_claim")
		}
		if rep.ResultFamily == "public-capacity" && len(profile.ClaimTypes) > 1 {
			var missingPerClaimEvidence []string
			for _, claimType := range profile.ClaimTypes {
				claimType = strings.TrimSpace(claimType)
				evidence, ok := rep.ClaimEvidenceByType[claimType]
				if !ok || !hasClaimEvidence(evidence) {
					missingPerClaimEvidence = append(missingPerClaimEvidence, claimType)
				}
			}
			if len(missingPerClaimEvidence) > 0 {
				reasons = append(reasons, "public-capacity multi-claim reports require per-claim evidence for "+strings.Join(missingPerClaimEvidence, ","))
			}
		}
		if rep.Environment.MachineProfile == "" {
			reasons = append(reasons, "machine_profile is required for public_claim")
		}
		if rep.Environment.CPUGovernor == "" {
			reasons = append(reasons, "cpu_governor is required for public_claim")
		}
		if rep.Environment.MemoryGiB == "" {
			reasons = append(reasons, "memory_gib is required for public_claim")
		}
		if issues := benchmarkSampleIssues(rep); len(issues) > 0 {
			reasons = append(reasons, "benchmark samples invalid: "+strings.Join(issues, ", "))
		}
		for _, claimType := range profile.ClaimTypes {
			switch claimType {
			case "chain_tps", "prover_rps", "user_latency":
				if !publicClaimFamilyAllowed(rep.ResultFamily, claimType) {
					reasons = append(reasons, fmt.Sprintf("%s claim requires result_family %s", claimType, strings.Join(allowedResultFamilies(claimType), "|")))
				}
				if issues := claimRowMetadataIssues(rep, claimType); len(issues) > 0 {
					reasons = append(reasons, fmt.Sprintf("%s benchmark rows invalid: %s", claimType, strings.Join(issues, ", ")))
				}
				if issues := claimSweepIssues(rep, claimType); len(issues) > 0 {
					reasons = append(reasons, fmt.Sprintf("%s sweep invalid: %s", claimType, strings.Join(issues, ", ")))
				}
				if missing := missingClaimMetrics(rep, claimType); len(missing) > 0 {
					reasons = append(reasons, fmt.Sprintf("%s metrics missing: %s", claimType, strings.Join(missing, ", ")))
				}
				if incomplete := incompleteClaimMetricRows(rep, claimType); len(incomplete) > 0 {
					reasons = append(reasons, fmt.Sprintf("%s metric rows incomplete: %s", claimType, strings.Join(incomplete, ", ")))
				}
				if invalid := invalidClaimMetrics(rep, claimType); len(invalid) > 0 {
					reasons = append(reasons, fmt.Sprintf("%s metrics invalid: %s", claimType, strings.Join(invalid, ", ")))
				}
				if missing := missingClaimEvidence(rep, claimType); len(missing) > 0 {
					reasons = append(reasons, fmt.Sprintf("%s evidence missing: %s", claimType, strings.Join(missing, ", ")))
				}
				if issues := evidenceSourceIssues(rep, claimType); len(issues) > 0 {
					reasons = append(reasons, fmt.Sprintf("%s source evidence invalid: %s", claimType, strings.Join(issues, ", ")))
				}
			default:
				reasons = append(reasons, fmt.Sprintf("unsupported claim_type %q", claimType))
			}
		}
	}
	profile.BlockingReasons = uniqueStrings(reasons)
	profile.Eligible = len(profile.BlockingReasons) == 0 && profile.RunProfile == "public_claim"
	return profile
}

func claimRowMetadataIssues(rep report, claimType string) []string {
	evidence := evidenceForClaim(rep, claimType)
	rows, tagged := claimBenchmarkRows(rep, claimType)
	if !tagged {
		return []string{fmt.Sprintf("at least one benchmark summary must set claim_type=%s", claimType)}
	}
	var issues []string
	for _, bench := range rows {
		name := benchmarkDisplayName(bench)
		if bench.DurationSeconds > 0 && evidence.SteadyStateSeconds > 0 && bench.DurationSeconds < evidence.SteadyStateSeconds {
			issues = append(issues, fmt.Sprintf("%s duration_seconds must be >= steady_state_seconds", name))
		}
		switch claimType {
		case "prover_rps":
			if strings.TrimSpace(bench.LoadProfile) == "" {
				issues = append(issues, fmt.Sprintf("%s load_profile is required", name))
			}
			if strings.TrimSpace(bench.Route) == "" {
				issues = append(issues, fmt.Sprintf("%s route is required", name))
			}
			if bench.Concurrency <= 0 {
				issues = append(issues, fmt.Sprintf("%s concurrency must be positive", name))
			}
			if bench.DurationSeconds <= 0 {
				issues = append(issues, fmt.Sprintf("%s duration_seconds must be positive", name))
			}
		case "chain_tps":
			if strings.TrimSpace(bench.LoadProfile) == "" {
				issues = append(issues, fmt.Sprintf("%s load_profile is required", name))
			}
			if bench.DurationSeconds <= 0 {
				issues = append(issues, fmt.Sprintf("%s duration_seconds must be positive", name))
			}
			if bench.TargetTxPerSec <= 0 {
				issues = append(issues, fmt.Sprintf("%s target_tx_per_sec must be positive", name))
			}
		case "user_latency":
			if strings.TrimSpace(bench.FlowProfile) == "" {
				issues = append(issues, fmt.Sprintf("%s flow_profile is required", name))
			}
			if strings.TrimSpace(bench.LatencyMode) == "" {
				issues = append(issues, fmt.Sprintf("%s latency_mode is required", name))
			} else if !validLatencyMode(bench.LatencyMode) {
				issues = append(issues, fmt.Sprintf("%s latency_mode must be native|remote|browser", name))
			}
			if evidence.LatencyMode != "" && bench.LatencyMode != "" && bench.LatencyMode != evidence.LatencyMode {
				issues = append(issues, fmt.Sprintf("%s latency_mode %q does not match claim_evidence latency_mode %q", name, bench.LatencyMode, evidence.LatencyMode))
			}
			if strings.TrimSpace(bench.ColdWarm) == "" {
				issues = append(issues, fmt.Sprintf("%s cold_warm is required", name))
			} else if bench.ColdWarm != "cold" && bench.ColdWarm != "warm" {
				issues = append(issues, fmt.Sprintf("%s cold_warm must be cold|warm", name))
			}
		}
	}
	return uniqueStrings(issues)
}

func claimSweepIssues(rep report, claimType string) []string {
	rows, tagged := claimBenchmarkRows(rep, claimType)
	if !tagged {
		return nil
	}
	switch claimType {
	case "prover_rps":
		if countUniquePositiveConcurrency(rows) < 2 {
			return []string{"at least two concurrency buckets are required"}
		}
	case "chain_tps":
		if countUniquePositiveTargetTPS(rows) < 2 {
			return []string{"at least two target_tx_per_sec buckets are required"}
		}
	}
	return nil
}

func countUniquePositiveConcurrency(rows []benchmarkSummary) int {
	seen := make(map[int]struct{})
	for _, row := range rows {
		if row.Concurrency <= 0 {
			continue
		}
		seen[row.Concurrency] = struct{}{}
	}
	return len(seen)
}

func countUniquePositiveTargetTPS(rows []benchmarkSummary) int {
	seen := make(map[string]struct{})
	for _, row := range rows {
		if row.TargetTxPerSec <= 0 {
			continue
		}
		seen[strconv.FormatFloat(row.TargetTxPerSec, 'g', -1, 64)] = struct{}{}
	}
	return len(seen)
}

func claimBenchmarkRows(rep report, claimType string) ([]benchmarkSummary, bool) {
	var tagged []benchmarkSummary
	for _, bench := range rep.Benchmarks {
		if strings.TrimSpace(bench.ClaimType) == claimType {
			tagged = append(tagged, bench)
		}
	}
	if len(tagged) > 0 {
		return tagged, true
	}
	return rep.Benchmarks, false
}

func benchmarkDisplayName(bench benchmarkSummary) string {
	name := strings.TrimSpace(bench.Name)
	if name == "" {
		return "<unnamed>"
	}
	return name
}

func validLatencyMode(value string) bool {
	switch strings.TrimSpace(value) {
	case "native", "remote", "browser":
		return true
	default:
		return false
	}
}

func evidenceForClaim(rep report, claimType string) claimEvidence {
	claimType = strings.TrimSpace(claimType)
	if evidence, ok := rep.ClaimEvidenceByType[claimType]; ok && hasClaimEvidence(evidence) {
		return evidence
	}
	return rep.ClaimEvidence
}

func missingClaimMetrics(rep report, claimType string) []string {
	rows, _ := claimBenchmarkRows(rep, claimType)
	switch claimType {
	case "prover_rps":
		return missingMetricGroups(
			rows,
			[]string{"proofs/sec", "requests/sec"},
			[]string{"latency_ms", "proof_latency_ms", "roundtrip_latency_ms"},
			[]string{"errors/op", "error_rate"},
			[]string{"cpu_percent"},
			[]string{"rss_bytes", "max_rss_bytes"},
		)
	case "chain_tps":
		return missingMetricGroups(
			rows,
			[]string{"tx/sec", "tps", "successful_tx/sec"},
			[]string{"inclusion_latency_ms"},
			[]string{"failed_tx_rate"},
		)
	case "user_latency":
		return missingMetricGroups(
			rows,
			[]string{"prepare_latency_ms"},
			[]string{"proof_latency_ms"},
			[]string{"time_to_submit_ms", "submit_latency_ms"},
			[]string{"total_latency_ms", "submit_ready_ms"},
			[]string{"error_rate"},
			[]string{"timeout_rate", "cancel_rate"},
		)
	default:
		return nil
	}
}

func incompleteClaimMetricRows(rep report, claimType string) []string {
	var incomplete []string
	rows, tagged := claimBenchmarkRows(rep, claimType)
	if !tagged {
		return nil
	}
	for _, bench := range rows {
		name := benchmarkDisplayName(bench)
		switch claimType {
		case "prover_rps":
			if missing := missingMetricGroupsInBenchmark(
				bench,
				[]string{"proofs/sec", "requests/sec"},
				[]string{"latency_ms", "proof_latency_ms", "roundtrip_latency_ms"},
				[]string{"errors/op", "error_rate"},
				[]string{"cpu_percent"},
				[]string{"rss_bytes", "max_rss_bytes"},
			); len(missing) > 0 {
				incomplete = append(incomplete, fmt.Sprintf("%s missing %s", name, strings.Join(missing, ", ")))
			}
		case "chain_tps":
			if missing := missingMetricGroupsInBenchmark(
				bench,
				[]string{"tx/sec", "tps", "successful_tx/sec"},
				[]string{"inclusion_latency_ms"},
				[]string{"failed_tx_rate"},
			); len(missing) > 0 {
				incomplete = append(incomplete, fmt.Sprintf("%s missing %s", name, strings.Join(missing, ", ")))
			}
		case "user_latency":
			if missing := missingMetricGroupsInBenchmark(
				bench,
				[]string{"prepare_latency_ms"},
				[]string{"proof_latency_ms"},
				[]string{"time_to_submit_ms", "submit_latency_ms"},
				[]string{"total_latency_ms", "submit_ready_ms"},
				[]string{"error_rate"},
				[]string{"timeout_rate", "cancel_rate"},
			); len(missing) > 0 {
				incomplete = append(incomplete, fmt.Sprintf("%s missing %s", name, strings.Join(missing, ", ")))
			}
		}
	}
	return incomplete
}

func missingMetricGroupsInBenchmark(bench benchmarkSummary, requiredAlternatives ...[]string) []string {
	missing := make([]string, 0)
	for _, alternatives := range requiredAlternatives {
		if !benchmarkHasMetric(bench, alternatives...) {
			missing = append(missing, strings.Join(alternatives, "|"))
		}
	}
	return missing
}

func missingClaimEvidence(rep report, claimType string) []string {
	evidence := evidenceForClaim(rep, claimType)
	var missing []string
	switch claimType {
	case "prover_rps":
		if evidence.SteadyStateSeconds < 600 {
			missing = append(missing, "steady_state_seconds>=600")
		}
		if evidence.LoadProfile == "" {
			missing = append(missing, "load_profile")
		}
		if evidence.InstanceProfile == "" {
			missing = append(missing, "instance_profile")
		}
		missing = appendProverConfigEvidence(missing, evidence)
		if evidence.PreflightMode != "strict" {
			missing = append(missing, "preflight_mode=strict")
		}
		if !stringBoolIsTrue(evidence.AuthEnabled) {
			missing = append(missing, "auth_enabled=true")
		}
		if !isPositiveFinite(evidence.LatencyP99SLOMS) {
			missing = append(missing, "latency_p99_slo_ms")
		}
		if !stringBoolIsTrue(evidence.RSSStable) {
			missing = append(missing, "rss_stable=true")
		}
		if evidence.SaturationProfile == "" {
			missing = append(missing, "saturation_profile")
		}
		missing = appendSaturationProfileEvidence(missing, evidence)
	case "chain_tps":
		if evidence.SteadyStateSeconds < 600 {
			missing = append(missing, "steady_state_seconds>=600")
		}
		if evidence.LoadProfile == "" {
			missing = append(missing, "load_profile")
		}
		if evidence.ChainConfig == "" {
			missing = append(missing, "chain_config")
		}
		missing = appendChainConfigFileEvidence(missing, evidence)
		if !stringBoolIsTrue(evidence.ReserveInvariant) {
			missing = append(missing, "reserve_invariant=true")
		}
		if !isPositiveFinite(evidence.InclusionP95SLOMS) {
			missing = append(missing, "inclusion_p95_slo_ms")
		}
		if evidence.ThroughputWindowSeconds <= 0 {
			missing = append(missing, "throughput_window_seconds")
		}
		if evidence.SaturationProfile == "" {
			missing = append(missing, "saturation_profile")
		}
		missing = appendSaturationProfileEvidence(missing, evidence)
		missing = appendReserveSnapshotEvidence(missing, evidence)
	case "user_latency":
		if evidence.LoadProfile == "" {
			missing = append(missing, "load_profile")
		}
		if evidence.LatencyMode == "" {
			missing = append(missing, "latency_mode")
		}
		if !stringBoolIsTrue(evidence.ColdWarmSeparated) {
			missing = append(missing, "cold_warm_separated=true")
		}
		if !isPositiveFinite(evidence.LatencyP99SLOMS) {
			missing = append(missing, "latency_p99_slo_ms")
		}
		switch evidence.LatencyMode {
		case "browser":
			if evidence.BrowserMatrix == "" {
				missing = append(missing, "browser_matrix")
			}
			missing = appendBrowserAdapterEvidence(missing, evidence)
		case "remote":
			if evidence.RemoteTopology == "" {
				missing = append(missing, "remote_topology")
			}
			if evidence.InstanceProfile == "" {
				missing = append(missing, "instance_profile")
			}
			missing = appendProverConfigEvidence(missing, evidence)
			missing = appendLinkedProverReportEvidence(missing, evidence)
		case "native":
		default:
			missing = append(missing, "latency_mode=native|remote|browser")
		}
		if userLatencyIncludesInclusion(rep) {
			if evidence.ChainConfig == "" {
				missing = append(missing, "chain_config")
			}
			missing = appendChainConfigFileEvidence(missing, evidence)
			if !isPositiveFinite(evidence.InclusionP95SLOMS) {
				missing = append(missing, "inclusion_p95_slo_ms")
			}
		}
	}
	return missing
}

func appendSaturationProfileEvidence(missing []string, evidence claimEvidence) []string {
	if evidence.SaturationProfileFile == "" {
		missing = append(missing, "saturation_profile_file")
	}
	if evidence.SaturationProfileSHA256 == "" {
		missing = append(missing, "saturation_profile_sha256")
	} else if !isSHA256Hex(evidence.SaturationProfileSHA256) {
		missing = append(missing, "saturation_profile_sha256=64hex")
	}
	return missing
}

func appendReserveSnapshotEvidence(missing []string, evidence claimEvidence) []string {
	if evidence.ReserveSnapshotBeforeFile == "" {
		missing = append(missing, "reserve_snapshot_before_file")
	}
	if evidence.ReserveSnapshotBeforeSHA256 == "" {
		missing = append(missing, "reserve_snapshot_before_sha256")
	} else if !isSHA256Hex(evidence.ReserveSnapshotBeforeSHA256) {
		missing = append(missing, "reserve_snapshot_before_sha256=64hex")
	}
	if evidence.ReserveSnapshotAfterFile == "" {
		missing = append(missing, "reserve_snapshot_after_file")
	}
	if evidence.ReserveSnapshotAfterSHA256 == "" {
		missing = append(missing, "reserve_snapshot_after_sha256")
	} else if !isSHA256Hex(evidence.ReserveSnapshotAfterSHA256) {
		missing = append(missing, "reserve_snapshot_after_sha256=64hex")
	}
	return missing
}

func appendProverConfigEvidence(missing []string, evidence claimEvidence) []string {
	if evidence.ProverConfigFile == "" {
		missing = append(missing, "prover_config_file")
	}
	if evidence.ProverConfigSHA256 == "" {
		missing = append(missing, "prover_config_sha256")
	} else if !isSHA256Hex(evidence.ProverConfigSHA256) {
		missing = append(missing, "prover_config_sha256=64hex")
	}
	return missing
}

func appendChainConfigFileEvidence(missing []string, evidence claimEvidence) []string {
	if evidence.ChainConfigFile == "" {
		missing = append(missing, "chain_config_file")
	}
	if evidence.ChainConfigSHA256 == "" {
		missing = append(missing, "chain_config_sha256")
	} else if !isSHA256Hex(evidence.ChainConfigSHA256) {
		missing = append(missing, "chain_config_sha256=64hex")
	}
	return missing
}

func appendBrowserAdapterEvidence(missing []string, evidence claimEvidence) []string {
	if !stringBoolIsTrue(evidence.BrowserAdapterReady) {
		missing = append(missing, "browser_adapter_ready=true")
	}
	if evidence.BrowserAdapterVersion == "" {
		missing = append(missing, "browser_adapter_version")
	}
	if evidence.BrowserAdapterFile == "" {
		missing = append(missing, "browser_adapter_file")
	}
	if evidence.BrowserAdapterSHA256 == "" {
		missing = append(missing, "browser_adapter_sha256")
	} else if !isSHA256Hex(evidence.BrowserAdapterSHA256) {
		missing = append(missing, "browser_adapter_sha256=64hex")
	}
	return missing
}

func appendLinkedProverReportEvidence(missing []string, evidence claimEvidence) []string {
	if evidence.LinkedProverReportFile == "" {
		missing = append(missing, "linked_prover_report_file")
	}
	if evidence.LinkedProverReportSHA256 == "" {
		missing = append(missing, "linked_prover_report_sha256")
	} else if !isSHA256Hex(evidence.LinkedProverReportSHA256) {
		missing = append(missing, "linked_prover_report_sha256=64hex")
	}
	return missing
}

func sourceFileHashIssues(rep report) []string {
	var issues []string
	sourceSet := make(map[string]struct{}, len(rep.SourceFiles))
	for _, path := range rep.SourceFiles {
		path = strings.TrimSpace(path)
		if path == "" {
			issues = append(issues, "source file path is empty")
			continue
		}
		sourceSet[path] = struct{}{}
		hash, ok := rep.SourceFileSHA256[path]
		if !ok {
			issues = append(issues, fmt.Sprintf("source file hash missing for %s", path))
			continue
		}
		if !isSHA256Hex(hash) {
			issues = append(issues, fmt.Sprintf("source file hash for %s must be 64hex", path))
		}
	}
	for path, hash := range rep.SourceFileSHA256 {
		if strings.TrimSpace(path) == "" {
			issues = append(issues, "source file hash has empty file path")
			continue
		}
		if _, ok := sourceSet[path]; !ok {
			issues = append(issues, fmt.Sprintf("source file hash has unlisted file %s", path))
			continue
		}
		if !isSHA256Hex(hash) {
			issues = append(issues, fmt.Sprintf("source file hash for %s must be 64hex", path))
		}
	}
	return uniqueStrings(issues)
}

func componentReportIssues(rep report) []string {
	if rep.ResultFamily != "public-capacity" {
		return nil
	}
	if len(rep.ComponentReports) == 0 {
		return []string{"component_reports is required for public-capacity"}
	}
	var issues []string
	for _, component := range rep.ComponentReports {
		path := strings.TrimSpace(component.Path)
		name := path
		if path == "" {
			name = "<missing path>"
			issues = append(issues, "component report path is required")
		}
		if component.SHA256 == "" {
			issues = append(issues, fmt.Sprintf("%s sha256 is required", name))
		} else if !isSHA256Hex(component.SHA256) {
			issues = append(issues, fmt.Sprintf("%s sha256 must be 64hex", name))
		}
		if path != "" && !hasString(rep.SourceFiles, path) {
			issues = append(issues, fmt.Sprintf("%s is not in source_files", name))
		}
		hash, ok := rep.SourceFileSHA256[path]
		if path != "" && !ok {
			issues = append(issues, fmt.Sprintf("%s is not in source_file_sha256", name))
		}
		if ok && component.SHA256 != "" && isSHA256Hex(component.SHA256) && !strings.EqualFold(hash, component.SHA256) {
			issues = append(issues, fmt.Sprintf("%s sha256 does not match source_file_sha256", name))
		}
		if component.RunProfile != "public_claim" {
			issues = append(issues, fmt.Sprintf("%s run_profile is not public_claim", name))
		}
		if !component.Eligible {
			issues = append(issues, fmt.Sprintf("%s is not eligible", name))
		}
		if len(component.ClaimTypes) == 0 {
			issues = append(issues, fmt.Sprintf("%s claim_types is empty", name))
		}
		for _, claimType := range component.ClaimTypes {
			if !hasString(rep.ClaimProfile.ClaimTypes, claimType) {
				issues = append(issues, fmt.Sprintf("%s claim_type %q is not in aggregate claim_types", name, claimType))
			}
		}
		if component.ActiveSetID != "" && rep.ActiveSetID != "" && component.ActiveSetID != rep.ActiveSetID {
			issues = append(issues, fmt.Sprintf("%s active_set_id %q does not match aggregate active_set_id %q", name, component.ActiveSetID, rep.ActiveSetID))
		}
		if component.ManifestSHA256 != "" && rep.ArtifactSet.ManifestSHA256 != "" && !strings.EqualFold(component.ManifestSHA256, rep.ArtifactSet.ManifestSHA256) {
			issues = append(issues, fmt.Sprintf("%s manifest_sha256 does not match aggregate manifest_sha256", name))
		}
	}
	return uniqueStrings(issues)
}

func evidenceSourceIssues(rep report, claimType string) []string {
	if rep.ResultFamily == "public-capacity" && len(rep.ComponentReports) > 0 {
		return nil
	}
	evidence := evidenceForClaim(rep, claimType)
	var issues []string
	switch claimType {
	case "prover_rps":
		issues = appendEvidenceSourceIssue(issues, rep, "prover_config_file", evidence.ProverConfigFile, evidence.ProverConfigSHA256)
		issues = appendEvidenceSourceIssue(issues, rep, "saturation_profile_file", evidence.SaturationProfileFile, evidence.SaturationProfileSHA256)
	case "chain_tps":
		issues = appendEvidenceSourceIssue(issues, rep, "chain_config_file", evidence.ChainConfigFile, evidence.ChainConfigSHA256)
		issues = appendEvidenceSourceIssue(issues, rep, "saturation_profile_file", evidence.SaturationProfileFile, evidence.SaturationProfileSHA256)
		issues = appendEvidenceSourceIssue(issues, rep, "reserve_snapshot_before_file", evidence.ReserveSnapshotBeforeFile, evidence.ReserveSnapshotBeforeSHA256)
		issues = appendEvidenceSourceIssue(issues, rep, "reserve_snapshot_after_file", evidence.ReserveSnapshotAfterFile, evidence.ReserveSnapshotAfterSHA256)
	case "user_latency":
		switch evidence.LatencyMode {
		case "browser":
			issues = appendEvidenceSourceIssue(issues, rep, "browser_adapter_file", evidence.BrowserAdapterFile, evidence.BrowserAdapterSHA256)
		case "remote":
			issues = appendEvidenceSourceIssue(issues, rep, "prover_config_file", evidence.ProverConfigFile, evidence.ProverConfigSHA256)
			issues = appendEvidenceSourceIssue(issues, rep, "linked_prover_report_file", evidence.LinkedProverReportFile, evidence.LinkedProverReportSHA256)
			issues = append(issues, linkedProverReportSemanticIssues(rep, evidence)...)
		}
		if userLatencyIncludesInclusion(rep) {
			issues = appendEvidenceSourceIssue(issues, rep, "chain_config_file", evidence.ChainConfigFile, evidence.ChainConfigSHA256)
		}
	}
	return issues
}

func appendEvidenceSourceIssue(issues []string, rep report, fieldName string, path string, expectedSHA256 string) []string {
	path = strings.TrimSpace(path)
	if path == "" {
		return issues
	}
	expectedSHA256 = strings.ToLower(strings.TrimSpace(expectedSHA256))
	if !hasString(rep.SourceFiles, path) {
		issues = append(issues, fmt.Sprintf("%s %q is not in source_files", fieldName, path))
	}
	hash, ok := rep.SourceFileSHA256[path]
	if !ok {
		issues = append(issues, fmt.Sprintf("%s %q is not in source_file_sha256", fieldName, path))
		return issues
	}
	if !isSHA256Hex(hash) {
		issues = append(issues, fmt.Sprintf("%s %q source_file_sha256 must be 64hex", fieldName, path))
		return issues
	}
	if expectedSHA256 != "" && isSHA256Hex(expectedSHA256) && strings.ToLower(hash) != expectedSHA256 {
		issues = append(issues, fmt.Sprintf("%s %q source_file_sha256 does not match evidence SHA-256", fieldName, path))
	}
	return issues
}

func linkedProverReportSemanticIssues(rep report, evidence claimEvidence) []string {
	path := strings.TrimSpace(evidence.LinkedProverReportFile)
	if path == "" {
		return nil
	}

	bz, err := os.ReadFile(path)
	if err != nil {
		return []string{fmt.Sprintf("linked_prover_report_file %q cannot be read: %v", path, err)}
	}
	sum := sha256.Sum256(bz)
	actualSHA := hex.EncodeToString(sum[:])
	expectedSHA := strings.ToLower(strings.TrimSpace(evidence.LinkedProverReportSHA256))

	var issues []string
	if expectedSHA != "" && isSHA256Hex(expectedSHA) && actualSHA != expectedSHA {
		issues = append(issues, fmt.Sprintf("linked_prover_report_file %q SHA-256 does not match linked_prover_report_sha256", path))
	}

	var linked report
	if err := json.Unmarshal(bz, &linked); err != nil {
		issues = append(issues, fmt.Sprintf("linked_prover_report_file %q is not a benchmark report JSON: %v", path, err))
		return issues
	}

	if linked.ClaimProfile.RunProfile != "public_claim" {
		issues = append(issues, fmt.Sprintf("linked_prover_report_file %q run_profile is not public_claim", path))
	}
	if !hasString(linked.ClaimProfile.ClaimTypes, "prover_rps") {
		issues = append(issues, fmt.Sprintf("linked_prover_report_file %q does not include prover_rps claim", path))
	}
	if !linked.ClaimProfile.Eligible {
		issues = append(issues, fmt.Sprintf("linked_prover_report_file %q is not marked eligible", path))
	}

	probe := linked
	probe.ClaimProfile.ClaimTypes = []string{"prover_rps"}
	probe.ClaimProfile.BlockingReasons = nil
	profile := evaluateClaimProfile(probe)
	if !profile.Eligible {
		issues = append(issues, fmt.Sprintf("linked_prover_report_file %q is not eligible for prover_rps: %s", path, strings.Join(profile.BlockingReasons, "; ")))
	}

	issues = appendLinkedReportMatchIssue(issues, path, "instance_profile", linked.ClaimEvidence.InstanceProfile, evidence.InstanceProfile)
	issues = appendLinkedReportMatchIssue(issues, path, "prover_config_sha256", linked.ClaimEvidence.ProverConfigSHA256, evidence.ProverConfigSHA256)
	issues = appendLinkedReportMatchIssue(issues, path, "active_set_id", linked.ActiveSetID, rep.ActiveSetID)
	issues = appendLinkedReportMatchIssue(issues, path, "artifact_set.manifest_sha256", linked.ArtifactSet.ManifestSHA256, rep.ArtifactSet.ManifestSHA256)
	return uniqueStrings(issues)
}

func appendLinkedReportMatchIssue(issues []string, path, fieldName, linkedValue, reportValue string) []string {
	linkedValue = strings.TrimSpace(linkedValue)
	reportValue = strings.TrimSpace(reportValue)
	if linkedValue == "" || reportValue == "" {
		return issues
	}
	if !strings.EqualFold(linkedValue, reportValue) {
		issues = append(issues, fmt.Sprintf("linked_prover_report_file %q %s %q does not match report %s %q", path, fieldName, linkedValue, fieldName, reportValue))
	}
	return issues
}

func runWindowIssues(rep report) []string {
	var issues []string
	startValue := strings.TrimSpace(rep.RunStartedAt)
	endValue := strings.TrimSpace(rep.RunEndedAt)
	if startValue == "" {
		issues = append(issues, "run_started_at is required for public_claim")
	}
	if endValue == "" {
		issues = append(issues, "run_ended_at is required for public_claim")
	}
	if startValue == "" || endValue == "" {
		return issues
	}
	start, err := time.Parse(time.RFC3339, startValue)
	if err != nil {
		issues = append(issues, "run_started_at must be RFC3339")
	}
	end, err := time.Parse(time.RFC3339, endValue)
	if err != nil {
		issues = append(issues, "run_ended_at must be RFC3339")
	}
	if len(issues) > 0 {
		return issues
	}
	if !end.After(start) {
		issues = append(issues, "run_ended_at must be after run_started_at")
	}
	if steadyStateSeconds := maxSteadyStateSeconds(rep); steadyStateSeconds > 0 && end.Sub(start) < time.Duration(steadyStateSeconds)*time.Second {
		issues = append(issues, "run window is shorter than steady_state_seconds")
	}
	return issues
}

func maxSteadyStateSeconds(rep report) int {
	maxSeconds := rep.ClaimEvidence.SteadyStateSeconds
	for _, evidence := range rep.ClaimEvidenceByType {
		if evidence.SteadyStateSeconds > maxSeconds {
			maxSeconds = evidence.SteadyStateSeconds
		}
	}
	return maxSeconds
}

const minPublicUserLatencySamples = 100

func benchmarkSampleIssues(rep report) []string {
	var issues []string
	checkUserLatencySamples := hasString(rep.ClaimProfile.ClaimTypes, "user_latency")
	for _, bench := range rep.Benchmarks {
		name := benchmarkDisplayName(bench)
		if bench.Samples <= 0 {
			issues = append(issues, fmt.Sprintf("%s samples must be positive", name))
			continue
		}
		if checkUserLatencySamples && strings.TrimSpace(bench.ClaimType) == "user_latency" && benchmarkLooksLikeUserLatency(bench) && bench.Samples < minPublicUserLatencySamples {
			issues = append(issues, fmt.Sprintf("%s samples must be >= %d for user_latency", name, minPublicUserLatencySamples))
		}
	}
	return issues
}

func publicClaimFamilyAllowed(resultFamily, claimType string) bool {
	resultFamily = strings.TrimSpace(resultFamily)
	for _, allowed := range allowedResultFamilies(claimType) {
		if resultFamily == allowed {
			return true
		}
	}
	return false
}

func allowedResultFamilies(claimType string) []string {
	switch claimType {
	case "prover_rps":
		return []string{"privacy-proverd-load", "public-capacity"}
	case "chain_tps":
		return []string{"privacy-localnet-tps", "public-capacity"}
	case "user_latency":
		return []string{"privacy-user-latency", "public-capacity"}
	default:
		return nil
	}
}

func invalidClaimMetrics(rep report, claimType string) []string {
	evidence := evidenceForClaim(rep, claimType)
	var invalid []string
	switch claimType {
	case "prover_rps":
		invalid = append(invalid, requireClaimMetricPositive(rep, claimType, "proofs/sec", "requests/sec")...)
		invalid = append(invalid, requireClaimMetricPositive(rep, claimType, "latency_ms", "proof_latency_ms", "roundtrip_latency_ms")...)
		invalid = append(invalid, requireClaimMetricP99AtMost(rep, claimType, evidence.LatencyP99SLOMS, "latency_ms", "proof_latency_ms", "roundtrip_latency_ms")...)
		invalid = append(invalid, requireClaimMetricRange(rep, claimType, 0, 0.001, "errors/op", "error_rate")...)
		invalid = append(invalid, requireClaimMetricPositive(rep, claimType, "cpu_percent")...)
		invalid = append(invalid, requireClaimMetricPositive(rep, claimType, "rss_bytes", "max_rss_bytes")...)
	case "chain_tps":
		invalid = append(invalid, requireClaimMetricPositive(rep, claimType, "tx/sec", "tps", "successful_tx/sec")...)
		invalid = append(invalid, requireClaimMetricPositive(rep, claimType, "inclusion_latency_ms")...)
		invalid = append(invalid, requireClaimMetricP95AtMost(rep, claimType, evidence.InclusionP95SLOMS, "inclusion_latency_ms")...)
		if _, ok := findClaimMetric(rep, claimType, "failed_tx_rate"); ok {
			invalid = append(invalid, requireClaimMetricRange(rep, claimType, 0, 0.001, "failed_tx_rate")...)
		}
	case "user_latency":
		invalid = append(invalid, requireClaimMetricPositive(rep, claimType, "prepare_latency_ms")...)
		invalid = append(invalid, requireClaimMetricPositive(rep, claimType, "time_to_submit_ms", "submit_latency_ms")...)
		invalid = append(invalid, requireClaimMetricPositive(rep, claimType, "total_latency_ms", "submit_ready_ms")...)
		invalid = append(invalid, requireClaimMetricP99AtMost(rep, claimType, evidence.LatencyP99SLOMS, "total_latency_ms", "submit_ready_ms")...)
		invalid = append(invalid, requireClaimMetricPositive(rep, claimType, "proof_latency_ms")...)
		if userLatencyIncludesInclusion(rep) {
			invalid = append(invalid, requireClaimMetricPositive(rep, claimType, userLatencyInclusionMetricNames()...)...)
			invalid = append(invalid, requireClaimMetricP95AtMost(rep, claimType, evidence.InclusionP95SLOMS, userLatencyInclusionMetricNames()...)...)
		}
		invalid = append(invalid, requireClaimMetricRange(rep, claimType, 0, 0.001, "error_rate", "timeout_rate", "cancel_rate")...)
	}
	return invalid
}

func userLatencyIncludesInclusion(rep report) bool {
	_, ok := findClaimMetric(rep, "user_latency", userLatencyInclusionMetricNames()...)
	return ok
}

func benchmarkLooksLikeUserLatency(bench benchmarkSummary) bool {
	return benchmarkHasMetric(bench, userLatencyRowMetricNames()...)
}

func userLatencyRowMetricNames() []string {
	return append([]string{
		"prepare_latency_ms",
		"time_to_submit_ms",
		"submit_latency_ms",
		"total_latency_ms",
		"submit_ready_ms",
	}, userLatencyInclusionMetricNames()...)
}

func userLatencyInclusionMetricNames() []string {
	return []string{"inclusion_latency_ms", "time_to_inclusion_ms", "included_latency_ms"}
}

func benchmarkHasMetric(bench benchmarkSummary, names ...string) bool {
	for _, name := range names {
		if _, ok := bench.Metrics[name]; ok {
			return true
		}
	}
	return false
}

func requireClaimMetricPositive(rep report, claimType string, names ...string) []string {
	var invalid []string
	for _, found := range findClaimMetrics(rep, claimType, names...) {
		metric := found.Metric
		if metric.Mean <= 0 || metric.P50 <= 0 || metric.P95 <= 0 || metric.P99 <= 0 || metric.Min <= 0 || metric.Max <= 0 {
			invalid = append(invalid, fmt.Sprintf("%s/%s must have positive mean/p50/p95/p99/min/max", found.BenchmarkName, found.MetricName))
		}
	}
	return invalid
}

func requireClaimMetricRange(rep report, claimType string, minValue float64, maxValue float64, names ...string) []string {
	var invalid []string
	for _, found := range findClaimMetrics(rep, claimType, names...) {
		observedMin := metricObservedMin(found.Metric)
		if observedMin < minValue {
			invalid = append(invalid, fmt.Sprintf("%s/%s min %.6f below %.6f", found.BenchmarkName, found.MetricName, observedMin, minValue))
			continue
		}
		observedMax := metricObservedMax(found.Metric)
		if observedMax > maxValue {
			invalid = append(invalid, fmt.Sprintf("%s/%s max %.6f exceeds %.6f", found.BenchmarkName, found.MetricName, observedMax, maxValue))
		}
	}
	return invalid
}

func requireClaimMetricP99AtMost(rep report, claimType string, maxValue float64, names ...string) []string {
	if maxValue <= 0 {
		return nil
	}
	var invalid []string
	for _, found := range findClaimMetrics(rep, claimType, names...) {
		if found.Metric.P99 > maxValue {
			invalid = append(invalid, fmt.Sprintf("%s/%s p99 %.6f exceeds %.6f", found.BenchmarkName, found.MetricName, found.Metric.P99, maxValue))
		}
	}
	return invalid
}

func requireClaimMetricP95AtMost(rep report, claimType string, maxValue float64, names ...string) []string {
	if maxValue <= 0 {
		return nil
	}
	var invalid []string
	for _, found := range findClaimMetrics(rep, claimType, names...) {
		if found.Metric.P95 > maxValue {
			invalid = append(invalid, fmt.Sprintf("%s/%s p95 %.6f exceeds %.6f", found.BenchmarkName, found.MetricName, found.Metric.P95, maxValue))
		}
	}
	return invalid
}

func findClaimMetric(rep report, claimType string, names ...string) (metricSummary, bool) {
	found := findClaimMetrics(rep, claimType, names...)
	if len(found) == 0 {
		return metricSummary{}, false
	}
	return found[0].Metric, true
}

func findClaimMetrics(rep report, claimType string, names ...string) []namedMetricSummary {
	rows, _ := claimBenchmarkRows(rep, claimType)
	return findMetricsInBenchmarks(rows, names...)
}

func requireMetricPositive(rep report, names ...string) []string {
	var invalid []string
	for _, found := range findMetrics(rep, names...) {
		metric := found.Metric
		if metric.Mean <= 0 || metric.P50 <= 0 || metric.P95 <= 0 || metric.P99 <= 0 || metric.Min <= 0 || metric.Max <= 0 {
			invalid = append(invalid, fmt.Sprintf("%s/%s must have positive mean/p50/p95/p99/min/max", found.BenchmarkName, found.MetricName))
		}
	}
	return invalid
}

func requireMetricMaxAtMost(rep report, maxValue float64, names ...string) []string {
	var invalid []string
	for _, found := range findMetrics(rep, names...) {
		observed := metricObservedMax(found.Metric)
		if observed > maxValue {
			invalid = append(invalid, fmt.Sprintf("%s/%s max %.6f exceeds %.6f", found.BenchmarkName, found.MetricName, observed, maxValue))
		}
	}
	return invalid
}

func requireMetricRange(rep report, minValue float64, maxValue float64, names ...string) []string {
	var invalid []string
	for _, found := range findMetrics(rep, names...) {
		observedMin := metricObservedMin(found.Metric)
		if observedMin < minValue {
			invalid = append(invalid, fmt.Sprintf("%s/%s min %.6f below %.6f", found.BenchmarkName, found.MetricName, observedMin, minValue))
			continue
		}
		observedMax := metricObservedMax(found.Metric)
		if observedMax > maxValue {
			invalid = append(invalid, fmt.Sprintf("%s/%s max %.6f exceeds %.6f", found.BenchmarkName, found.MetricName, observedMax, maxValue))
		}
	}
	return invalid
}

func requireMetricP99AtMost(rep report, maxValue float64, names ...string) []string {
	if maxValue <= 0 {
		return nil
	}
	var invalid []string
	for _, found := range findMetrics(rep, names...) {
		if found.Metric.P99 > maxValue {
			invalid = append(invalid, fmt.Sprintf("%s/%s p99 %.6f exceeds %.6f", found.BenchmarkName, found.MetricName, found.Metric.P99, maxValue))
		}
	}
	return invalid
}

func requireMetricP95AtMost(rep report, maxValue float64, names ...string) []string {
	if maxValue <= 0 {
		return nil
	}
	var invalid []string
	for _, found := range findMetrics(rep, names...) {
		if found.Metric.P95 > maxValue {
			invalid = append(invalid, fmt.Sprintf("%s/%s p95 %.6f exceeds %.6f", found.BenchmarkName, found.MetricName, found.Metric.P95, maxValue))
		}
	}
	return invalid
}

func findMetric(rep report, names ...string) (metricSummary, bool) {
	found := findMetrics(rep, names...)
	if len(found) == 0 {
		return metricSummary{}, false
	}
	return found[0].Metric, true
}

func findMetrics(rep report, names ...string) []namedMetricSummary {
	return findMetricsInBenchmarks(rep.Benchmarks, names...)
}

func findMetricsInBenchmarks(benchmarks []benchmarkSummary, names ...string) []namedMetricSummary {
	nameSet := make(map[string]struct{}, len(names))
	for _, name := range names {
		nameSet[name] = struct{}{}
	}
	var found []namedMetricSummary
	for _, bench := range benchmarks {
		for metricName, metric := range bench.Metrics {
			if _, ok := nameSet[metricName]; !ok {
				continue
			}
			found = append(found, namedMetricSummary{
				BenchmarkName: bench.Name,
				MetricName:    metricName,
				Metric:        metric,
			})
		}
	}
	sort.Slice(found, func(i, j int) bool {
		if found[i].BenchmarkName == found[j].BenchmarkName {
			return found[i].MetricName < found[j].MetricName
		}
		return found[i].BenchmarkName < found[j].BenchmarkName
	})
	return found
}

func metricObservedMax(metric metricSummary) float64 {
	return max(metricObservedValues(metric))
}

func metricObservedMin(metric metricSummary) float64 {
	return min(metricObservedValues(metric))
}

func metricObservedValues(metric metricSummary) []float64 {
	return []float64{metric.Mean, metric.P50, metric.P95, metric.P99, metric.Min, metric.Max}
}

func stringBoolIsTrue(value string) bool {
	parsed, err := strconv.ParseBool(strings.TrimSpace(value))
	return err == nil && parsed
}

func isPositiveFinite(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func hasClaimEvidence(evidence claimEvidence) bool {
	return evidence.SteadyStateSeconds != 0 ||
		evidence.LoadProfile != "" ||
		evidence.PreflightMode != "" ||
		evidence.AuthEnabled != "" ||
		evidence.InstanceProfile != "" ||
		evidence.ProverConfigFile != "" ||
		evidence.ProverConfigSHA256 != "" ||
		evidence.ChainConfig != "" ||
		evidence.ChainConfigFile != "" ||
		evidence.ChainConfigSHA256 != "" ||
		evidence.ReserveInvariant != "" ||
		evidence.LatencyP99SLOMS != 0 ||
		evidence.InclusionP95SLOMS != 0 ||
		evidence.RSSStable != "" ||
		evidence.SaturationProfile != "" ||
		evidence.SaturationProfileFile != "" ||
		evidence.SaturationProfileSHA256 != "" ||
		evidence.ThroughputWindowSeconds != 0 ||
		evidence.ReserveSnapshotBeforeFile != "" ||
		evidence.ReserveSnapshotBeforeSHA256 != "" ||
		evidence.ReserveSnapshotAfterFile != "" ||
		evidence.ReserveSnapshotAfterSHA256 != "" ||
		evidence.LatencyMode != "" ||
		evidence.ColdWarmSeparated != "" ||
		evidence.BrowserMatrix != "" ||
		evidence.BrowserAdapterReady != "" ||
		evidence.BrowserAdapterVersion != "" ||
		evidence.BrowserAdapterFile != "" ||
		evidence.BrowserAdapterSHA256 != "" ||
		evidence.RemoteTopology != "" ||
		evidence.LinkedProverReportFile != "" ||
		evidence.LinkedProverReportSHA256 != ""
}

func missingMetricGroups(benchmarks []benchmarkSummary, requiredAlternatives ...[]string) []string {
	missing := make([]string, 0)
	for _, alternatives := range requiredAlternatives {
		if !hasAnyMetric(benchmarks, alternatives...) {
			missing = append(missing, strings.Join(alternatives, "|"))
		}
	}
	return missing
}

func hasAnyMetric(benchmarks []benchmarkSummary, names ...string) bool {
	for _, bench := range benchmarks {
		for _, name := range names {
			if _, ok := bench.Metrics[name]; ok {
				return true
			}
		}
	}
	return false
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func hasString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func summarizeFees(metrics []txMetric, model feeModel) ([]feeSummary, error) {
	if len(metrics) == 0 {
		return nil, nil
	}
	if strings.TrimSpace(model.FeeDenom) == "" {
		return nil, fmt.Errorf("fee denom is required when tx metrics are provided")
	}
	if strings.TrimSpace(model.MinGasPrice) == "" {
		return nil, fmt.Errorf("min gas price is required when tx metrics are provided")
	}
	if strings.TrimSpace(model.GasAdjustment) == "" {
		model.GasAdjustment = "1"
	}

	minGasPrice, err := parsePositiveRat("min gas price", model.MinGasPrice)
	if err != nil {
		return nil, err
	}
	gasAdjustment, err := parsePositiveRat("gas adjustment", model.GasAdjustment)
	if err != nil {
		return nil, err
	}

	type groupedFeeMetrics struct {
		gasUsed       []float64
		gasWantedMax  uint64
		failedSamples int
	}
	grouped := make(map[string]*groupedFeeMetrics)
	for _, metric := range metrics {
		txType := strings.TrimSpace(metric.TxType)
		if txType == "" {
			return nil, fmt.Errorf("tx metric is missing tx_type")
		}
		group := grouped[txType]
		if group == nil {
			group = &groupedFeeMetrics{}
			grouped[txType] = group
		}
		if metric.Success == nil || !*metric.Success {
			group.failedSamples++
			continue
		}
		if metric.GasUsed == 0 {
			return nil, fmt.Errorf("tx metric %q has zero gas_used", txType)
		}
		group.gasUsed = append(group.gasUsed, float64(metric.GasUsed))
		if metric.GasWanted > group.gasWantedMax {
			group.gasWantedMax = metric.GasWanted
		}
	}

	names := make([]string, 0, len(grouped))
	for name := range grouped {
		names = append(names, name)
	}
	sort.Strings(names)

	summaries := make([]feeSummary, 0, len(names))
	for _, name := range names {
		group := grouped[name]
		if len(group.gasUsed) == 0 {
			continue
		}
		p50 := roundFloat(percentile(group.gasUsed, 50))
		p95 := roundFloat(percentile(group.gasUsed, 95))
		summaries = append(summaries, feeSummary{
			TxType:          name,
			Samples:         len(group.gasUsed),
			FailedSamples:   group.failedSamples,
			GasUsedMean:     roundFloat(mean(group.gasUsed)),
			GasUsedP50:      p50,
			GasUsedP95:      p95,
			GasUsedMax:      roundFloat(max(group.gasUsed)),
			GasWantedMax:    group.gasWantedMax,
			GasAdjustment:   model.GasAdjustment,
			MinGasPrice:     model.MinGasPrice,
			FeeDenom:        model.FeeDenom,
			EstimatedFeeP50: estimateFee(p50, gasAdjustment, minGasPrice, model.FeeDenom),
			EstimatedFeeP95: estimateFee(p95, gasAdjustment, minGasPrice, model.FeeDenom),
		})
	}
	return summaries, nil
}

func parsePositiveRat(name, value string) (*big.Rat, error) {
	parsed, ok := new(big.Rat).SetString(strings.TrimSpace(value))
	if !ok || parsed.Sign() < 0 {
		return nil, fmt.Errorf("%s must be a non-negative decimal", name)
	}
	return parsed, nil
}

func estimateFee(gas uint64, gasAdjustment, minGasPrice *big.Rat, denom string) string {
	fee := new(big.Rat).SetUint64(gas)
	fee.Mul(fee, gasAdjustment)
	fee.Mul(fee, minGasPrice)
	return ceilRat(fee).String() + denom
}

func ceilRat(value *big.Rat) *big.Int {
	numerator := new(big.Int).Set(value.Num())
	denominator := new(big.Int).Set(value.Denom())
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(numerator, denominator, remainder)
	if value.Sign() > 0 && remainder.Sign() != 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	return quotient
}

func roundFloat(value float64) uint64 {
	if value <= 0 {
		return 0
	}
	return uint64(math.Round(value))
}

func writeJSON(path string, rep report) error {
	bz, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(bz, '\n'), 0o644)
}

func moduleVersion(modulePath string) string {
	return commandOutput("go", "list", "-m", "-f", "{{.Version}}", modulePath)
}

func commandOutput(name string, args ...string) string {
	cmd := exec.Command(name, args...)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func resolveManifestPath(explicit string) string {
	candidates := []string{}
	if strings.TrimSpace(explicit) != "" {
		candidates = append(candidates, strings.TrimSpace(explicit))
	}
	if dir := strings.TrimSpace(os.Getenv(privacyzk.ZKArtifactDirEnv)); dir != "" {
		candidates = append(candidates, filepath.Join(dir, privacyzk.ArtifactManifestFile))
	}
	candidates = append(candidates, filepath.Join("artifacts", "privacy", privacyzk.ArtifactManifestFile))

	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

func loadArtifactSet(path, activeSetID, manifestSHA256 string) (artifactSet, error) {
	manifest, err := privacyzk.LoadArtifactManifest(path)
	if err != nil {
		return artifactSet{}, err
	}

	result := artifactSet{
		ActiveSetID:          activeSetID,
		ManifestActiveSetID:  strings.TrimSpace(manifest.ActiveSetID),
		ManifestSHA256:       manifestSHA256,
		ArtifactSHA256ByFile: make(map[string]string, len(manifest.Artifacts)),
	}
	for _, descriptor := range manifest.Artifacts {
		reportDescriptor := artifactDescriptorReport{
			CircuitID:    strings.TrimSpace(descriptor.CircuitID),
			ArtifactType: strings.TrimSpace(descriptor.ArtifactType),
			Filename:     strings.TrimSpace(descriptor.Filename),
			SHA256:       strings.TrimSpace(descriptor.SHA256),
		}
		result.ArtifactDescriptors = append(result.ArtifactDescriptors, reportDescriptor)
		if reportDescriptor.Filename != "" {
			result.ArtifactSHA256ByFile[reportDescriptor.Filename] = reportDescriptor.SHA256
		}
	}
	result.DescriptorIssues = artifactDescriptorIssues(manifest.Artifacts)
	result.DescriptorComplete = len(result.DescriptorIssues) == 0
	result.ArtifactFileIssues = artifactFileIssues(filepath.Dir(path), manifest.Artifacts)
	result.ArtifactFilesVerified = len(result.ArtifactFileIssues) == 0 && len(manifest.Artifacts) > 0
	return result, nil
}

func artifactDescriptorIssues(descriptors []privacyzk.ArtifactDescriptor) []string {
	type descriptorKey struct {
		circuitID    string
		artifactType string
		filename     string
	}
	seen := make(map[descriptorKey]string, len(descriptors))
	for _, descriptor := range descriptors {
		key := descriptorKey{
			circuitID:    strings.TrimSpace(descriptor.CircuitID),
			artifactType: strings.TrimSpace(descriptor.ArtifactType),
			filename:     strings.TrimSpace(descriptor.Filename),
		}
		seen[key] = strings.TrimSpace(descriptor.SHA256)
	}

	var issues []string
	for _, expected := range privacyzk.DefaultArtifactDescriptors() {
		key := descriptorKey{
			circuitID:    expected.CircuitID,
			artifactType: expected.ArtifactType,
			filename:     expected.Filename,
		}
		sha, ok := seen[key]
		if !ok {
			issues = append(issues, fmt.Sprintf("missing descriptor for %s/%s/%s", key.circuitID, key.artifactType, key.filename))
			continue
		}
		if sha == "" {
			issues = append(issues, fmt.Sprintf("missing sha256 for %s", key.filename))
			continue
		}
		if !validSHA256Hex(sha) {
			issues = append(issues, fmt.Sprintf("invalid sha256 for %s", key.filename))
		}
	}
	sort.Strings(issues)
	return issues
}

func validSHA256Hex(value string) bool {
	decoded, err := hex.DecodeString(strings.TrimSpace(value))
	return err == nil && len(decoded) == sha256.Size
}

func artifactFileIssues(baseDir string, descriptors []privacyzk.ArtifactDescriptor) []string {
	var issues []string
	for _, descriptor := range descriptors {
		filename := strings.TrimSpace(descriptor.Filename)
		expectedSHA := strings.TrimSpace(descriptor.SHA256)
		if filename == "" || expectedSHA == "" || !validSHA256Hex(expectedSHA) {
			continue
		}
		cleanName := filepath.Clean(filename)
		if filepath.IsAbs(cleanName) || cleanName == ".." || strings.HasPrefix(cleanName, ".."+string(filepath.Separator)) {
			issues = append(issues, fmt.Sprintf("unsafe artifact path %q", filename))
			continue
		}
		actualSHA, err := fileSHA256(filepath.Join(baseDir, cleanName))
		if err != nil {
			issues = append(issues, fmt.Sprintf("cannot hash %s: %v", filename, err))
			continue
		}
		if !strings.EqualFold(actualSHA, expectedSHA) {
			issues = append(issues, fmt.Sprintf("checksum mismatch for %s", filename))
		}
	}
	sort.Strings(issues)
	return issues
}

func fileSHA256(path string) (string, error) {
	bz, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(bz)
	return hex.EncodeToString(sum[:]), nil
}

func mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var sum float64
	for _, value := range values {
		sum += value
	}
	return sum / float64(len(values))
}

func percentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	if len(sorted) == 1 {
		return sorted[0]
	}
	rank := (p / 100) * float64(len(sorted)-1)
	lower := int(math.Floor(rank))
	upper := int(math.Ceil(rank))
	if lower == upper {
		return sorted[lower]
	}
	weight := rank - float64(lower)
	return sorted[lower]*(1-weight) + sorted[upper]*weight
}

func min(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	result := values[0]
	for _, value := range values[1:] {
		if value < result {
			result = value
		}
	}
	return result
}

func max(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	result := values[0]
	for _, value := range values[1:] {
		if value > result {
			result = value
		}
	}
	return result
}

func formatDurationNS(ns float64) string {
	switch {
	case ns >= 1e9:
		return fmt.Sprintf("%.3fs", ns/1e9)
	case ns >= 1e6:
		return fmt.Sprintf("%.3fms", ns/1e6)
	case ns >= 1e3:
		return fmt.Sprintf("%.3fus", ns/1e3)
	default:
		return fmt.Sprintf("%.0fns", ns)
	}
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
