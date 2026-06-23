package main

import "testing"

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
	if got := summary.Metrics["prepare_latency_ms"].Mean; got != 15 {
		t.Fatalf("unexpected prepare mean %.3f", got)
	}
	if got := summary.Metrics["proof_latency_ms"].P50; got != 95 {
		t.Fatalf("unexpected proof p50 %.3f", got)
	}
	if got := summary.Metrics["time_to_submit_ms"].Mean; got != 25 {
		t.Fatalf("unexpected submit mean %.3f", got)
	}
	if got := summary.Metrics["total_latency_ms"].P99; got != 159.7 {
		t.Fatalf("unexpected total p99 %.3f", got)
	}
	if got := summary.Metrics["inclusion_latency_ms"].Mean; got != 1500 {
		t.Fatalf("unexpected inclusion mean %.3f", got)
	}
	if got := summary.Metrics["error_rate"].Mean; got != 0 {
		t.Fatalf("unexpected error rate %.3f", got)
	}
}

func TestSummarizeLatencyTraceRejectsMissingFlowID(t *testing.T) {
	_, err := summarizeLatencyTrace([]latencyTraceEvent{{Phase: "prepare", DurationMS: 1, Success: true}}, nil)
	if err == nil {
		t.Fatal("expected missing flow_id error")
	}
}
