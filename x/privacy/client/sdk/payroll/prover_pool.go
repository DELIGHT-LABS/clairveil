package payroll

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	privacytransfer "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/transfer"
)

type ProverEndpoint struct {
	ID     string
	Runner PreparedProofRunner
}

const MultiProverFailoverPrivacyWarning = "multi-prover failover can send the same privacy-sensitive prover payload to multiple endpoints"

// MultiProverFailoverOptIn explicitly allows a prepared prover payload to be
// retried against more than one endpoint. EndpointIDs is the complete ordered
// set of endpoints that may receive the same payload.
type MultiProverFailoverOptIn struct {
	EndpointIDs                []string
	PrivacyWarningAcknowledged bool
}

// MultiProverFailoverDisclosure is suitable for presenting the expanded
// privacy boundary before multi-prover failover is enabled.
type MultiProverFailoverDisclosure struct {
	EndpointIDs    []string
	PrivacyWarning string
}

func (o MultiProverFailoverOptIn) Disclosure() MultiProverFailoverDisclosure {
	return MultiProverFailoverDisclosure{
		EndpointIDs:    append([]string(nil), o.EndpointIDs...),
		PrivacyWarning: MultiProverFailoverPrivacyWarning,
	}
}

type ProverPool struct {
	Endpoints                 []ProverEndpoint
	RequestTimeout            time.Duration
	MaxConcurrencyPerEndpoint int
	MultiProverFailover       *MultiProverFailoverOptIn

	mu         sync.Mutex
	next       int
	semaphores map[int]chan struct{}
}

type proverEndpointAttempt struct {
	Endpoint ProverEndpoint
	Index    int
}

func (p *ProverPool) BuildPreparedTransferProof(ctx context.Context, payload privacytransfer.PreparedTransferPayload) (*privacytransfer.PreparedTransferProof, error) {
	endpointIndices, err := p.endpointIndicesForRequest()
	if err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var failures []string
	var lastErr error
	var busy []proverEndpointAttempt
	for attempt := 0; attempt < len(endpointIndices); attempt++ {
		index := endpointIndices[attempt]
		endpoint := p.Endpoints[index]
		if endpoint.Runner == nil {
			lastErr = fmt.Errorf("runner is nil")
			failures = append(failures, fmt.Sprintf("%s: %v", endpointName(endpoint, index), lastErr))
			continue
		}
		release, ok := p.tryAcquireEndpoint(index)
		if !ok {
			busy = append(busy, proverEndpointAttempt{Endpoint: endpoint, Index: index})
			continue
		}
		attemptCtx := ctx
		cancel := func() {}
		if p.RequestTimeout > 0 {
			attemptCtx, cancel = context.WithTimeout(ctx, p.RequestTimeout)
		}
		proof, err := runProverEndpointAttempt(attemptCtx, endpoint, payload, release)
		if err == nil {
			cancel()
			return proof, nil
		}
		cancel()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		lastErr = err
		failures = append(failures, fmt.Sprintf("%s: %v", endpointName(endpoint, index), err))
	}
	for _, candidate := range busy {
		attemptCtx := ctx
		cancel := func() {}
		if p.RequestTimeout > 0 {
			attemptCtx, cancel = context.WithTimeout(ctx, p.RequestTimeout)
		}
		release, err := p.acquireEndpoint(attemptCtx, candidate.Index)
		if err != nil {
			cancel()
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			lastErr = err
			failures = append(failures, fmt.Sprintf("%s: %v", endpointName(candidate.Endpoint, candidate.Index), err))
			continue
		}
		proof, err := runProverEndpointAttempt(attemptCtx, candidate.Endpoint, payload, release)
		if err == nil {
			cancel()
			return proof, nil
		}
		cancel()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		lastErr = err
		failures = append(failures, fmt.Sprintf("%s: %v", endpointName(candidate.Endpoint, candidate.Index), err))
	}

	if lastErr != nil {
		return nil, fmt.Errorf("selected prover endpoints failed: %s: %w", strings.Join(failures, "; "), lastErr)
	}
	return nil, fmt.Errorf("selected prover endpoints failed: %s", strings.Join(failures, "; "))
}

// Validate rejects an ambiguous multi-prover privacy boundary before any
// prepared payload is sent. Without MultiProverFailover, validation still
// permits multiple configured endpoints because each request selects exactly
// one of them and never fails over to another endpoint.
func (p *ProverPool) Validate() error {
	_, err := p.configuredFailoverEndpointIndices()
	return err
}

func (p *ProverPool) endpointIndicesForRequest() ([]int, error) {
	indices, err := p.configuredFailoverEndpointIndices()
	if err != nil {
		return nil, err
	}
	start := p.nextEndpointIndex(len(indices))
	if p.MultiProverFailover != nil {
		ordered := make([]int, 0, len(indices))
		ordered = append(ordered, indices[start:]...)
		ordered = append(ordered, indices[:start]...)
		return ordered, nil
	}
	return []int{indices[start]}, nil
}

func (p *ProverPool) configuredFailoverEndpointIndices() ([]int, error) {
	if p == nil {
		return nil, fmt.Errorf("prover pool is required")
	}
	if len(p.Endpoints) == 0 {
		return nil, fmt.Errorf("at least one prover endpoint is required")
	}
	if p.MultiProverFailover == nil {
		indices := make([]int, len(p.Endpoints))
		for i := range p.Endpoints {
			indices[i] = i
		}
		return indices, nil
	}

	optIn := p.MultiProverFailover
	if !optIn.PrivacyWarningAcknowledged {
		return nil, fmt.Errorf("multi-prover failover requires acknowledging the privacy warning: %s", MultiProverFailoverPrivacyWarning)
	}
	if len(optIn.EndpointIDs) < 2 {
		return nil, fmt.Errorf("multi-prover failover requires at least two endpoint IDs")
	}

	configured := make(map[string]int, len(p.Endpoints))
	duplicateConfigured := make(map[string]struct{})
	for i, endpoint := range p.Endpoints {
		if endpoint.ID == "" {
			continue
		}
		if _, exists := configured[endpoint.ID]; exists {
			duplicateConfigured[endpoint.ID] = struct{}{}
			continue
		}
		configured[endpoint.ID] = i
	}

	indices := make([]int, 0, len(optIn.EndpointIDs))
	seen := make(map[string]struct{}, len(optIn.EndpointIDs))
	for _, endpointID := range optIn.EndpointIDs {
		if strings.TrimSpace(endpointID) == "" {
			return nil, fmt.Errorf("multi-prover failover endpoint ID cannot be empty")
		}
		if _, exists := seen[endpointID]; exists {
			return nil, fmt.Errorf("multi-prover failover endpoint ID %q is duplicated", endpointID)
		}
		seen[endpointID] = struct{}{}
		if _, duplicate := duplicateConfigured[endpointID]; duplicate {
			return nil, fmt.Errorf("configured prover endpoint ID %q is not unique", endpointID)
		}
		index, exists := configured[endpointID]
		if !exists {
			return nil, fmt.Errorf("multi-prover failover endpoint ID %q is not configured", endpointID)
		}
		indices = append(indices, index)
	}
	return indices, nil
}

func runProverEndpointAttempt(ctx context.Context, endpoint ProverEndpoint, payload privacytransfer.PreparedTransferPayload, release func()) (*privacytransfer.PreparedTransferProof, error) {
	defer release()
	proof, err := endpoint.Runner.BuildPreparedTransferProof(ctx, payload)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	if err != nil {
		return nil, err
	}
	if proof == nil {
		return nil, fmt.Errorf("returned nil proof")
	}
	return proof, nil
}

func (p *ProverPool) tryAcquireEndpoint(index int) (func(), bool) {
	if p.MaxConcurrencyPerEndpoint <= 0 {
		return func() {}, true
	}
	sem := p.endpointSemaphore(index)
	select {
	case sem <- struct{}{}:
		return func() { <-sem }, true
	default:
		return nil, false
	}
}

func (p *ProverPool) acquireEndpoint(ctx context.Context, index int) (func(), error) {
	if p.MaxConcurrencyPerEndpoint <= 0 {
		return func() {}, nil
	}

	sem := p.endpointSemaphore(index)
	select {
	case sem <- struct{}{}:
		return func() { <-sem }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (p *ProverPool) endpointSemaphore(index int) chan struct{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.semaphores == nil {
		p.semaphores = make(map[int]chan struct{}, len(p.Endpoints))
	}
	sem := p.semaphores[index]
	if sem == nil {
		sem = make(chan struct{}, p.MaxConcurrencyPerEndpoint)
		p.semaphores[index] = sem
	}
	return sem
}

func (p *ProverPool) nextEndpointIndex(endpointCount int) int {
	p.mu.Lock()
	defer p.mu.Unlock()

	index := p.next % endpointCount
	p.next = (index + 1) % endpointCount
	return index
}

func endpointName(endpoint ProverEndpoint, index int) string {
	if endpoint.ID != "" {
		return endpoint.ID
	}
	return fmt.Sprintf("endpoint-%d", index)
}
