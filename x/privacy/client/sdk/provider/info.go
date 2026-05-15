package provider

import (
	"context"
	"fmt"

	"google.golang.org/grpc"

	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

type TreeStateQuerier interface {
	TreeState(ctx context.Context, in *privacytypes.QueryTreeStateRequest, opts ...grpc.CallOption) (*privacytypes.QueryTreeStateResponse, error)
}

type CommitmentInfoQuerier interface {
	CommitmentInfo(ctx context.Context, in *privacytypes.QueryCommitmentInfoRequest, opts ...grpc.CallOption) (*privacytypes.QueryCommitmentInfoResponse, error)
}

type DisclosureConfigQuerier interface {
	DisclosureConfig(ctx context.Context, in *privacytypes.QueryDisclosureConfigRequest, opts ...grpc.CallOption) (*privacytypes.QueryDisclosureConfigResponse, error)
}

type CircuitConfigQuerier interface {
	CircuitConfig(ctx context.Context, in *privacytypes.QueryCircuitConfigRequest, opts ...grpc.CallOption) (*privacytypes.QueryCircuitConfigResponse, error)
}

type WalletInfoQueryProvider struct {
	TreeStateQuerier        TreeStateQuerier
	CommitmentInfoQuerier   CommitmentInfoQuerier
	DisclosureConfigQuerier DisclosureConfigQuerier
	CircuitConfigQuerier    CircuitConfigQuerier
}

type TreeState struct {
	RootHex         string
	Root            []byte
	LeafCount       uint64
	Depth           uint32
	Initialized     bool
	MaxLeaves       uint64
	RemainingLeaves uint64
}

type CommitmentInfo struct {
	Found     bool
	LeafIndex uint64
}

type DisclosureConfig struct {
	PayloadVersion          string
	AuditDisclosureRequired bool
	SupportedUserPolicies   []string
	SupportedUserModes      []string
}

type CircuitArtifact struct {
	CircuitID    string
	ArtifactType string
	Filename     string
	ChecksumEnv  string
	SHA256       string
}

type CircuitConfig struct {
	SchemaVersion     string
	ActiveSetID       string
	Curve             string
	ManifestFile      string
	ManifestAvailable bool
	ChecksumSource    string
	GeneratedAt       string
	Artifacts         []CircuitArtifact
}

func NewWalletInfoQueryProvider(queryClient privacytypes.QueryClient) WalletInfoQueryProvider {
	return WalletInfoQueryProvider{
		TreeStateQuerier:        queryClient,
		CommitmentInfoQuerier:   queryClient,
		DisclosureConfigQuerier: queryClient,
		CircuitConfigQuerier:    queryClient,
	}
}

func (p WalletInfoQueryProvider) TreeState(ctx context.Context) (*TreeState, error) {
	if p.TreeStateQuerier == nil {
		return nil, fmt.Errorf("a tree state querier is required")
	}

	response, err := p.TreeStateQuerier.TreeState(ctx, &privacytypes.QueryTreeStateRequest{})
	if err != nil {
		return nil, err
	}
	if response == nil {
		return nil, fmt.Errorf("tree state query response is unavailable")
	}

	rootBytes, err := privacyfield.DecodeCanonicalHex(response.Root, "tree root")
	if err != nil {
		return nil, err
	}

	return &TreeState{
		RootHex:         response.Root,
		Root:            rootBytes,
		LeafCount:       response.LeafCount,
		Depth:           response.Depth,
		Initialized:     response.Initialized,
		MaxLeaves:       response.MaxLeaves,
		RemainingLeaves: response.RemainingLeaves,
	}, nil
}

func (p WalletInfoQueryProvider) CommitmentInfo(ctx context.Context, commitmentHex string) (*CommitmentInfo, error) {
	if p.CommitmentInfoQuerier == nil {
		return nil, fmt.Errorf("a commitment info querier is required")
	}

	response, err := p.CommitmentInfoQuerier.CommitmentInfo(ctx, &privacytypes.QueryCommitmentInfoRequest{
		CommitmentHex: commitmentHex,
	})
	if err != nil {
		return nil, err
	}
	if response == nil {
		return nil, fmt.Errorf("commitment info query response is unavailable")
	}

	return &CommitmentInfo{
		Found:     response.Found,
		LeafIndex: response.LeafIndex,
	}, nil
}

func (p WalletInfoQueryProvider) DisclosureConfig(ctx context.Context) (*DisclosureConfig, error) {
	if p.DisclosureConfigQuerier == nil {
		return nil, fmt.Errorf("a disclosure config querier is required")
	}

	response, err := p.DisclosureConfigQuerier.DisclosureConfig(ctx, &privacytypes.QueryDisclosureConfigRequest{})
	if err != nil {
		return nil, err
	}
	if response == nil {
		return nil, fmt.Errorf("disclosure config query response is unavailable")
	}

	return &DisclosureConfig{
		PayloadVersion:          response.PayloadVersion,
		AuditDisclosureRequired: response.AuditDisclosureRequired,
		SupportedUserPolicies:   append([]string(nil), response.SupportedUserPolicies...),
		SupportedUserModes:      append([]string(nil), response.SupportedUserModes...),
	}, nil
}

func (p WalletInfoQueryProvider) CircuitConfig(ctx context.Context) (*CircuitConfig, error) {
	if p.CircuitConfigQuerier == nil {
		return nil, fmt.Errorf("a circuit config querier is required")
	}

	response, err := p.CircuitConfigQuerier.CircuitConfig(ctx, &privacytypes.QueryCircuitConfigRequest{})
	if err != nil {
		return nil, err
	}
	if response == nil {
		return nil, fmt.Errorf("circuit config query response is unavailable")
	}

	artifacts := make([]CircuitArtifact, 0, len(response.Artifacts))
	for _, artifact := range response.Artifacts {
		if artifact == nil {
			continue
		}
		artifacts = append(artifacts, CircuitArtifact{
			CircuitID:    artifact.CircuitId,
			ArtifactType: artifact.ArtifactType,
			Filename:     artifact.Filename,
			ChecksumEnv:  artifact.ChecksumEnv,
			SHA256:       artifact.Sha256,
		})
	}

	return &CircuitConfig{
		SchemaVersion:     response.SchemaVersion,
		ActiveSetID:       response.ActiveSetId,
		Curve:             response.Curve,
		ManifestFile:      response.ManifestFile,
		ManifestAvailable: response.ManifestAvailable,
		ChecksumSource:    response.ChecksumSource,
		GeneratedAt:       response.GeneratedAt,
		Artifacts:         artifacts,
	}, nil
}
