package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"
)

type metricSummary struct {
	Mean float64 `json:"mean"`
	P50  float64 `json:"p50"`
	P95  float64 `json:"p95"`
	P99  float64 `json:"p99"`
	Min  float64 `json:"min"`
	Max  float64 `json:"max"`
}

type benchmarkSummary struct {
	Name            string                   `json:"name"`
	Samples         int                      `json:"samples"`
	MetricKind      string                   `json:"metric_kind"`
	LoadProfile     string                   `json:"load_profile,omitempty"`
	DurationSeconds int                      `json:"duration_seconds,omitempty"`
	TargetTxPerSec  float64                  `json:"target_tx_per_sec,omitempty"`
	Metrics         map[string]metricSummary `json:"metric_summaries,omitempty"`
}

type benchmarkSummaryEnvelope struct {
	Benchmarks []benchmarkSummary `json:"benchmarks"`
}

type bulkScenario struct {
	Name             string
	Tenants          int
	Recipients       int
	ChunkSize        int
	ProverUnits      int
	ProofsPerSecond  float64
	TxPerSecond      float64
	ReservationError float64
}

type bulkEstimate struct {
	TotalRecipients       int
	ProofCount            int
	TxEnvelopeCount       int
	ChunkCount            int
	ProofSeconds          float64
	TxSubmitSeconds       float64
	EstimatedSeconds      float64
	PayrollItemsPerSec    float64
	EffectiveProofsPerSec float64
	EffectiveTxPerSec     float64
}

func main() {
	var outPath string
	var recipients int
	var tenants int
	var recipientsPerTenant int
	var chunkSize int
	var proverUnits int
	var proofsPerSecond float64
	var txPerSecond float64
	var scenarioName string

	flag.StringVar(&outPath, "out", "benchmarks/privacy-bulk-transfer/bulk-summary.json", "structured benchmark summary output path")
	flag.IntVar(&recipients, "recipients", 100000, "total recipients when -recipients-per-tenant is zero")
	flag.IntVar(&tenants, "tenants", 1, "tenant count")
	flag.IntVar(&recipientsPerTenant, "recipients-per-tenant", 0, "recipients per tenant; overrides -recipients when positive")
	flag.IntVar(&chunkSize, "chunk-size", 20, "transfer messages per tx envelope")
	flag.IntVar(&proverUnits, "prover-units", 1, "number of prover units")
	flag.Float64Var(&proofsPerSecond, "proofs-per-sec", 6.92638, "single prover unit transfer proof throughput")
	flag.Float64Var(&txPerSecond, "tx-per-sec", 1, "tx envelope submission throughput")
	flag.StringVar(&scenarioName, "scenario", "single-company-100k", "scenario name")
	flag.Parse()

	if recipientsPerTenant > 0 {
		recipients = tenants * recipientsPerTenant
	}
	scenario := bulkScenario{
		Name:            scenarioName,
		Tenants:         tenants,
		Recipients:      recipients,
		ChunkSize:       chunkSize,
		ProverUnits:     proverUnits,
		ProofsPerSecond: proofsPerSecond,
		TxPerSecond:     txPerSecond,
	}
	estimate, err := estimateBulkScenario(scenario)
	if err != nil {
		fatalf("%v", err)
	}

	summary := benchmarkSummary{
		Name:            "BulkPayroll" + scenario.Name,
		Samples:         1,
		MetricKind:      "bulk_payroll_simulation",
		LoadProfile:     scenario.Name,
		DurationSeconds: int(math.Ceil(estimate.EstimatedSeconds)),
		TargetTxPerSec:  txPerSecond,
		Metrics: map[string]metricSummary{
			"tenant_count":            scalar(float64(tenants)),
			"recipient_count":         scalar(float64(estimate.TotalRecipients)),
			"chunk_size":              scalar(float64(chunkSize)),
			"prover_units":            scalar(float64(proverUnits)),
			"proof_count":             scalar(float64(estimate.ProofCount)),
			"tx_envelope_count":       scalar(float64(estimate.TxEnvelopeCount)),
			"chunk_count":             scalar(float64(estimate.ChunkCount)),
			"proof_seconds":           scalar(estimate.ProofSeconds),
			"tx_submit_seconds":       scalar(estimate.TxSubmitSeconds),
			"estimated_total_seconds": scalar(estimate.EstimatedSeconds),
			"payroll_item_per_sec":    scalar(estimate.PayrollItemsPerSec),
			"effective_proof_per_sec": scalar(estimate.EffectiveProofsPerSec),
			"effective_tx_per_sec":    scalar(estimate.EffectiveTxPerSec),
			"reservation_conflicts":   scalar(0),
			"replan_count":            scalar(0),
			"manual_review_count":     scalar(0),
		},
	}

	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		fatalf("create output dir: %v", err)
	}
	bz, err := json.MarshalIndent(benchmarkSummaryEnvelope{Benchmarks: []benchmarkSummary{summary}}, "", "  ")
	if err != nil {
		fatalf("marshal summary: %v", err)
	}
	if err := os.WriteFile(outPath, append(bz, '\n'), 0o644); err != nil {
		fatalf("write summary: %v", err)
	}

	fmt.Printf("bulk transfer benchmark summary written to %s at %s\n", outPath, time.Now().UTC().Format(time.RFC3339))
}

func estimateBulkScenario(s bulkScenario) (bulkEstimate, error) {
	if s.Recipients <= 0 {
		return bulkEstimate{}, fmt.Errorf("recipients must be positive")
	}
	if s.ChunkSize <= 0 {
		return bulkEstimate{}, fmt.Errorf("chunk size must be positive")
	}
	if s.ProverUnits <= 0 {
		return bulkEstimate{}, fmt.Errorf("prover units must be positive")
	}
	if s.ProofsPerSecond <= 0 {
		return bulkEstimate{}, fmt.Errorf("proofs per second must be positive")
	}
	if s.TxPerSecond <= 0 {
		return bulkEstimate{}, fmt.Errorf("tx per second must be positive")
	}

	proofCount := s.Recipients
	txEnvelopeCount := int(math.Ceil(float64(s.Recipients) / float64(s.ChunkSize)))
	effectiveProofsPerSec := s.ProofsPerSecond * float64(s.ProverUnits)
	proofSeconds := float64(proofCount) / effectiveProofsPerSec
	txSeconds := float64(txEnvelopeCount) / s.TxPerSecond
	estimatedSeconds := math.Max(proofSeconds, txSeconds)
	return bulkEstimate{
		TotalRecipients:       s.Recipients,
		ProofCount:            proofCount,
		TxEnvelopeCount:       txEnvelopeCount,
		ChunkCount:            txEnvelopeCount,
		ProofSeconds:          proofSeconds,
		TxSubmitSeconds:       txSeconds,
		EstimatedSeconds:      estimatedSeconds,
		PayrollItemsPerSec:    float64(s.Recipients) / estimatedSeconds,
		EffectiveProofsPerSec: effectiveProofsPerSec,
		EffectiveTxPerSec:     float64(txEnvelopeCount) / txSeconds,
	}, nil
}

func scalar(value float64) metricSummary {
	return metricSummary{
		Mean: value,
		P50:  value,
		P95:  value,
		P99:  value,
		Min:  value,
		Max:  value,
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
