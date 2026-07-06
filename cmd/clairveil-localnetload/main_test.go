package main

import (
	"math"
	"testing"
)

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
				TxType:          "deposit",
				Height:          2,
				GasUsed:         100,
				MessageCount:    1,
				TxJSONSizeBytes: 900,
				Success:         &success,
				SubmittedAt:     "2026-06-12T00:00:01Z",
				IncludedAt:      "2026-06-12T00:00:03Z",
			},
			{
				TxType:          "transfer",
				Height:          3,
				GasUsed:         200,
				MessageCount:    4,
				TxJSONSizeBytes: 1200,
				Success:         &failed,
				SubmittedAt:     "2026-06-12T00:00:02Z",
				IncludedAt:      "2026-06-12T00:00:06Z",
			},
		},
	})
	if err != nil {
		t.Fatalf("summarize bucket: %v", err)
	}
	if summary.Name != "LocalnetTPSMixedTarget2" || summary.ClaimType != "chain_tps" || summary.TargetTxPerSec != 2 {
		t.Fatalf("unexpected summary metadata: %+v", summary)
	}
	requireFloatNear(t, "successful tx/sec", summary.Metrics["tx/sec"].Mean, 0.1)
	requireFloatNear(t, "submitted tx/sec", summary.Metrics["submitted_tx/sec"].Mean, 0.2)
	requireFloatNear(t, "failed tx rate", summary.Metrics["failed_tx_rate"].Mean, 0.5)
	requireFloatNear(t, "inclusion p50", summary.Metrics["inclusion_latency_ms"].P50, 3000)
	requireFloatNear(t, "gas p95", summary.Metrics["gas_used"].P95, 195)
	requireFloatNear(t, "message count max", summary.Metrics["message_count"].Max, 4)
	requireFloatNear(t, "tx json size mean", summary.Metrics["tx_json_size_bytes"].Mean, 1050)
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

func TestSummarizeBucketTreatsMissingSuccessAsFailed(t *testing.T) {
	summary, err := summarizeBucket(txMetricBucket{
		Name:           "mixed-target-1",
		LoadProfile:    "mixed_deposit_transfer_withdraw",
		TargetTxPerSec: 1,
		StartedAt:      "2026-06-12T00:00:00Z",
		EndedAt:        "2026-06-12T00:00:10Z",
		Transactions: []txMetric{
			{
				TxType:      "deposit",
				Height:      2,
				GasUsed:     100,
				SubmittedAt: "2026-06-12T00:00:01Z",
				IncludedAt:  "2026-06-12T00:00:03Z",
			},
		},
	})
	if err != nil {
		t.Fatalf("summarize bucket: %v", err)
	}
	requireFloatNear(t, "successful tx/sec", summary.Metrics["tx/sec"].Mean, 0)
	requireFloatNear(t, "failed tx rate", summary.Metrics["failed_tx_rate"].Mean, 1)
}

func TestSummarizeBucketOmitsMissingInclusionLatency(t *testing.T) {
	success := true
	summary, err := summarizeBucket(txMetricBucket{
		Name:           "mixed-target-1",
		LoadProfile:    "mixed_deposit_transfer_withdraw",
		TargetTxPerSec: 1,
		StartedAt:      "2026-06-12T00:00:00Z",
		EndedAt:        "2026-06-12T00:00:10Z",
		Transactions: []txMetric{
			{
				TxType:      "deposit",
				Height:      2,
				GasUsed:     100,
				Success:     &success,
				SubmittedAt: "2026-06-12T00:00:01Z",
				IncludedAt:  "2026-06-12T00:00:01Z",
			},
		},
	})
	if err != nil {
		t.Fatalf("summarize bucket: %v", err)
	}
	if _, ok := summary.Metrics["inclusion_latency_ms"]; ok {
		t.Fatalf("expected non-positive inclusion latency samples to be omitted: %+v", summary.Metrics["inclusion_latency_ms"])
	}
}

func requireFloatNear(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("unexpected %s: got %.12g want %.12g", name, got, want)
	}
}
