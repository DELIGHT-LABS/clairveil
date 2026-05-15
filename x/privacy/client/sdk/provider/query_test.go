package provider

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestTransferQueryProviderAuditMasterPubkeyHex(t *testing.T) {
	provider := TransferQueryProvider{
		AuditConfigQuerier: stubAuditConfigQuerier{
			response: &privacytypes.QueryAuditConfigResponse{AuditMasterPubkeyHex: "abcd"},
		},
	}

	value, err := provider.AuditMasterPubkeyHex(context.Background())
	require.NoError(t, err)
	require.Equal(t, "abcd", value)
}

func TestTransferQueryProviderLookupMerklePath(t *testing.T) {
	provider := TransferQueryProvider{
		MerklePathQuerier: stubMerklePathQuerier{
			response: &privacytypes.QueryMerklePathResponse{
				Root:       "0000000000000000000000000000000000000000000000000000000000000005",
				Path:       []string{"01", "02"},
				PathHelper: []uint32{0, 1},
			},
		},
	}

	result, err := provider.LookupMerklePath(context.Background(), "commitment")
	require.NoError(t, err)
	require.Len(t, result.Root, 32)
	require.Equal(t, []string{"01", "02"}, result.Path)
	require.Equal(t, []uint32{0, 1}, result.PathHelper)
}

func TestWithdrawQueryProviderLookupMerklePath(t *testing.T) {
	provider := WithdrawQueryProvider{
		MerklePathQuerier: stubMerklePathQuerier{
			response: &privacytypes.QueryMerklePathResponse{
				Root:       "0000000000000000000000000000000000000000000000000000000000000007",
				Path:       []string{"0a"},
				PathHelper: []uint32{1},
			},
		},
	}

	result, err := provider.LookupMerklePath(context.Background(), "commitment")
	require.NoError(t, err)
	require.Len(t, result.Root, 32)
	require.Equal(t, []string{"0a"}, result.Path)
	require.Equal(t, []uint32{1}, result.PathHelper)
}

type stubAuditConfigQuerier struct {
	response *privacytypes.QueryAuditConfigResponse
	err      error
}

func (s stubAuditConfigQuerier) AuditConfig(_ context.Context, _ *privacytypes.QueryAuditConfigRequest, _ ...grpc.CallOption) (*privacytypes.QueryAuditConfigResponse, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.response, nil
}

type stubMerklePathQuerier struct {
	response *privacytypes.QueryMerklePathResponse
	err      error
}

func (s stubMerklePathQuerier) MerklePath(_ context.Context, _ *privacytypes.QueryMerklePathRequest, _ ...grpc.CallOption) (*privacytypes.QueryMerklePathResponse, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.response, nil
}
