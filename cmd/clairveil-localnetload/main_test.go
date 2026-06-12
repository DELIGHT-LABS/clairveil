package main

import "testing"

func TestSummarizeBucket(t *testing.T) {
	success := true
	failed := false
	summary, err := summarizeBucket(txMetricBucket{
		Name:           "mixed-target-2",
		LoadProfile:    "mixed_deposit_transfer_withdraw",
		TargetTxPerSec: 2,
		StartedAt:      "2026-06-12T00:00:00Z",
		EndedAt:        "2026-06-12T00:00:10Z",
		Transactions: []txMetric{
			{
				TxType:      "deposit",
				Height:      2,
				GasUsed:     100,
				Success:     &success,
				SubmittedAt: "2026-06-12T00:00:01Z",
				IncludedAt:  "2026-06-12T00:00:03Z",
			},
			{
				TxType:      "transfer",
				Height:      3,
				GasUsed:     200,
				Success:     &failed,
				SubmittedAt: "2026-06-12T00:00:02Z",
				IncludedAt:  "2026-06-12T00:00:06Z",
			},
		},
	})
	if err != nil {
		t.Fatalf("summarize bucket: %v", err)
	}
	if summary.Name != "LocalnetTPSMixedTarget2" || summary.ClaimType != "chain_tps" || summary.TargetTxPerSec != 2 {
		t.Fatalf("unexpected summary metadata: %+v", summary)
	}
	if got := summary.Metrics["tx/sec"].Mean; got != 0.1 {
		t.Fatalf("unexpected successful tx/sec %.3f", got)
	}
	if got := summary.Metrics["submitted_tx/sec"].Mean; got != 0.2 {
		t.Fatalf("unexpected submitted tx/sec %.3f", got)
	}
	if got := summary.Metrics["failed_tx_rate"].Mean; got != 0.5 {
		t.Fatalf("unexpected failed tx rate %.3f", got)
	}
	if got := summary.Metrics["inclusion_latency_ms"].P50; got != 3000 {
		t.Fatalf("unexpected inclusion p50 %.3f", got)
	}
	if got := summary.Metrics["gas_used"].P95; got != 195 {
		t.Fatalf("unexpected gas p95 %.3f", got)
	}
}

func TestSummarizeBucketRequiresDuration(t *testing.T) {
	success := true
	_, err := summarizeBucket(txMetricBucket{
		LoadProfile:    "deposit_only",
		TargetTxPerSec: 1,
		Transactions: []txMetric{
			{TxType: "deposit", GasUsed: 100, Success: &success},
		},
	})
	if err == nil {
		t.Fatalf("expected missing duration error")
	}
}
