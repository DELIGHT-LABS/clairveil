package provider

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestWalletInfoQueryProviderTreeState(t *testing.T) {
	provider := WalletInfoQueryProvider{
		TreeStateQuerier: stubTreeStateQuerier{
			response: &privacytypes.QueryTreeStateResponse{
				Root:            "0000000000000000000000000000000000000000000000000000000000000005",
				LeafCount:       7,
				Depth:           32,
				Initialized:     true,
				MaxLeaves:       4294967296,
				RemainingLeaves: 4294967289,
			},
		},
	}

	result, err := provider.TreeState(context.Background())
	require.NoError(t, err)
	require.Equal(t, "0000000000000000000000000000000000000000000000000000000000000005", result.RootHex)
	require.Len(t, result.Root, 32)
	require.Equal(t, uint64(7), result.LeafCount)
	require.Equal(t, uint32(32), result.Depth)
	require.True(t, result.Initialized)
	require.Equal(t, uint64(4294967296), result.MaxLeaves)
	require.Equal(t, uint64(4294967289), result.RemainingLeaves)
}

func TestWalletInfoQueryProviderCommitmentInfo(t *testing.T) {
	provider := WalletInfoQueryProvider{
		CommitmentInfoQuerier: stubCommitmentInfoQuerier{
			response: &privacytypes.QueryCommitmentInfoResponse{
				Found:     true,
				LeafIndex: 9,
			},
		},
	}

	result, err := provider.CommitmentInfo(
		context.Background(),
		"0000000000000000000000000000000000000000000000000000000000000009",
	)
	require.NoError(t, err)
	require.True(t, result.Found)
	require.Equal(t, uint64(9), result.LeafIndex)
}

func TestWalletInfoQueryProviderDisclosureConfig(t *testing.T) {
	provider := WalletInfoQueryProvider{
		DisclosureConfigQuerier: stubDisclosureConfigQuerier{
			response: &privacytypes.QueryDisclosureConfigResponse{
				PayloadVersion:          "v4",
				AuditDisclosureRequired: true,
				SupportedUserPolicies:   []string{"all-private", "amount"},
				SupportedUserModes:      []string{"none", "public"},
			},
		},
	}

	result, err := provider.DisclosureConfig(context.Background())
	require.NoError(t, err)
	require.Equal(t, "v4", result.PayloadVersion)
	require.True(t, result.AuditDisclosureRequired)
	require.Equal(t, []string{"all-private", "amount"}, result.SupportedUserPolicies)
	require.Equal(t, []string{"none", "public"}, result.SupportedUserModes)
}

func TestWalletInfoQueryProviderCircuitConfig(t *testing.T) {
	provider := WalletInfoQueryProvider{
		CircuitConfigQuerier: stubCircuitConfigQuerier{
			response: &privacytypes.QueryCircuitConfigResponse{
				SchemaVersion:     "v1",
				ActiveSetId:       "latest-single-transfer",
				Curve:             "BN254",
				ManifestFile:      "privacy_zk_manifest.json",
				ManifestAvailable: true,
				ChecksumSource:    "manifest",
				GeneratedAt:       "2026-04-15T00:00:00Z",
				Artifacts: []*privacytypes.QueryCircuitArtifact{
					{
						CircuitId:    "spend",
						ArtifactType: "r1cs",
						Filename:     "privacy_spend_r1cs.bin",
						ChecksumEnv:  "CLAIRVEIL_PRIVACY_SPEND_R1CS_SHA256",
						Sha256:       "abcd",
					},
				},
			},
		},
	}

	result, err := provider.CircuitConfig(context.Background())
	require.NoError(t, err)
	require.Equal(t, "v1", result.SchemaVersion)
	require.Equal(t, "latest-single-transfer", result.ActiveSetID)
	require.Equal(t, "BN254", result.Curve)
	require.Equal(t, "privacy_zk_manifest.json", result.ManifestFile)
	require.True(t, result.ManifestAvailable)
	require.Equal(t, "manifest", result.ChecksumSource)
	require.Equal(t, "2026-04-15T00:00:00Z", result.GeneratedAt)
	require.Len(t, result.Artifacts, 1)
	require.Equal(t, "spend", result.Artifacts[0].CircuitID)
	require.Equal(t, "abcd", result.Artifacts[0].SHA256)
}

type stubTreeStateQuerier struct {
	response *privacytypes.QueryTreeStateResponse
	err      error
}

func (s stubTreeStateQuerier) TreeState(_ context.Context, _ *privacytypes.QueryTreeStateRequest, _ ...grpc.CallOption) (*privacytypes.QueryTreeStateResponse, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.response, nil
}

type stubCommitmentInfoQuerier struct {
	response *privacytypes.QueryCommitmentInfoResponse
	err      error
}

func (s stubCommitmentInfoQuerier) CommitmentInfo(_ context.Context, _ *privacytypes.QueryCommitmentInfoRequest, _ ...grpc.CallOption) (*privacytypes.QueryCommitmentInfoResponse, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.response, nil
}

type stubDisclosureConfigQuerier struct {
	response *privacytypes.QueryDisclosureConfigResponse
	err      error
}

func (s stubDisclosureConfigQuerier) DisclosureConfig(_ context.Context, _ *privacytypes.QueryDisclosureConfigRequest, _ ...grpc.CallOption) (*privacytypes.QueryDisclosureConfigResponse, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.response, nil
}

type stubCircuitConfigQuerier struct {
	response *privacytypes.QueryCircuitConfigResponse
	err      error
}

func (s stubCircuitConfigQuerier) CircuitConfig(_ context.Context, _ *privacytypes.QueryCircuitConfigRequest, _ ...grpc.CallOption) (*privacytypes.QueryCircuitConfigResponse, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.response, nil
}
