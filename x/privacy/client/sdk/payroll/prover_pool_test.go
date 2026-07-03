package payroll

import (
	"context"
	"fmt"
	"testing"

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
