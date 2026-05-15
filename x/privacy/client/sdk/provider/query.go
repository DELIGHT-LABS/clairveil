package provider

import (
	"context"
	"fmt"

	"google.golang.org/grpc"

	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
	privacytransfer "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/transfer"
	privacywithdraw "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/withdraw"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

type MerklePathQuerier interface {
	MerklePath(ctx context.Context, in *privacytypes.QueryMerklePathRequest, opts ...grpc.CallOption) (*privacytypes.QueryMerklePathResponse, error)
}

type AuditConfigQuerier interface {
	AuditConfig(ctx context.Context, in *privacytypes.QueryAuditConfigRequest, opts ...grpc.CallOption) (*privacytypes.QueryAuditConfigResponse, error)
}

type TransferQueryProvider struct {
	MerklePathQuerier  MerklePathQuerier
	AuditConfigQuerier AuditConfigQuerier
}

func NewTransferQueryProvider(queryClient privacytypes.QueryClient) TransferQueryProvider {
	return TransferQueryProvider{
		MerklePathQuerier:  queryClient,
		AuditConfigQuerier: queryClient,
	}
}

func (p TransferQueryProvider) AuditMasterPubkeyHex(ctx context.Context) (string, error) {
	if p.AuditConfigQuerier == nil {
		return "", fmt.Errorf("an audit config querier is required")
	}

	resp, err := p.AuditConfigQuerier.AuditConfig(ctx, &privacytypes.QueryAuditConfigRequest{})
	if err != nil {
		return "", err
	}
	return resp.AuditMasterPubkeyHex, nil
}

func (p TransferQueryProvider) LookupMerklePath(ctx context.Context, commitmentHex string) (*privacytransfer.MerklePathResult, error) {
	if p.MerklePathQuerier == nil {
		return nil, fmt.Errorf("a merkle path querier is required")
	}

	response, err := p.MerklePathQuerier.MerklePath(ctx, &privacytypes.QueryMerklePathRequest{CommitmentHex: commitmentHex})
	if err != nil {
		return nil, err
	}

	rootBytes, err := privacyfield.DecodeCanonicalHex(response.Root, "merkle root")
	if err != nil {
		return nil, err
	}

	return &privacytransfer.MerklePathResult{
		Root:       rootBytes,
		Path:       response.Path,
		PathHelper: response.PathHelper,
	}, nil
}

type WithdrawQueryProvider struct {
	MerklePathQuerier MerklePathQuerier
}

func NewWithdrawQueryProvider(queryClient privacytypes.QueryClient) WithdrawQueryProvider {
	return WithdrawQueryProvider{
		MerklePathQuerier: queryClient,
	}
}

func (p WithdrawQueryProvider) LookupMerklePath(ctx context.Context, commitmentHex string) (*privacywithdraw.MerklePathResult, error) {
	if p.MerklePathQuerier == nil {
		return nil, fmt.Errorf("a merkle path querier is required")
	}

	response, err := p.MerklePathQuerier.MerklePath(ctx, &privacytypes.QueryMerklePathRequest{CommitmentHex: commitmentHex})
	if err != nil {
		return nil, err
	}

	rootBytes, err := privacyfield.DecodeCanonicalHex(response.Root, "merkle root")
	if err != nil {
		return nil, err
	}

	return &privacywithdraw.MerklePathResult{
		Root:       rootBytes,
		Path:       response.Path,
		PathHelper: response.PathHelper,
	}, nil
}
