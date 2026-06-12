package cli

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	privacyLatencyTraceFileEnv   = "CLAIRVEIL_PRIVACY_LATENCY_TRACE_FILE"
	privacyLatencyFlowIDEnv      = "CLAIRVEIL_PRIVACY_LATENCY_FLOW_ID"
	privacyLatencyFlowProfileEnv = "CLAIRVEIL_PRIVACY_LATENCY_FLOW_PROFILE"
	privacyLatencyModeEnv        = "CLAIRVEIL_PRIVACY_LATENCY_MODE"
	privacyLatencyColdWarmEnv    = "CLAIRVEIL_PRIVACY_LATENCY_COLD_WARM"
)

type privacyLatencyFlow struct {
	mu              sync.Mutex
	traceFile       string
	flowID          string
	flowProfile     string
	latencyMode     string
	coldWarm        string
	startedAt       time.Time
	prepareRecorded bool
	finished        bool
}

type privacyLatencyTraceEvent struct {
	SchemaVersion string  `json:"schema_version"`
	FlowID        string  `json:"flow_id"`
	FlowProfile   string  `json:"flow_profile"`
	LatencyMode   string  `json:"latency_mode"`
	ColdWarm      string  `json:"cold_warm"`
	Phase         string  `json:"phase"`
	StartedAt     string  `json:"started_at"`
	EndedAt       string  `json:"ended_at"`
	DurationMS    float64 `json:"duration_ms"`
	Success       bool    `json:"success"`
	Error         string  `json:"error,omitempty"`
	TxHash        string  `json:"txhash,omitempty"`
}

func newPrivacyLatencyFlow(defaultFlowProfile string) *privacyLatencyFlow {
	traceFile := strings.TrimSpace(os.Getenv(privacyLatencyTraceFileEnv))
	if traceFile == "" {
		return nil
	}

	flowProfile := strings.TrimSpace(os.Getenv(privacyLatencyFlowProfileEnv))
	if flowProfile == "" {
		flowProfile = strings.TrimSpace(defaultFlowProfile)
	}
	latencyMode := strings.TrimSpace(os.Getenv(privacyLatencyModeEnv))
	if latencyMode == "" {
		latencyMode = "native"
	}
	coldWarm := strings.TrimSpace(os.Getenv(privacyLatencyColdWarmEnv))
	if coldWarm == "" {
		coldWarm = "warm"
	}

	return &privacyLatencyFlow{
		traceFile:   traceFile,
		flowID:      latencyFlowID(),
		flowProfile: flowProfile,
		latencyMode: latencyMode,
		coldWarm:    coldWarm,
		startedAt:   time.Now(),
	}
}

func latencyFlowID() string {
	if value := strings.TrimSpace(os.Getenv(privacyLatencyFlowIDEnv)); value != "" {
		return value
	}

	var bz [16]byte
	if _, err := rand.Read(bz[:]); err == nil {
		return hex.EncodeToString(bz[:])
	}
	return time.Now().UTC().Format("20060102T150405.000000000")
}

func (f *privacyLatencyFlow) recordPrepareUntil(endedAt time.Time) {
	if f == nil {
		return
	}

	f.mu.Lock()
	if f.prepareRecorded {
		f.mu.Unlock()
		return
	}
	f.prepareRecorded = true
	startedAt := f.startedAt
	f.mu.Unlock()

	f.recordPhaseWindow("prepare", startedAt, endedAt, nil, "")
}

func (f *privacyLatencyFlow) recordPhase(phase string, startedAt time.Time, err error) {
	if f == nil {
		return
	}
	f.recordPhaseWindow(phase, startedAt, time.Now(), err, "")
}

func (f *privacyLatencyFlow) recordSubmit(startedAt time.Time, txHash string, err error) {
	if f == nil {
		return
	}
	f.recordPhaseWindow("submit", startedAt, time.Now(), err, txHash)
}

func (f *privacyLatencyFlow) finish(err error) {
	if f == nil {
		return
	}

	f.mu.Lock()
	if f.finished {
		f.mu.Unlock()
		return
	}
	f.finished = true
	startedAt := f.startedAt
	f.mu.Unlock()

	f.recordPhaseWindow("total", startedAt, time.Now(), err, "")
}

func (f *privacyLatencyFlow) recordPhaseWindow(phase string, startedAt time.Time, endedAt time.Time, err error, txHash string) {
	if f == nil || strings.TrimSpace(phase) == "" {
		return
	}
	if endedAt.Before(startedAt) {
		endedAt = startedAt
	}

	event := privacyLatencyTraceEvent{
		SchemaVersion: "clairveil.privacy_latency_trace.v1",
		FlowID:        f.flowID,
		FlowProfile:   f.flowProfile,
		LatencyMode:   f.latencyMode,
		ColdWarm:      f.coldWarm,
		Phase:         phase,
		StartedAt:     startedAt.UTC().Format(time.RFC3339Nano),
		EndedAt:       endedAt.UTC().Format(time.RFC3339Nano),
		DurationMS:    float64(endedAt.Sub(startedAt)) / float64(time.Millisecond),
		Success:       err == nil,
		TxHash:        strings.TrimSpace(txHash),
	}
	if err != nil {
		event.Error = err.Error()
	}

	_ = appendPrivacyLatencyTraceEvent(f.traceFile, event)
}

func appendPrivacyLatencyTraceEvent(path string, event privacyLatencyTraceEvent) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	return encoder.Encode(event)
}

func observePrivacyLatencyPhase[T any](flow *privacyLatencyFlow, phase string, fn func() (T, error)) (T, error) {
	startedAt := time.Now()
	result, err := fn()
	if flow != nil {
		flow.recordPhase(phase, startedAt, err)
	}
	return result, err
}
