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

var benchmarkLinePattern = regexp.MustCompile(`^(Benchmark\S+)-\d+\s+\d+\s+(\d+(?:\.\d+)?)\s+ns/op(?:\s+(\d+(?:\.\d+)?)\s+B/op)?(?:\s+(\d+(?:\.\d+)?)\s+allocs/op)?`)

type report struct {
	SchemaVersion    string             `json:"schema_version"`
	GeneratedAt      string             `json:"generated_at"`
	Commit           string             `json:"commit"`
	Dirty            bool               `json:"dirty"`
	ActiveSetID      string             `json:"active_set_id"`
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
}

type benchmarkSummary struct {
	Name       string  `json:"name"`
	Samples    int     `json:"samples"`
	NSOpMean   float64 `json:"ns_per_op_mean"`
	NSOpP50    float64 `json:"ns_per_op_p50"`
	NSOpP95    float64 `json:"ns_per_op_p95"`
	NSOpMin    float64 `json:"ns_per_op_min"`
	NSOpMax    float64 `json:"ns_per_op_max"`
	OpsPerSec  float64 `json:"ops_per_sec_mean"`
	BytesOp    float64 `json:"bytes_per_op_mean,omitempty"`
	AllocsOp   float64 `json:"allocs_per_op_mean,omitempty"`
	MetricKind string  `json:"metric_kind"`
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
	var commitOverride string
	var dirtyOverride string
	flag.StringVar(&inputPath, "input", "", "raw go test -bench output")
	flag.StringVar(&outDir, "out", "benchmarks/privacy-circuits", "output directory for benchmark reports")
	flag.StringVar(&activeSetID, "active-set-id", privacyzk.ActiveCircuitSetID, "active circuit set id")
	flag.StringVar(&manifestPath, "manifest", "", "optional privacy_zk_manifest.json path")
	flag.StringVar(&feeDenom, "fee-denom", "", "fee denom used for expected fee calculations")
	flag.StringVar(&minGasPrice, "min-gas-price", "", "minimum gas price used for expected fee calculations")
	flag.StringVar(&gasAdjustment, "gas-adjustment", "", "gas adjustment used for expected fee calculations")
	flag.StringVar(&txMetricsPath, "tx-metrics", "", "optional JSON file with observed tx gas metrics")
	flag.StringVar(&commitOverride, "commit", "", "source commit hash override captured before benchmark output files are written")
	flag.StringVar(&dirtyOverride, "dirty", "", "source dirty state override captured before benchmark output files are written")
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
	if strings.TrimSpace(inputPath) == "" && strings.TrimSpace(txMetricsPath) == "" {
		fatalf("-input or -tx-metrics is required")
	}
	sourceCommit, sourceDirty, err := sourceMetadata(commitOverride, dirtyOverride)
	if err != nil {
		fatalf("source metadata: %v", err)
	}

	rep := report{
		SchemaVersion: reportSchemaVersion,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		Commit:        sourceCommit,
		Dirty:         sourceDirty,
		ActiveSetID:   activeSetID,
		GoVersion:     runtime.Version(),
		GnarkVersion:  moduleVersion("github.com/consensys/gnark"),
		GnarkCrypto:   moduleVersion("github.com/consensys/gnark-crypto"),
		OS:            runtime.GOOS,
		Arch:          runtime.GOARCH,
		CPU:           cpu,
		FeeModel: feeModel{
			FeeDenom:      strings.TrimSpace(feeDenom),
			MinGasPrice:   strings.TrimSpace(minGasPrice),
			GasAdjustment: strings.TrimSpace(gasAdjustment),
		},
		Benchmarks: summarizeBenchmarks(samples),
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

	resolvedManifest := resolveManifestPath(manifestPath)
	if resolvedManifest != "" {
		checksum, err := fileSHA256(resolvedManifest)
		if err != nil {
			fatalf("hash manifest %s: %v", resolvedManifest, err)
		}
		rep.ManifestPath = resolvedManifest
		rep.ManifestChecksum = checksum
	}

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
		match := benchmarkLinePattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		sample := benchmarkSample{
			Name: match[1],
			NSOp: mustParseFloat(match[2]),
		}
		if match[3] != "" {
			sample.BytesOp = mustParseFloat(match[3])
		}
		if match[4] != "" {
			sample.AllocsOp = mustParseFloat(match[4])
		}
		samples = append(samples, sample)
	}
	return samples, cpu
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
		for _, sample := range group {
			nsValues = append(nsValues, sample.NSOp)
			if sample.BytesOp > 0 {
				bytesValues = append(bytesValues, sample.BytesOp)
			}
			if sample.AllocsOp > 0 {
				allocValues = append(allocValues, sample.AllocsOp)
			}
		}
		meanNS := mean(nsValues)
		summary := benchmarkSummary{
			Name:       name,
			Samples:    len(group),
			NSOpMean:   meanNS,
			NSOpP50:    percentile(nsValues, 50),
			NSOpP95:    percentile(nsValues, 95),
			NSOpMin:    min(nsValues),
			NSOpMax:    max(nsValues),
			OpsPerSec:  1e9 / meanNS,
			BytesOp:    mean(bytesValues),
			AllocsOp:   mean(allocValues),
			MetricKind: benchmarkMetricKind(name),
		}
		summaries = append(summaries, summary)
	}
	return summaries
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

func renderMarkdown(rep report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Clairveil Privacy Benchmark Report\n\n")
	fmt.Fprintf(&b, "- generated_at: `%s`\n", rep.GeneratedAt)
	fmt.Fprintf(&b, "- commit: `%s`\n", rep.Commit)
	fmt.Fprintf(&b, "- dirty: `%t`\n", rep.Dirty)
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
	if rep.FeeModel.FeeDenom != "" || rep.FeeModel.MinGasPrice != "" || rep.FeeModel.GasAdjustment != "" {
		fmt.Fprintf(&b, "- fee_model: denom=`%s`, min_gas_price=`%s`, gas_adjustment=`%s`\n", rep.FeeModel.FeeDenom, rep.FeeModel.MinGasPrice, rep.FeeModel.GasAdjustment)
	}

	fmt.Fprintf(&b, "\n")
	if rep.Dirty {
		fmt.Fprintf(&b, "> Note: this benchmark was generated from a dirty worktree and should be treated as a development result.\n\n")
	}
	fmt.Fprintf(&b, "These numbers describe the measured benchmark scope only. Do not infer chain TPS from native proving or verification rows.\n\n")

	if len(rep.Benchmarks) > 0 {
		fmt.Fprintf(&b, "| Benchmark | Kind | Samples | Mean | p50 | p95 | ops/sec | B/op | allocs/op |\n")
		fmt.Fprintf(&b, "| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
		for _, bench := range rep.Benchmarks {
			fmt.Fprintf(
				&b,
				"| `%s` | `%s` | %d | %s | %s | %s | %.2f | %.0f | %.0f |\n",
				bench.Name,
				bench.MetricKind,
				bench.Samples,
				formatDurationNS(bench.NSOpMean),
				formatDurationNS(bench.NSOpP50),
				formatDurationNS(bench.NSOpP95),
				bench.OpsPerSec,
				bench.BytesOp,
				bench.AllocsOp,
			)
		}
		fmt.Fprintf(&b, "\n")
	}

	if len(rep.Fees) > 0 {
		fmt.Fprintf(&b, "## Expected Fees\n\n")
		fmt.Fprintf(&b, "Fee estimates are derived from observed `gas_used` only. They do not include prover infrastructure cost.\n\n")
		fmt.Fprintf(&b, "| Tx type | Samples | Gas mean | Gas p50 | Gas p95 | Gas max | Estimated fee p50 | Estimated fee p95 |\n")
		fmt.Fprintf(&b, "| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
		for _, fee := range rep.Fees {
			fmt.Fprintf(
				&b,
				"| `%s` | %d | %d | %d | %d | %d | `%s` | `%s` |\n",
				fee.TxType,
				fee.Samples,
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

func sourceMetadata(commitOverride string, dirtyOverride string) (string, bool, error) {
	commit := strings.TrimSpace(commitOverride)
	if commit == "" {
		commit = commandOutput("git", "rev-parse", "HEAD")
	}

	dirty := commandOutput("git", "status", "--short") != ""
	if strings.TrimSpace(dirtyOverride) != "" {
		parsed, err := strconv.ParseBool(strings.TrimSpace(dirtyOverride))
		if err != nil {
			return "", false, fmt.Errorf("dirty override must be true or false: %w", err)
		}
		dirty = parsed
	}

	return commit, dirty, nil
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

func fileSHA256(path string) (string, error) {
	bz, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(bz)
	return hex.EncodeToString(sum[:]), nil
}

func mustParseFloat(value string) float64 {
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		panic(err)
	}
	return parsed
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
