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
	Endpoints      []ProverEndpoint
	RequestTimeout time.Duration

	mu   sync.Mutex
	next int
}

func (p *ProverPool) BuildPreparedTransferProof(ctx context.Context, payload privacytransfer.PreparedTransferPayload) (*privacytransfer.PreparedTransferProof, error) {
	if len(p.Endpoints) == 0 {
		return nil, fmt.Errorf("at least one prover endpoint is required")
	}

	start := p.nextEndpointIndex()
	var failures []string
	for attempt := 0; attempt < len(p.Endpoints); attempt++ {
		endpoint := p.Endpoints[(start+attempt)%len(p.Endpoints)]
		if endpoint.Runner == nil {
			failures = append(failures, fmt.Sprintf("%s: runner is nil", endpointName(endpoint, attempt)))
			continue
		}

		proofCtx := ctx
		cancel := func() {}
		if p.RequestTimeout > 0 {
			proofCtx, cancel = context.WithTimeout(ctx, p.RequestTimeout)
		}
		proof, err := endpoint.Runner.BuildPreparedTransferProof(proofCtx, payload)
		cancel()
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
