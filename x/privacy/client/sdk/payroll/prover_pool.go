package payroll

import (
	"context"
	"errors"
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

func (p *ProverPool) BuildPreparedTransferProof(ctx context.Context, payload privacytransfer.PreparedTransferPayload) (*privacytransfer.PreparedTransferProof, error) {
	if len(p.Endpoints) == 0 {
		return nil, fmt.Errorf("at least one prover endpoint is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	start := p.nextEndpointIndex()
	var failures []string
	for attempt := 0; attempt < len(p.Endpoints); attempt++ {
		endpoint := p.Endpoints[(start+attempt)%len(p.Endpoints)]
		if endpoint.Runner == nil {
			failures = append(failures, fmt.Sprintf("%s: runner is nil", endpointName(endpoint, attempt)))
			continue
		}
		release, err := p.acquireEndpoint(ctx, (start+attempt)%len(p.Endpoints))
		if err != nil {
			return nil, err
		}

		proofCtx := ctx
		cancel := func() {}
		if p.RequestTimeout > 0 {
			proofCtx, cancel = context.WithTimeout(ctx, p.RequestTimeout)
		}
		proof, err := endpoint.Runner.BuildPreparedTransferProof(proofCtx, payload)
		cancel()
		release()
		if err == nil {
			return proof, nil
		}
		if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, ctx.Err()
		}
		failures = append(failures, fmt.Sprintf("%s: %v", endpointName(endpoint, attempt), err))
	}

	return nil, fmt.Errorf("all prover endpoints failed: %s", strings.Join(failures, "; "))
}

func (p *ProverPool) acquireEndpoint(ctx context.Context, index int) (func(), error) {
	if p.MaxConcurrencyPerEndpoint <= 0 {
		return func() {}, nil
	}

	p.mu.Lock()
	if p.semaphores == nil {
		p.semaphores = make(map[int]chan struct{}, len(p.Endpoints))
	}
	sem := p.semaphores[index]
	if sem == nil {
		sem = make(chan struct{}, p.MaxConcurrencyPerEndpoint)
		p.semaphores[index] = sem
	}
	p.mu.Unlock()

	select {
	case sem <- struct{}{}:
		return func() { <-sem }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
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
