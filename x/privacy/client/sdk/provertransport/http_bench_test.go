package provertransport

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"
)

func BenchmarkHTTPProverClientTransferRoundTrip(b *testing.B) {
	payload, artifacts, runner := testPreparedTransferPayload(b)
	request, err := NewTransferProofRequest(payload)
	if err != nil {
		b.Fatal(err)
	}

	server := httptest.NewServer(NewHTTPHandler(
		ReferenceTransferProver{Artifacts: artifacts, Runner: runner},
		nil,
		nil,
	))
	defer server.Close()

	client := HTTPProverClient{
		BaseURL: server.URL,
		Client:  server.Client(),
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := client.ProveTransfer(context.Background(), *request); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkHTTPProverClientWithdrawRoundTrip(b *testing.B) {
	now := time.Now()
	payload, artifacts, runner := testPreparedWithdrawProverPayload(b, now)
	request, err := NewWithdrawProofRequest(payload, now)
	if err != nil {
		b.Fatal(err)
	}

	server := httptest.NewServer(NewHTTPHandler(
		nil,
		ReferenceWithdrawProver{Artifacts: artifacts, Runner: runner},
		func() time.Time { return now },
	))
	defer server.Close()

	client := HTTPProverClient{
		BaseURL: server.URL,
		Client:  server.Client(),
		Now:     func() time.Time { return now },
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := client.ProveWithdraw(context.Background(), *request); err != nil {
			b.Fatal(err)
		}
	}
}
