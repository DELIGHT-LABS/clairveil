package main

import (
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
		"# Clairveil Privacy Circuit Benchmark",
		"dirty worktree",
		"| `BenchmarkDepositCircuitVerify` | `native_verification` | 2 | 50.000ms |",
		"Do not infer chain TPS",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered markdown missing %q:\n%s", want, out)
		}
	}
}
