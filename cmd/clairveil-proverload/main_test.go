package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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
}

type errBoom struct{}

func (errBoom) Error() string { return "boom" }
