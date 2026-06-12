package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseBenchmarkOutput(t *testing.T) {
	raw := `
goos: darwin
goarch: arm64
pkg: github.com/DELIGHT-LABS/clairveil/x/privacy/circuit
cpu: Apple M3 Max
BenchmarkDepositCircuitProve-16             1   1200000000 ns/op   4096 B/op   12 allocs/op
BenchmarkDepositCircuitProve-16             1   1000000000 ns/op   2048 B/op   10 allocs/op
BenchmarkDepositCircuitVerify-16            1     50000000 ns/op   1024 B/op    4 allocs/op
PASS
`

	samples, cpu := parseBenchmarkOutput(raw)
	if cpu != "Apple M3 Max" {
		t.Fatalf("unexpected cpu %q", cpu)
	}
	if len(samples) != 3 {
		t.Fatalf("expected 3 samples, got %d", len(samples))
	}
	if samples[0].Name != "BenchmarkDepositCircuitProve" {
		t.Fatalf("unexpected benchmark name %q", samples[0].Name)
	}
	if samples[0].NSOp != 1200000000 {
		t.Fatalf("unexpected ns/op %.0f", samples[0].NSOp)
	}
	if samples[0].BytesOp != 4096 || samples[0].AllocsOp != 12 {
		t.Fatalf("unexpected allocation metrics %.0f %.0f", samples[0].BytesOp, samples[0].AllocsOp)
	}
}

func TestSummarizeBenchmarks(t *testing.T) {
	summaries := summarizeBenchmarks([]benchmarkSample{
		{Name: "BenchmarkDepositCircuitProve", NSOp: 100, BytesOp: 10, AllocsOp: 1},
		{Name: "BenchmarkDepositCircuitProve", NSOp: 200, BytesOp: 30, AllocsOp: 3},
		{Name: "BenchmarkDepositCircuitVerify", NSOp: 50, BytesOp: 5, AllocsOp: 1},
	})

	if len(summaries) != 2 {
		t.Fatalf("expected 2 summaries, got %d", len(summaries))
	}
	prove := summaries[0]
	if prove.Name != "BenchmarkDepositCircuitProve" {
		t.Fatalf("unexpected first summary %q", prove.Name)
	}
	if prove.MetricKind != "native_proving" {
		t.Fatalf("unexpected metric kind %q", prove.MetricKind)
	}
	if prove.NSOpMean != 150 || prove.NSOpP50 != 150 || prove.NSOpP95 != 195 {
		t.Fatalf("unexpected ns summary: mean %.0f p50 %.0f p95 %.0f", prove.NSOpMean, prove.NSOpP50, prove.NSOpP95)
	}
	if prove.BytesOp != 20 || prove.AllocsOp != 2 {
		t.Fatalf("unexpected allocation summary %.0f %.0f", prove.BytesOp, prove.AllocsOp)
	}
}

func TestBenchmarkMetricKindClassifiesHTTPProverRoundTrip(t *testing.T) {
	if got := benchmarkMetricKind("BenchmarkHTTPProverClientTransferRoundTrip"); got != "prover_http_client_roundtrip" {
		t.Fatalf("unexpected metric kind %q", got)
	}
}

func TestRenderMarkdownIncludesBenchmarkTable(t *testing.T) {
	out := renderMarkdown(report{
		SchemaVersion: reportSchemaVersion,
		GeneratedAt:   "2026-06-12T00:00:00Z",
		Commit:        "abc123",
		Dirty:         true,
		ActiveSetID:   "privacy-v1",
		GoVersion:     "go1.24.0",
		GnarkVersion:  "v0.14.0",
		GnarkCrypto:   "v0.18.0",
		OS:            "darwin",
		Arch:          "arm64",
		CPU:           "Apple M3 Max",
		Benchmarks: []benchmarkSummary{
			{
				Name:       "BenchmarkDepositCircuitVerify",
				Samples:    2,
				NSOpMean:   50000000,
				NSOpP50:    50000000,
				NSOpP95:    55000000,
				OpsPerSec:  20,
				BytesOp:    1024,
				AllocsOp:   4,
				MetricKind: "native_verification",
			},
		},
	})

	for _, want := range []string{
		"# Clairveil Privacy Benchmark Report",
		"dirty worktree",
		"| `BenchmarkDepositCircuitVerify` | `native_verification` | 2 | 50.000ms |",
		"Do not infer chain TPS",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered markdown missing %q:\n%s", want, out)
		}
	}
}

func TestRenderMarkdownIncludesFeeFailures(t *testing.T) {
	out := renderMarkdown(report{
		SchemaVersion: reportSchemaVersion,
		GeneratedAt:   "2026-06-12T00:00:00Z",
		Commit:        "abc123",
		ActiveSetID:   "privacy-v1",
		GoVersion:     "go1.24.0",
		OS:            "darwin",
		Arch:          "arm64",
		FeeModel: feeModel{
			FeeDenom:      "uclair",
			MinGasPrice:   "0.025",
			GasAdjustment: "1.2",
		},
		Fees: []feeSummary{
			{
				TxType:          "deposit",
				Samples:         2,
				FailedSamples:   1,
				GasUsedMean:     150,
				GasUsedP50:      150,
				GasUsedP95:      195,
				GasUsedMax:      200,
				EstimatedFeeP50: "5uclair",
				EstimatedFeeP95: "6uclair",
			},
		},
	})

	for _, want := range []string{
		"| Tx type | Samples | Failed | Gas mean |",
		"| `deposit` | 2 | 1 | 150 | 150 | 195 | 200 | `5uclair` | `6uclair` |",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered markdown missing %q:\n%s", want, out)
		}
	}
}

func TestSummarizeFees(t *testing.T) {
	success := true
	failed := false
	summaries, err := summarizeFees([]txMetric{
		{TxType: "deposit", GasUsed: 100, GasWanted: 200, Success: &success},
		{TxType: "deposit", GasUsed: 200, GasWanted: 300, Success: &success},
		{TxType: "deposit", GasUsed: 999, GasWanted: 999, Success: &failed},
	}, feeModel{
		FeeDenom:      "uclair",
		MinGasPrice:   "0.025",
		GasAdjustment: "1.2",
	})
	if err != nil {
		t.Fatalf("summarize fees: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	got := summaries[0]
	if got.Samples != 2 || got.FailedSamples != 1 {
		t.Fatalf("unexpected sample counts: %+v", got)
	}
	if got.GasUsedMean != 150 || got.GasUsedP50 != 150 || got.GasUsedP95 != 195 || got.GasUsedMax != 200 {
		t.Fatalf("unexpected gas summary: %+v", got)
	}
	if got.EstimatedFeeP50 != "5uclair" || got.EstimatedFeeP95 != "6uclair" {
		t.Fatalf("unexpected fee estimates: %+v", got)
	}
}

func TestSummarizeFeesRequiresFeePolicy(t *testing.T) {
	_, err := summarizeFees([]txMetric{{TxType: "deposit", GasUsed: 100}}, feeModel{})
	if err == nil {
		t.Fatal("expected missing fee policy error")
	}
}

func TestReadTxMetricsAcceptsEnvelope(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tx-metrics.json")
	err := os.WriteFile(path, []byte(`{
  "schema_version": "clairveil.tx_metrics.v1",
  "transactions": [
    {"tx_type": "deposit", "gas_used": 100, "gas_wanted": 200, "success": true}
  ]
}`), 0o644)
	if err != nil {
		t.Fatalf("write tx metrics: %v", err)
	}

	metrics, err := readTxMetrics(path)
	if err != nil {
		t.Fatalf("read tx metrics: %v", err)
	}
	if len(metrics) != 1 || metrics[0].TxType != "deposit" || metrics[0].GasUsed != 100 {
		t.Fatalf("unexpected tx metrics: %+v", metrics)
	}
}

func TestSourceMetadataUsesOverrides(t *testing.T) {
	commit, dirty, err := sourceMetadata("abc123", "false")
	if err != nil {
		t.Fatalf("source metadata: %v", err)
	}
	if commit != "abc123" || dirty {
		t.Fatalf("unexpected source metadata: commit=%q dirty=%t", commit, dirty)
	}
}

func TestSourceMetadataRejectsInvalidDirtyOverride(t *testing.T) {
	_, _, err := sourceMetadata("abc123", "sometimes")
	if err == nil {
		t.Fatal("expected invalid dirty override error")
	}
}
