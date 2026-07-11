package provider

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	cmttypes "github.com/cometbft/cometbft/rpc/core/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestScanQueryProviderLatestBlockHeight(t *testing.T) {
	rpcClient := stubScanRPCClient{
		statusResponse: &cmttypes.ResultStatus{
			SyncInfo: cmttypes.SyncInfo{LatestBlockHeight: 17},
		},
	}
	provider := ScanQueryProvider{
		RPCClient: &rpcClient,
	}

	height, err := provider.LatestBlockHeight(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(17), height)
}

func TestScanQueryProviderSearchPrivacyTxs(t *testing.T) {
	rpcClient := stubScanRPCClient{
		txSearchResponse: &cmttypes.ResultTxSearch{
			Txs: []*cmttypes.ResultTx{
				{Hash: []byte{0xAA}},
			},
		},
	}
	provider := ScanQueryProvider{
		RPCClient: &rpcClient,
	}

	txs, err := provider.SearchPrivacyTxs(context.Background(), 9, 2, 50)
	require.NoError(t, err)
	require.Len(t, txs, 1)
	require.Equal(t, "AA", txs[0].Hash.String())
	require.Equal(t, "message.module='privacy' AND tx.height > 9", rpcClient.lastQuery)
	require.Equal(t, 2, rpcClient.lastPage)
	require.Equal(t, 50, rpcClient.lastLimit)
}

func TestScanQueryProviderSearchPrivacyTxsAggregatesClampedRPCTxSearch(t *testing.T) {
	rpcClient := stubScanRPCClient{
		txSearchResponses: []*cmttypes.ResultTxSearch{
			{Txs: testResultTxs(1, 100), TotalCount: 250},
			{Txs: testResultTxs(101, 100), TotalCount: 250},
			{Txs: testResultTxs(201, 50), TotalCount: 250},
		},
	}
	provider := ScanQueryProvider{
		RPCClient: &rpcClient,
	}

	txs, err := provider.SearchPrivacyTxs(context.Background(), 9, 1, 1000)
	require.NoError(t, err)
	require.Len(t, txs, 250)
	require.Len(t, rpcClient.txSearchRequests, 3)
	require.Equal(t, 1, rpcClient.txSearchRequests[0].page)
	require.Equal(t, 100, rpcClient.txSearchRequests[0].limit)
	require.Equal(t, 2, rpcClient.txSearchRequests[1].page)
	require.Equal(t, 100, rpcClient.txSearchRequests[1].limit)
	require.Equal(t, 3, rpcClient.txSearchRequests[2].page)
	require.Equal(t, 100, rpcClient.txSearchRequests[2].limit)
}

func TestScanQueryProviderSearchPrivacyTxsMapsLogicalPagesForRPCTxSearch(t *testing.T) {
	rpcClient := stubScanRPCClient{
		txSearchResponse: &cmttypes.ResultTxSearch{
			Txs:        testResultTxs(1001, 1),
			TotalCount: 1001,
		},
	}
	provider := ScanQueryProvider{
		RPCClient: &rpcClient,
	}

	txs, err := provider.SearchPrivacyTxs(context.Background(), 9, 2, 1000)
	require.NoError(t, err)
	require.Len(t, txs, 1)
	require.Len(t, rpcClient.txSearchRequests, 1)
	require.Equal(t, 11, rpcClient.txSearchRequests[0].page)
	require.Equal(t, 100, rpcClient.txSearchRequests[0].limit)
}

func TestScanQueryProviderSearchPrivacyTxsReturnsEmptyForRPCTxSearchPagePastEnd(t *testing.T) {
	rpcClient := stubScanRPCClient{
		txSearchErr: fmt.Errorf("page should be within [1, 10] range, given 11"),
	}
	provider := ScanQueryProvider{
		RPCClient: &rpcClient,
	}

	txs, err := provider.SearchPrivacyTxs(context.Background(), 9, 2, 1000)
	require.NoError(t, err)
	require.Empty(t, txs)
	require.Len(t, rpcClient.txSearchRequests, 1)
	require.Equal(t, 11, rpcClient.txSearchRequests[0].page)
	require.Equal(t, 100, rpcClient.txSearchRequests[0].limit)
}

func TestScanQueryProviderSearchPrivacyTxsUsesPrivacyEventsQuery(t *testing.T) {
	provider := ScanQueryProvider{
		PrivacyEventsQuerier: &stubPrivacyEventsQuerier{
			response: &privacytypes.QueryPrivacyEventsResponse{
				Events: []*privacytypes.QueryPrivacyEvent{
					{
						Sequence:  1,
						Height:    22,
						TxHashHex: "AABB",
						EventType: privacytypes.EventTypeDeposit,
						Attributes: []*privacytypes.QueryPrivacyEventAttribute{
							{Key: privacytypes.AttributeKeyEncryptedNote, Value: "deadbeef"},
						},
					},
				},
			},
		},
	}

	txs, err := provider.SearchPrivacyTxs(context.Background(), 9, 2, 50)
	require.NoError(t, err)
	require.Len(t, txs, 1)
	require.Equal(t, "AABB", txs[0].Hash.String())
	require.Equal(t, int64(22), txs[0].Height)
	require.Len(t, txs[0].TxResult.Events, 1)
	require.Equal(t, privacytypes.EventTypeDeposit, txs[0].TxResult.Events[0].Type)
	require.Equal(t, []abci.EventAttribute{
		{Key: privacytypes.AttributeKeyEncryptedNote, Value: "deadbeef"},
	}, txs[0].TxResult.Events[0].Attributes)
}

func TestScanQueryProviderSearchPrivacyTxsFallsBackToRPC(t *testing.T) {
	rpcClient := stubScanRPCClient{
		txSearchResponse: &cmttypes.ResultTxSearch{
			Txs: []*cmttypes.ResultTx{
				{Hash: []byte{0xCC}},
			},
		},
	}
	provider := ScanQueryProvider{
		RPCClient: &rpcClient,
		PrivacyEventsQuerier: &stubPrivacyEventsQuerier{
			err: grpc.ErrClientConnClosing,
		},
	}

	txs, err := provider.SearchPrivacyTxs(context.Background(), 5, 1, 10)
	require.NoError(t, err)
	require.Len(t, txs, 1)
	require.Equal(t, "CC", txs[0].Hash.String())
	require.Equal(t, "message.module='privacy' AND tx.height > 5", rpcClient.lastQuery)
}

func TestScanQueryProviderSearchPrivacyTxsAggregatesClampedPrivacyEvents(t *testing.T) {
	querier := &stubPrivacyEventsQuerier{
		responses: []*privacytypes.QueryPrivacyEventsResponse{
			{
				Events:  testPrivacyEvents(1, 200),
				Limit:   200,
				HasMore: true,
			},
			{
				Events:  testPrivacyEvents(201, 50),
				Limit:   200,
				HasMore: false,
			},
		},
	}
	provider := ScanQueryProvider{
		PrivacyEventsQuerier: querier,
	}

	txs, err := provider.SearchPrivacyTxs(context.Background(), 9, 1, 1000)
	require.NoError(t, err)
	require.Len(t, txs, 250)
	require.Len(t, querier.requests, 2)
	require.Equal(t, uint64(1), querier.requests[0].Page)
	require.Equal(t, uint64(200), querier.requests[0].Limit)
	require.Equal(t, uint64(2), querier.requests[1].Page)
	require.Equal(t, uint64(200), querier.requests[1].Limit)
}

func TestScanQueryProviderSearchPrivacyTxsMapsLogicalPagesWhenAggregating(t *testing.T) {
	querier := &stubPrivacyEventsQuerier{
		response: &privacytypes.QueryPrivacyEventsResponse{
			Events:  testPrivacyEvents(1001, 1),
			Limit:   200,
			HasMore: false,
		},
	}
	provider := ScanQueryProvider{
		PrivacyEventsQuerier: querier,
	}

	txs, err := provider.SearchPrivacyTxs(context.Background(), 9, 2, 1000)
	require.NoError(t, err)
	require.Len(t, txs, 1)
	require.Len(t, querier.requests, 1)
	require.Equal(t, uint64(6), querier.requests[0].Page)
	require.Equal(t, uint64(200), querier.requests[0].Limit)
}

func TestScanQueryProviderCheckNullifierUsed(t *testing.T) {
	provider := ScanQueryProvider{
		NullifierQuerier: stubNullifierQuerier{
			response: &privacytypes.QueryCheckNullifierResponse{Used: true},
		},
	}

	used, err := provider.CheckNullifierUsed(
		context.Background(),
		"00000000000000000000000000000000000000000000000000000000000000aa",
	)
	require.NoError(t, err)
	require.True(t, used)
}

func TestScanQueryProviderScanPrivacyEvents(t *testing.T) {
	querier := &stubScanEventsQuerier{
		response: &privacytypes.QueryScanEventsResponse{
			Events: []*privacytypes.QueryScanEvent{
				{Sequence: 3, Height: 12, EventType: privacytypes.EventTypeDeposit},
			},
			NextHeight:   12,
			NextSequence: 3,
		},
	}
	provider := ScanQueryProvider{
		ScanEventsQuerier: querier,
	}

	resp, err := provider.ScanPrivacyEvents(context.Background(), 9, 2, 50)
	require.NoError(t, err)
	require.Len(t, resp.Events, 1)
	require.Equal(t, int64(9), querier.request.AfterHeight)
	require.Equal(t, uint64(2), querier.request.AfterSequence)
	require.Equal(t, uint64(50), querier.request.Limit)
	require.Equal(t, []string{privacytypes.EventTypeDeposit, privacytypes.EventTypeShieldedTransfer}, querier.request.EventTypes)
}

func TestScanQueryProviderCheckNullifiersUsed(t *testing.T) {
	querier := &stubBatchNullifierQuerier{
		response: &privacytypes.QueryCheckNullifiersResponse{
			Statuses: []*privacytypes.QueryNullifierStatus{
				{
					Nullifier: "00000000000000000000000000000000000000000000000000000000000000aa",
					Used:      true,
				},
			},
		},
	}
	provider := ScanQueryProvider{
		BatchNullifierQuerier: querier,
	}

	used, err := provider.CheckNullifiersUsed(context.Background(), []string{
		"00000000000000000000000000000000000000000000000000000000000000AA",
	})
	require.NoError(t, err)
	require.Len(t, querier.requests, 1)
	require.Equal(t, []string{"00000000000000000000000000000000000000000000000000000000000000aa"}, querier.requests[0].Nullifiers)
	require.True(t, used["00000000000000000000000000000000000000000000000000000000000000aa"])
}

func TestScanQueryProviderCheckNullifiersUsedChunksLargeBatches(t *testing.T) {
	nullifiers := make([]string, 1001)
	statuses := make([]*privacytypes.QueryNullifierStatus, 0, len(nullifiers))
	for i := range nullifiers {
		nullifiers[i] = testNullifierHex(i + 1)
		statuses = append(statuses, &privacytypes.QueryNullifierStatus{
			Nullifier: nullifiers[i],
			Used:      i == 1000,
		})
	}
	querier := &stubBatchNullifierQuerier{
		responses: []*privacytypes.QueryCheckNullifiersResponse{
			{Statuses: statuses[:1000]},
			{Statuses: statuses[1000:]},
		},
	}
	provider := ScanQueryProvider{
		BatchNullifierQuerier: querier,
	}

	used, err := provider.CheckNullifiersUsed(context.Background(), nullifiers)
	require.NoError(t, err)
	require.Len(t, querier.requests, 2)
	require.Len(t, querier.requests[0].Nullifiers, 1000)
	require.Len(t, querier.requests[1].Nullifiers, 1)
	require.False(t, used[nullifiers[0]])
	require.True(t, used[nullifiers[1000]])
}

func TestScanQueryProviderCheckNullifiersUsedRejectsCorruptResponseFraming(t *testing.T) {
	requested := testNullifierHex(1)
	tests := []struct {
		name     string
		statuses []*privacytypes.QueryNullifierStatus
		want     string
	}{
		{name: "missing", statuses: nil, want: "incomplete"},
		{name: "nil", statuses: []*privacytypes.QueryNullifierStatus{nil}, want: "nil status"},
		{name: "unrequested", statuses: []*privacytypes.QueryNullifierStatus{{Nullifier: testNullifierHex(2)}}, want: "unrequested"},
		{name: "duplicate", statuses: []*privacytypes.QueryNullifierStatus{{Nullifier: requested}, {Nullifier: requested}}, want: "duplicate"},
		{name: "noncanonical", statuses: []*privacytypes.QueryNullifierStatus{{Nullifier: strings.Repeat("ff", 32)}}, want: "canonical"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			provider := ScanQueryProvider{BatchNullifierQuerier: &stubBatchNullifierQuerier{response: &privacytypes.QueryCheckNullifiersResponse{Statuses: tc.statuses}}}
			_, err := provider.CheckNullifiersUsed(context.Background(), []string{requested})
			require.ErrorContains(t, err, tc.want)
		})
	}
}

type stubScanRPCClient struct {
	statusResponse    *cmttypes.ResultStatus
	statusErr         error
	txSearchResponse  *cmttypes.ResultTxSearch
	txSearchResponses []*cmttypes.ResultTxSearch
	txSearchRequests  []txSearchRequest
	txSearchErr       error
	lastQuery         string
	lastPage          int
	lastLimit         int
}

type txSearchRequest struct {
	query string
	page  int
	limit int
}

func (s *stubScanRPCClient) Status(context.Context) (*cmttypes.ResultStatus, error) {
	if s.statusErr != nil {
		return nil, s.statusErr
	}
	return s.statusResponse, nil
}

func (s *stubScanRPCClient) TxSearch(_ context.Context, query string, _ bool, page, perPage *int, _ string) (*cmttypes.ResultTxSearch, error) {
	s.lastQuery = query
	if page != nil {
		s.lastPage = *page
	}
	if perPage != nil {
		s.lastLimit = *perPage
	}
	s.txSearchRequests = append(s.txSearchRequests, txSearchRequest{
		query: query,
		page:  s.lastPage,
		limit: s.lastLimit,
	})
	if s.txSearchErr != nil {
		return nil, s.txSearchErr
	}
	if len(s.txSearchResponses) > 0 {
		response := s.txSearchResponses[0]
		s.txSearchResponses = s.txSearchResponses[1:]
		return response, nil
	}
	if s.txSearchResponse == nil {
		return &cmttypes.ResultTxSearch{}, nil
	}
	return s.txSearchResponse, nil
}

type stubNullifierQuerier struct {
	response *privacytypes.QueryCheckNullifierResponse
	err      error
}

func (s stubNullifierQuerier) CheckNullifier(_ context.Context, _ *privacytypes.QueryCheckNullifierRequest, _ ...grpc.CallOption) (*privacytypes.QueryCheckNullifierResponse, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.response, nil
}

type stubBatchNullifierQuerier struct {
	requests  []*privacytypes.QueryCheckNullifiersRequest
	response  *privacytypes.QueryCheckNullifiersResponse
	responses []*privacytypes.QueryCheckNullifiersResponse
	err       error
}

func (s *stubBatchNullifierQuerier) CheckNullifiers(_ context.Context, req *privacytypes.QueryCheckNullifiersRequest, _ ...grpc.CallOption) (*privacytypes.QueryCheckNullifiersResponse, error) {
	s.requests = append(s.requests, req)
	if s.err != nil {
		return nil, s.err
	}
	if len(s.responses) > 0 {
		response := s.responses[0]
		s.responses = s.responses[1:]
		return response, nil
	}
	return s.response, nil
}

func testNullifierHex(i int) string {
	bz := make([]byte, 32)
	bz[30] = byte(i >> 8)
	bz[31] = byte(i)
	return hex.EncodeToString(bz)
}

func testResultTxs(start, count int) []*cmttypes.ResultTx {
	txs := make([]*cmttypes.ResultTx, 0, count)
	for i := 0; i < count; i++ {
		txs = append(txs, &cmttypes.ResultTx{Hash: []byte{byte(start + i)}})
	}
	return txs
}

type stubPrivacyEventsQuerier struct {
	requests  []*privacytypes.QueryPrivacyEventsRequest
	response  *privacytypes.QueryPrivacyEventsResponse
	responses []*privacytypes.QueryPrivacyEventsResponse
	err       error
}

func (s *stubPrivacyEventsQuerier) PrivacyEvents(_ context.Context, req *privacytypes.QueryPrivacyEventsRequest, _ ...grpc.CallOption) (*privacytypes.QueryPrivacyEventsResponse, error) {
	s.requests = append(s.requests, req)
	if s.err != nil {
		return nil, s.err
	}
	if len(s.responses) > 0 {
		response := s.responses[0]
		s.responses = s.responses[1:]
		return response, nil
	}
	return s.response, nil
}

func testPrivacyEvents(start, count int) []*privacytypes.QueryPrivacyEvent {
	events := make([]*privacytypes.QueryPrivacyEvent, 0, count)
	for i := 0; i < count; i++ {
		sequence := uint64(start + i)
		events = append(events, &privacytypes.QueryPrivacyEvent{
			Sequence:  sequence,
			Height:    int64(20 + i),
			TxHashHex: fmt.Sprintf("%064x", sequence),
			EventType: privacytypes.EventTypeDeposit,
		})
	}
	return events
}

type stubScanEventsQuerier struct {
	request  *privacytypes.QueryScanEventsRequest
	response *privacytypes.QueryScanEventsResponse
	err      error
}

func (s *stubScanEventsQuerier) ScanEvents(_ context.Context, req *privacytypes.QueryScanEventsRequest, _ ...grpc.CallOption) (*privacytypes.QueryScanEventsResponse, error) {
	s.request = req
	if s.err != nil {
		return nil, s.err
	}
	return s.response, nil
}
