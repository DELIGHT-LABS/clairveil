package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	privacyzk "github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
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

func TestParseBenchmarkOutputKeepsCustomMetrics(t *testing.T) {
	raw := `
cpu: Apple M5 Pro
BenchmarkHTTPProverLoadTransfer-16     100   1000000 ns/op   12.5 proofs/sec   0.01 errors/op   4096 B/op   4 allocs/op
`

	samples, cpu := parseBenchmarkOutput(raw)
	if cpu != "Apple M5 Pro" {
		t.Fatalf("unexpected cpu %q", cpu)
	}
	if len(samples) != 1 {
		t.Fatalf("expected 1 sample, got %d", len(samples))
	}
	got := samples[0]
	if got.Name != "BenchmarkHTTPProverLoadTransfer" {
		t.Fatalf("unexpected benchmark name %q", got.Name)
	}
	if got.Metrics["proofs/sec"] != 12.5 || got.Metrics["errors/op"] != 0.01 {
		t.Fatalf("custom metrics not retained: %+v", got.Metrics)
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

func TestSummarizeBenchmarksIncludesMetricSummaries(t *testing.T) {
	summaries := summarizeBenchmarks([]benchmarkSample{
		{Name: "BenchmarkHTTPProverLoadTransfer", NSOp: 100, Metrics: map[string]float64{"ns/op": 100, "proofs/sec": 10}},
		{Name: "BenchmarkHTTPProverLoadTransfer", NSOp: 200, Metrics: map[string]float64{"ns/op": 200, "proofs/sec": 20}},
	})

	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	proofs := summaries[0].Metrics["proofs/sec"]
	if proofs.Mean != 15 || proofs.P50 != 15 || proofs.P95 != 19.5 {
		t.Fatalf("unexpected proofs/sec summary: %+v", proofs)
	}
}

func TestBenchmarkMetricKindClassifiesHTTPProverRoundTrip(t *testing.T) {
	if got := benchmarkMetricKind("BenchmarkHTTPProverClientTransferRoundTrip"); got != "prover_http_client_roundtrip" {
		t.Fatalf("unexpected metric kind %q", got)
	}
}

func TestReportSourceFilesIncludesActualInputs(t *testing.T) {
	got := reportSourceFiles(
		"benchmarks/manual-evidence.json, benchmarks/raw.txt",
		"benchmarks/raw.txt",
		"benchmarks/summary.json",
		"benchmarks/tx-metrics.json",
		"benchmarks/prover-config.json",
	)
	want := []string{
		"benchmarks/manual-evidence.json",
		"benchmarks/raw.txt",
		"benchmarks/summary.json",
		"benchmarks/tx-metrics.json",
		"benchmarks/prover-config.json",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected source files: got %+v want %+v", got, want)
	}
}

func TestResolveConfigSHA256HashesConfigFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prover.json")
	if err := os.WriteFile(path, []byte(`{"auth":"required"}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	want, err := fileSHA256(path)
	if err != nil {
		t.Fatalf("hash config: %v", err)
	}

	got, err := resolveConfigSHA256("prover", "", path)
	if err != nil {
		t.Fatalf("resolve config hash: %v", err)
	}
	if got != want {
		t.Fatalf("unexpected computed hash: got %s want %s", got, want)
	}

	got, err = resolveConfigSHA256("prover", strings.ToUpper(want), path)
	if err != nil {
		t.Fatalf("resolve config hash with explicit hash: %v", err)
	}
	if got != want {
		t.Fatalf("expected canonical lowercase hash, got %s", got)
	}
}

func TestResolveConfigSHA256RejectsMalformedOrMismatchedHash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chain.json")
	if err := os.WriteFile(path, []byte(`{"block_time":"1s"}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := resolveConfigSHA256("chain", "not-a-hash", ""); err == nil {
		t.Fatal("expected malformed hash error")
	}
	if _, err := resolveConfigSHA256("chain", strings.Repeat("0", 64), path); err == nil {
		t.Fatal("expected mismatched hash error")
	}
}

func TestRenderMarkdownIncludesBenchmarkTable(t *testing.T) {
	out := renderMarkdown(report{
		SchemaVersion: reportSchemaVersion,
		GeneratedAt:   "2026-06-12T00:00:00Z",
		ResultFamily:  "privacy-circuits",
		SourceFiles:   []string{"benchmarks/privacy-circuits/raw.txt"},
		SourceFileSHA256: map[string]string{
			"benchmarks/privacy-circuits/raw.txt": strings.Repeat("a", 64),
		},
		RunStartedAt: "2026-06-12T00:00:00Z",
		RunEndedAt:   "2026-06-12T00:01:00Z",
		Commit:       "abc123",
		Dirty:        true,
		ActiveSetID:  "privacy-v1",
		GoVersion:    "go1.24.0",
		GnarkVersion: "v0.14.0",
		GnarkCrypto:  "v0.18.0",
		OS:           "darwin",
		Arch:         "arm64",
		CPU:          "Apple M3 Max",
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
				Metrics: map[string]metricSummary{
					"proofs/sec": {Mean: 12, P50: 12, P95: 12, P99: 12, Min: 10, Max: 13},
				},
			},
		},
	})

	for _, want := range []string{
		"# Clairveil Privacy Benchmark Report",
		"dirty worktree",
		"result_family: `privacy-circuits`",
		"source_files: `benchmarks/privacy-circuits/raw.txt`",
		"## Source Files",
		"claim_eligible: `false`",
		"artifact_files_verified: `false`",
		"| `BenchmarkDepositCircuitVerify` | `native_verification` | 2 | 50.000ms |",
		"## Custom Metrics",
		"| `BenchmarkDepositCircuitVerify` | `proofs/sec` | 12 |",
		"Do not infer chain TPS",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered markdown missing %q:\n%s", want, out)
		}
	}
}

func TestRenderMarkdownSkipsGoTableForStructuredOnlyMetrics(t *testing.T) {
	out := renderMarkdown(report{
		SchemaVersion: reportSchemaVersion,
		GeneratedAt:   "2026-06-12T00:00:00Z",
		Commit:        "abc123",
		ActiveSetID:   "privacy-v1",
		GoVersion:     "go1.24.0",
		OS:            "darwin",
		Arch:          "arm64",
		Benchmarks: []benchmarkSummary{
			{
				Name:       "ProverLoadTransferC8",
				Samples:    600,
				MetricKind: "prover_load",
				Metrics: map[string]metricSummary{
					"proofs/sec": {Mean: 80, P50: 80, P95: 78, P99: 75, Min: 70, Max: 85},
				},
			},
		},
	})

	if strings.Contains(out, "| Benchmark | Kind | Samples | Mean |") {
		t.Fatalf("structured-only metrics should not render the Go benchmark table:\n%s", out)
	}
	if strings.Contains(out, "0.000ns") {
		t.Fatalf("structured-only metrics should not render zero ns/op values:\n%s", out)
	}
	if !strings.Contains(out, "## Custom Metrics") || !strings.Contains(out, "| `ProverLoadTransferC8` | `proofs/sec` | 80 |") {
		t.Fatalf("structured metrics missing custom metrics table:\n%s", out)
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

func TestBuildAggregateReportCombinesEligibleComponents(t *testing.T) {
	dir := t.TempDir()
	prover := completePublicProverReport()
	prover.ClaimProfile = evaluateClaimProfile(prover)
	if !prover.ClaimProfile.Eligible {
		t.Fatalf("prover fixture is not eligible: %+v", prover.ClaimProfile)
	}
	chain := completePublicChainReport()
	chain.ClaimProfile = evaluateClaimProfile(chain)
	if !chain.ClaimProfile.Eligible {
		t.Fatalf("chain fixture is not eligible: %+v", chain.ClaimProfile)
	}
	proverPath := filepath.Join(dir, "prover.json")
	chainPath := filepath.Join(dir, "chain.json")
	if err := writeJSON(proverPath, prover); err != nil {
		t.Fatalf("write prover report: %v", err)
	}
	if err := writeJSON(chainPath, chain); err != nil {
		t.Fatalf("write chain report: %v", err)
	}
	proverSHA, err := fileSHA256(proverPath)
	if err != nil {
		t.Fatalf("hash prover report: %v", err)
	}
	chainSHA, err := fileSHA256(chainPath)
	if err != nil {
		t.Fatalf("hash chain report: %v", err)
	}

	rep, err := buildAggregateReport(
		[]string{proverPath, chainPath},
		"2026-06-12T00:20:00Z",
		"abc123",
		false,
		"public_claim",
		environment{MachineProfile: "m5-pro-reference", CPUGovernor: "high-power", MemoryGiB: "48"},
	)
	if err != nil {
		t.Fatalf("build aggregate report: %v", err)
	}
	if rep.ResultFamily != "public-capacity" {
		t.Fatalf("unexpected result family %q", rep.ResultFamily)
	}
	if !rep.ClaimProfile.Eligible {
		t.Fatalf("aggregate report should be eligible when all component claims are eligible: %+v", rep.ClaimProfile.BlockingReasons)
	}
	if !containsString(rep.ClaimProfile.ClaimTypes, "prover_rps") || !containsString(rep.ClaimProfile.ClaimTypes, "chain_tps") {
		t.Fatalf("aggregate claim types missing: %+v", rep.ClaimProfile.ClaimTypes)
	}
	if len(rep.ClaimProfile.BlockingReasons) != 0 {
		t.Fatalf("unexpected aggregate blockers: %+v", rep.ClaimProfile.BlockingReasons)
	}
	if rep.ClaimEvidenceByType["prover_rps"].ProverConfigSHA256 != prover.ClaimEvidence.ProverConfigSHA256 {
		t.Fatalf("prover evidence not retained: %+v", rep.ClaimEvidenceByType)
	}
	if rep.ClaimEvidenceByType["chain_tps"].ChainConfigSHA256 != chain.ClaimEvidence.ChainConfigSHA256 {
		t.Fatalf("chain evidence not retained: %+v", rep.ClaimEvidenceByType)
	}
	if len(rep.ComponentReports) != 2 {
		t.Fatalf("expected two component reports, got %+v", rep.ComponentReports)
	}
	if rep.ComponentReports[0].SHA256 != proverSHA || rep.ComponentReports[1].SHA256 != chainSHA {
		t.Fatalf("component hashes not retained: %+v", rep.ComponentReports)
	}
	if rep.SourceFileSHA256[proverPath] != proverSHA || rep.SourceFileSHA256[chainPath] != chainSHA {
		t.Fatalf("aggregate source hashes not retained: %+v", rep.SourceFileSHA256)
	}
	if len(rep.Benchmarks) != len(prover.Benchmarks)+len(chain.Benchmarks) {
		t.Fatalf("aggregate benchmark rows missing: got %d", len(rep.Benchmarks))
	}

	md := renderMarkdown(rep)
	if !strings.Contains(md, "## Claim Evidence By Type") || !strings.Contains(md, "| `chain_tps` | `mixed-shielded` |") {
		t.Fatalf("claim evidence by type markdown missing:\n%s", md)
	}
	if !strings.Contains(md, "## Component Reports") || !strings.Contains(md, "| `"+proverPath+"` | `privacy-proverd-load` | `prover_rps` | `true` |") {
		t.Fatalf("component report markdown missing:\n%s", md)
	}
}

func TestSummarizeFees(t *testing.T) {
	success := true
	failed := false
	summaries, err := summarizeFees([]txMetric{
		{TxType: "deposit", GasUsed: 100, GasWanted: 200, Success: &success},
		{TxType: "deposit", GasUsed: 200, GasWanted: 300, Success: &success},
		{TxType: "deposit", GasUsed: 999, GasWanted: 999, Success: &failed},
		{TxType: "deposit", GasUsed: 888, GasWanted: 888},
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
	if got.Samples != 2 || got.FailedSamples != 2 {
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

func TestReadTxMetricsRejectsEmptyInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tx-metrics.json")
	if err := os.WriteFile(path, []byte(`{"transactions":[]}`), 0o644); err != nil {
		t.Fatalf("write tx metrics: %v", err)
	}

	_, err := readTxMetrics(path)
	if err == nil || !strings.Contains(err.Error(), "tx metrics are empty") {
		t.Fatalf("expected empty tx metrics error, got %v", err)
	}
}

func TestReadBenchmarkSummariesAcceptsStructuredEnvelope(t *testing.T) {
	path := filepath.Join(t.TempDir(), "summaries.json")
	err := os.WriteFile(path, []byte(`{
  "benchmarks": [
    {
      "name": "ProverLoadTransfer",
      "samples": 600,
      "metric_kind": "prover_load",
      "metric_summaries": {
        "proofs/sec": {"mean": 12, "p50": 12, "p95": 12, "p99": 12, "min": 10, "max": 13}
      }
    }
  ]
}`), 0o644)
	if err != nil {
		t.Fatalf("write benchmark summaries: %v", err)
	}

	summaries, err := readBenchmarkSummaries(path)
	if err != nil {
		t.Fatalf("read benchmark summaries: %v", err)
	}
	if len(summaries) != 1 || summaries[0].Name != "ProverLoadTransfer" {
		t.Fatalf("unexpected benchmark summaries: %+v", summaries)
	}
	if summaries[0].Metrics["proofs/sec"].Mean != 12 {
		t.Fatalf("custom metric summary not retained: %+v", summaries[0].Metrics)
	}
}

func TestReadBenchmarkSummariesRequiresPositiveSamples(t *testing.T) {
	path := filepath.Join(t.TempDir(), "summaries.json")
	err := os.WriteFile(path, []byte(`[
  {
    "name": "ProverLoadTransfer",
    "samples": 0,
    "metric_kind": "prover_load",
    "metric_summaries": {
      "proofs/sec": {"mean": 12, "p50": 12, "p95": 12, "p99": 12, "min": 10, "max": 13}
    }
  }
]`), 0o644)
	if err != nil {
		t.Fatalf("write benchmark summaries: %v", err)
	}

	_, err = readBenchmarkSummaries(path)
	if err == nil || !strings.Contains(err.Error(), `benchmark summary "ProverLoadTransfer" has non-positive samples`) {
		t.Fatalf("expected non-positive samples error, got %v", err)
	}
}

func TestReadBenchmarkSummariesRequiresMetricSummaries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "summaries.json")
	err := os.WriteFile(path, []byte(`[{"name":"ProverLoadTransfer","metric_kind":"prover_load"}]`), 0o644)
	if err != nil {
		t.Fatalf("write benchmark summaries: %v", err)
	}

	if _, err := readBenchmarkSummaries(path); err == nil {
		t.Fatal("expected missing metric_summaries error")
	}
}

func TestReadBenchmarkSummariesRequiresCompleteMetricStats(t *testing.T) {
	path := filepath.Join(t.TempDir(), "summaries.json")
	err := os.WriteFile(path, []byte(`[
	  {
	    "name": "ProverLoadTransfer",
	    "samples": 600,
	    "metric_kind": "prover_load",
	    "metric_summaries": {
      "error_rate": {"mean": 0.02, "p50": 0.02, "p95": 0.02, "p99": 0.02, "min": 0.02}
    }
  }
]`), 0o644)
	if err != nil {
		t.Fatalf("write benchmark summaries: %v", err)
	}

	_, err = readBenchmarkSummaries(path)
	if err == nil || !strings.Contains(err.Error(), `metric "error_rate" is missing max`) {
		t.Fatalf("expected missing max error, got %v", err)
	}
}

func TestReadBenchmarkSummariesRejectsNullMetricStats(t *testing.T) {
	path := filepath.Join(t.TempDir(), "summaries.json")
	err := os.WriteFile(path, []byte(`[
	  {
	    "name": "ProverLoadTransfer",
	    "samples": 600,
	    "metric_kind": "prover_load",
	    "metric_summaries": {
      "error_rate": {"mean": null, "p50": 0, "p95": 0, "p99": 0, "min": 0, "max": 0}
    }
  }
]`), 0o644)
	if err != nil {
		t.Fatalf("write benchmark summaries: %v", err)
	}

	_, err = readBenchmarkSummaries(path)
	if err == nil || !strings.Contains(err.Error(), `metric "error_rate" has non-numeric mean`) {
		t.Fatalf("expected non-numeric mean error, got %v", err)
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

func TestSourceStatusDirtyIgnoresGeneratedArtifactsOnly(t *testing.T) {
	status := strings.Join([]string{
		"?? benchmarks/privacy-circuits/latest.json",
		"?? benchmarks/privacy-proverd/raw.txt",
		"?? clairveil-benchreport",
		"?? clairveil-localnetload",
	}, "\n")
	if sourceStatusDirty(status) {
		t.Fatalf("expected generated benchmark files and build artifacts to be ignored")
	}

	status = strings.Join([]string{
		"?? benchmarks/privacy-circuits/latest.json",
		" M benchmarks/README.md",
	}, "\n")
	if !sourceStatusDirty(status) {
		t.Fatalf("expected non-generated benchmark file to mark source dirty")
	}

	status = strings.Join([]string{
		" M benchmarks/privacy-circuits/latest.json",
	}, "\n")
	if !sourceStatusDirty(status) {
		t.Fatalf("expected tracked generated benchmark edit to mark source dirty")
	}

	status = strings.Join([]string{
		" M clairveil-benchreport",
	}, "\n")
	if !sourceStatusDirty(status) {
		t.Fatalf("expected tracked build artifact edit to mark source dirty")
	}

	status = strings.Join([]string{
		"?? clairveil-benchreport.tmp",
	}, "\n")
	if !sourceStatusDirty(status) {
		t.Fatalf("expected build-artifact-like untracked file to mark source dirty")
	}
}

func TestParseRunProfileRejectsUnknownValue(t *testing.T) {
	if _, err := parseRunProfile("public-claim"); err == nil {
		t.Fatal("expected invalid run profile error")
	}
}

func TestArtifactDescriptorIssuesRequireCompleteChecksums(t *testing.T) {
	descriptors := privacyzk.DefaultArtifactDescriptors()
	for i := range descriptors {
		descriptors[i].SHA256 = strings.Repeat("a", 64)
	}
	if issues := artifactDescriptorIssues(descriptors); len(issues) != 0 {
		t.Fatalf("expected complete descriptors, got %v", issues)
	}

	descriptors[0].SHA256 = ""
	issues := artifactDescriptorIssues(descriptors)
	if !containsString(issues, "missing sha256 for privacy_deposit_r1cs.bin") {
		t.Fatalf("expected missing sha issue, got %v", issues)
	}
}

func TestArtifactFileIssuesRequireMatchingFiles(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "privacy_deposit_r1cs.bin")
	if err := os.WriteFile(artifactPath, []byte("artifact"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	sum, err := fileSHA256(artifactPath)
	if err != nil {
		t.Fatalf("hash artifact: %v", err)
	}

	descriptors := []privacyzk.ArtifactDescriptor{
		{Filename: "privacy_deposit_r1cs.bin", SHA256: sum},
		{Filename: "privacy_spend_r1cs.bin", SHA256: strings.Repeat("a", 64)},
		{Filename: "../escape.bin", SHA256: strings.Repeat("b", 64)},
	}
	issues := artifactFileIssues(dir, descriptors)
	if !containsString(issues, "cannot hash privacy_spend_r1cs.bin: open "+filepath.Join(dir, "privacy_spend_r1cs.bin")+": no such file or directory") {
		t.Fatalf("expected missing file issue, got %v", issues)
	}
	if !containsString(issues, `unsafe artifact path "../escape.bin"`) {
		t.Fatalf("expected unsafe path issue, got %v", issues)
	}

	if issues := artifactFileIssues(dir, descriptors[:1]); len(issues) != 0 {
		t.Fatalf("expected matching artifact file, got %v", issues)
	}
}

func TestEvaluateClaimProfileBlocksSmokeAndMissingPublicMetadata(t *testing.T) {
	rep := report{
		Dirty:       false,
		ActiveSetID: "privacy-accounting-v2",
		ClaimProfile: claimProfile{
			RunProfile: "smoke",
		},
		ArtifactSet: artifactSet{ActiveSetID: "privacy-accounting-v2"},
	}
	smoke := evaluateClaimProfile(rep)
	if smoke.Eligible || !containsString(smoke.BlockingReasons, "run_profile is smoke") {
		t.Fatalf("unexpected smoke profile: %+v", smoke)
	}

	rep.ClaimProfile = claimProfile{
		RunProfile: "public_claim",
		ClaimTypes: []string{"chain_tps"},
	}
	public := evaluateClaimProfile(rep)
	if public.Eligible {
		t.Fatalf("expected missing public metadata to block eligibility")
	}
	for _, want := range []string{
		"artifact manifest checksum is missing",
		"result_family is required for public_claim",
		"source_files is required for public_claim",
		"run_started_at is required for public_claim",
		"run_ended_at is required for public_claim",
		"machine_profile is required for public_claim",
		"cpu_governor is required for public_claim",
		"memory_gib is required for public_claim",
	} {
		if !containsString(public.BlockingReasons, want) {
			t.Fatalf("missing blocking reason %q in %+v", want, public.BlockingReasons)
		}
	}
}

func TestEvaluateClaimProfileAllowsCompletePublicMetadata(t *testing.T) {
	rep := report{
		Dirty:        false,
		ResultFamily: "privacy-proverd-load",
		SourceFiles: []string{
			"benchmarks/privacy-proverd-load/latest.json",
			"benchmarks/privacy-proverd-load/prover-config.json",
			"benchmarks/privacy-proverd-load/saturation-profile.json",
		},
		SourceFileSHA256: map[string]string{
			"benchmarks/privacy-proverd-load/latest.json":             strings.Repeat("a", 64),
			"benchmarks/privacy-proverd-load/prover-config.json":      strings.Repeat("b", 64),
			"benchmarks/privacy-proverd-load/saturation-profile.json": strings.Repeat("c", 64),
		},
		RunStartedAt: "2026-06-12T00:00:00Z",
		RunEndedAt:   "2026-06-12T00:10:01Z",
		ActiveSetID:  "privacy-accounting-v2",
		ClaimProfile: claimProfile{
			RunProfile: "public_claim",
			ClaimTypes: []string{"prover_rps"},
		},
		Environment: environment{
			MachineProfile: "m5-pro-reference",
			CPUGovernor:    "high-power",
			MemoryGiB:      "48",
		},
		ArtifactSet: artifactSet{
			ActiveSetID:           "privacy-accounting-v2",
			ManifestActiveSetID:   "privacy-accounting-v2",
			ManifestSHA256:        strings.Repeat("1", 64),
			DescriptorComplete:    true,
			ArtifactFilesVerified: true,
		},
		ClaimEvidence: claimEvidence{
			SteadyStateSeconds:      600,
			LoadProfile:             "transfer_only",
			PreflightMode:           "strict",
			AuthEnabled:             "true",
			InstanceProfile:         "m5-pro-reference",
			ProverConfigFile:        "benchmarks/privacy-proverd-load/prover-config.json",
			ProverConfigSHA256:      strings.Repeat("b", 64),
			LatencyP99SLOMS:         200,
			RSSStable:               "true",
			SaturationProfile:       "transfer-sweep-1-32",
			SaturationProfileFile:   "benchmarks/privacy-proverd-load/saturation-profile.json",
			SaturationProfileSHA256: strings.Repeat("c", 64),
		},
		Benchmarks: []benchmarkSummary{
			{
				Name:            "BenchmarkProverLoadTransferC8",
				Samples:         600,
				ClaimType:       "prover_rps",
				LoadProfile:     "transfer_only",
				Route:           "transfer",
				Concurrency:     8,
				WarmupSeconds:   60,
				DurationSeconds: 600,
				Metrics: map[string]metricSummary{
					"proofs/sec":    {Mean: 10, P50: 10, P95: 10, P99: 10, Min: 9, Max: 11},
					"latency_ms":    {Mean: 100, P50: 90, P95: 120, P99: 150, Min: 80, Max: 180},
					"errors/op":     {Mean: 0, P50: 0, P95: 0, P99: 0, Max: 0},
					"cpu_percent":   {Mean: 80, P50: 80, P95: 85, P99: 90, Min: 70, Max: 92},
					"max_rss_bytes": {Mean: 1024, P50: 1024, P95: 2048, P99: 4096, Min: 1024, Max: 4096},
				},
			},
			{
				Name:            "BenchmarkProverLoadTransferC16",
				Samples:         600,
				ClaimType:       "prover_rps",
				LoadProfile:     "transfer_only",
				Route:           "transfer",
				Concurrency:     16,
				WarmupSeconds:   60,
				DurationSeconds: 600,
				Metrics: map[string]metricSummary{
					"proofs/sec":    {Mean: 18, P50: 18, P95: 17, P99: 16, Min: 15, Max: 19},
					"latency_ms":    {Mean: 120, P50: 110, P95: 150, P99: 180, Min: 90, Max: 190},
					"errors/op":     {Mean: 0, P50: 0, P95: 0, P99: 0, Min: 0, Max: 0},
					"cpu_percent":   {Mean: 88, P50: 88, P95: 92, P99: 95, Min: 80, Max: 96},
					"max_rss_bytes": {Mean: 2048, P50: 2048, P95: 4096, P99: 4096, Min: 2048, Max: 4096},
				},
			},
		},
	}

	profile := evaluateClaimProfile(rep)
	if !profile.Eligible || len(profile.BlockingReasons) != 0 {
		t.Fatalf("expected public metadata to be eligible, got %+v", profile)
	}
}

func TestEvaluateClaimProfileRejectsMalformedManifestChecksum(t *testing.T) {
	rep := completePublicProverReport()
	rep.ArtifactSet.ManifestSHA256 = "abc123"

	profile := evaluateClaimProfile(rep)
	if profile.Eligible {
		t.Fatalf("expected public claim to be blocked")
	}
	if !containsString(profile.BlockingReasons, "artifact manifest checksum must be 64hex") {
		t.Fatalf("expected malformed manifest checksum blocker, got %+v", profile.BlockingReasons)
	}
}

func TestEvaluateClaimProfileBlocksNegativeRateMetric(t *testing.T) {
	rep := completePublicProverReport()
	rep.Benchmarks[0].Metrics["errors/op"] = metricSummary{
		Mean: -0.01,
		P50:  -0.01,
		P95:  -0.01,
		P99:  -0.01,
		Min:  -0.01,
		Max:  -0.01,
	}

	profile := evaluateClaimProfile(rep)
	if profile.Eligible {
		t.Fatalf("expected public claim to be blocked")
	}
	if !containsString(profile.BlockingReasons, "prover_rps metrics invalid: BenchmarkProverLoadTransfer/errors/op min -0.010000 below 0.000000") {
		t.Fatalf("expected negative rate blocker, got %+v", profile.BlockingReasons)
	}
}

func TestEvaluateClaimProfileRequiresPositiveSamples(t *testing.T) {
	rep := completePublicProverReport()
	rep.Benchmarks[0].Samples = 0

	profile := evaluateClaimProfile(rep)
	if profile.Eligible {
		t.Fatalf("expected public claim to be blocked")
	}
	if !containsString(profile.BlockingReasons, "benchmark samples invalid: BenchmarkProverLoadTransfer samples must be positive") {
		t.Fatalf("expected sample count blocker, got %+v", profile.BlockingReasons)
	}
}

func TestEvaluateClaimProfileRequiresClaimTypedBenchmarkRows(t *testing.T) {
	rep := completePublicProverReport()
	for i := range rep.Benchmarks {
		rep.Benchmarks[i].ClaimType = ""
	}

	profile := evaluateClaimProfile(rep)
	if profile.Eligible {
		t.Fatalf("expected public claim to be blocked")
	}
	if !containsString(profile.BlockingReasons, "prover_rps benchmark rows invalid: at least one benchmark summary must set claim_type=prover_rps") {
		t.Fatalf("expected missing claim_type blocker, got %+v", profile.BlockingReasons)
	}
}

func TestEvaluateClaimProfileRequiresUserLatencyBucketMetadata(t *testing.T) {
	rep := completePublicUserLatencyReport()
	rep.Benchmarks[0].FlowProfile = ""
	rep.Benchmarks[0].ColdWarm = ""

	profile := evaluateClaimProfile(rep)
	if profile.Eligible {
		t.Fatalf("expected public claim to be blocked")
	}
	if !containsString(profile.BlockingReasons, "user_latency benchmark rows invalid: UserLatencyTransferWarm flow_profile is required, UserLatencyTransferWarm cold_warm is required") {
		t.Fatalf("expected user latency bucket metadata blockers, got %+v", profile.BlockingReasons)
	}
}

func TestEvaluateClaimProfileScopesMetricsToClaimRows(t *testing.T) {
	rep := completePublicProverReport()
	for i := range rep.Benchmarks {
		rep.Benchmarks[i].ClaimType = "user_latency"
	}
	rep.Benchmarks = append(rep.Benchmarks, benchmarkSummary{
		Name:            "ProverMetadataOnly",
		Samples:         600,
		ClaimType:       "prover_rps",
		LoadProfile:     "transfer_only",
		Route:           "transfer",
		Concurrency:     8,
		WarmupSeconds:   60,
		DurationSeconds: 600,
		Metrics: map[string]metricSummary{
			"proofs/sec": {Mean: 10, P50: 10, P95: 10, P99: 10, Min: 9, Max: 11},
		},
	})

	profile := evaluateClaimProfile(rep)
	if profile.Eligible {
		t.Fatalf("expected public claim to be blocked")
	}
	if !containsString(profile.BlockingReasons, "prover_rps metrics missing: latency_ms|proof_latency_ms|roundtrip_latency_ms, errors/op|error_rate, cpu_percent, rss_bytes|max_rss_bytes") {
		t.Fatalf("expected prover metrics to be scoped to prover rows, got %+v", profile.BlockingReasons)
	}
}

func TestEvaluateClaimProfileRequiresProverConcurrencySweep(t *testing.T) {
	rep := completePublicProverReport()
	rep.Benchmarks = rep.Benchmarks[:1]

	profile := evaluateClaimProfile(rep)
	if profile.Eligible {
		t.Fatalf("expected public claim to be blocked")
	}
	if !containsString(profile.BlockingReasons, "prover_rps sweep invalid: at least two concurrency buckets are required") {
		t.Fatalf("expected prover sweep blocker, got %+v", profile.BlockingReasons)
	}
}

func TestEvaluateClaimProfileRequiresUserLatencyMinimumSamples(t *testing.T) {
	rep := completePublicUserLatencyReport()
	rep.Benchmarks[0].Samples = minPublicUserLatencySamples - 1

	profile := evaluateClaimProfile(rep)
	if profile.Eligible {
		t.Fatalf("expected public claim to be blocked")
	}
	if !containsString(profile.BlockingReasons, "benchmark samples invalid: UserLatencyTransferWarm samples must be >= 100 for user_latency") {
		t.Fatalf("expected user latency sample count blocker, got %+v", profile.BlockingReasons)
	}
}

func TestEvaluateClaimProfileRequiresUserLatencySegmentMetrics(t *testing.T) {
	rep := completePublicUserLatencyReport()
	delete(rep.Benchmarks[0].Metrics, "prepare_latency_ms")
	delete(rep.Benchmarks[0].Metrics, "time_to_submit_ms")

	profile := evaluateClaimProfile(rep)
	if profile.Eligible {
		t.Fatalf("expected public claim to be blocked")
	}
	if !containsString(profile.BlockingReasons, "user_latency metrics missing: prepare_latency_ms, time_to_submit_ms|submit_latency_ms") {
		t.Fatalf("expected user latency segment metric blockers, got %+v", profile.BlockingReasons)
	}
}

func TestEvaluateClaimProfileRequiresUserLatencyErrorRateMetric(t *testing.T) {
	rep := completePublicUserLatencyReport()
	delete(rep.Benchmarks[0].Metrics, "error_rate")

	profile := evaluateClaimProfile(rep)
	if profile.Eligible {
		t.Fatalf("expected public claim to be blocked")
	}
	if !containsString(profile.BlockingReasons, "user_latency metrics missing: error_rate") {
		t.Fatalf("expected user latency error rate blocker, got %+v", profile.BlockingReasons)
	}
}

func TestEvaluateClaimProfileRejectsUserLatencyErrorRate(t *testing.T) {
	rep := completePublicUserLatencyReport()
	rep.Benchmarks[0].Metrics["error_rate"] = metricSummary{
		Mean: 0.01,
		P50:  0.01,
		P95:  0.01,
		P99:  0.01,
		Min:  0.01,
		Max:  0.01,
	}

	profile := evaluateClaimProfile(rep)
	if profile.Eligible {
		t.Fatalf("expected public claim to be blocked")
	}
	if !containsString(profile.BlockingReasons, "user_latency metrics invalid: UserLatencyTransferWarm/error_rate max 0.010000 exceeds 0.001000") {
		t.Fatalf("expected user latency error rate blocker, got %+v", profile.BlockingReasons)
	}
}

func TestEvaluateClaimProfileRequiresUserLatencyMetricsOnSameRow(t *testing.T) {
	rep := completePublicUserLatencyReport()
	rep.Benchmarks = []benchmarkSummary{
		{
			Name:        "UserLatencyPrepareAndProofOnly",
			Samples:     minPublicUserLatencySamples,
			ClaimType:   "user_latency",
			FlowProfile: "transfer_all_private",
			LatencyMode: "native",
			ColdWarm:    "warm",
			Metrics: map[string]metricSummary{
				"prepare_latency_ms": {Mean: 20, P50: 18, P95: 28, P99: 32, Min: 10, Max: 40},
				"proof_latency_ms":   {Mean: 150, P50: 140, P95: 210, P99: 260, Min: 120, Max: 280},
				"error_rate":         {Mean: 0, P50: 0, P95: 0, P99: 0, Min: 0, Max: 0},
			},
		},
		{
			Name:        "UserLatencySubmitOnly",
			Samples:     minPublicUserLatencySamples,
			ClaimType:   "user_latency",
			FlowProfile: "transfer_all_private",
			LatencyMode: "native",
			ColdWarm:    "warm",
			Metrics: map[string]metricSummary{
				"time_to_submit_ms": {Mean: 30, P50: 25, P95: 45, P99: 55, Min: 15, Max: 60},
				"submit_ready_ms":   {Mean: 200, P50: 180, P95: 260, P99: 320, Min: 150, Max: 350},
				"error_rate":        {Mean: 0, P50: 0, P95: 0, P99: 0, Min: 0, Max: 0},
				"timeout_rate":      {Mean: 0, P50: 0, P95: 0, P99: 0, Min: 0, Max: 0},
			},
		},
	}

	profile := evaluateClaimProfile(rep)
	if profile.Eligible {
		t.Fatalf("expected public claim to be blocked")
	}
	if !containsString(profile.BlockingReasons, "user_latency metric rows incomplete: UserLatencyPrepareAndProofOnly missing time_to_submit_ms|submit_latency_ms, total_latency_ms|submit_ready_ms, timeout_rate|cancel_rate, UserLatencySubmitOnly missing prepare_latency_ms, proof_latency_ms") {
		t.Fatalf("expected row completeness blocker, got %+v", profile.BlockingReasons)
	}
}

func TestEvaluateClaimProfileRejectsTaggedClaimRowWithoutMetrics(t *testing.T) {
	rep := completePublicChainReport()
	rep.Benchmarks = append(rep.Benchmarks, benchmarkSummary{
		Name:            "ChainTPSEmptyManualRow",
		Samples:         600,
		ClaimType:       "chain_tps",
		LoadProfile:     "mixed-shielded",
		DurationSeconds: 600,
		TargetTxPerSec:  16,
	})

	profile := evaluateClaimProfile(rep)
	if profile.Eligible {
		t.Fatalf("expected public claim to be blocked")
	}
	if !containsString(profile.BlockingReasons, "chain_tps metric rows incomplete: ChainTPSEmptyManualRow missing tx/sec|tps|successful_tx/sec, inclusion_latency_ms, failed_tx_rate") {
		t.Fatalf("expected empty tagged row blocker, got %+v", profile.BlockingReasons)
	}
}

func TestEvaluateClaimProfileDoesNotApplyUserLatencySampleFloorToProverOnlyRows(t *testing.T) {
	rep := completePublicUserLatencyReport()
	rep.Benchmarks = append(rep.Benchmarks, benchmarkSummary{
		Name:      "ProverOnlyLatencyProbe",
		Samples:   1,
		ClaimType: "prover_rps",
		Metrics: map[string]metricSummary{
			"proof_latency_ms": {Mean: 150, P50: 140, P95: 210, P99: 260, Min: 120, Max: 280},
		},
	})

	profile := evaluateClaimProfile(rep)
	if !profile.Eligible || len(profile.BlockingReasons) != 0 {
		t.Fatalf("expected prover-only proof latency row not to trigger user latency sample floor, got %+v", profile)
	}
}

func TestEvaluateClaimProfileBlocksNegativePositiveMetricExtrema(t *testing.T) {
	rep := completePublicProverReport()
	rep.Benchmarks[0].Metrics["latency_ms"] = metricSummary{
		Mean: 100,
		P50:  90,
		P95:  120,
		P99:  150,
		Min:  -1,
		Max:  180,
	}

	profile := evaluateClaimProfile(rep)
	if profile.Eligible {
		t.Fatalf("expected public claim to be blocked")
	}
	if !containsString(profile.BlockingReasons, "prover_rps metrics invalid: BenchmarkProverLoadTransfer/latency_ms must have positive mean/p50/p95/p99/min/max") {
		t.Fatalf("expected positive metric extrema blocker, got %+v", profile.BlockingReasons)
	}
}

func TestEvaluateClaimProfileRequiresFailedTxRateForChainTPS(t *testing.T) {
	rep := completePublicChainReport()
	for i := range rep.Benchmarks {
		rep.Benchmarks[i].Metrics["failed_tx_count"] = metricSummary{Mean: 0, P50: 0, P95: 0, P99: 0, Min: 0, Max: 0}
		delete(rep.Benchmarks[i].Metrics, "failed_tx_rate")
	}

	profile := evaluateClaimProfile(rep)
	if profile.Eligible {
		t.Fatalf("expected public claim to be blocked")
	}
	if !containsString(profile.BlockingReasons, "chain_tps metrics missing: failed_tx_rate") {
		t.Fatalf("expected missing failed_tx_rate blocker, got %+v", profile.BlockingReasons)
	}
}

func TestEvaluateClaimProfileRequiresChainTargetTPSSweep(t *testing.T) {
	rep := completePublicChainReport()
	rep.Benchmarks = rep.Benchmarks[:1]

	profile := evaluateClaimProfile(rep)
	if profile.Eligible {
		t.Fatalf("expected public claim to be blocked")
	}
	if !containsString(profile.BlockingReasons, "chain_tps sweep invalid: at least two target_tx_per_sec buckets are required") {
		t.Fatalf("expected chain target sweep blocker, got %+v", profile.BlockingReasons)
	}
}

func TestEvaluateClaimProfileRequiresConfigHashes(t *testing.T) {
	prover := completePublicProverReport()
	prover.ClaimEvidence.ProverConfigFile = ""
	proverProfile := evaluateClaimProfile(prover)
	if proverProfile.Eligible {
		t.Fatalf("expected prover public claim to be blocked")
	}
	if !containsString(proverProfile.BlockingReasons, "prover_rps evidence missing: prover_config_file") {
		t.Fatalf("expected missing prover config file blocker, got %+v", proverProfile.BlockingReasons)
	}

	prover = completePublicProverReport()
	prover.ClaimEvidence.ProverConfigSHA256 = ""
	proverProfile = evaluateClaimProfile(prover)
	if proverProfile.Eligible {
		t.Fatalf("expected prover public claim to be blocked")
	}
	if !containsString(proverProfile.BlockingReasons, "prover_rps evidence missing: prover_config_sha256") {
		t.Fatalf("expected missing prover config hash blocker, got %+v", proverProfile.BlockingReasons)
	}

	chain := completePublicChainReport()
	chain.ClaimEvidence.ChainConfigFile = ""
	chainProfile := evaluateClaimProfile(chain)
	if chainProfile.Eligible {
		t.Fatalf("expected chain public claim to be blocked")
	}
	if !containsString(chainProfile.BlockingReasons, "chain_tps evidence missing: chain_config_file") {
		t.Fatalf("expected missing chain config file blocker, got %+v", chainProfile.BlockingReasons)
	}

	chain = completePublicChainReport()
	chain.ClaimEvidence.ChainConfigSHA256 = ""
	chainProfile = evaluateClaimProfile(chain)
	if chainProfile.Eligible {
		t.Fatalf("expected chain public claim to be blocked")
	}
	if !containsString(chainProfile.BlockingReasons, "chain_tps evidence missing: chain_config_sha256") {
		t.Fatalf("expected missing chain config hash blocker, got %+v", chainProfile.BlockingReasons)
	}
}

func TestEvaluateClaimProfileRequiresProverSaturationEvidenceFile(t *testing.T) {
	rep := completePublicProverReport()
	rep.ClaimEvidence.SaturationProfileFile = ""
	rep.ClaimEvidence.SaturationProfileSHA256 = ""

	profile := evaluateClaimProfile(rep)
	if profile.Eligible {
		t.Fatalf("expected public claim to be blocked")
	}
	if !containsString(profile.BlockingReasons, "prover_rps evidence missing: saturation_profile_file, saturation_profile_sha256") {
		t.Fatalf("expected prover saturation evidence blocker, got %+v", profile.BlockingReasons)
	}
}

func TestEvaluateClaimProfileRequiresChainReserveAndWindowEvidence(t *testing.T) {
	rep := completePublicChainReport()
	rep.ClaimEvidence.ThroughputWindowSeconds = 0
	rep.ClaimEvidence.ReserveSnapshotBeforeFile = ""
	rep.ClaimEvidence.ReserveSnapshotBeforeSHA256 = ""
	rep.ClaimEvidence.ReserveSnapshotAfterFile = ""
	rep.ClaimEvidence.ReserveSnapshotAfterSHA256 = ""

	profile := evaluateClaimProfile(rep)
	if profile.Eligible {
		t.Fatalf("expected public claim to be blocked")
	}
	if !containsString(profile.BlockingReasons, "chain_tps evidence missing: throughput_window_seconds, reserve_snapshot_before_file, reserve_snapshot_before_sha256, reserve_snapshot_after_file, reserve_snapshot_after_sha256") {
		t.Fatalf("expected chain reserve/window evidence blockers, got %+v", profile.BlockingReasons)
	}
}

func TestEvaluateClaimProfileRequiresRemoteUserLatencyProverConfig(t *testing.T) {
	rep := completePublicUserLatencyReport()
	rep.ClaimEvidence.LatencyMode = "remote"
	rep.ClaimEvidence.RemoteTopology = "proverd-a:443"
	rep.ClaimEvidence.InstanceProfile = "m5-pro-reference"
	rep.ClaimEvidence.LinkedProverReportFile = "benchmarks/privacy-proverd-load/latest.json"
	rep.ClaimEvidence.LinkedProverReportSHA256 = strings.Repeat("e", 64)
	rep.SourceFiles = append(rep.SourceFiles, rep.ClaimEvidence.LinkedProverReportFile)
	rep.SourceFileSHA256[rep.ClaimEvidence.LinkedProverReportFile] = rep.ClaimEvidence.LinkedProverReportSHA256
	rep.ClaimEvidence.ProverConfigFile = ""
	rep.ClaimEvidence.ProverConfigSHA256 = strings.Repeat("d", 64)

	profile := evaluateClaimProfile(rep)
	if profile.Eligible {
		t.Fatalf("expected public claim to be blocked")
	}
	if !containsString(profile.BlockingReasons, "user_latency evidence missing: prover_config_file") {
		t.Fatalf("expected remote prover config blocker, got %+v", profile.BlockingReasons)
	}
}

func TestEvaluateClaimProfileRequiresRemoteUserLatencyLinkedProverReport(t *testing.T) {
	rep := completePublicUserLatencyReport()
	rep.ClaimEvidence.LatencyMode = "remote"
	rep.ClaimEvidence.RemoteTopology = "proverd-a:443"
	rep.ClaimEvidence.InstanceProfile = "m5-pro-reference"
	rep.ClaimEvidence.ProverConfigFile = "benchmarks/privacy-user-latency/prover-config.json"
	rep.ClaimEvidence.ProverConfigSHA256 = strings.Repeat("d", 64)
	rep.SourceFiles = append(rep.SourceFiles, rep.ClaimEvidence.ProverConfigFile)
	rep.SourceFileSHA256[rep.ClaimEvidence.ProverConfigFile] = rep.ClaimEvidence.ProverConfigSHA256

	profile := evaluateClaimProfile(rep)
	if profile.Eligible {
		t.Fatalf("expected public claim to be blocked")
	}
	if !containsString(profile.BlockingReasons, "user_latency evidence missing: linked_prover_report_file, linked_prover_report_sha256") {
		t.Fatalf("expected linked prover report blocker, got %+v", profile.BlockingReasons)
	}
}

func TestEvaluateClaimProfileValidatesRemoteUserLatencyLinkedProverReportSemantics(t *testing.T) {
	rep := completePublicUserLatencyReport()
	linked := completePublicProverReport()
	linkedPath, linkedSHA := writeLinkedProverReport(t, linked)
	attachRemoteUserLatencyEvidence(&rep, linkedPath, linkedSHA, linked)

	profile := evaluateClaimProfile(rep)
	if !profile.Eligible || len(profile.BlockingReasons) != 0 {
		t.Fatalf("expected remote user latency report with matching linked prover report to be eligible, got %+v", profile)
	}

	staleLinked := completePublicProverReport()
	staleLinked.ClaimEvidence.InstanceProfile = "different-instance"
	stalePath, staleSHA := writeLinkedProverReport(t, staleLinked)
	attachRemoteUserLatencyEvidence(&rep, stalePath, staleSHA, linked)

	profile = evaluateClaimProfile(rep)
	if profile.Eligible {
		t.Fatalf("expected public claim to be blocked")
	}
	if !containsString(profile.BlockingReasons, fmt.Sprintf(`user_latency source evidence invalid: linked_prover_report_file %q instance_profile "different-instance" does not match report instance_profile "m5-pro-reference"`, stalePath)) {
		t.Fatalf("expected linked report instance mismatch blocker, got %+v", profile.BlockingReasons)
	}
}

func TestEvaluateClaimProfileRequiresBrowserUserLatencyAdapterEvidence(t *testing.T) {
	rep := completePublicUserLatencyReport()
	rep.ClaimEvidence.LatencyMode = "browser"
	rep.ClaimEvidence.BrowserMatrix = "chrome-desktop"

	profile := evaluateClaimProfile(rep)
	if profile.Eligible {
		t.Fatalf("expected public claim to be blocked")
	}
	if !containsString(profile.BlockingReasons, "user_latency evidence missing: browser_adapter_ready=true, browser_adapter_version, browser_adapter_file, browser_adapter_sha256") {
		t.Fatalf("expected browser adapter blocker, got %+v", profile.BlockingReasons)
	}
}

func TestEvaluateClaimProfileRequiresEvidenceFilesInSourceFiles(t *testing.T) {
	rep := completePublicProverReport()
	rep.SourceFiles = []string{"benchmarks/privacy-proverd-load/latest.json"}
	delete(rep.SourceFileSHA256, rep.ClaimEvidence.ProverConfigFile)

	profile := evaluateClaimProfile(rep)
	if profile.Eligible {
		t.Fatalf("expected public claim to be blocked")
	}
	if !strings.Contains(strings.Join(profile.BlockingReasons, "\n"), `prover_config_file "benchmarks/privacy-proverd-load/prover-config.json" is not in source_files, prover_config_file "benchmarks/privacy-proverd-load/prover-config.json" is not in source_file_sha256`) {
		t.Fatalf("expected source evidence blocker, got %+v", profile.BlockingReasons)
	}
}

func TestEvaluateClaimProfileRequiresEvidenceSHA256ToMatchSourceFileHash(t *testing.T) {
	rep := completePublicProverReport()
	rep.SourceFileSHA256[rep.ClaimEvidence.ProverConfigFile] = strings.Repeat("c", 64)

	profile := evaluateClaimProfile(rep)
	if profile.Eligible {
		t.Fatalf("expected public claim to be blocked")
	}
	if !containsString(profile.BlockingReasons, `prover_rps source evidence invalid: prover_config_file "benchmarks/privacy-proverd-load/prover-config.json" source_file_sha256 does not match evidence SHA-256`) {
		t.Fatalf("expected source evidence SHA mismatch blocker, got %+v", profile.BlockingReasons)
	}

	chain := completePublicChainReport()
	chain.SourceFileSHA256[chain.ClaimEvidence.ReserveSnapshotAfterFile] = strings.Repeat("0", 64)
	chainProfile := evaluateClaimProfile(chain)
	if chainProfile.Eligible {
		t.Fatalf("expected chain public claim to be blocked")
	}
	if !containsString(chainProfile.BlockingReasons, `chain_tps source evidence invalid: reserve_snapshot_after_file "benchmarks/privacy-localnet-tps/reserve-after.json" source_file_sha256 does not match evidence SHA-256`) {
		t.Fatalf("expected reserve snapshot SHA mismatch blocker, got %+v", chainProfile.BlockingReasons)
	}
}

func TestEvaluateClaimProfileRequiresUserLatencyInclusionChainConfig(t *testing.T) {
	rep := completePublicUserLatencyReport()
	rep.ClaimEvidence.InclusionP95SLOMS = 500
	rep.Benchmarks[0].Metrics["time_to_inclusion_ms"] = metricSummary{Mean: 100, P50: 90, P95: 120, P99: 150, Min: 80, Max: 180}

	profile := evaluateClaimProfile(rep)
	if profile.Eligible {
		t.Fatalf("expected public claim to be blocked")
	}
	if !containsString(profile.BlockingReasons, "user_latency evidence missing: chain_config, chain_config_file, chain_config_sha256") {
		t.Fatalf("expected inclusion chain config blocker, got %+v", profile.BlockingReasons)
	}
}

func TestEvaluateClaimProfileRejectsMalformedConfigHashes(t *testing.T) {
	prover := completePublicProverReport()
	prover.ClaimEvidence.ProverConfigSHA256 = "not-a-hash"
	proverProfile := evaluateClaimProfile(prover)
	if proverProfile.Eligible {
		t.Fatalf("expected prover public claim to be blocked")
	}
	if !containsString(proverProfile.BlockingReasons, "prover_rps evidence missing: prover_config_sha256=64hex") {
		t.Fatalf("expected malformed prover config hash blocker, got %+v", proverProfile.BlockingReasons)
	}

	chain := completePublicChainReport()
	chain.ClaimEvidence.ChainConfigSHA256 = "not-a-hash"
	chainProfile := evaluateClaimProfile(chain)
	if chainProfile.Eligible {
		t.Fatalf("expected chain public claim to be blocked")
	}
	if !containsString(chainProfile.BlockingReasons, "chain_tps evidence missing: chain_config_sha256=64hex") {
		t.Fatalf("expected malformed chain config hash blocker, got %+v", chainProfile.BlockingReasons)
	}

	browser := completePublicUserLatencyReport()
	browser.ClaimEvidence.LatencyMode = "browser"
	browser.ClaimEvidence.BrowserMatrix = "chrome-desktop"
	browser.ClaimEvidence.BrowserAdapterReady = "true"
	browser.ClaimEvidence.BrowserAdapterVersion = "wasm-v1"
	browser.ClaimEvidence.BrowserAdapterFile = "benchmarks/privacy-user-latency/browser-adapter.json"
	browser.ClaimEvidence.BrowserAdapterSHA256 = "not-a-hash"
	browser.SourceFiles = append(browser.SourceFiles, browser.ClaimEvidence.BrowserAdapterFile)
	browser.SourceFileSHA256[browser.ClaimEvidence.BrowserAdapterFile] = strings.Repeat("f", 64)
	browserProfile := evaluateClaimProfile(browser)
	if browserProfile.Eligible {
		t.Fatalf("expected browser public claim to be blocked")
	}
	if !containsString(browserProfile.BlockingReasons, "user_latency evidence missing: browser_adapter_sha256=64hex") {
		t.Fatalf("expected malformed browser adapter hash blocker, got %+v", browserProfile.BlockingReasons)
	}
}

func TestEvaluateClaimProfileRejectsNonFiniteSLOs(t *testing.T) {
	prover := completePublicProverReport()
	prover.ClaimEvidence.LatencyP99SLOMS = math.NaN()
	proverProfile := evaluateClaimProfile(prover)
	if proverProfile.Eligible {
		t.Fatalf("expected prover public claim to be blocked")
	}
	if !containsString(proverProfile.BlockingReasons, "prover_rps evidence missing: latency_p99_slo_ms") {
		t.Fatalf("expected non-finite latency SLO blocker, got %+v", proverProfile.BlockingReasons)
	}

	chain := completePublicChainReport()
	chain.ClaimEvidence.InclusionP95SLOMS = math.Inf(1)
	chainProfile := evaluateClaimProfile(chain)
	if chainProfile.Eligible {
		t.Fatalf("expected chain public claim to be blocked")
	}
	if !containsString(chainProfile.BlockingReasons, "chain_tps evidence missing: inclusion_p95_slo_ms") {
		t.Fatalf("expected non-finite inclusion SLO blocker, got %+v", chainProfile.BlockingReasons)
	}
}

func TestEvaluateClaimProfileRequiresRunWindowAtLeastSteadyState(t *testing.T) {
	rep := completePublicProverReport()
	rep.RunEndedAt = "2026-06-12T00:01:00Z"

	profile := evaluateClaimProfile(rep)
	if profile.Eligible {
		t.Fatalf("expected public claim to be blocked")
	}
	if !containsString(profile.BlockingReasons, "run window is shorter than steady_state_seconds") {
		t.Fatalf("expected short run window blocker, got %+v", profile.BlockingReasons)
	}
}

func TestEvaluateClaimProfileBlocksWrongFamilyAndSLO(t *testing.T) {
	rep := report{
		Dirty:        false,
		ResultFamily: "privacy-proverd",
		SourceFiles:  []string{"benchmarks/privacy-proverd/raw.txt"},
		SourceFileSHA256: map[string]string{
			"benchmarks/privacy-proverd/raw.txt": strings.Repeat("a", 64),
		},
		RunStartedAt: "2026-06-12T00:00:00Z",
		RunEndedAt:   "2026-06-12T00:10:01Z",
		ActiveSetID:  "privacy-accounting-v2",
		ClaimProfile: claimProfile{
			RunProfile: "public_claim",
			ClaimTypes: []string{"prover_rps"},
		},
		Environment: environment{
			MachineProfile: "m5-pro-reference",
			CPUGovernor:    "high-power",
			MemoryGiB:      "48",
		},
		ArtifactSet: artifactSet{
			ActiveSetID:           "privacy-accounting-v2",
			ManifestActiveSetID:   "privacy-accounting-v2",
			ManifestSHA256:        strings.Repeat("1", 64),
			DescriptorComplete:    true,
			ArtifactFilesVerified: true,
		},
		ClaimEvidence: claimEvidence{
			SteadyStateSeconds: 600,
			LoadProfile:        "transfer_only",
			PreflightMode:      "strict",
			AuthEnabled:        "true",
			InstanceProfile:    "m5-pro-reference",
			ProverConfigFile:   "benchmarks/privacy-proverd-load/prover-config.json",
			ProverConfigSHA256: strings.Repeat("b", 64),
			LatencyP99SLOMS:    120,
			RSSStable:          "true",
			SaturationProfile:  "transfer-sweep-1-32",
		},
		Benchmarks: []benchmarkSummary{
			{
				Name:            "BenchmarkProverLoadTransfer",
				Samples:         600,
				ClaimType:       "prover_rps",
				LoadProfile:     "transfer_only",
				Route:           "transfer",
				Concurrency:     8,
				WarmupSeconds:   60,
				DurationSeconds: 600,
				Metrics: map[string]metricSummary{
					"proofs/sec":    {Mean: 10, P50: 10, P95: 10, P99: 10, Min: 9, Max: 11},
					"latency_ms":    {Mean: 100, P50: 90, P95: 120, P99: 150, Min: 80, Max: 180},
					"errors/op":     {Mean: 0.02, P50: 0.02, P95: 0.02, P99: 0.02, Max: 0.02},
					"cpu_percent":   {Mean: 80, P50: 80, P95: 85, P99: 90, Min: 70, Max: 92},
					"max_rss_bytes": {Mean: 1024, P50: 1024, P95: 2048, P99: 4096, Min: 1024, Max: 4096},
				},
			},
		},
	}

	profile := evaluateClaimProfile(rep)
	if profile.Eligible {
		t.Fatalf("expected public claim to be blocked")
	}
	if !containsString(profile.BlockingReasons, "prover_rps claim requires result_family privacy-proverd-load|public-capacity") {
		t.Fatalf("expected wrong family blocker, got %+v", profile.BlockingReasons)
	}
	if !containsString(profile.BlockingReasons, "prover_rps metrics invalid: BenchmarkProverLoadTransfer/latency_ms p99 150.000000 exceeds 120.000000, BenchmarkProverLoadTransfer/errors/op max 0.020000 exceeds 0.001000") {
		t.Fatalf("expected SLO blocker, got %+v", profile.BlockingReasons)
	}
}

func TestEvaluateClaimProfileBlocksPublicCapacityMultiClaimWithoutPerClaimEvidence(t *testing.T) {
	rep := completePublicProverReport()
	rep.ResultFamily = "public-capacity"
	rep.ClaimProfile.ClaimTypes = []string{"prover_rps", "user_latency"}

	profile := evaluateClaimProfile(rep)
	if profile.Eligible {
		t.Fatalf("expected public-capacity multi-claim report to be blocked")
	}
	if !containsString(profile.BlockingReasons, "public-capacity multi-claim reports require per-claim evidence for prover_rps,user_latency") {
		t.Fatalf("expected per-claim evidence schema blocker, got %+v", profile.BlockingReasons)
	}
}

func TestEvaluateClaimProfileValidatesPublicCapacityComponentReports(t *testing.T) {
	rep := completePublicProverReport()
	rep.ResultFamily = "public-capacity"
	rep.ClaimEvidenceByType = map[string]claimEvidence{
		"prover_rps": rep.ClaimEvidence,
	}
	rep.ComponentReports = []componentReport{
		{
			Path:           "benchmarks/privacy-proverd-load/latest.json",
			SHA256:         strings.Repeat("f", 64),
			ResultFamily:   "privacy-proverd-load",
			RunProfile:     "public_claim",
			ClaimTypes:     []string{"prover_rps"},
			Eligible:       true,
			ActiveSetID:    rep.ActiveSetID,
			ManifestSHA256: rep.ArtifactSet.ManifestSHA256,
		},
	}

	profile := evaluateClaimProfile(rep)
	if profile.Eligible {
		t.Fatalf("expected public-capacity report to be blocked")
	}
	if !containsString(profile.BlockingReasons, "component reports invalid: benchmarks/privacy-proverd-load/latest.json sha256 does not match source_file_sha256") {
		t.Fatalf("expected component report hash blocker, got %+v", profile.BlockingReasons)
	}
}

func TestEvaluateClaimProfileRequiresComponentReportsAsSourceEvidence(t *testing.T) {
	rep := completePublicProverReport()
	rep.ResultFamily = "public-capacity"
	rep.ClaimEvidenceByType = map[string]claimEvidence{
		"prover_rps": rep.ClaimEvidence,
	}
	rep.ComponentReports = []componentReport{
		{
			Path:           "benchmarks/privacy-proverd-load/component.json",
			SHA256:         strings.Repeat("a", 64),
			ResultFamily:   "privacy-proverd-load",
			RunProfile:     "public_claim",
			ClaimTypes:     []string{"prover_rps"},
			Eligible:       true,
			ActiveSetID:    rep.ActiveSetID,
			ManifestSHA256: rep.ArtifactSet.ManifestSHA256,
		},
	}

	profile := evaluateClaimProfile(rep)
	if profile.Eligible {
		t.Fatalf("expected public-capacity report to be blocked")
	}
	if !containsString(profile.BlockingReasons, "component reports invalid: benchmarks/privacy-proverd-load/component.json is not in source_files, benchmarks/privacy-proverd-load/component.json is not in source_file_sha256") {
		t.Fatalf("expected component report source evidence blocker, got %+v", profile.BlockingReasons)
	}
}

func TestEvaluateClaimProfileBlocksAnyMatchingMetricSLO(t *testing.T) {
	rep := report{
		Dirty:        false,
		ResultFamily: "privacy-proverd-load",
		SourceFiles: []string{
			"benchmarks/privacy-proverd-load/latest.json",
			"benchmarks/privacy-proverd-load/prover-config.json",
			"benchmarks/privacy-proverd-load/saturation-profile.json",
		},
		SourceFileSHA256: map[string]string{
			"benchmarks/privacy-proverd-load/latest.json":             strings.Repeat("a", 64),
			"benchmarks/privacy-proverd-load/prover-config.json":      strings.Repeat("b", 64),
			"benchmarks/privacy-proverd-load/saturation-profile.json": strings.Repeat("c", 64),
		},
		RunStartedAt: "2026-06-12T00:00:00Z",
		RunEndedAt:   "2026-06-12T00:10:01Z",
		ActiveSetID:  "privacy-accounting-v2",
		ClaimProfile: claimProfile{
			RunProfile: "public_claim",
			ClaimTypes: []string{"prover_rps"},
		},
		Environment: environment{
			MachineProfile: "m5-pro-reference",
			CPUGovernor:    "high-power",
			MemoryGiB:      "48",
		},
		ArtifactSet: artifactSet{
			ActiveSetID:           "privacy-accounting-v2",
			ManifestActiveSetID:   "privacy-accounting-v2",
			ManifestSHA256:        strings.Repeat("1", 64),
			DescriptorComplete:    true,
			ArtifactFilesVerified: true,
		},
		ClaimEvidence: claimEvidence{
			SteadyStateSeconds:      600,
			LoadProfile:             "mixed",
			PreflightMode:           "strict",
			AuthEnabled:             "true",
			InstanceProfile:         "m5-pro-reference",
			ProverConfigFile:        "benchmarks/privacy-proverd-load/prover-config.json",
			ProverConfigSHA256:      strings.Repeat("b", 64),
			LatencyP99SLOMS:         200,
			RSSStable:               "true",
			SaturationProfile:       "transfer-sweep-1-32",
			SaturationProfileFile:   "benchmarks/privacy-proverd-load/saturation-profile.json",
			SaturationProfileSHA256: strings.Repeat("c", 64),
		},
		Benchmarks: []benchmarkSummary{
			{
				Name:            "ProverLoadTransferC1",
				Samples:         600,
				ClaimType:       "prover_rps",
				LoadProfile:     "transfer_only",
				Route:           "transfer",
				Concurrency:     1,
				WarmupSeconds:   60,
				DurationSeconds: 600,
				Metrics: map[string]metricSummary{
					"proofs/sec":    {Mean: 10, P50: 10, P95: 10, P99: 10, Min: 9, Max: 11},
					"latency_ms":    {Mean: 100, P50: 90, P95: 120, P99: 150, Min: 80, Max: 180},
					"errors/op":     {Mean: 0, P50: 0, P95: 0, P99: 0, Max: 0},
					"cpu_percent":   {Mean: 80, P50: 80, P95: 85, P99: 90, Min: 70, Max: 92},
					"max_rss_bytes": {Mean: 1024, P50: 1024, P95: 2048, P99: 4096, Min: 1024, Max: 4096},
				},
			},
			{
				Name:            "ProverLoadTransferC32",
				Samples:         600,
				ClaimType:       "prover_rps",
				LoadProfile:     "transfer_only",
				Route:           "transfer",
				Concurrency:     32,
				WarmupSeconds:   60,
				DurationSeconds: 600,
				Metrics: map[string]metricSummary{
					"proofs/sec":    {Mean: 8, P50: 8, P95: 8, P99: 8, Min: 7, Max: 9},
					"latency_ms":    {Mean: 100, P50: 90, P95: 120, P99: 150, Min: 80, Max: 180},
					"errors/op":     {Mean: 0.02, P50: 0.02, P95: 0.02, P99: 0.02, Max: 0.02},
					"cpu_percent":   {Mean: 80, P50: 80, P95: 85, P99: 90, Min: 70, Max: 92},
					"max_rss_bytes": {Mean: 1024, P50: 1024, P95: 2048, P99: 4096, Min: 1024, Max: 4096},
				},
			},
		},
	}

	profile := evaluateClaimProfile(rep)
	if profile.Eligible {
		t.Fatalf("expected public claim to be blocked")
	}
	if !containsString(profile.BlockingReasons, "prover_rps metrics invalid: ProverLoadTransferC32/errors/op max 0.020000 exceeds 0.001000") {
		t.Fatalf("expected second row SLO blocker, got %+v", profile.BlockingReasons)
	}
}

func completePublicProverReport() report {
	return report{
		Dirty:        false,
		ResultFamily: "privacy-proverd-load",
		SourceFiles: []string{
			"benchmarks/privacy-proverd-load/latest.json",
			"benchmarks/privacy-proverd-load/prover-config.json",
			"benchmarks/privacy-proverd-load/saturation-profile.json",
		},
		SourceFileSHA256: map[string]string{
			"benchmarks/privacy-proverd-load/latest.json":             strings.Repeat("a", 64),
			"benchmarks/privacy-proverd-load/prover-config.json":      strings.Repeat("b", 64),
			"benchmarks/privacy-proverd-load/saturation-profile.json": strings.Repeat("c", 64),
		},
		RunStartedAt: "2026-06-12T00:00:00Z",
		RunEndedAt:   "2026-06-12T00:10:01Z",
		ActiveSetID:  "privacy-accounting-v2",
		ClaimProfile: claimProfile{
			RunProfile: "public_claim",
			ClaimTypes: []string{"prover_rps"},
		},
		Environment: environment{
			MachineProfile: "m5-pro-reference",
			CPUGovernor:    "high-power",
			MemoryGiB:      "48",
		},
		ArtifactSet: artifactSet{
			ActiveSetID:           "privacy-accounting-v2",
			ManifestActiveSetID:   "privacy-accounting-v2",
			ManifestSHA256:        strings.Repeat("1", 64),
			DescriptorComplete:    true,
			ArtifactFilesVerified: true,
		},
		ClaimEvidence: claimEvidence{
			SteadyStateSeconds:      600,
			LoadProfile:             "transfer_only",
			PreflightMode:           "strict",
			AuthEnabled:             "true",
			InstanceProfile:         "m5-pro-reference",
			ProverConfigFile:        "benchmarks/privacy-proverd-load/prover-config.json",
			ProverConfigSHA256:      strings.Repeat("b", 64),
			LatencyP99SLOMS:         200,
			RSSStable:               "true",
			SaturationProfile:       "transfer-sweep-1-32",
			SaturationProfileFile:   "benchmarks/privacy-proverd-load/saturation-profile.json",
			SaturationProfileSHA256: strings.Repeat("c", 64),
		},
		Benchmarks: []benchmarkSummary{
			{
				Name:            "BenchmarkProverLoadTransfer",
				Samples:         600,
				ClaimType:       "prover_rps",
				LoadProfile:     "transfer_only",
				Route:           "transfer",
				Concurrency:     8,
				WarmupSeconds:   60,
				DurationSeconds: 600,
				Metrics: map[string]metricSummary{
					"proofs/sec":    {Mean: 10, P50: 10, P95: 10, P99: 10, Min: 9, Max: 11},
					"latency_ms":    {Mean: 100, P50: 90, P95: 120, P99: 150, Min: 80, Max: 180},
					"errors/op":     {Mean: 0, P50: 0, P95: 0, P99: 0, Max: 0},
					"cpu_percent":   {Mean: 80, P50: 80, P95: 85, P99: 90, Min: 70, Max: 92},
					"max_rss_bytes": {Mean: 1024, P50: 1024, P95: 2048, P99: 4096, Min: 1024, Max: 4096},
				},
			},
			{
				Name:            "BenchmarkProverLoadTransferC16",
				Samples:         600,
				ClaimType:       "prover_rps",
				LoadProfile:     "transfer_only",
				Route:           "transfer",
				Concurrency:     16,
				WarmupSeconds:   60,
				DurationSeconds: 600,
				Metrics: map[string]metricSummary{
					"proofs/sec":    {Mean: 18, P50: 18, P95: 17, P99: 16, Min: 15, Max: 19},
					"latency_ms":    {Mean: 120, P50: 110, P95: 150, P99: 180, Min: 90, Max: 190},
					"errors/op":     {Mean: 0, P50: 0, P95: 0, P99: 0, Min: 0, Max: 0},
					"cpu_percent":   {Mean: 88, P50: 88, P95: 92, P99: 95, Min: 80, Max: 96},
					"max_rss_bytes": {Mean: 2048, P50: 2048, P95: 4096, P99: 4096, Min: 2048, Max: 4096},
				},
			},
		},
	}
}

func completePublicChainReport() report {
	return report{
		Dirty:        false,
		ResultFamily: "privacy-localnet-tps",
		SourceFiles: []string{
			"benchmarks/privacy-localnet-tps/latest.json",
			"benchmarks/privacy-localnet-tps/chain-config.json",
			"benchmarks/privacy-localnet-tps/saturation-profile.json",
			"benchmarks/privacy-localnet-tps/reserve-before.json",
			"benchmarks/privacy-localnet-tps/reserve-after.json",
		},
		SourceFileSHA256: map[string]string{
			"benchmarks/privacy-localnet-tps/latest.json":             strings.Repeat("a", 64),
			"benchmarks/privacy-localnet-tps/chain-config.json":       strings.Repeat("c", 64),
			"benchmarks/privacy-localnet-tps/saturation-profile.json": strings.Repeat("d", 64),
			"benchmarks/privacy-localnet-tps/reserve-before.json":     strings.Repeat("e", 64),
			"benchmarks/privacy-localnet-tps/reserve-after.json":      strings.Repeat("f", 64),
		},
		RunStartedAt: "2026-06-12T00:00:00Z",
		RunEndedAt:   "2026-06-12T00:10:01Z",
		ActiveSetID:  "privacy-accounting-v2",
		ClaimProfile: claimProfile{
			RunProfile: "public_claim",
			ClaimTypes: []string{"chain_tps"},
		},
		Environment: environment{
			MachineProfile: "m5-pro-reference",
			CPUGovernor:    "high-power",
			MemoryGiB:      "48",
		},
		ArtifactSet: artifactSet{
			ActiveSetID:           "privacy-accounting-v2",
			ManifestActiveSetID:   "privacy-accounting-v2",
			ManifestSHA256:        strings.Repeat("1", 64),
			DescriptorComplete:    true,
			ArtifactFilesVerified: true,
		},
		ClaimEvidence: claimEvidence{
			SteadyStateSeconds:          600,
			LoadProfile:                 "mixed-shielded",
			ChainConfig:                 "single-validator-localnet-a",
			ChainConfigFile:             "benchmarks/privacy-localnet-tps/chain-config.json",
			ChainConfigSHA256:           strings.Repeat("c", 64),
			ReserveInvariant:            "true",
			InclusionP95SLOMS:           500,
			SaturationProfile:           "mixed-sweep-4-16",
			SaturationProfileFile:       "benchmarks/privacy-localnet-tps/saturation-profile.json",
			SaturationProfileSHA256:     strings.Repeat("d", 64),
			ThroughputWindowSeconds:     10,
			ReserveSnapshotBeforeFile:   "benchmarks/privacy-localnet-tps/reserve-before.json",
			ReserveSnapshotBeforeSHA256: strings.Repeat("e", 64),
			ReserveSnapshotAfterFile:    "benchmarks/privacy-localnet-tps/reserve-after.json",
			ReserveSnapshotAfterSHA256:  strings.Repeat("f", 64),
		},
		Benchmarks: []benchmarkSummary{
			{
				Name:            "ChainTPSMixedTarget8",
				Samples:         600,
				ClaimType:       "chain_tps",
				LoadProfile:     "mixed-shielded",
				DurationSeconds: 600,
				TargetTxPerSec:  8,
				Metrics: map[string]metricSummary{
					"tx/sec":               {Mean: 8, P50: 8, P95: 8, P99: 8, Min: 7, Max: 9},
					"inclusion_latency_ms": {Mean: 100, P50: 100, P95: 120, P99: 150, Min: 80, Max: 200},
					"failed_tx_rate":       {Mean: 0, P50: 0, P95: 0, P99: 0, Min: 0, Max: 0},
				},
			},
			{
				Name:            "ChainTPSMixedTarget12",
				Samples:         600,
				ClaimType:       "chain_tps",
				LoadProfile:     "mixed-shielded",
				DurationSeconds: 600,
				TargetTxPerSec:  12,
				Metrics: map[string]metricSummary{
					"tx/sec":               {Mean: 10, P50: 10, P95: 10, P99: 10, Min: 9, Max: 11},
					"inclusion_latency_ms": {Mean: 120, P50: 110, P95: 150, P99: 180, Min: 90, Max: 220},
					"failed_tx_rate":       {Mean: 0, P50: 0, P95: 0, P99: 0, Min: 0, Max: 0},
				},
			},
		},
	}
}

func completePublicUserLatencyReport() report {
	return report{
		Dirty:        false,
		ResultFamily: "privacy-user-latency",
		SourceFiles:  []string{"benchmarks/privacy-user-latency/latest.json"},
		SourceFileSHA256: map[string]string{
			"benchmarks/privacy-user-latency/latest.json": strings.Repeat("a", 64),
		},
		RunStartedAt: "2026-06-12T00:00:00Z",
		RunEndedAt:   "2026-06-12T00:10:01Z",
		ActiveSetID:  "privacy-accounting-v2",
		ClaimProfile: claimProfile{
			RunProfile: "public_claim",
			ClaimTypes: []string{"user_latency"},
		},
		Environment: environment{
			MachineProfile: "m5-pro-reference",
			CPUGovernor:    "high-power",
			MemoryGiB:      "48",
		},
		ArtifactSet: artifactSet{
			ActiveSetID:           "privacy-accounting-v2",
			ManifestActiveSetID:   "privacy-accounting-v2",
			ManifestSHA256:        strings.Repeat("1", 64),
			DescriptorComplete:    true,
			ArtifactFilesVerified: true,
		},
		ClaimEvidence: claimEvidence{
			LoadProfile:        "transfer_all_private",
			LatencyMode:        "native",
			ColdWarmSeparated:  "true",
			LatencyP99SLOMS:    500,
			ProverConfigFile:   "benchmarks/privacy-user-latency/prover-config.json",
			ProverConfigSHA256: strings.Repeat("d", 64),
		},
		Benchmarks: []benchmarkSummary{
			{
				Name:        "UserLatencyTransferWarm",
				Samples:     100,
				ClaimType:   "user_latency",
				FlowProfile: "transfer_all_private",
				LatencyMode: "native",
				ColdWarm:    "warm",
				Metrics: map[string]metricSummary{
					"prepare_latency_ms": {Mean: 20, P50: 18, P95: 28, P99: 32, Min: 10, Max: 40},
					"proof_latency_ms":   {Mean: 150, P50: 140, P95: 210, P99: 260, Min: 120, Max: 280},
					"time_to_submit_ms":  {Mean: 30, P50: 25, P95: 45, P99: 55, Min: 15, Max: 60},
					"submit_ready_ms":    {Mean: 200, P50: 180, P95: 260, P99: 320, Min: 150, Max: 350},
					"error_rate":         {Mean: 0, P50: 0, P95: 0, P99: 0, Min: 0, Max: 0},
					"timeout_rate":       {Mean: 0, P50: 0, P95: 0, P99: 0, Min: 0, Max: 0},
				},
			},
		},
	}
}

func writeLinkedProverReport(t *testing.T, rep report) (string, string) {
	t.Helper()
	rep.ClaimProfile = evaluateClaimProfile(rep)
	if !rep.ClaimProfile.Eligible {
		t.Fatalf("linked prover report fixture is not eligible: %+v", rep.ClaimProfile)
	}
	path := filepath.Join(t.TempDir(), "linked-prover-report.json")
	if err := writeJSON(path, rep); err != nil {
		t.Fatalf("write linked prover report: %v", err)
	}
	sha, err := fileSHA256(path)
	if err != nil {
		t.Fatalf("hash linked prover report: %v", err)
	}
	return path, sha
}

func attachRemoteUserLatencyEvidence(rep *report, linkedPath, linkedSHA string, linked report) {
	rep.ClaimEvidence.LatencyMode = "remote"
	rep.ClaimEvidence.RemoteTopology = "proverd-a:443"
	rep.ClaimEvidence.InstanceProfile = linked.ClaimEvidence.InstanceProfile
	rep.ClaimEvidence.ProverConfigFile = linked.ClaimEvidence.ProverConfigFile
	rep.ClaimEvidence.ProverConfigSHA256 = linked.ClaimEvidence.ProverConfigSHA256
	rep.ClaimEvidence.LinkedProverReportFile = linkedPath
	rep.ClaimEvidence.LinkedProverReportSHA256 = linkedSHA
	for i := range rep.Benchmarks {
		if rep.Benchmarks[i].ClaimType == "user_latency" {
			rep.Benchmarks[i].LatencyMode = "remote"
		}
	}
	rep.SourceFiles = append(rep.SourceFiles, linked.ClaimEvidence.ProverConfigFile, linkedPath)
	rep.SourceFileSHA256[linked.ClaimEvidence.ProverConfigFile] = linked.ClaimEvidence.ProverConfigSHA256
	rep.SourceFileSHA256[linkedPath] = linkedSHA
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
