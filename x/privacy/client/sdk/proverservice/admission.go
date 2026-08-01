package proverservice

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	privacyprovertransport "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/provertransport"
)

const (
	DefaultMaxInFlightPerCircuit = 1
	DefaultMaxQueuedPerCircuit   = 4
)

var (
	ErrAdmissionQueueFull       = errors.New("prover admission queue is full")
	ErrAdmissionCircuitDisabled = errors.New("prover circuit is not configured")
)

type CircuitAdmissionConfig struct {
	MaxInFlight int
	MaxQueued   int
}

type AdmissionConfig struct {
	Circuits map[string]CircuitAdmissionConfig
	Now      func() time.Time
}

type CircuitAdmissionMetrics struct {
	MaxInFlight          int    `json:"max_in_flight"`
	MaxQueued            int    `json:"max_queued"`
	InFlight             int    `json:"in_flight"`
	Queued               int    `json:"queued"`
	Proving              int    `json:"proving"`
	TotalAdmitted        uint64 `json:"total_admitted"`
	TotalQueued          uint64 `json:"total_queued"`
	TotalRejected        uint64 `json:"total_rejected"`
	TotalCanceled        uint64 `json:"total_canceled"`
	TotalReleased        uint64 `json:"total_released"`
	TotalProveCompleted  uint64 `json:"total_prove_completed"`
	QueueWaitNanoseconds uint64 `json:"queue_wait_nanoseconds"`
	ProveNanoseconds     uint64 `json:"prove_nanoseconds"`
}

type AdmissionMetricsSnapshot struct {
	Circuits map[string]CircuitAdmissionMetrics `json:"circuits"`
}

type AdmissionController struct {
	circuits map[string]*circuitAdmission
}

type circuitAdmission struct {
	config CircuitAdmissionConfig
	sem    chan struct{}
	now    func() time.Time

	mu      sync.Mutex
	metrics CircuitAdmissionMetrics
}

type admissionPermit struct {
	gate        *circuitAdmission
	startOnce   sync.Once
	releaseOnce sync.Once

	mu        sync.Mutex
	startedAt time.Time
	started   bool
	released  bool
}

func DefaultAdmissionConfig() AdmissionConfig {
	return AdmissionConfig{
		Circuits: map[string]CircuitAdmissionConfig{
			privacyprovertransport.DepositProofCircuitID: {
				MaxInFlight: DefaultMaxInFlightPerCircuit,
				MaxQueued:   DefaultMaxQueuedPerCircuit,
			},
			privacyprovertransport.TransferProofCircuitID: {
				MaxInFlight: DefaultMaxInFlightPerCircuit,
				MaxQueued:   DefaultMaxQueuedPerCircuit,
			},
			privacyprovertransport.WithdrawProofCircuitID: {
				MaxInFlight: DefaultMaxInFlightPerCircuit,
				MaxQueued:   DefaultMaxQueuedPerCircuit,
			},
			privacyprovertransport.BatchTransferProofCircuitID: {
				MaxInFlight: DefaultMaxInFlightPerCircuit,
				MaxQueued:   DefaultMaxQueuedPerCircuit,
			},
		},
		Now: time.Now,
	}
}

func NewAdmissionController(config AdmissionConfig) (*AdmissionController, error) {
	if len(config.Circuits) == 0 {
		return nil, fmt.Errorf("at least one prover circuit admission configuration is required")
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	controller := &AdmissionController{
		circuits: make(map[string]*circuitAdmission, len(config.Circuits)),
	}
	for circuitID, circuitConfig := range config.Circuits {
		if circuitID == "" {
			return nil, fmt.Errorf("prover admission circuit id is required")
		}
		if circuitConfig.MaxInFlight <= 0 {
			return nil, fmt.Errorf("prover admission max in-flight for %s must be positive", circuitID)
		}
		if circuitConfig.MaxQueued < 0 {
			return nil, fmt.Errorf("prover admission max queued for %s cannot be negative", circuitID)
		}
		controller.circuits[circuitID] = &circuitAdmission{
			config: circuitConfig,
			sem:    make(chan struct{}, circuitConfig.MaxInFlight),
			now:    now,
			metrics: CircuitAdmissionMetrics{
				MaxInFlight: circuitConfig.MaxInFlight,
				MaxQueued:   circuitConfig.MaxQueued,
			},
		}
	}
	return controller, nil
}

func (c *AdmissionController) Acquire(ctx context.Context, circuitID string) (privacyprovertransport.ProofPermit, error) {
	if c == nil {
		return nil, ErrAdmissionCircuitDisabled
	}
	gate := c.circuits[circuitID]
	if gate == nil {
		return nil, fmt.Errorf("%w: %s", ErrAdmissionCircuitDisabled, circuitID)
	}
	return gate.acquire(ctx)
}

func (c *AdmissionController) Snapshot() AdmissionMetricsSnapshot {
	if c == nil {
		return AdmissionMetricsSnapshot{Circuits: map[string]CircuitAdmissionMetrics{}}
	}
	snapshot := AdmissionMetricsSnapshot{Circuits: make(map[string]CircuitAdmissionMetrics, len(c.circuits))}
	ids := make([]string, 0, len(c.circuits))
	for circuitID := range c.circuits {
		ids = append(ids, circuitID)
	}
	sort.Strings(ids)
	for _, circuitID := range ids {
		gate := c.circuits[circuitID]
		gate.mu.Lock()
		snapshot.Circuits[circuitID] = gate.metrics
		gate.mu.Unlock()
	}
	return snapshot
}

func (g *circuitAdmission) acquire(ctx context.Context) (*admissionPermit, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		g.mu.Lock()
		g.metrics.TotalCanceled++
		g.mu.Unlock()
		return nil, fmt.Errorf("prover admission canceled: %w", err)
	}
	select {
	case g.sem <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-g.sem
			g.mu.Lock()
			g.metrics.TotalCanceled++
			g.mu.Unlock()
			return nil, fmt.Errorf("prover admission canceled: %w", err)
		}
		g.recordAdmitted(0)
		return &admissionPermit{gate: g}, nil
	default:
	}

	queuedAt := g.now()
	g.mu.Lock()
	if g.metrics.Queued >= g.config.MaxQueued {
		g.metrics.TotalRejected++
		g.mu.Unlock()
		return nil, ErrAdmissionQueueFull
	}
	g.metrics.Queued++
	g.metrics.TotalQueued++
	g.mu.Unlock()

	select {
	case g.sem <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-g.sem
			g.mu.Lock()
			g.metrics.Queued--
			g.metrics.TotalCanceled++
			g.mu.Unlock()
			return nil, fmt.Errorf("prover admission canceled: %w", err)
		}
		waited := g.now().Sub(queuedAt)
		if waited < 0 {
			waited = 0
		}
		g.mu.Lock()
		g.metrics.Queued--
		g.metrics.InFlight++
		g.metrics.TotalAdmitted++
		g.metrics.QueueWaitNanoseconds += uint64(waited)
		g.mu.Unlock()
		return &admissionPermit{gate: g}, nil
	case <-ctx.Done():
		g.mu.Lock()
		g.metrics.Queued--
		g.metrics.TotalCanceled++
		g.mu.Unlock()
		return nil, fmt.Errorf("prover admission canceled: %w", ctx.Err())
	}
}

func (g *circuitAdmission) recordAdmitted(waited time.Duration) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.metrics.InFlight++
	g.metrics.TotalAdmitted++
	if waited > 0 {
		g.metrics.QueueWaitNanoseconds += uint64(waited)
	}
}

func (p *admissionPermit) StartProve() {
	if p == nil || p.gate == nil {
		return
	}
	p.startOnce.Do(func() {
		startedAt := p.gate.now()
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.released {
			return
		}
		p.startedAt = startedAt
		p.started = true
		p.gate.mu.Lock()
		p.gate.metrics.Proving++
		p.gate.mu.Unlock()
	})
}

func (p *admissionPermit) Release() {
	if p == nil || p.gate == nil {
		return
	}
	p.releaseOnce.Do(func() {
		finishedAt := p.gate.now()
		p.mu.Lock()
		p.released = true
		startedAt := p.startedAt
		started := p.started

		p.gate.mu.Lock()
		p.gate.metrics.InFlight--
		p.gate.metrics.TotalReleased++
		if started {
			p.gate.metrics.Proving--
			p.gate.metrics.TotalProveCompleted++
			elapsed := finishedAt.Sub(startedAt)
			if elapsed > 0 {
				p.gate.metrics.ProveNanoseconds += uint64(elapsed)
			}
		}
		p.gate.mu.Unlock()
		p.mu.Unlock()
		<-p.gate.sem
	})
}

var _ privacyprovertransport.ProofAdmission = (*AdmissionController)(nil)
var _ privacyprovertransport.ProofPermit = (*admissionPermit)(nil)
