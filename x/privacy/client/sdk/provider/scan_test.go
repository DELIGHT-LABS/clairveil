package provider

import (
	"context"
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

func TestScanQueryProviderSearchPrivacyTxsUsesPrivacyEventsQuery(t *testing.T) {
	provider := ScanQueryProvider{
		PrivacyEventsQuerier: stubPrivacyEventsQuerier{
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
		PrivacyEventsQuerier: stubPrivacyEventsQuerier{
			err: grpc.ErrClientConnClosing,
		},
	}

	txs, err := provider.SearchPrivacyTxs(context.Background(), 5, 1, 10)
	require.NoError(t, err)
	require.Len(t, txs, 1)
	require.Equal(t, "CC", txs[0].Hash.String())
	require.Equal(t, "message.module='privacy' AND tx.height > 5", rpcClient.lastQuery)
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

type stubScanRPCClient struct {
	statusResponse   *cmttypes.ResultStatus
	statusErr        error
	txSearchResponse *cmttypes.ResultTxSearch
	txSearchErr      error
	lastQuery        string
	lastPage         int
	lastLimit        int
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
	if s.txSearchErr != nil {
		return nil, s.txSearchErr
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

type stubPrivacyEventsQuerier struct {
	response *privacytypes.QueryPrivacyEventsResponse
	err      error
}

func (s stubPrivacyEventsQuerier) PrivacyEvents(_ context.Context, _ *privacytypes.QueryPrivacyEventsRequest, _ ...grpc.CallOption) (*privacytypes.QueryPrivacyEventsResponse, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.response, nil
}
