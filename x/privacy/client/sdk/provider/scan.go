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

const maxBatchNullifiersPerRequest = 1000
const maxPrivacyEventsPerRequest = 200
const maxRPCTxSearchPerRequest = 100

type ScanRPCClient interface {
	Status(ctx context.Context) (*cmttypes.ResultStatus, error)
	TxSearch(ctx context.Context, query string, prove bool, page, perPage *int, orderBy string) (*cmttypes.ResultTxSearch, error)
}

type NullifierQuerier interface {
	CheckNullifier(ctx context.Context, in *privacytypes.QueryCheckNullifierRequest, opts ...grpc.CallOption) (*privacytypes.QueryCheckNullifierResponse, error)
}

type BatchNullifierQuerier interface {
	CheckNullifiers(ctx context.Context, in *privacytypes.QueryCheckNullifiersRequest, opts ...grpc.CallOption) (*privacytypes.QueryCheckNullifiersResponse, error)
}

type PrivacyEventsQuerier interface {
	PrivacyEvents(ctx context.Context, in *privacytypes.QueryPrivacyEventsRequest, opts ...grpc.CallOption) (*privacytypes.QueryPrivacyEventsResponse, error)
}

type ScanEventsQuerier interface {
	ScanEvents(ctx context.Context, in *privacytypes.QueryScanEventsRequest, opts ...grpc.CallOption) (*privacytypes.QueryScanEventsResponse, error)
}

type PrivacyScanQuerier interface {
	PrivacyScan(ctx context.Context, in *privacytypes.QueryPrivacyScanRequest, opts ...grpc.CallOption) (*privacytypes.QueryPrivacyScanResponse, error)
}

type CommitmentPathsAtRootQuerier interface {
	CommitmentPathsAtRoot(ctx context.Context, in *privacytypes.QueryCommitmentPathsAtRootRequest, opts ...grpc.CallOption) (*privacytypes.QueryCommitmentPathsAtRootResponse, error)
}

type AssetByIDQuerier interface {
	AssetByID(ctx context.Context, in *privacytypes.QueryAssetByIDRequest, opts ...grpc.CallOption) (*privacytypes.QueryAssetByIDResponse, error)
}

type ScanQueryProvider struct {
	RPCClient             ScanRPCClient
	NullifierQuerier      NullifierQuerier
	BatchNullifierQuerier BatchNullifierQuerier
	PrivacyEventsQuerier  PrivacyEventsQuerier
	ScanEventsQuerier     ScanEventsQuerier
	PrivacyScanQuerier    PrivacyScanQuerier
	PathsAtRootQuerier    CommitmentPathsAtRootQuerier
	AssetByIDQuerier      AssetByIDQuerier
}

func NewScanQueryProvider(rpcClient ScanRPCClient, queryClient privacytypes.QueryClient) ScanQueryProvider {
	return ScanQueryProvider{
		RPCClient:             rpcClient,
		NullifierQuerier:      queryClient,
		BatchNullifierQuerier: queryClient,
		PrivacyEventsQuerier:  queryClient,
		ScanEventsQuerier:     queryClient,
		PrivacyScanQuerier:    queryClient,
		PathsAtRootQuerier:    queryClient,
		AssetByIDQuerier:      queryClient,
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
	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = 100
	}

	if p.PrivacyEventsQuerier != nil {
		txs, err := p.searchPrivacyEventsTxs(ctx, afterHeight, page, limit)
		if err == nil {
			return txs, nil
		}
		if p.RPCClient == nil {
			return nil, err
		}
	}

	if p.RPCClient == nil {
		return nil, fmt.Errorf("an rpc client is required")
	}

	return p.searchRPCTxs(ctx, afterHeight, page, limit)
}

func (p ScanQueryProvider) searchPrivacyEventsTxs(ctx context.Context, afterHeight int64, page, limit int) ([]*cmttypes.ResultTx, error) {
	queryLimit := limit
	if queryLimit > maxPrivacyEventsPerRequest {
		queryLimit = maxPrivacyEventsPerRequest
	}

	offset := (page - 1) * limit
	queryPage := offset/queryLimit + 1
	skipWithinPage := offset % queryLimit
	txs := make([]*cmttypes.ResultTx, 0, limit)

	for len(txs) < limit {
		response, err := p.PrivacyEventsQuerier.PrivacyEvents(ctx, &privacytypes.QueryPrivacyEventsRequest{
			AfterHeight: afterHeight,
			Page:        uint64(queryPage),
			Limit:       uint64(queryLimit),
			EventTypes:  []string{privacytypes.EventTypeDeposit, privacytypes.EventTypeShieldedTransfer},
		})
		if err != nil {
			return nil, err
		}

		pageTxs, err := privacyEventsToResultTxs(response)
		if err != nil {
			return nil, err
		}
		if len(pageTxs) == 0 {
			break
		}

		if skipWithinPage > 0 {
			if skipWithinPage >= len(pageTxs) {
				skipWithinPage -= len(pageTxs)
				if response == nil || !response.HasMore {
					break
				}
				queryPage++
				continue
			}
			pageTxs = pageTxs[skipWithinPage:]
			skipWithinPage = 0
		}

		remaining := limit - len(txs)
		if len(pageTxs) > remaining {
			pageTxs = pageTxs[:remaining]
		}
		txs = append(txs, pageTxs...)

		if response == nil || !response.HasMore {
			break
		}
		queryPage++
	}

	return txs, nil
}

func (p ScanQueryProvider) searchRPCTxs(ctx context.Context, afterHeight int64, page, limit int) ([]*cmttypes.ResultTx, error) {
	queryLimit := limit
	if queryLimit > maxRPCTxSearchPerRequest {
		queryLimit = maxRPCTxSearchPerRequest
	}

	offset := (page - 1) * limit
	queryPage := offset/queryLimit + 1
	skipWithinPage := offset % queryLimit
	query := fmt.Sprintf("message.module='privacy' AND tx.height > %d", afterHeight)
	txs := make([]*cmttypes.ResultTx, 0, limit)

	for len(txs) < limit {
		currentPage := queryPage
		response, err := p.RPCClient.TxSearch(ctx, query, false, &currentPage, &queryLimit, "")
		if err != nil {
			if isRPCTxSearchPageOutOfRange(err) {
				break
			}
			return nil, err
		}
		if response == nil || len(response.Txs) == 0 {
			break
		}

		pageTxs := response.Txs
		if skipWithinPage > 0 {
			if skipWithinPage >= len(pageTxs) {
				skipWithinPage -= len(pageTxs)
				if !rpcTxSearchHasMore(response, queryPage, queryLimit) {
					break
				}
				queryPage++
				continue
			}
			pageTxs = pageTxs[skipWithinPage:]
			skipWithinPage = 0
		}

		remaining := limit - len(txs)
		if len(pageTxs) > remaining {
			pageTxs = pageTxs[:remaining]
		}
		txs = append(txs, pageTxs...)

		if !rpcTxSearchHasMore(response, queryPage, queryLimit) {
			break
		}
		queryPage++
	}

	return txs, nil
}

func rpcTxSearchHasMore(response *cmttypes.ResultTxSearch, page, perPage int) bool {
	if response == nil || perPage <= 0 {
		return false
	}
	return response.TotalCount > 0 && page*perPage < response.TotalCount
}

func isRPCTxSearchPageOutOfRange(err error) bool {
	return err != nil && strings.Contains(err.Error(), "page should be within")
}

func (p ScanQueryProvider) ScanPrivacyEvents(ctx context.Context, afterHeight int64, afterSequence uint64, limit int) (*privacytypes.QueryScanEventsResponse, error) {
	if p.ScanEventsQuerier == nil {
		return nil, fmt.Errorf("a scan events querier is required")
	}
	if limit <= 0 {
		limit = 500
	}

	return p.ScanEventsQuerier.ScanEvents(ctx, &privacytypes.QueryScanEventsRequest{
		AfterHeight:   afterHeight,
		AfterSequence: afterSequence,
		Limit:         uint64(limit),
		EventTypes:    []string{privacytypes.EventTypeDeposit, privacytypes.EventTypeShieldedTransfer},
	})
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

func (p ScanQueryProvider) CheckNullifiersUsed(ctx context.Context, nullifierHexes []string) (map[string]bool, error) {
	if p.BatchNullifierQuerier == nil {
		return nil, fmt.Errorf("a batch nullifier querier is required")
	}

	canonicalHexes := make([]string, 0, len(nullifierHexes))
	for _, nullifierHex := range nullifierHexes {
		nullifierBytes, err := privacyfield.DecodeCanonicalHex(nullifierHex, "nullifier")
		if err != nil {
			return nil, err
		}
		canonicalHexes = append(canonicalHexes, hex.EncodeToString(nullifierBytes))
	}

	usedByNullifier := make(map[string]bool, len(canonicalHexes))
	for start := 0; start < len(canonicalHexes); start += maxBatchNullifiersPerRequest {
		end := start + maxBatchNullifiersPerRequest
		if end > len(canonicalHexes) {
			end = len(canonicalHexes)
		}

		response, err := p.BatchNullifierQuerier.CheckNullifiers(ctx, &privacytypes.QueryCheckNullifiersRequest{
			Nullifiers: canonicalHexes[start:end],
		})
		if err != nil {
			return nil, err
		}
		if response == nil {
			return nil, fmt.Errorf("nullifier batch query response is unavailable")
		}

		requested := make(map[string]struct{}, end-start)
		for _, canonical := range canonicalHexes[start:end] {
			requested[canonical] = struct{}{}
		}
		seen := make(map[string]struct{}, end-start)
		for _, status := range response.Statuses {
			if status == nil {
				return nil, fmt.Errorf("nullifier batch query returned a nil status")
			}
			statusBytes, err := privacyfield.DecodeCanonicalHex(status.Nullifier, "nullifier response")
			if err != nil {
				return nil, err
			}
			canonical := hex.EncodeToString(statusBytes)
			if _, ok := requested[canonical]; !ok {
				return nil, fmt.Errorf("nullifier batch query returned an unrequested status")
			}
			if _, duplicate := seen[canonical]; duplicate {
				return nil, fmt.Errorf("nullifier batch query returned a duplicate status")
			}
			seen[canonical] = struct{}{}
			usedByNullifier[canonical] = status.Used
		}
		if len(seen) != len(requested) {
			return nil, fmt.Errorf("nullifier batch query response is incomplete")
		}
	}

	return usedByNullifier, nil
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
