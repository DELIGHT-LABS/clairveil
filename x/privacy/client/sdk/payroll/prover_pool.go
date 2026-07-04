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

type ProverPool struct {
	Endpoints                 []ProverEndpoint
	RequestTimeout            time.Duration
	MaxConcurrencyPerEndpoint int

	mu         sync.Mutex
	next       int
	semaphores map[int]chan struct{}
}

type proverEndpointAttempt struct {
	Endpoint ProverEndpoint
	Index    int
	Attempt  int
}

func (p *ProverPool) BuildPreparedTransferProof(ctx context.Context, payload privacytransfer.PreparedTransferPayload) (*privacytransfer.PreparedTransferProof, error) {
	if len(p.Endpoints) == 0 {
		return nil, fmt.Errorf("at least one prover endpoint is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	start := p.nextEndpointIndex()
	var failures []string
	var lastErr error
	var busy []proverEndpointAttempt
	for attempt := 0; attempt < len(p.Endpoints); attempt++ {
		index := (start + attempt) % len(p.Endpoints)
		endpoint := p.Endpoints[index]
		if endpoint.Runner == nil {
			lastErr = fmt.Errorf("runner is nil")
			failures = append(failures, fmt.Sprintf("%s: %v", endpointName(endpoint, attempt), lastErr))
			continue
		}
		release, ok := p.tryAcquireEndpoint(index)
		if !ok {
			busy = append(busy, proverEndpointAttempt{Endpoint: endpoint, Index: index, Attempt: attempt})
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
		failures = append(failures, fmt.Sprintf("%s: %v", endpointName(endpoint, attempt), err))
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
			failures = append(failures, fmt.Sprintf("%s: %v", endpointName(candidate.Endpoint, candidate.Attempt), err))
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
		failures = append(failures, fmt.Sprintf("%s: %v", endpointName(candidate.Endpoint, candidate.Attempt), err))
	}

	if lastErr != nil {
		return nil, fmt.Errorf("all prover endpoints failed: %s: %w", strings.Join(failures, "; "), lastErr)
	}
	return nil, fmt.Errorf("all prover endpoints failed: %s", strings.Join(failures, "; "))
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

func (p *ProverPool) nextEndpointIndex() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	index := p.next
	p.next = (p.next + 1) % len(p.Endpoints)
	return index
}

func endpointName(endpoint ProverEndpoint, index int) string {
	if endpoint.ID != "" {
		return endpoint.ID
	}
	return fmt.Sprintf("endpoint-%d", index)
}
