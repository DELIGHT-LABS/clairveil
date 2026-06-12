package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	privacyprovertransport "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/provertransport"
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
	Route           string                   `json:"route,omitempty"`
	Concurrency     int                      `json:"concurrency,omitempty"`
	WarmupSeconds   int                      `json:"warmup_seconds,omitempty"`
	DurationSeconds int                      `json:"duration_seconds,omitempty"`
	Metrics         map[string]metricSummary `json:"metric_summaries,omitempty"`
}

type benchmarkSummaryEnvelope struct {
	Benchmarks []benchmarkSummary `json:"benchmarks"`
}

type requestPayload struct {
	Route string
	Path  string
	Body  []byte
}

type loadResult struct {
	LatencyMS     float64
	RequestBytes  int
	ResponseBytes int
	Err           error
	Timeout       bool
}

type exampleBundle struct {
	Transfer struct {
		Request json.RawMessage `json:"request"`
	} `json:"transfer"`
	Withdraw struct {
		Request json.RawMessage `json:"request"`
	} `json:"withdraw"`
}

func main() {
	var baseURL string
	var bearerToken string
	var fixtureBundle string
	var transferRequestFile string
	var withdrawRequestFile string
	var profile string
	var concurrencyList string
	var durationValue string
	var warmupValue string
	var timeoutValue string
	var outPath string

	flag.StringVar(&baseURL, "base-url", "", "clairveil-proverd base URL")
	flag.StringVar(&bearerToken, "bearer-token", strings.TrimSpace(os.Getenv("CLAIRVEIL_PROVERD_BEARER_TOKEN")), "optional bearer token for clairveil-proverd")
	flag.StringVar(&fixtureBundle, "fixture-bundle", "x/privacy/client/sdk/conformance/testdata/privacy_prover_example_bundle.json", "optional prover example bundle containing transfer and withdraw requests")
	flag.StringVar(&transferRequestFile, "transfer-request", "", "transfer proof request JSON file")
	flag.StringVar(&withdrawRequestFile, "withdraw-request", "", "withdraw proof request JSON file")
	flag.StringVar(&profile, "profile", "transfer_only", "load profile: transfer_only, withdraw_only, mixed_80_20")
	flag.StringVar(&concurrencyList, "concurrency", "1,2", "comma-separated concurrency levels")
	flag.StringVar(&durationValue, "duration", "30s", "steady-state duration per concurrency bucket")
	flag.StringVar(&warmupValue, "warmup", "5s", "warmup duration per concurrency bucket")
	flag.StringVar(&timeoutValue, "timeout", "2m", "per-request timeout")
	flag.StringVar(&outPath, "out", "benchmarks/privacy-proverd-load/load-summary.json", "structured benchmark summary output path")
	flag.Parse()

	if strings.TrimSpace(baseURL) == "" {
		fatalf("-base-url is required")
	}
	duration, err := time.ParseDuration(durationValue)
	if err != nil || duration <= 0 {
		fatalf("-duration must be a positive duration")
	}
	warmup, err := time.ParseDuration(warmupValue)
	if err != nil || warmup < 0 {
		fatalf("-warmup must be a non-negative duration")
	}
	requestTimeout, err := time.ParseDuration(timeoutValue)
	if err != nil || requestTimeout <= 0 {
		fatalf("-timeout must be a positive duration")
	}
	concurrency, err := parsePositiveInts(concurrencyList)
	if err != nil {
		fatalf("parse concurrency: %v", err)
	}
	requests, err := loadRequests(profile, fixtureBundle, transferRequestFile, withdrawRequestFile)
	if err != nil {
		fatalf("load requests: %v", err)
	}

	client := &http.Client{Timeout: requestTimeout}
	var summaries []benchmarkSummary
	for _, level := range concurrency {
		if warmup > 0 {
			_, _ = runLoadBucket(context.Background(), client, baseURL, bearerToken, requests, level, warmup, true)
		}
		results, elapsed := runLoadBucket(context.Background(), client, baseURL, bearerToken, requests, level, duration, false)
		summaries = append(summaries, summarizeLoadBucket(profile, requests, level, warmup, elapsed, results))
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
	fmt.Printf("prover load summary written to %s\n", outPath)
}

func parsePositiveInts(value string) ([]int, error) {
	var result []int
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		parsed, err := strconv.Atoi(part)
		if err != nil || parsed <= 0 {
			return nil, fmt.Errorf("invalid positive integer %q", part)
		}
		result = append(result, parsed)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("at least one concurrency level is required")
	}
	return result, nil
}

func loadRequests(profile, fixtureBundle, transferRequestFile, withdrawRequestFile string) ([]requestPayload, error) {
	var bundle exampleBundle
	if strings.TrimSpace(fixtureBundle) != "" {
		bz, err := os.ReadFile(fixtureBundle)
		if err != nil {
			return nil, fmt.Errorf("read fixture bundle: %w", err)
		}
		if err := json.Unmarshal(bz, &bundle); err != nil {
			return nil, fmt.Errorf("decode fixture bundle: %w", err)
		}
	}
	transferBody, err := requestBody(transferRequestFile, bundle.Transfer.Request)
	if err != nil {
		return nil, fmt.Errorf("transfer request: %w", err)
	}
	withdrawBody, err := requestBody(withdrawRequestFile, bundle.Withdraw.Request)
	if err != nil {
		return nil, fmt.Errorf("withdraw request: %w", err)
	}
	transfer := requestPayload{Route: "transfer", Path: privacyprovertransport.TransferProofPath, Body: transferBody}
	withdraw := requestPayload{Route: "withdraw", Path: privacyprovertransport.WithdrawProofPath, Body: withdrawBody}

	switch strings.TrimSpace(profile) {
	case "transfer_only":
		return []requestPayload{transfer}, nil
	case "withdraw_only":
		return []requestPayload{withdraw}, nil
	case "mixed_80_20":
		return []requestPayload{transfer, transfer, transfer, transfer, withdraw}, nil
	default:
		return nil, fmt.Errorf("unsupported profile %q", profile)
	}
}

func requestBody(path string, fallback json.RawMessage) ([]byte, error) {
	if strings.TrimSpace(path) != "" {
		return os.ReadFile(path)
	}
	if len(fallback) == 0 {
		return nil, fmt.Errorf("request file is required when fixture bundle does not contain the request")
	}
	return append([]byte(nil), fallback...), nil
}

func runLoadBucket(ctx context.Context, client *http.Client, baseURL, bearerToken string, requests []requestPayload, concurrency int, duration time.Duration, quiet bool) ([]loadResult, time.Duration) {
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()

	started := time.Now()
	results := make(chan loadResult, concurrency*4)
	var wg sync.WaitGroup
	var counter atomic.Uint64
	for worker := 0; worker < concurrency; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}
				index := counter.Add(1) - 1
				payload := requests[int(index)%len(requests)]
				result := doRequest(ctx, client, baseURL, bearerToken, payload)
				if !quiet {
					results <- result
				}
			}
		}()
	}
	wg.Wait()
	close(results)

	collected := make([]loadResult, 0, len(results))
	for result := range results {
		collected = append(collected, result)
	}
	return collected, time.Since(started)
}

func doRequest(ctx context.Context, client *http.Client, baseURL, bearerToken string, payload requestPayload) loadResult {
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+payload.Path, bytes.NewReader(payload.Body))
	if err != nil {
		return loadResult{Err: err}
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(bearerToken) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(bearerToken))
	}
	resp, err := client.Do(req)
	if err != nil {
		return loadResult{Err: err, Timeout: ctx.Err() != nil}
	}
	defer resp.Body.Close()
	responseBytes, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return loadResult{Err: readErr}
	}
	result := loadResult{
		LatencyMS:     float64(time.Since(start)) / float64(time.Millisecond),
		RequestBytes:  len(payload.Body),
		ResponseBytes: len(responseBytes),
	}
	if resp.StatusCode != http.StatusOK {
		result.Err = fmt.Errorf("status %d", resp.StatusCode)
	}
	return result
}

func summarizeLoadBucket(profile string, requests []requestPayload, concurrency int, warmup time.Duration, elapsed time.Duration, results []loadResult) benchmarkSummary {
	latencies := make([]float64, 0, len(results))
	requestBytes := make([]float64, 0, len(results))
	responseBytes := make([]float64, 0, len(results))
	errors := 0
	timeouts := 0
	for _, result := range results {
		if result.Err != nil {
			errors++
			if result.Timeout {
				timeouts++
			}
			continue
		}
		latencies = append(latencies, result.LatencyMS)
		requestBytes = append(requestBytes, float64(result.RequestBytes))
		responseBytes = append(responseBytes, float64(result.ResponseBytes))
	}
	total := len(results)
	successes := len(latencies)
	elapsedSeconds := elapsed.Seconds()
	if elapsedSeconds <= 0 {
		elapsedSeconds = 1
	}
	route := requests[0].Route
	if len(requests) > 1 {
		route = "mixed"
	}
	return benchmarkSummary{
		Name:            fmt.Sprintf("ProverLoad%sC%d", profileName(profile), concurrency),
		Samples:         total,
		MetricKind:      "prover_load",
		ClaimType:       "prover_rps",
		LoadProfile:     profile,
		Route:           route,
		Concurrency:     concurrency,
		WarmupSeconds:   int(warmup.Round(time.Second).Seconds()),
		DurationSeconds: int(elapsed.Round(time.Second).Seconds()),
		Metrics: map[string]metricSummary{
			"requests/sec":   scalarMetric(float64(successes) / elapsedSeconds),
			"latency_ms":     summarizeValues(latencies),
			"error_rate":     scalarMetric(rate(errors, total)),
			"timeout_rate":   scalarMetric(rate(timeouts, total)),
			"request_bytes":  summarizeValues(requestBytes),
			"response_bytes": summarizeValues(responseBytes),
		},
	}
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
