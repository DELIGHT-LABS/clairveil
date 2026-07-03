package main

import (
	"math"
	"testing"
)

func TestEstimateBulkScenario(t *testing.T) {
	estimate, err := estimateBulkScenario(bulkScenario{
		Recipients:      100000,
		ChunkSize:       20,
		ProverUnits:     1,
		ProofsPerSecond: 6.92638,
		TxPerSecond:     1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if estimate.ProofCount != 100000 {
		t.Fatalf("unexpected proof count %d", estimate.ProofCount)
	}
	if estimate.TxEnvelopeCount != 5000 {
		t.Fatalf("unexpected tx envelope count %d", estimate.TxEnvelopeCount)
	}
	if math.Abs(estimate.ProofSeconds-14437.56) > 1 {
		t.Fatalf("unexpected proof seconds %.2f", estimate.ProofSeconds)
	}
	if estimate.EstimatedSeconds != estimate.ProofSeconds {
		t.Fatalf("expected proof bottleneck")
	}
}
