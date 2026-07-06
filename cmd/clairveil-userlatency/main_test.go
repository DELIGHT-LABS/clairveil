package main

import (
	"math"
	"testing"
)

func TestSummarizeLatencyTraceGroupsFlowsAndInclusion(t *testing.T) {
	events := []latencyTraceEvent{
		{FlowID: "flow-1", FlowProfile: "transfer_all_private", LatencyMode: "native", ColdWarm: "warm", Phase: "prepare", DurationMS: 10, Success: true},
		{FlowID: "flow-1", FlowProfile: "transfer_all_private", LatencyMode: "native", ColdWarm: "warm", Phase: "proof", DurationMS: 90, Success: true},
		{FlowID: "flow-1", FlowProfile: "transfer_all_private", LatencyMode: "native", ColdWarm: "warm", Phase: "submit", DurationMS: 20, Success: true, TxHash: "ABC"},
		{FlowID: "flow-1", FlowProfile: "transfer_all_private", LatencyMode: "native", ColdWarm: "warm", Phase: "total", DurationMS: 130, Success: true},
		{FlowID: "flow-2", FlowProfile: "transfer_all_private", LatencyMode: "native", ColdWarm: "warm", Phase: "prepare", DurationMS: 20, Success: true},
		{FlowID: "flow-2", FlowProfile: "transfer_all_private", LatencyMode: "native", ColdWarm: "warm", Phase: "proof", DurationMS: 100, Success: true},
		{FlowID: "flow-2", FlowProfile: "transfer_all_private", LatencyMode: "native", ColdWarm: "warm", Phase: "submit", DurationMS: 30, Success: true, TxHash: "DEF"},
		{FlowID: "flow-2", FlowProfile: "transfer_all_private", LatencyMode: "native", ColdWarm: "warm", Phase: "total", DurationMS: 160, Success: true},
	}

	summaries, err := summarizeLatencyTrace(events, map[string]inclusionMetric{
		"ABC": {LatencyMS: 1000},
		"DEF": {LatencyMS: 2000},
	})
	if err != nil {
		t.Fatalf("summarize trace: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected one bucket, got %d", len(summaries))
	}
	summary := summaries[0]
	if summary.Name != "UserLatencyTransferAllPrivateNativeWarm" || summary.Samples != 2 {
		t.Fatalf("unexpected summary metadata: %+v", summary)
	}
	requireFloatNear(t, "prepare mean", summary.Metrics["prepare_latency_ms"].Mean, 15)
	requireFloatNear(t, "proof p50", summary.Metrics["proof_latency_ms"].P50, 95)
	requireFloatNear(t, "submit mean", summary.Metrics["time_to_submit_ms"].Mean, 25)
	requireFloatNear(t, "total p99", summary.Metrics["total_latency_ms"].P99, 159.7)
	requireFloatNear(t, "inclusion mean", summary.Metrics["inclusion_latency_ms"].Mean, 1500)
	requireFloatNear(t, "error rate", summary.Metrics["error_rate"].Mean, 0)
}

func TestSummarizeLatencyTraceRejectsMissingFlowID(t *testing.T) {
	_, err := summarizeLatencyTrace([]latencyTraceEvent{{Phase: "prepare", DurationMS: 1, Success: true}}, nil)
	if err == nil {
		t.Fatal("expected missing flow_id error")
	}
}

func requireFloatNear(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("unexpected %s: got %.12g want %.12g", name, got, want)
	}
}
