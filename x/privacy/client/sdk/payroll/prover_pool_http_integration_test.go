package payroll

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/provertransport"
	privacytransfer "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/transfer"
)

const proverFailoverEvidenceEnv = "CLAIRVEIL_PROVER_FAILOVER_EVIDENCE_OUT"

type liveProverFixture struct {
	SchemaVersion string `json:"schema_version"`
	Transfer      struct {
		Request  provertransport.TransferProofRequest  `json:"request"`
		Response provertransport.TransferProofResponse `json:"response"`
	} `json:"transfer"`
}

type liveHTTPPreparedProofRunner struct {
	client provertransport.HTTPProverClient
	now    time.Time
}

func (r liveHTTPPreparedProofRunner) BuildPreparedTransferProof(ctx context.Context, payload privacytransfer.PreparedTransferPayload) (*privacytransfer.PreparedTransferProof, error) {
	request, err := provertransport.NewTransferProofRequestAt(payload, r.now)
	if err != nil {
		return nil, err
	}
	response, err := r.client.ProveTransfer(ctx, *request)
	if err != nil {
		return nil, err
	}
	return &response.Proof, nil
}

type observedHTTPProver struct {
	contacts atomic.Int64
	mu       sync.Mutex
	bodies   [][]byte
	handle   func(http.ResponseWriter, *http.Request)
}

func (p *observedHTTPProver) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.contacts.Add(1)
	body, err := io.ReadAll(io.LimitReader(r.Body, provertransport.DefaultMaxRequestBytes+1))
	if err != nil {
		http.Error(w, "request read failed", http.StatusBadRequest)
		return
	}
	p.mu.Lock()
	p.bodies = append(p.bodies, append([]byte(nil), body...))
	p.mu.Unlock()
	if p.handle == nil {
		http.Error(w, "handler unavailable", http.StatusServiceUnavailable)
		return
	}
	p.handle(w, r)
}

func (p *observedHTTPProver) snapshotBodies() [][]byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([][]byte, len(p.bodies))
	for i := range p.bodies {
		out[i] = append([]byte(nil), p.bodies[i]...)
	}
	return out
}

func TestProverPoolLiveHTTPFailoverPrivacyBoundary(t *testing.T) {
	fixture := loadLiveProverFixture(t)
	now := time.Unix(4102444800, 0)
	expectedRequestBody, err := json.Marshal(fixture.Transfer.Request)
	require.NoError(t, err)
	responseBody, err := json.Marshal(fixture.Transfer.Response)
	require.NoError(t, err)

	type endpointEvidence struct {
		TimeoutEndpointContacts int64 `json:"timeout_endpoint_contacts"`
		HealthyEndpointContacts int64 `json:"healthy_endpoint_contacts"`
		TimeoutBodyObserved     bool  `json:"timeout_body_observed"`
		HealthyBodyObserved     bool  `json:"healthy_body_observed"`
		BodiesEqual             bool  `json:"bodies_equal,omitempty"`
		CompletedFromHealthy    bool  `json:"completed_from_healthy,omitempty"`
	}
	evidence := struct {
		SchemaVersion  string           `json:"schema_version"`
		Default        endpointEvidence `json:"default_no_failover"`
		OptIn          endpointEvidence `json:"explicit_opt_in_failover"`
		FailureClasses struct {
			TimeoutDistinct             bool `json:"timeout_distinct"`
			MalformedResponseDistinct   bool `json:"malformed_response_distinct"`
			ValidationFailureDistinct   bool `json:"validation_failure_distinct"`
			EachEndpointContactCountOne bool `json:"each_endpoint_contact_count_one"`
		} `json:"failure_classes"`
	}{SchemaVersion: "clairveil.prover-failover-live-evidence.v1"}

	t.Run("default_no_failover", func(t *testing.T) {
		timeoutEndpoint, timeoutServer := newObservedTimeoutServer(t)
		healthyEndpoint, healthyServer := newObservedResponseServer(t, responseBody)
		pool := liveHTTPProverPool(now, timeoutServer.URL, healthyServer.URL, nil)

		proof, err := pool.BuildPreparedTransferProof(context.Background(), fixture.Transfer.Request.Payload)
		require.Nil(t, proof)
		require.ErrorIs(t, err, context.DeadlineExceeded)

		timeoutBodies := timeoutEndpoint.snapshotBodies()
		healthyBodies := healthyEndpoint.snapshotBodies()
		evidence.Default = endpointEvidence{
			TimeoutEndpointContacts: timeoutEndpoint.contacts.Load(),
			HealthyEndpointContacts: healthyEndpoint.contacts.Load(),
			TimeoutBodyObserved:     len(timeoutBodies) == 1 && bytes.Equal(timeoutBodies[0], expectedRequestBody),
			HealthyBodyObserved:     len(healthyBodies) != 0,
		}
		require.Equal(t, int64(1), evidence.Default.TimeoutEndpointContacts)
		require.Equal(t, int64(0), evidence.Default.HealthyEndpointContacts)
		if !evidence.Default.TimeoutBodyObserved || evidence.Default.HealthyBodyObserved {
			t.Fatal("default no-failover request-body observation did not match the privacy boundary")
		}
	})

	t.Run("explicit_opt_in_failover", func(t *testing.T) {
		timeoutEndpoint, timeoutServer := newObservedTimeoutServer(t)
		healthyEndpoint, healthyServer := newObservedResponseServer(t, responseBody)
		pool := liveHTTPProverPool(now, timeoutServer.URL, healthyServer.URL, acknowledgedMultiProverFailover("timeout", "healthy"))

		proof, err := pool.BuildPreparedTransferProof(context.Background(), fixture.Transfer.Request.Payload)
		require.NoError(t, err)
		if proof == nil || proof.PayloadHash != fixture.Transfer.Response.Proof.PayloadHash || proof.ProofHex != fixture.Transfer.Response.Proof.ProofHex {
			t.Fatal("opt-in failover did not complete with the healthy endpoint response")
		}

		timeoutBodies := timeoutEndpoint.snapshotBodies()
		healthyBodies := healthyEndpoint.snapshotBodies()
		bodiesEqual := len(timeoutBodies) == 1 && len(healthyBodies) == 1 && bytes.Equal(timeoutBodies[0], expectedRequestBody) && bytes.Equal(healthyBodies[0], expectedRequestBody) && bytes.Equal(timeoutBodies[0], healthyBodies[0])
		evidence.OptIn = endpointEvidence{
			TimeoutEndpointContacts: timeoutEndpoint.contacts.Load(),
			HealthyEndpointContacts: healthyEndpoint.contacts.Load(),
			TimeoutBodyObserved:     len(timeoutBodies) == 1,
			HealthyBodyObserved:     len(healthyBodies) == 1,
			BodiesEqual:             bodiesEqual,
			CompletedFromHealthy:    true,
		}
		require.Equal(t, int64(1), evidence.OptIn.TimeoutEndpointContacts)
		require.Equal(t, int64(1), evidence.OptIn.HealthyEndpointContacts)
		if !bodiesEqual {
			t.Fatal("opt-in failover endpoints did not observe the same expected request body")
		}
	})

	t.Run("failure_classes", func(t *testing.T) {
		timeoutEndpoint, timeoutServer := newObservedTimeoutServer(t)
		timeoutPool := liveHTTPProverPool(now, timeoutServer.URL, "", nil)
		_, timeoutErr := timeoutPool.BuildPreparedTransferProof(context.Background(), fixture.Transfer.Request.Payload)
		evidence.FailureClasses.TimeoutDistinct = errors.Is(timeoutErr, context.DeadlineExceeded)

		malformedEndpoint, malformedServer := newObservedResponseServer(t, []byte("{"))
		malformedPool := liveHTTPProverPool(now, malformedServer.URL, "", nil)
		_, malformedErr := malformedPool.BuildPreparedTransferProof(context.Background(), fixture.Transfer.Request.Payload)
		evidence.FailureClasses.MalformedResponseDistinct = malformedErr != nil && errors.Is(malformedErr, context.DeadlineExceeded) == false && containsError(malformedErr, "invalid transfer proof response JSON")

		invalidResponse := fixture.Transfer.Response
		invalidResponse.Proof.PayloadHash = "00"
		invalidResponseBody, err := json.Marshal(invalidResponse)
		require.NoError(t, err)
		validationEndpoint, validationServer := newObservedResponseServer(t, invalidResponseBody)
		validationPool := liveHTTPProverPool(now, validationServer.URL, "", nil)
		_, validationErr := validationPool.BuildPreparedTransferProof(context.Background(), fixture.Transfer.Request.Payload)
		evidence.FailureClasses.ValidationFailureDistinct = validationErr != nil && errors.Is(validationErr, context.DeadlineExceeded) == false && containsError(validationErr, "transfer proof payload hash mismatch")
		evidence.FailureClasses.EachEndpointContactCountOne = timeoutEndpoint.contacts.Load() == 1 && malformedEndpoint.contacts.Load() == 1 && validationEndpoint.contacts.Load() == 1

		if !evidence.FailureClasses.TimeoutDistinct || !evidence.FailureClasses.MalformedResponseDistinct || !evidence.FailureClasses.ValidationFailureDistinct || !evidence.FailureClasses.EachEndpointContactCountOne {
			t.Fatal("live HTTP prover failure classes were not distinct or endpoint counts were unexpected")
		}
	})

	writeProverFailoverEvidence(t, evidence)
}

func loadLiveProverFixture(t *testing.T) liveProverFixture {
	t.Helper()
	payload, err := os.ReadFile("../conformance/testdata/privacy_prover_example_bundle.json")
	require.NoError(t, err)
	var fixture liveProverFixture
	require.NoError(t, json.Unmarshal(payload, &fixture))
	require.Equal(t, "v2", fixture.SchemaVersion)
	return fixture
}

func liveHTTPProverPool(now time.Time, timeoutURL, healthyURL string, optIn *MultiProverFailoverOptIn) *ProverPool {
	endpoints := []ProverEndpoint{{ID: "timeout", Runner: liveHTTPPreparedProofRunner{
		client: provertransport.HTTPProverClient{BaseURL: timeoutURL, Client: &http.Client{}, Now: func() time.Time { return now }},
		now:    now,
	}}}
	if healthyURL != "" {
		endpoints = append(endpoints, ProverEndpoint{ID: "healthy", Runner: liveHTTPPreparedProofRunner{
			client: provertransport.HTTPProverClient{BaseURL: healthyURL, Client: &http.Client{}, Now: func() time.Time { return now }},
			now:    now,
		}})
	}
	return &ProverPool{Endpoints: endpoints, RequestTimeout: 100 * time.Millisecond, MultiProverFailover: optIn}
}

func newObservedTimeoutServer(t *testing.T) (*observedHTTPProver, *httptest.Server) {
	t.Helper()
	endpoint := &observedHTTPProver{handle: func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}}
	server := httptest.NewServer(endpoint)
	t.Cleanup(server.Close)
	return endpoint, server
}

func newObservedResponseServer(t *testing.T, response []byte) (*observedHTTPProver, *httptest.Server) {
	t.Helper()
	endpoint := &observedHTTPProver{handle: func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(response)
	}}
	server := httptest.NewServer(endpoint)
	t.Cleanup(server.Close)
	return endpoint, server
}

func containsError(err error, fragment string) bool {
	return err != nil && bytes.Contains([]byte(err.Error()), []byte(fragment))
}

func writeProverFailoverEvidence(t *testing.T, evidence any) {
	t.Helper()
	path := os.Getenv(proverFailoverEvidenceEnv)
	if path == "" {
		return
	}
	payload, err := json.MarshalIndent(evidence, "", "  ")
	require.NoError(t, err)
	payload = append(payload, '\n')
	require.NoError(t, os.WriteFile(path, payload, 0o600))
	require.NoError(t, os.Chmod(path, 0o600))
}
