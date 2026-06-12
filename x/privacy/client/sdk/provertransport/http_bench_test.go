package provertransport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
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
	var firstErr atomic.Value
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := client.ProveTransfer(context.Background(), *request); err != nil {
				firstErr.Store(err.Error())
				return
			}
		}
	})
	if err, ok := firstErr.Load().(string); ok {
		b.Fatal(err)
	}
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
	var firstErr atomic.Value
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := client.ProveWithdraw(context.Background(), *request); err != nil {
				firstErr.Store(err.Error())
				return
			}
		}
	})
	if err, ok := firstErr.Load().(string); ok {
		b.Fatal(err)
	}
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

	httpClient, cleanupClient := benchmarkHTTPClient()
	client := HTTPProverClient{
		BaseURL: server.URL,
		Client:  httpClient,
	}

	return request, client, func() {
		cleanupClient()
		server.Close()
	}
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

	httpClient, cleanupClient := benchmarkHTTPClient()
	client := HTTPProverClient{
		BaseURL: server.URL,
		Client:  httpClient,
		Now:     func() time.Time { return now },
	}

	return request, client, func() {
		cleanupClient()
		server.Close()
	}
}

func benchmarkHTTPClient() (*http.Client, func()) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = 1024
	transport.MaxIdleConnsPerHost = 1024

	return &http.Client{
			Transport: transport,
			Timeout:   30 * time.Second,
		}, func() {
			transport.CloseIdleConnections()
		}
}
