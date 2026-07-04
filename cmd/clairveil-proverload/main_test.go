package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	privacyproverservice "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/proverservice"
	privacyprovertransport "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/provertransport"
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

func TestParseBaseURLsPrefersCommaSeparatedPool(t *testing.T) {
	got, err := parseBaseURLs("http://ignored:9090", "http://a:9090, http://b:9090, http://a:9090")
	if err != nil {
		t.Fatalf("parse base urls: %v", err)
	}
	if len(got) != 2 || got[0] != "http://a:9090" || got[1] != "http://b:9090" {
		t.Fatalf("unexpected base urls: %+v", got)
	}
}

func TestParseBaseURLsRequiresAtLeastOneURL(t *testing.T) {
	if _, err := parseBaseURLs("", " , "); err == nil {
		t.Fatalf("expected missing base url error")
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

func TestLoadRequestsGeneratesTransportValidDefaults(t *testing.T) {
	requests, err := loadRequests("mixed_80_20", "", "", "")
	if err != nil {
		t.Fatalf("load requests: %v", err)
	}
	if len(requests) != 5 {
		t.Fatalf("expected mixed profile request schedule, got %d", len(requests))
	}

	transferRequest, err := privacyprovertransport.DecodeTransferProofRequestJSON(requests[0].Body)
	if err != nil {
		t.Fatalf("decode generated transfer request: %v", err)
	}
	if err := privacyprovertransport.ValidateTransferProofRequest(*transferRequest); err != nil {
		t.Fatalf("validate generated transfer request: %v", err)
	}
	withdrawRequest, err := privacyprovertransport.DecodeWithdrawProofRequestJSON(requests[4].Body)
	if err != nil {
		t.Fatalf("decode generated withdraw request: %v", err)
	}
	if err := privacyprovertransport.ValidateWithdrawProofRequest(*withdrawRequest, time.Now()); err != nil {
		t.Fatalf("validate generated withdraw request: %v", err)
	}
}

func TestPreflightRequestsFailsBeforeMeasuredLoad(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"constraint #32267 is not satisfied"}`))
	}))
	defer server.Close()

	err := preflightRequests(
		context.Background(),
		server.Client(),
		server.URL,
		"",
		[]requestPayload{{Route: "transfer", Path: "/prove/transfer", Body: []byte(`{}`)}},
	)
	if err == nil {
		t.Fatal("expected preflight failure")
	}
	if !strings.Contains(err.Error(), "status=400") || !strings.Contains(err.Error(), "constraint #32267") {
		t.Fatalf("expected status and prover response in preflight error, got %v", err)
	}
}

func TestPreflightEndpointPoolAllowsUnhealthyEndpoints(t *testing.T) {
	healthyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer healthyServer.Close()
	unhealthyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"warming"}`))
	}))
	defer unhealthyServer.Close()

	healthy, failures, err := preflightEndpointPool(
		context.Background(),
		healthyServer.Client(),
		[]string{healthyServer.URL, unhealthyServer.URL},
		"",
		[]requestPayload{{Route: "transfer", Path: "/prove/transfer", Body: []byte(`{}`)}},
		true,
	)
	if err != nil {
		t.Fatalf("expected degraded preflight to continue with healthy endpoints: %v", err)
	}
	if len(healthy) != 1 || healthy[0] != healthyServer.URL {
		t.Fatalf("unexpected healthy endpoints: %+v", healthy)
	}
	if len(failures) != 1 || failures[0].Endpoint != unhealthyServer.URL {
		t.Fatalf("unexpected preflight failures: %+v", failures)
	}
}

func TestPreflightEndpointPoolFailsWhenAllEndpointsUnhealthy(t *testing.T) {
	unhealthyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer unhealthyServer.Close()

	_, failures, err := preflightEndpointPool(
		context.Background(),
		unhealthyServer.Client(),
		[]string{unhealthyServer.URL},
		"",
		[]requestPayload{{Route: "transfer", Path: "/prove/transfer", Body: []byte(`{}`)}},
		true,
	)
	if err == nil {
		t.Fatal("expected all-unhealthy preflight failure")
	}
	if len(failures) != 1 {
		t.Fatalf("expected one failed endpoint, got %+v", failures)
	}
}

func TestSummarizeLoadBucket(t *testing.T) {
	started := time.Unix(1_700_000_000, 0)
	summary := summarizeLoadBucket(
		"transfer_only",
		[]requestPayload{{Route: "transfer", Body: []byte("request")}},
		2,
		1,
		2,
		5*time.Second,
		10*time.Second,
		[]loadResult{
			{Endpoint: "http://a:9090", LatencyMS: 100, RequestBytes: 10, ResponseBytes: 20},
			{Endpoint: "http://b:9090", LatencyMS: 200, RequestBytes: 10, ResponseBytes: 30},
			{Endpoint: "http://a:9090", Err: errBoom{}},
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
	if summary.EndpointCount != 2 {
		t.Fatalf("unexpected endpoint count: %+v", summary)
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
	if got := summary.Metrics["endpoint_count"].Mean; got != 2 {
		t.Fatalf("unexpected endpoint count metric %.3f", got)
	}
	if got := summary.Metrics["configured_endpoint_count"].Mean; got != 3 {
		t.Fatalf("unexpected configured endpoint count metric %.3f", got)
	}
	if got := summary.Metrics["unhealthy_endpoint_count"].Mean; got != 1 {
		t.Fatalf("unexpected unhealthy endpoint count metric %.3f", got)
	}
	if got := summary.Metrics["requests_per_endpoint"].Max; got != 2 {
		t.Fatalf("unexpected requests per endpoint max %.3f", got)
	}
	if got := summary.Metrics["telemetry_error_rate"].Mean; got != 0 {
		t.Fatalf("unexpected telemetry error rate %.3f", got)
	}
}

func TestTelemetryCPUPercentGroupsByEndpoint(t *testing.T) {
	started := time.Unix(1_700_000_000, 0)
	got, ok := telemetryCPUPercent([]telemetrySample{
		{
			Endpoint:   "http://a:9090",
			CapturedAt: started,
			Metrics:    privacyproverservice.MetricsResponse{ProcessCPUSeconds: 10},
		},
		{
			Endpoint:   "http://b:9090",
			CapturedAt: started.Add(time.Second),
			Metrics:    privacyproverservice.MetricsResponse{ProcessCPUSeconds: 100},
		},
		{
			Endpoint:   "http://a:9090",
			CapturedAt: started.Add(10 * time.Second),
			Metrics:    privacyproverservice.MetricsResponse{ProcessCPUSeconds: 11},
		},
		{
			Endpoint:   "http://b:9090",
			CapturedAt: started.Add(11 * time.Second),
			Metrics:    privacyproverservice.MetricsResponse{ProcessCPUSeconds: 102},
		},
	})
	if !ok {
		t.Fatal("expected endpoint CPU percent")
	}
	if got != 15 {
		t.Fatalf("expected mean endpoint CPU percent 15, got %.3f", got)
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
			[]string{server.URL},
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

func TestRunLoadBucketDistributesRequestsAcrossEndpoints(t *testing.T) {
	var first atomic.Int64
	var second atomic.Int64
	firstServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		first.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer firstServer.Close()
	secondServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		second.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer secondServer.Close()

	results, _, _ := runLoadBucket(
		context.Background(),
		firstServer.Client(),
		[]string{firstServer.URL, secondServer.URL},
		"",
		[]requestPayload{{Route: "transfer", Path: "/prove/transfer", Body: []byte(`{}`)}},
		2,
		50*time.Millisecond,
		false,
		0,
	)

	if len(results) == 0 {
		t.Fatal("expected load results")
	}
	if first.Load() == 0 || second.Load() == 0 {
		t.Fatalf("expected both endpoints to receive requests, got first=%d second=%d", first.Load(), second.Load())
	}
}

func TestRequestForLoadIndexDistributesMixedProfileAcrossEndpoints(t *testing.T) {
	requests, err := scheduleRequests("mixed_80_20", map[string]requestPayload{
		"transfer": {Route: "transfer", Body: []byte(`{}`)},
		"withdraw": {Route: "withdraw", Body: []byte(`{}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	endpointCount := 5
	counts := make([]map[string]int, endpointCount)
	for endpoint := range counts {
		counts[endpoint] = make(map[string]int)
	}
	for index := uint64(0); index < uint64(endpointCount*len(requests)); index++ {
		endpoint := int(index) % endpointCount
		payload := requestForLoadIndex(index, endpointCount, requests)
		counts[endpoint][payload.Route]++
	}
	for endpoint, routes := range counts {
		if routes["transfer"] != 4 || routes["withdraw"] != 1 {
			t.Fatalf("endpoint %d got skewed route mix: %+v", endpoint, routes)
		}
	}
}

func TestRunLoadBucketLetsInFlightRequestFinishAtDurationDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(30 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client := server.Client()
	client.Timeout = time.Second
	results, _, _ := runLoadBucket(
		context.Background(),
		client,
		[]string{server.URL},
		"",
		[]requestPayload{{Route: "transfer", Path: "/prove/transfer", Body: []byte(`{}`)}},
		1,
		5*time.Millisecond,
		false,
		0,
	)
	if len(results) == 0 {
		t.Fatal("expected the in-flight request result to be recorded")
	}
	for _, result := range results {
		if result.Err != nil {
			t.Fatalf("duration cutoff should not be counted as a request error: %+v", result)
		}
	}
}

func TestDoRequestClassifiesClientTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := server.Client()
	client.Timeout = 10 * time.Millisecond
	result := doRequest(
		context.Background(),
		client,
		server.URL,
		"",
		requestPayload{Route: "transfer", Path: "/prove/transfer", Body: []byte(`{}`)},
	)
	if result.Err == nil {
		t.Fatal("expected timeout error")
	}
	if !result.Timeout {
		t.Fatalf("expected client timeout to be classified for timeout_rate, got %+v", result)
	}
}

func TestDoRequestClassifiesBodyReadTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1024")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"partial":`))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		time.Sleep(100 * time.Millisecond)
		_, _ = w.Write([]byte(`true}`))
	}))
	defer server.Close()

	client := server.Client()
	client.Timeout = 10 * time.Millisecond
	result := doRequest(
		context.Background(),
		client,
		server.URL,
		"",
		requestPayload{Route: "transfer", Path: "/prove/transfer", Body: []byte(`{"request":true}`)},
	)
	if result.Err == nil {
		t.Fatal("expected body read timeout error")
	}
	if !result.Timeout {
		t.Fatalf("expected body read timeout to be classified for timeout_rate, got %+v", result)
	}
	if result.StatusCode != http.StatusOK {
		t.Fatalf("expected response status to be preserved, got %+v", result)
	}
	if result.RequestBytes == 0 || result.LatencyMS <= 0 {
		t.Fatalf("expected request metadata to be preserved, got %+v", result)
	}
}

type errBoom struct{}

func (errBoom) Error() string { return "boom" }
