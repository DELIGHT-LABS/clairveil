package provider

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"

	abci "github.com/cometbft/cometbft/abci/types"
	cmttypes "github.com/cometbft/cometbft/rpc/core/types"
	"google.golang.org/grpc"

	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

type ScanRPCClient interface {
	Status(ctx context.Context) (*cmttypes.ResultStatus, error)
	TxSearch(ctx context.Context, query string, prove bool, page, perPage *int, orderBy string) (*cmttypes.ResultTxSearch, error)
}

type NullifierQuerier interface {
	CheckNullifier(ctx context.Context, in *privacytypes.QueryCheckNullifierRequest, opts ...grpc.CallOption) (*privacytypes.QueryCheckNullifierResponse, error)
}

type PrivacyEventsQuerier interface {
	PrivacyEvents(ctx context.Context, in *privacytypes.QueryPrivacyEventsRequest, opts ...grpc.CallOption) (*privacytypes.QueryPrivacyEventsResponse, error)
}

type ScanQueryProvider struct {
	RPCClient            ScanRPCClient
	NullifierQuerier     NullifierQuerier
	PrivacyEventsQuerier PrivacyEventsQuerier
}

func NewScanQueryProvider(rpcClient ScanRPCClient, queryClient privacytypes.QueryClient) ScanQueryProvider {
	return ScanQueryProvider{
		RPCClient:            rpcClient,
		NullifierQuerier:     queryClient,
		PrivacyEventsQuerier: queryClient,
	}
}

func (p ScanQueryProvider) LatestBlockHeight(ctx context.Context) (int64, error) {
	if p.RPCClient == nil {
		return 0, fmt.Errorf("an rpc client is required")
	}

	status, err := p.RPCClient.Status(ctx)
	if err != nil {
		return 0, err
	}
	if status == nil {
		return 0, fmt.Errorf("rpc status response is unavailable")
	}

	return status.SyncInfo.LatestBlockHeight, nil
}

func (p ScanQueryProvider) SearchPrivacyTxs(ctx context.Context, afterHeight int64, page, limit int) ([]*cmttypes.ResultTx, error) {
	if limit <= 0 {
		limit = 100
	}

	if p.PrivacyEventsQuerier != nil {
		response, err := p.PrivacyEventsQuerier.PrivacyEvents(ctx, &privacytypes.QueryPrivacyEventsRequest{
			AfterHeight: afterHeight,
			Page:        uint64(page),
			Limit:       uint64(limit),
			EventTypes:  []string{privacytypes.EventTypeDeposit, privacytypes.EventTypeShieldedTransfer},
		})
		if err == nil {
			return privacyEventsToResultTxs(response)
		}
		if p.RPCClient == nil {
			return nil, err
		}
	}

	if p.RPCClient == nil {
		return nil, fmt.Errorf("an rpc client is required")
	}

	response, err := p.RPCClient.TxSearch(
		ctx,
		fmt.Sprintf("message.module='privacy' AND tx.height > %d", afterHeight),
		false,
		&page,
		&limit,
		"",
	)
	if err != nil {
		return nil, err
	}
	if response == nil {
		return nil, nil
	}

	return response.Txs, nil
}

func (p ScanQueryProvider) CheckNullifierUsed(ctx context.Context, nullifierHex string) (bool, error) {
	if p.NullifierQuerier == nil {
		return false, fmt.Errorf("a nullifier querier is required")
	}

	nullifierBytes, err := privacyfield.DecodeCanonicalHex(nullifierHex, "nullifier")
	if err != nil {
		return false, err
	}

	response, err := p.NullifierQuerier.CheckNullifier(ctx, &privacytypes.QueryCheckNullifierRequest{
		Nullifier: hex.EncodeToString(nullifierBytes),
	})
	if err != nil {
		return false, err
	}
	if response == nil {
		return false, fmt.Errorf("nullifier query response is unavailable")
	}

	return response.Used, nil
}

func privacyEventsToResultTxs(response *privacytypes.QueryPrivacyEventsResponse) ([]*cmttypes.ResultTx, error) {
	if response == nil {
		return nil, nil
	}

	txs := make([]*cmttypes.ResultTx, 0, len(response.Events))
	for _, event := range response.Events {
		if event == nil {
			continue
		}

		var hash []byte
		if strings.TrimSpace(event.TxHashHex) != "" {
			var err error
			hash, err = hex.DecodeString(strings.TrimSpace(event.TxHashHex))
			if err != nil {
				return nil, fmt.Errorf("privacy event tx hash must be valid hex: %w", err)
			}
		}

		attrs := make([]abci.EventAttribute, 0, len(event.Attributes))
		for _, attr := range event.Attributes {
			if attr == nil {
				continue
			}
			attrs = append(attrs, abci.EventAttribute{
				Key:   attr.Key,
				Value: attr.Value,
			})
		}

		txs = append(txs, &cmttypes.ResultTx{
			Hash:   hash,
			Height: event.Height,
			TxResult: abci.ExecTxResult{
				Events: []abci.Event{
					{
						Type:       event.EventType,
						Attributes: attrs,
					},
				},
			},
		})
	}

	return txs, nil
}
