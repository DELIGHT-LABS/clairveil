package provertransport

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"
)

func BenchmarkHTTPProverClientTransferRoundTrip(b *testing.B) {
	request, client, cleanup := benchmarkTransferClient(b)
	defer cleanup()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := client.ProveTransfer(context.Background(), *request); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkHTTPProverClientTransferParallelRoundTrip(b *testing.B) {
	request, client, cleanup := benchmarkTransferClient(b)
	defer cleanup()

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := client.ProveTransfer(context.Background(), *request); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkHTTPProverClientWithdrawRoundTrip(b *testing.B) {
	request, client, cleanup := benchmarkWithdrawClient(b)
	defer cleanup()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := client.ProveWithdraw(context.Background(), *request); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkHTTPProverClientWithdrawParallelRoundTrip(b *testing.B) {
	request, client, cleanup := benchmarkWithdrawClient(b)
	defer cleanup()

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := client.ProveWithdraw(context.Background(), *request); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func benchmarkTransferClient(b testing.TB) (*TransferProofRequest, HTTPProverClient, func()) {
	b.Helper()

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

	client := HTTPProverClient{
		BaseURL: server.URL,
		Client:  server.Client(),
	}

	return request, client, server.Close
}

func benchmarkWithdrawClient(b testing.TB) (*WithdrawProofRequest, HTTPProverClient, func()) {
	b.Helper()

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

	client := HTTPProverClient{
		BaseURL: server.URL,
		Client:  server.Client(),
		Now:     func() time.Time { return now },
	}

	return request, client, server.Close
}
