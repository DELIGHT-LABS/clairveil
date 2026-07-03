package payroll

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	privacytransfer "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/transfer"
)

func TestProverPoolRoundRobinsEndpoints(t *testing.T) {
	first := &recordingProofRunner{id: "first"}
	second := &recordingProofRunner{id: "second"}
	pool := &ProverPool{
		Endpoints: []ProverEndpoint{
			{ID: "first", Runner: first},
			{ID: "second", Runner: second},
		},
	}

	_, err := pool.BuildPreparedTransferProof(context.Background(), privacytransfer.PreparedTransferPayload{PayloadHash: "a"})
	require.NoError(t, err)
	_, err = pool.BuildPreparedTransferProof(context.Background(), privacytransfer.PreparedTransferPayload{PayloadHash: "b"})
	require.NoError(t, err)
	_, err = pool.BuildPreparedTransferProof(context.Background(), privacytransfer.PreparedTransferPayload{PayloadHash: "c"})
	require.NoError(t, err)

	require.Equal(t, 2, first.calls)
	require.Equal(t, 1, second.calls)
}

func TestProverPoolFallsBackToNextEndpoint(t *testing.T) {
	failing := &recordingProofRunner{id: "failing", err: fmt.Errorf("temporary outage")}
	healthy := &recordingProofRunner{id: "healthy"}
	pool := &ProverPool{
		Endpoints: []ProverEndpoint{
			{ID: "failing", Runner: failing},
			{ID: "healthy", Runner: healthy},
		},
	}

	proof, err := pool.BuildPreparedTransferProof(context.Background(), privacytransfer.PreparedTransferPayload{PayloadHash: "payload"})
	require.NoError(t, err)
	require.Equal(t, "payload", proof.PayloadHash)
	require.Equal(t, 1, failing.calls)
	require.Equal(t, 1, healthy.calls)
}

func TestProverPoolLimitsEndpointConcurrency(t *testing.T) {
	runner := &blockingProofRunner{
		entered: make(chan struct{}, 2),
		release: make(chan struct{}),
	}
	pool := &ProverPool{
		Endpoints: []ProverEndpoint{
			{ID: "single", Runner: runner},
		},
		MaxConcurrencyPerEndpoint: 1,
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, err := pool.BuildPreparedTransferProof(context.Background(), privacytransfer.PreparedTransferPayload{PayloadHash: "a"})
		require.NoError(t, err)
	}()
	<-runner.entered

	secondDone := make(chan struct{})
	go func() {
		defer wg.Done()
		defer close(secondDone)
		_, err := pool.BuildPreparedTransferProof(context.Background(), privacytransfer.PreparedTransferPayload{PayloadHash: "b"})
		require.NoError(t, err)
	}()

	select {
	case <-runner.entered:
		t.Fatal("second proof entered endpoint before first proof released")
	case <-time.After(25 * time.Millisecond):
	}

	runner.release <- struct{}{}
	<-runner.entered
	runner.release <- struct{}{}
	wg.Wait()
	<-secondDone
}

type recordingProofRunner struct {
	id    string
	err   error
	calls int
}

func (r *recordingProofRunner) BuildPreparedTransferProof(_ context.Context, payload privacytransfer.PreparedTransferPayload) (*privacytransfer.PreparedTransferProof, error) {
	r.calls++
	if r.err != nil {
		return nil, r.err
	}
	return &privacytransfer.PreparedTransferProof{
		Version:     privacytransfer.PreparedTransferProofVersion,
		PayloadHash: payload.PayloadHash,
		ProofHex:    r.id + "-proof",
	}, nil
}

type blockingProofRunner struct {
	entered chan struct{}
	release chan struct{}
}

func (r *blockingProofRunner) BuildPreparedTransferProof(_ context.Context, payload privacytransfer.PreparedTransferPayload) (*privacytransfer.PreparedTransferProof, error) {
	r.entered <- struct{}{}
	<-r.release
	return &privacytransfer.PreparedTransferProof{
		Version:     privacytransfer.PreparedTransferProofVersion,
		PayloadHash: payload.PayloadHash,
		ProofHex:    "proof",
	}, nil
}
