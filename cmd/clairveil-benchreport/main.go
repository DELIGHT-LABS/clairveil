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
	SchemaVersion    string             `json:"schema_version"`
	GeneratedAt      string             `json:"generated_at"`
	ResultFamily     string             `json:"result_family,omitempty"`
	SourceFiles      []string           `json:"source_files,omitempty"`
	SourceFileSHA256 map[string]string  `json:"source_file_sha256,omitempty"`
	SourceFileIssues []string           `json:"source_file_issues,omitempty"`
	RunStartedAt     string             `json:"run_started_at,omitempty"`
	RunEndedAt       string             `json:"run_ended_at,omitempty"`
	Commit           string             `json:"commit"`
	Dirty            bool               `json:"dirty"`
	ActiveSetID      string             `json:"active_set_id"`
	ClaimProfile     claimProfile       `json:"claim_profile"`
	ClaimEvidence    claimEvidence      `json:"claim_evidence"`
	Environment      environment        `json:"environment"`
	ArtifactSet      artifactSet        `json:"artifact_set"`
	GoVersion        string             `json:"go_version"`
	GnarkVersion     string             `json:"gnark_version"`
	GnarkCrypto      string             `json:"gnark_crypto_version"`
	OS               string             `json:"os"`
	Arch             string             `json:"arch"`
	CPU              string             `json:"cpu"`
	ManifestPath     string             `json:"manifest_path,omitempty"`
	ManifestChecksum string             `json:"manifest_sha256,omitempty"`
	FeeModel         feeModel           `json:"fee_model"`
	Benchmarks       []benchmarkSummary `json:"benchmarks,omitempty"`
	Fees             []feeSummary       `json:"fees,omitempty"`
}

type claimProfile struct {
	RunProfile      string   `json:"run_profile"`
	ClaimTypes      []string `json:"claim_types,omitempty"`
	Eligible        bool     `json:"eligible"`
	BlockingReasons []string `json:"blocking_reasons,omitempty"`
}

type claimEvidence struct {
	SteadyStateSeconds       int     `json:"steady_state_seconds,omitempty"`
	LoadProfile              string  `json:"load_profile,omitempty"`
	PreflightMode            string  `json:"preflight_mode,omitempty"`
	AuthEnabled              string  `json:"auth_enabled,omitempty"`
	InstanceProfile          string  `json:"instance_profile,omitempty"`
	ProverConfigFile         string  `json:"prover_config_file,omitempty"`
	ProverConfigSHA256       string  `json:"prover_config_sha256,omitempty"`
	ChainConfig              string  `json:"chain_config,omitempty"`
	ChainConfigFile          string  `json:"chain_config_file,omitempty"`
	ChainConfigSHA256        string  `json:"chain_config_sha256,omitempty"`
	ReserveInvariant         string  `json:"reserve_invariant,omitempty"`
	LatencyP99SLOMS          float64 `json:"latency_p99_slo_ms,omitempty"`
	InclusionP95SLOMS        float64 `json:"inclusion_p95_slo_ms,omitempty"`
	RSSStable                string  `json:"rss_stable,omitempty"`
	SaturationProfile        string  `json:"saturation_profile,omitempty"`
	LatencyMode              string  `json:"latency_mode,omitempty"`
	ColdWarmSeparated        string  `json:"cold_warm_separated,omitempty"`
	BrowserMatrix            string  `json:"browser_matrix,omitempty"`
	BrowserAdapterReady      string  `json:"browser_adapter_ready,omitempty"`
	BrowserAdapterVersion    string  `json:"browser_adapter_version,omitempty"`
	BrowserAdapterFile       string  `json:"browser_adapter_file,omitempty"`
	BrowserAdapterSHA256     string  `json:"browser_adapter_sha256,omitempty"`
	RemoteTopology           string  `json:"remote_topology,omitempty"`
	LinkedProverReportFile   string  `json:"linked_prover_report_file,omitempty"`
	LinkedProverReportSHA256 string  `json:"linked_prover_report_sha256,omitempty"`
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

func main() {
	var inputPath string
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
			SteadyStateSeconds:       steadyStateSeconds,
			LoadProfile:              strings.TrimSpace(loadProfile),
			PreflightMode:            strings.TrimSpace(preflightMode),
			AuthEnabled:              strings.TrimSpace(authEnabled),
			InstanceProfile:          strings.TrimSpace(instanceProfile),
			ProverConfigFile:         strings.TrimSpace(proverConfigFile),
			ProverConfigSHA256:       resolvedProverConfigSHA256,
			ChainConfig:              strings.TrimSpace(chainConfig),
			ChainConfigFile:          strings.TrimSpace(chainConfigFile),
			ChainConfigSHA256:        resolvedChainConfigSHA256,
			ReserveInvariant:         strings.TrimSpace(reserveInvariant),
			LatencyP99SLOMS:          latencyP99SLOMS,
			InclusionP95SLOMS:        inclusionP95SLOMS,
			RSSStable:                strings.TrimSpace(rssStable),
			SaturationProfile:        strings.TrimSpace(saturationProfile),
			LatencyMode:              strings.TrimSpace(latencyMode),
			ColdWarmSeparated:        strings.TrimSpace(coldWarmSeparated),
			BrowserMatrix:            strings.TrimSpace(browserMatrix),
			BrowserAdapterReady:      strings.TrimSpace(browserAdapterReady),
			BrowserAdapterVersion:    strings.TrimSpace(browserAdapterVersion),
			BrowserAdapterFile:       strings.TrimSpace(browserAdapterFile),
			BrowserAdapterSHA256:     resolvedBrowserAdapterSHA256,
			RemoteTopology:           strings.TrimSpace(remoteTopology),
			LinkedProverReportFile:   strings.TrimSpace(linkedProverReportFile),
			LinkedProverReportSHA256: resolvedLinkedProverReportSHA256,
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

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		fatalf("create output directory: %v", err)
	}

	stamp := time.Now().UTC().Format("20060102T150405Z")
	shortCommit := rep.Commit
	if len(shortCommit) > 12 {
		shortCommit = shortCommit[:12]
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

	fmt.Printf("benchmark report written to %s\n", outDir)
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

	if rep.ClaimProfile.RunProfile == "public_claim" || len(rep.ClaimProfile.ClaimTypes) > 0 || hasClaimEvidence(rep.ClaimEvidence) {
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

	if len(rep.SourceFileSHA256) > 0 {
		fmt.Fprintf(&b, "## Source Files\n\n")
		fmt.Fprintf(&b, "| File | SHA-256 |\n")
		fmt.Fprintf(&b, "| --- | --- |\n")
		for _, path := range sortedMapKeys(rep.SourceFileSHA256) {
			fmt.Fprintf(&b, "| `%s` | `%s` |\n", path, rep.SourceFileSHA256[path])
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

func readTxMetrics(path string) ([]txMetric, error) {
	bz, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var direct []txMetric
	if err := json.Unmarshal(bz, &direct); err == nil {
		return direct, nil
	}

	var envelope txMetricEnvelope
	if err := json.Unmarshal(bz, &envelope); err != nil {
		return nil, err
	}
	return envelope.Transactions, nil
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
	return sourceStatusDirty(commandOutput("git", "status", "--short", "--", "."))
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
			reasons = append(reasons, "public-capacity multi-claim reports require per-claim evidence schema")
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
	rows, tagged := claimBenchmarkRows(rep, claimType)
	if !tagged {
		return []string{fmt.Sprintf("at least one benchmark summary must set claim_type=%s", claimType)}
	}
	var issues []string
	for _, bench := range rows {
		name := benchmarkDisplayName(bench)
		if bench.DurationSeconds > 0 && rep.ClaimEvidence.SteadyStateSeconds > 0 && bench.DurationSeconds < rep.ClaimEvidence.SteadyStateSeconds {
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
			if rep.ClaimEvidence.LatencyMode != "" && bench.LatencyMode != "" && bench.LatencyMode != rep.ClaimEvidence.LatencyMode {
				issues = append(issues, fmt.Sprintf("%s latency_mode %q does not match claim_evidence latency_mode %q", name, bench.LatencyMode, rep.ClaimEvidence.LatencyMode))
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
			[]string{"timeout_rate", "cancel_rate"},
		)
	default:
		return nil
	}
}

func incompleteClaimMetricRows(rep report, claimType string) []string {
	var incomplete []string
	rows, _ := claimBenchmarkRows(rep, claimType)
	for _, bench := range rows {
		name := benchmarkDisplayName(bench)
		switch claimType {
		case "prover_rps":
			if benchmarkHasMetric(bench, "proofs/sec", "requests/sec", "latency_ms", "proof_latency_ms", "roundtrip_latency_ms", "errors/op", "error_rate", "cpu_percent", "rss_bytes", "max_rss_bytes") {
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
			}
		case "chain_tps":
			if benchmarkHasMetric(bench, "tx/sec", "tps", "successful_tx/sec", "inclusion_latency_ms", "failed_tx_rate") {
				if missing := missingMetricGroupsInBenchmark(
					bench,
					[]string{"tx/sec", "tps", "successful_tx/sec"},
					[]string{"inclusion_latency_ms"},
					[]string{"failed_tx_rate"},
				); len(missing) > 0 {
					incomplete = append(incomplete, fmt.Sprintf("%s missing %s", name, strings.Join(missing, ", ")))
				}
			}
		case "user_latency":
			if benchmarkLooksLikeUserLatency(bench) {
				if missing := missingMetricGroupsInBenchmark(
					bench,
					[]string{"prepare_latency_ms"},
					[]string{"proof_latency_ms"},
					[]string{"time_to_submit_ms", "submit_latency_ms"},
					[]string{"total_latency_ms", "submit_ready_ms"},
					[]string{"timeout_rate", "cancel_rate"},
				); len(missing) > 0 {
					incomplete = append(incomplete, fmt.Sprintf("%s missing %s", name, strings.Join(missing, ", ")))
				}
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
	evidence := rep.ClaimEvidence
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

func evidenceSourceIssues(rep report, claimType string) []string {
	evidence := rep.ClaimEvidence
	var issues []string
	switch claimType {
	case "prover_rps":
		issues = appendEvidenceSourceIssue(issues, rep, "prover_config_file", evidence.ProverConfigFile, evidence.ProverConfigSHA256)
	case "chain_tps":
		issues = appendEvidenceSourceIssue(issues, rep, "chain_config_file", evidence.ChainConfigFile, evidence.ChainConfigSHA256)
	case "user_latency":
		switch evidence.LatencyMode {
		case "browser":
			issues = appendEvidenceSourceIssue(issues, rep, "browser_adapter_file", evidence.BrowserAdapterFile, evidence.BrowserAdapterSHA256)
		case "remote":
			issues = appendEvidenceSourceIssue(issues, rep, "prover_config_file", evidence.ProverConfigFile, evidence.ProverConfigSHA256)
			issues = appendEvidenceSourceIssue(issues, rep, "linked_prover_report_file", evidence.LinkedProverReportFile, evidence.LinkedProverReportSHA256)
			issues = append(issues, linkedProverReportSemanticIssues(rep)...)
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

func linkedProverReportSemanticIssues(rep report) []string {
	evidence := rep.ClaimEvidence
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
	if rep.ClaimEvidence.SteadyStateSeconds > 0 && end.Sub(start) < time.Duration(rep.ClaimEvidence.SteadyStateSeconds)*time.Second {
		issues = append(issues, "run window is shorter than steady_state_seconds")
	}
	return issues
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
	var invalid []string
	switch claimType {
	case "prover_rps":
		invalid = append(invalid, requireClaimMetricPositive(rep, claimType, "proofs/sec", "requests/sec")...)
		invalid = append(invalid, requireClaimMetricPositive(rep, claimType, "latency_ms", "proof_latency_ms", "roundtrip_latency_ms")...)
		invalid = append(invalid, requireClaimMetricP99AtMost(rep, claimType, rep.ClaimEvidence.LatencyP99SLOMS, "latency_ms", "proof_latency_ms", "roundtrip_latency_ms")...)
		invalid = append(invalid, requireClaimMetricRange(rep, claimType, 0, 0.001, "errors/op", "error_rate")...)
		invalid = append(invalid, requireClaimMetricPositive(rep, claimType, "cpu_percent")...)
		invalid = append(invalid, requireClaimMetricPositive(rep, claimType, "rss_bytes", "max_rss_bytes")...)
	case "chain_tps":
		invalid = append(invalid, requireClaimMetricPositive(rep, claimType, "tx/sec", "tps", "successful_tx/sec")...)
		invalid = append(invalid, requireClaimMetricPositive(rep, claimType, "inclusion_latency_ms")...)
		invalid = append(invalid, requireClaimMetricP95AtMost(rep, claimType, rep.ClaimEvidence.InclusionP95SLOMS, "inclusion_latency_ms")...)
		if _, ok := findClaimMetric(rep, claimType, "failed_tx_rate"); ok {
			invalid = append(invalid, requireClaimMetricRange(rep, claimType, 0, 0.001, "failed_tx_rate")...)
		}
	case "user_latency":
		invalid = append(invalid, requireClaimMetricPositive(rep, claimType, "prepare_latency_ms")...)
		invalid = append(invalid, requireClaimMetricPositive(rep, claimType, "time_to_submit_ms", "submit_latency_ms")...)
		invalid = append(invalid, requireClaimMetricPositive(rep, claimType, "total_latency_ms", "submit_ready_ms")...)
		invalid = append(invalid, requireClaimMetricP99AtMost(rep, claimType, rep.ClaimEvidence.LatencyP99SLOMS, "total_latency_ms", "submit_ready_ms")...)
		invalid = append(invalid, requireClaimMetricPositive(rep, claimType, "proof_latency_ms")...)
		if userLatencyIncludesInclusion(rep) {
			invalid = append(invalid, requireClaimMetricPositive(rep, claimType, userLatencyInclusionMetricNames()...)...)
			invalid = append(invalid, requireClaimMetricP95AtMost(rep, claimType, rep.ClaimEvidence.InclusionP95SLOMS, userLatencyInclusionMetricNames()...)...)
		}
		invalid = append(invalid, requireClaimMetricRange(rep, claimType, 0, 0.001, "timeout_rate", "cancel_rate")...)
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
		if metric.Success != nil && !*metric.Success {
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
