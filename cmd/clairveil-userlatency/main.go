package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
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
	Name        string                   `json:"name"`
	Samples     int                      `json:"samples"`
	MetricKind  string                   `json:"metric_kind"`
	ClaimType   string                   `json:"claim_type,omitempty"`
	FlowProfile string                   `json:"flow_profile,omitempty"`
	LatencyMode string                   `json:"latency_mode,omitempty"`
	ColdWarm    string                   `json:"cold_warm,omitempty"`
	Metrics     map[string]metricSummary `json:"metric_summaries,omitempty"`
}

type benchmarkSummaryEnvelope struct {
	Benchmarks []benchmarkSummary `json:"benchmarks"`
}

type latencyTraceEvent struct {
	SchemaVersion string  `json:"schema_version"`
	FlowID        string  `json:"flow_id"`
	FlowProfile   string  `json:"flow_profile"`
	LatencyMode   string  `json:"latency_mode"`
	ColdWarm      string  `json:"cold_warm"`
	Phase         string  `json:"phase"`
	StartedAt     string  `json:"started_at"`
	EndedAt       string  `json:"ended_at"`
	DurationMS    float64 `json:"duration_ms"`
	Success       bool    `json:"success"`
	Error         string  `json:"error,omitempty"`
	TxHash        string  `json:"txhash,omitempty"`
}

type txMetricEnvelope struct {
	Transactions []txMetric `json:"transactions"`
}

type txMetric struct {
	TxHash      string `json:"txhash"`
	SubmittedAt string `json:"submitted_at"`
	IncludedAt  string `json:"included_at"`
}

type inclusionMetric struct {
	LatencyMS float64
}

type flowAggregate struct {
	FlowID      string
	Profile     string
	Mode        string
	ColdWarm    string
	PhaseMS     map[string]float64
	TxHashes    []string
	Failed      bool
	TimedOut    bool
	Cancelled   bool
	FirstSeenAt string
}

type latencyBucket struct {
	Profile   string
	Mode      string
	ColdWarm  string
	PrepareMS []float64
	ProofMS   []float64
	SubmitMS  []float64
	ReadyMS   []float64
	TotalMS   []float64
	Errors    []float64
	Timeouts  []float64
	Cancels   []float64
	Inclusion []float64
}

func main() {
	var tracePath string
	var txMetricsPath string
	var flowProfileFilter string
	var latencyModeFilter string
	var coldWarmFilter string
	var outPath string

	flag.StringVar(&tracePath, "trace", "", "privacy latency trace JSONL or JSON array")
	flag.StringVar(&txMetricsPath, "tx-metrics", "", "optional tx metrics JSON with submitted_at/included_at")
	flag.StringVar(&flowProfileFilter, "flow-profile", "", "optional exact flow_profile filter")
	flag.StringVar(&latencyModeFilter, "latency-mode", "", "optional exact latency_mode filter")
	flag.StringVar(&coldWarmFilter, "cold-warm", "", "optional exact cold_warm filter")
	flag.StringVar(&outPath, "out", "benchmarks/privacy-user-latency/user-latency-summary.json", "structured benchmark summary output path")
	flag.Parse()

	if strings.TrimSpace(tracePath) == "" {
		fatalf("-trace is required")
	}

	events, err := readLatencyTrace(tracePath)
	if err != nil {
		fatalf("read trace: %v", err)
	}
	events = filterLatencyTrace(events, flowProfileFilter, latencyModeFilter, coldWarmFilter)
	inclusions := map[string]inclusionMetric{}
	if strings.TrimSpace(txMetricsPath) != "" {
		inclusions, err = readInclusionMetrics(txMetricsPath)
		if err != nil {
			fatalf("read tx metrics: %v", err)
		}
	}

	summaries, err := summarizeLatencyTrace(events, inclusions)
	if err != nil {
		fatalf("summarize latency trace: %v", err)
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
	fmt.Printf("user latency summary written to %s\n", outPath)
}

func filterLatencyTrace(events []latencyTraceEvent, flowProfile string, latencyMode string, coldWarm string) []latencyTraceEvent {
	flowProfile = strings.TrimSpace(flowProfile)
	latencyMode = strings.TrimSpace(latencyMode)
	coldWarm = strings.TrimSpace(coldWarm)
	if flowProfile == "" && latencyMode == "" && coldWarm == "" {
		return events
	}

	filtered := make([]latencyTraceEvent, 0, len(events))
	for _, event := range events {
		if flowProfile != "" && strings.TrimSpace(event.FlowProfile) != flowProfile {
			continue
		}
		if latencyMode != "" && strings.TrimSpace(event.LatencyMode) != latencyMode {
			continue
		}
		if coldWarm != "" && strings.TrimSpace(event.ColdWarm) != coldWarm {
			continue
		}
		filtered = append(filtered, event)
	}
	return filtered
}

func readLatencyTrace(path string) ([]latencyTraceEvent, error) {
	bz, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(bz))) == 0 {
		return nil, fmt.Errorf("trace is empty")
	}
	if strings.HasPrefix(strings.TrimSpace(string(bz)), "[") {
		var events []latencyTraceEvent
		if err := json.Unmarshal(bz, &events); err != nil {
			return nil, err
		}
		return events, nil
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var events []latencyTraceEvent
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event latencyTraceEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return events, nil
}

func readInclusionMetrics(path string) (map[string]inclusionMetric, error) {
	bz, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var envelope txMetricEnvelope
	if err := json.Unmarshal(bz, &envelope); err != nil {
		return nil, err
	}

	result := make(map[string]inclusionMetric, len(envelope.Transactions))
	for _, tx := range envelope.Transactions {
		txHash := strings.TrimSpace(tx.TxHash)
		if txHash == "" || strings.TrimSpace(tx.SubmittedAt) == "" || strings.TrimSpace(tx.IncludedAt) == "" {
			continue
		}
		submittedAt, err := parseTime(tx.SubmittedAt)
		if err != nil {
			return nil, fmt.Errorf("tx %s submitted_at: %w", txHash, err)
		}
		includedAt, err := parseTime(tx.IncludedAt)
		if err != nil {
			return nil, fmt.Errorf("tx %s included_at: %w", txHash, err)
		}
		if includedAt.Before(submittedAt) {
			continue
		}
		result[txHash] = inclusionMetric{
			LatencyMS: float64(includedAt.Sub(submittedAt)) / float64(time.Millisecond),
		}
	}
	return result, nil
}

func summarizeLatencyTrace(events []latencyTraceEvent, inclusions map[string]inclusionMetric) ([]benchmarkSummary, error) {
	flows := make(map[string]*flowAggregate)
	for index, event := range events {
		if strings.TrimSpace(event.FlowID) == "" {
			return nil, fmt.Errorf("event %d is missing flow_id", index)
		}
		if strings.TrimSpace(event.Phase) == "" {
			return nil, fmt.Errorf("event %d is missing phase", index)
		}
		if event.DurationMS < 0 || math.IsNaN(event.DurationMS) || math.IsInf(event.DurationMS, 0) {
			return nil, fmt.Errorf("event %d has invalid duration_ms", index)
		}
		flow := flows[event.FlowID]
		if flow == nil {
			flow = &flowAggregate{
				FlowID:      event.FlowID,
				Profile:     fallback(event.FlowProfile, "unknown_flow"),
				Mode:        fallback(event.LatencyMode, "native"),
				ColdWarm:    fallback(event.ColdWarm, "warm"),
				PhaseMS:     map[string]float64{},
				FirstSeenAt: event.StartedAt,
			}
			flows[event.FlowID] = flow
		}
		flow.PhaseMS[event.Phase] += event.DurationMS
		if strings.TrimSpace(event.TxHash) != "" {
			flow.TxHashes = append(flow.TxHashes, strings.TrimSpace(event.TxHash))
		}
		if !event.Success {
			flow.Failed = true
			lowerErr := strings.ToLower(event.Error)
			if strings.Contains(lowerErr, "timeout") || strings.Contains(lowerErr, "deadline") {
				flow.TimedOut = true
			}
			if strings.Contains(lowerErr, "cancel") {
				flow.Cancelled = true
			}
		}
	}
	if len(flows) == 0 {
		return nil, fmt.Errorf("trace contains no events")
	}

	buckets := map[string]*latencyBucket{}
	for _, flow := range flows {
		key := flow.Profile + "\x00" + flow.Mode + "\x00" + flow.ColdWarm
		bucket := buckets[key]
		if bucket == nil {
			bucket = &latencyBucket{
				Profile:  flow.Profile,
				Mode:     flow.Mode,
				ColdWarm: flow.ColdWarm,
			}
			buckets[key] = bucket
		}
		prepare := flow.PhaseMS["prepare"]
		proof := flow.PhaseMS["proof"]
		submit := flow.PhaseMS["submit"]
		total := flow.PhaseMS["total"]
		if total == 0 {
			total = prepare + proof + submit
		}
		ready := total
		if ready == 0 {
			ready = prepare + proof + submit
		}

		bucket.PrepareMS = append(bucket.PrepareMS, prepare)
		bucket.ProofMS = append(bucket.ProofMS, proof)
		bucket.SubmitMS = append(bucket.SubmitMS, submit)
		bucket.ReadyMS = append(bucket.ReadyMS, ready)
		bucket.TotalMS = append(bucket.TotalMS, total)
		bucket.Errors = append(bucket.Errors, boolFloat(flow.Failed))
		bucket.Timeouts = append(bucket.Timeouts, boolFloat(flow.TimedOut))
		bucket.Cancels = append(bucket.Cancels, boolFloat(flow.Cancelled))
		for _, txHash := range flow.TxHashes {
			if inclusion, ok := inclusions[txHash]; ok {
				bucket.Inclusion = append(bucket.Inclusion, inclusion.LatencyMS)
				break
			}
		}
	}

	keys := make([]string, 0, len(buckets))
	for key := range buckets {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	summaries := make([]benchmarkSummary, 0, len(keys))
	for _, key := range keys {
		bucket := buckets[key]
		metrics := map[string]metricSummary{
			"prepare_latency_ms": summarizeValues(bucket.PrepareMS),
			"proof_latency_ms":   summarizeValues(bucket.ProofMS),
			"time_to_submit_ms":  summarizeValues(bucket.SubmitMS),
			"submit_ready_ms":    summarizeValues(bucket.ReadyMS),
			"total_latency_ms":   summarizeValues(bucket.TotalMS),
			"error_rate":         summarizeValues(bucket.Errors),
			"timeout_rate":       summarizeValues(bucket.Timeouts),
			"cancel_rate":        summarizeValues(bucket.Cancels),
		}
		if len(bucket.Inclusion) > 0 {
			metrics["inclusion_latency_ms"] = summarizeValues(bucket.Inclusion)
			metrics["time_to_inclusion_ms"] = summarizeValues(bucket.Inclusion)
		}
		summaries = append(summaries, benchmarkSummary{
			Name:        "UserLatency" + profileName(bucket.Profile) + profileName(bucket.Mode) + profileName(bucket.ColdWarm),
			Samples:     len(bucket.TotalMS),
			MetricKind:  "user_latency",
			ClaimType:   "user_latency",
			FlowProfile: bucket.Profile,
			LatencyMode: bucket.Mode,
			ColdWarm:    bucket.ColdWarm,
			Metrics:     metrics,
		})
	}
	return summaries, nil
}

func parseTime(value string) (time.Time, error) {
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed, nil
	}
	return time.Parse(time.RFC3339, value)
}

func fallback(value, fallbackValue string) string {
	if strings.TrimSpace(value) == "" {
		return fallbackValue
	}
	return strings.TrimSpace(value)
}

func boolFloat(value bool) float64 {
	if value {
		return 1
	}
	return 0
}

func profileName(profile string) string {
	parts := strings.FieldsFunc(profile, func(r rune) bool { return r == '_' || r == '-' })
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, "")
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
	result := values[0]
	for _, value := range values[1:] {
		if value < result {
			result = value
		}
	}
	return result
}

func max(values []float64) float64 {
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
