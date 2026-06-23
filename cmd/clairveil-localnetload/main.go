package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type metricSummary struct {
	Mean float64 `json:"mean"`
	P50  float64 `json:"p50"`
	P95  float64 `json:"p95"`
	P99  float64 `json:"p99"`
	Min  float64 `json:"min"`
	Max  float64 `json:"max"`
}

type benchmarkSummary struct {
	Name            string                   `json:"name"`
	Samples         int                      `json:"samples"`
	MetricKind      string                   `json:"metric_kind"`
	ClaimType       string                   `json:"claim_type,omitempty"`
	LoadProfile     string                   `json:"load_profile,omitempty"`
	DurationSeconds int                      `json:"duration_seconds,omitempty"`
	TargetTxPerSec  float64                  `json:"target_tx_per_sec,omitempty"`
	Metrics         map[string]metricSummary `json:"metric_summaries,omitempty"`
}

type benchmarkSummaryEnvelope struct {
	Benchmarks []benchmarkSummary `json:"benchmarks"`
}

type txMetricEnvelope struct {
	SchemaVersion string           `json:"schema_version,omitempty"`
	Source        string           `json:"source,omitempty"`
	Buckets       []txMetricBucket `json:"buckets,omitempty"`
	Transactions  []txMetric       `json:"transactions,omitempty"`
}

type txMetricBucket struct {
	Name           string     `json:"name,omitempty"`
	LoadProfile    string     `json:"load_profile,omitempty"`
	TargetTxPerSec float64    `json:"target_tx_per_sec,omitempty"`
	StartedAt      string     `json:"started_at,omitempty"`
	EndedAt        string     `json:"ended_at,omitempty"`
	Transactions   []txMetric `json:"transactions"`
}

type txMetric struct {
	TxType      string `json:"tx_type"`
	TxHash      string `json:"txhash,omitempty"`
	SourceFile  string `json:"source_file,omitempty"`
	Height      int64  `json:"height,omitempty"`
	GasUsed     uint64 `json:"gas_used"`
	GasWanted   uint64 `json:"gas_wanted,omitempty"`
	Success     *bool  `json:"success,omitempty"`
	SubmittedAt string `json:"submitted_at,omitempty"`
	IncludedAt  string `json:"included_at,omitempty"`
}

func main() {
	var metricsPath string
	var outPath string
	var loadProfile string
	var targetTxSecValue string
	var startedAt string
	var endedAt string
	flag.StringVar(&metricsPath, "tx-metrics", "", "tx metrics JSON produced by localnet benchmark runner")
	flag.StringVar(&outPath, "out", "benchmarks/privacy-localnet-tps/localnet-summary.json", "structured benchmark summary output path")
	flag.StringVar(&loadProfile, "load-profile", "mixed_deposit_transfer_withdraw", "default load profile for non-bucketed tx metrics")
	flag.StringVar(&targetTxSecValue, "target-tx-sec", "1", "default target tx/sec for non-bucketed tx metrics")
	flag.StringVar(&startedAt, "started-at", "", "default RFC3339 start timestamp for non-bucketed tx metrics")
	flag.StringVar(&endedAt, "ended-at", "", "default RFC3339 end timestamp for non-bucketed tx metrics")
	flag.Parse()

	if strings.TrimSpace(metricsPath) == "" {
		fatalf("-tx-metrics is required")
	}
	targetTxSec, err := strconv.ParseFloat(strings.TrimSpace(targetTxSecValue), 64)
	if err != nil || targetTxSec <= 0 {
		fatalf("-target-tx-sec must be positive")
	}
	envelope, err := readTxMetricEnvelope(metricsPath)
	if err != nil {
		fatalf("read tx metrics: %v", err)
	}
	buckets := normalizeBuckets(envelope, loadProfile, targetTxSec, startedAt, endedAt)
	if len(buckets) == 0 {
		fatalf("tx metrics contain no buckets or transactions")
	}

	summaries := make([]benchmarkSummary, 0, len(buckets))
	for _, bucket := range buckets {
		summary, err := summarizeBucket(bucket)
		if err != nil {
			fatalf("summarize bucket %q: %v", bucket.Name, err)
		}
		summaries = append(summaries, summary)
	}

	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		fatalf("create output dir: %v", err)
	}
	bz, err := json.MarshalIndent(benchmarkSummaryEnvelope{Benchmarks: summaries}, "", "  ")
	if err != nil {
		fatalf("marshal summary: %v", err)
	}
	if err := os.WriteFile(outPath, append(bz, '\n'), 0o644); err != nil {
		fatalf("write summary: %v", err)
	}
	fmt.Printf("localnet load summary written to %s\n", outPath)
}

func readTxMetricEnvelope(path string) (txMetricEnvelope, error) {
	bz, err := os.ReadFile(path)
	if err != nil {
		return txMetricEnvelope{}, err
	}
	var envelope txMetricEnvelope
	if err := json.Unmarshal(bz, &envelope); err == nil && (len(envelope.Buckets) > 0 || len(envelope.Transactions) > 0) {
		return envelope, nil
	}
	var transactions []txMetric
	if err := json.Unmarshal(bz, &transactions); err != nil {
		return txMetricEnvelope{}, err
	}
	return txMetricEnvelope{Transactions: transactions}, nil
}

func normalizeBuckets(envelope txMetricEnvelope, loadProfile string, targetTxSec float64, startedAt string, endedAt string) []txMetricBucket {
	if len(envelope.Buckets) > 0 {
		return envelope.Buckets
	}
	if len(envelope.Transactions) == 0 {
		return nil
	}
	return []txMetricBucket{
		{
			Name:           profileName(loadProfile),
			LoadProfile:    loadProfile,
			TargetTxPerSec: targetTxSec,
			StartedAt:      startedAt,
			EndedAt:        endedAt,
			Transactions:   envelope.Transactions,
		},
	}
}

func summarizeBucket(bucket txMetricBucket) (benchmarkSummary, error) {
	if len(bucket.Transactions) == 0 {
		return benchmarkSummary{}, fmt.Errorf("transactions are empty")
	}
	loadProfile := strings.TrimSpace(bucket.LoadProfile)
	if loadProfile == "" {
		loadProfile = "mixed_deposit_transfer_withdraw"
	}
	if bucket.TargetTxPerSec <= 0 {
		return benchmarkSummary{}, fmt.Errorf("target_tx_per_sec must be positive")
	}
	duration := bucketDuration(bucket)
	if duration <= 0 {
		return benchmarkSummary{}, fmt.Errorf("bucket duration must be positive; provide started_at/ended_at or per-tx timestamps")
	}

	included := 0
	successful := 0
	failed := 0
	heights := make(map[int64]struct{})
	var inclusionLatencies []float64
	var gasUsed []float64
	for _, tx := range bucket.Transactions {
		if tx.Height > 0 {
			included++
			heights[tx.Height] = struct{}{}
		}
		if tx.Success != nil && *tx.Success {
			successful++
		} else {
			failed++
		}
		if tx.GasUsed > 0 {
			gasUsed = append(gasUsed, float64(tx.GasUsed))
		}
		if latency, ok := txInclusionLatencyMS(tx); ok {
			inclusionLatencies = append(inclusionLatencies, latency)
		}
	}
	total := len(bucket.Transactions)
	durationSeconds := duration.Seconds()
	accepted := total
	name := strings.TrimSpace(bucket.Name)
	if name == "" {
		name = fmt.Sprintf("%sTarget%s", profileName(loadProfile), formatTarget(bucket.TargetTxPerSec))
	}
	metrics := map[string]metricSummary{
		"submitted_tx/sec":  scalarMetric(float64(total) / durationSeconds),
		"accepted_tx/sec":   scalarMetric(float64(accepted) / durationSeconds),
		"included_tx/sec":   scalarMetric(float64(included) / durationSeconds),
		"successful_tx/sec": scalarMetric(float64(successful) / durationSeconds),
		"tx/sec":            scalarMetric(float64(successful) / durationSeconds),
		"failed_tx_rate":    scalarMetric(rate(failed, total)),
		"retried_tx_count":  scalarMetric(0),
		"block_count":       scalarMetric(float64(len(heights))),
		"gas_used":          summarizeValues(gasUsed),
	}
	if len(inclusionLatencies) > 0 {
		metrics["inclusion_latency_ms"] = summarizeValues(inclusionLatencies)
	}
	return benchmarkSummary{
		Name:            "LocalnetTPS" + profileName(name),
		Samples:         total,
		MetricKind:      "chain_tps",
		ClaimType:       "chain_tps",
		LoadProfile:     loadProfile,
		DurationSeconds: int(duration.Round(time.Second).Seconds()),
		TargetTxPerSec:  bucket.TargetTxPerSec,
		Metrics:         metrics,
	}, nil
}

func bucketDuration(bucket txMetricBucket) time.Duration {
	start, startOK := parseTime(bucket.StartedAt)
	end, endOK := parseTime(bucket.EndedAt)
	if startOK && endOK && end.After(start) {
		return end.Sub(start)
	}

	var minTime time.Time
	var maxTime time.Time
	for _, tx := range bucket.Transactions {
		for _, value := range []string{tx.SubmittedAt, tx.IncludedAt} {
			parsed, ok := parseTime(value)
			if !ok {
				continue
			}
			if minTime.IsZero() || parsed.Before(minTime) {
				minTime = parsed
			}
			if maxTime.IsZero() || parsed.After(maxTime) {
				maxTime = parsed
			}
		}
	}
	if !minTime.IsZero() && maxTime.After(minTime) {
		return maxTime.Sub(minTime)
	}
	return 0
}

func txInclusionLatencyMS(tx txMetric) (float64, bool) {
	submittedAt, submittedOK := parseTime(tx.SubmittedAt)
	includedAt, includedOK := parseTime(tx.IncludedAt)
	if !submittedOK || !includedOK || !includedAt.After(submittedAt) {
		return 0, false
	}
	return float64(includedAt.Sub(submittedAt)) / float64(time.Millisecond), true
}

func parseTime(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		parsed, err = time.Parse(time.RFC3339, value)
	}
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

func profileName(profile string) string {
	parts := strings.FieldsFunc(profile, func(r rune) bool { return r == '_' || r == '-' || r == '.' })
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, "")
}

func formatTarget(target float64) string {
	return strings.ReplaceAll(strconv.FormatFloat(target, 'f', -1, 64), ".", "p")
}

func scalarMetric(value float64) metricSummary {
	return metricSummary{Mean: value, P50: value, P95: value, P99: value, Min: value, Max: value}
}

func summarizeValues(values []float64) metricSummary {
	if len(values) == 0 {
		return metricSummary{}
	}
	return metricSummary{
		Mean: mean(values),
		P50:  percentile(values, 50),
		P95:  percentile(values, 95),
		P99:  percentile(values, 99),
		Min:  min(values),
		Max:  max(values),
	}
}

func rate(count int, total int) float64 {
	if total <= 0 {
		return 0
	}
	return float64(count) / float64(total)
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

func percentile(values []float64, pct float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	if len(sorted) == 1 {
		return sorted[0]
	}
	rank := (pct / 100) * float64(len(sorted)-1)
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

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
