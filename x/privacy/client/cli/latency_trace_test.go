package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPrivacyLatencyFlowNoopsWithoutTraceFile(t *testing.T) {
	t.Setenv(privacyLatencyTraceFileEnv, "")

	flow := newPrivacyLatencyFlow("transfer_all_private")
	if flow != nil {
		t.Fatalf("expected nil latency flow when %s is unset", privacyLatencyTraceFileEnv)
	}
}

func TestPrivacyLatencyFlowWritesJSONLine(t *testing.T) {
	tracePath := filepath.Join(t.TempDir(), "trace.jsonl")
	t.Setenv(privacyLatencyTraceFileEnv, tracePath)
	t.Setenv(privacyLatencyFlowIDEnv, "flow-1")
	t.Setenv(privacyLatencyModeEnv, "native")
	t.Setenv(privacyLatencyColdWarmEnv, "warm")

	flow := newPrivacyLatencyFlow("transfer_all_private")
	if flow == nil {
		t.Fatal("expected latency flow")
	}
	startedAt := time.Now().Add(-10 * time.Millisecond)
	flow.recordSubmit(startedAt, "ABC", nil)

	bz, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatalf("read trace: %v", err)
	}
	var event privacyLatencyTraceEvent
	if err := json.Unmarshal(bz, &event); err != nil {
		t.Fatalf("decode trace event: %v", err)
	}
	if event.FlowID != "flow-1" || event.FlowProfile != "transfer_all_private" || event.Phase != "submit" || event.TxHash != "ABC" {
		t.Fatalf("unexpected trace event: %+v", event)
	}
	if event.DurationMS <= 0 {
		t.Fatalf("expected positive duration, got %.3f", event.DurationMS)
	}
}
