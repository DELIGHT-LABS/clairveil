package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	privacyproverservice "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/proverservice"
)

func TestParsePositiveInts(t *testing.T) {
	got, err := parsePositiveInts("1, 2,4")
	if err != nil {
		t.Fatalf("parse positive ints: %v", err)
	}
	if len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 4 {
		t.Fatalf("unexpected parsed ints: %+v", got)
	}
	if _, err := parsePositiveInts("1,0"); err == nil {
		t.Fatalf("expected non-positive concurrency error")
	}
}

func TestLoadRequestsFromFixtureBundle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bundle.json")
	err := os.WriteFile(path, []byte(`{
  "transfer": {"request": {"version":"v1","payload":{"kind":"transfer"}}},
  "withdraw": {"request": {"version":"v1","payload":{"kind":"withdraw"}}}
}`), 0o644)
	if err != nil {
		t.Fatalf("write fixture bundle: %v", err)
	}

	requests, err := loadRequests("mixed_80_20", path, "", "")
	if err != nil {
		t.Fatalf("load requests: %v", err)
	}
	if len(requests) != 5 {
		t.Fatalf("expected mixed profile request schedule, got %d", len(requests))
	}
	if requests[0].Route != "transfer" || requests[4].Route != "withdraw" {
		t.Fatalf("unexpected request routes: %+v", requests)
	}
}

func TestSummarizeLoadBucket(t *testing.T) {
	started := time.Unix(1_700_000_000, 0)
	summary := summarizeLoadBucket(
		"transfer_only",
		[]requestPayload{{Route: "transfer", Body: []byte("request")}},
		2,
		5*time.Second,
		10*time.Second,
		[]loadResult{
			{LatencyMS: 100, RequestBytes: 10, ResponseBytes: 20},
			{LatencyMS: 200, RequestBytes: 10, ResponseBytes: 30},
			{Err: errBoom{}},
		},
		[]telemetrySample{
			{
				CapturedAt: started,
				Metrics: privacyproverservice.MetricsResponse{
					Goroutines:        8,
					HeapAllocBytes:    1024,
					HeapSysBytes:      4096,
					SysBytes:          8192,
					RSSBytes:          16_384,
					MaxRSSBytes:       16_384,
					ProcessCPUSeconds: 10,
				},
			},
			{
				CapturedAt: started.Add(10 * time.Second),
				Metrics: privacyproverservice.MetricsResponse{
					Goroutines:        10,
					HeapAllocBytes:    2048,
					HeapSysBytes:      4096,
					SysBytes:          8192,
					RSSBytes:          20_480,
					MaxRSSBytes:       20_480,
					ProcessCPUSeconds: 12,
				},
			},
		},
	)

	if summary.Name != "ProverLoadTransferOnlyC2" {
		t.Fatalf("unexpected summary name %q", summary.Name)
	}
	if summary.Samples != 3 || summary.ClaimType != "prover_rps" || summary.Concurrency != 2 {
		t.Fatalf("unexpected summary metadata: %+v", summary)
	}
	if got := summary.Metrics["requests/sec"].Mean; got != 0.2 {
		t.Fatalf("unexpected requests/sec %.3f", got)
	}
	if got := summary.Metrics["latency_ms"].P50; got != 150 {
		t.Fatalf("unexpected latency p50 %.3f", got)
	}
	if got := summary.Metrics["error_rate"].Mean; got != 1.0/3.0 {
		t.Fatalf("unexpected error rate %.6f", got)
	}
	if got := summary.Metrics["cpu_percent"].Mean; got != 20 {
		t.Fatalf("unexpected cpu percent %.3f", got)
	}
	if got := summary.Metrics["max_rss_bytes"].Mean; got != 20_480 {
		t.Fatalf("unexpected max rss %.3f", got)
	}
	if got := summary.Metrics["telemetry_error_rate"].Mean; got != 0 {
		t.Fatalf("unexpected telemetry error rate %.3f", got)
	}
}

func TestRunLoadBucketDrainsMoreResultsThanChannelBuffer(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	done := make(chan []loadResult, 1)
	go func() {
		results, _, _ := runLoadBucket(
			context.Background(),
			server.Client(),
			server.URL,
			"",
			[]requestPayload{{Route: "transfer", Path: "/prove/transfer", Body: []byte(`{}`)}},
			1,
			50*time.Millisecond,
			false,
			0,
		)
		done <- results
	}()

	select {
	case results := <-done:
		if len(results) <= 4 {
			t.Fatalf("expected more results than the old channel buffer, got %d after %d requests", len(results), requests.Load())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runLoadBucket blocked while publishing benchmark results")
	}
}

type errBoom struct{}

func (errBoom) Error() string { return "boom" }
