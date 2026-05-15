package keeper

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
)

var _ types.QueryServer = Keeper{}

func (k Keeper) CheckNullifier(goCtx context.Context, req *types.QueryCheckNullifierRequest) (*types.QueryCheckNullifierResponse, error) {
	if req == nil {
		return nil, invalidQueryRequestErr()
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	nullifierBytes, err := decodeHexQueryArg(req.Nullifier, "nullifier must be valid hex")
	if err != nil {
		return nil, err
	}

	canonicalNullifier, err := validateFieldElementBytesStrict(nullifierBytes)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "nullifier must be canonical 32-byte field bytes")
	}
	return &types.QueryCheckNullifierResponse{Used: k.HasNullifier(ctx, canonicalNullifier)}, nil
}

func (k Keeper) TreeState(goCtx context.Context, req *types.QueryTreeStateRequest) (*types.QueryTreeStateResponse, error) {
	if req == nil {
		return nil, invalidQueryRequestErr()
	}

	ctx := sdk.UnwrapSDKContext(goCtx)
	leafCount := k.GetLeafCount(ctx)
	remainingLeaves, err := remainingMerkleLeaves(leafCount)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	root := canonicalZeroFieldHex()
	if leafCount > 0 {
		if err := k.ensureIncrementalTreeState(ctx); err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		rootBytes := k.GetMerkleNode(ctx, uint8(MerkleDepth), 0)
		if len(rootBytes) == 0 {
			var err error
			rootBytes, err = k.RecalculateRoot(ctx, leafCount)
			if err != nil {
				return nil, status.Error(codes.Internal, err.Error())
			}
		}
		root = hex.EncodeToString(canonicalizeFieldBytesOrOriginal(rootBytes))
	}

	return &types.QueryTreeStateResponse{
		Root:            root,
		LeafCount:       leafCount,
		Depth:           uint32(MerkleDepth),
		Initialized:     leafCount > 0,
		MaxLeaves:       MaxMerkleLeaves,
		RemainingLeaves: remainingLeaves,
	}, nil
}

func (k Keeper) CommitmentInfo(goCtx context.Context, req *types.QueryCommitmentInfoRequest) (*types.QueryCommitmentInfoResponse, error) {
	if req == nil {
		return nil, invalidQueryRequestErr()
	}

	ctx := sdk.UnwrapSDKContext(goCtx)
	leafCount := k.GetLeafCount(ctx)
	if err := k.validateMerkleCachedRootOrSmallRebuild(ctx, leafCount); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	commitBytes, err := decodeHexQueryArg(req.CommitmentHex, "commitment_hex must be valid hex")
	if err != nil {
		return nil, err
	}

	canonicalCommitment, err := validateFieldElementBytesStrict(commitBytes)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "commitment must be canonical 32-byte field bytes")
	}

	leafIndex, found := k.GetCommitmentIndex(ctx, canonicalCommitment)
	return &types.QueryCommitmentInfoResponse{
		Found:     found,
		LeafIndex: leafIndex,
	}, nil
}

func (k Keeper) PrivacyEvents(goCtx context.Context, req *types.QueryPrivacyEventsRequest) (*types.QueryPrivacyEventsResponse, error) {
	if req == nil {
		return nil, invalidQueryRequestErr()
	}
	if req.AfterHeight < 0 {
		return nil, status.Error(codes.InvalidArgument, "after_height must not be negative")
	}

	page := req.Page
	if page == 0 {
		page = defaultPrivacyEventsPage
	}

	limit := req.Limit
	if limit == 0 {
		limit = defaultPrivacyEventsLimit
	}
	if limit > maxPrivacyEventsLimit {
		limit = maxPrivacyEventsLimit
	}

	ctx := sdk.UnwrapSDKContext(goCtx)
	events, hasMore, err := k.GetPrivacyEvents(ctx, req.AfterHeight, page, limit, req.EventTypes)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryPrivacyEventsResponse{
		Events:  events,
		Page:    page,
		Limit:   limit,
		HasMore: hasMore,
	}, nil
}

func (k Keeper) MerklePath(goCtx context.Context, req *types.QueryMerklePathRequest) (*types.QueryMerklePathResponse, error) {
	if req == nil {
		return nil, invalidQueryRequestErr()
	}
	ctx := sdk.UnwrapSDKContext(goCtx)

	commitBytes, err := decodeHexQueryArg(req.CommitmentHex, "commitment_hex must be valid hex")
	if err != nil {
		return nil, err
	}

	canonicalCommitment, err := validateFieldElementBytesStrict(commitBytes)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "commitment must be canonical 32-byte field bytes")
	}

	path, helper, root, err := k.GetPath(ctx, canonicalCommitment)
	if err != nil {
		if !errors.Is(err, errMerkleCommitmentNotFound) {
			return nil, status.Error(codes.Internal, err.Error())
		}
		return nil, status.Error(codes.NotFound, err.Error())
	}

	return &types.QueryMerklePathResponse{
		Path:       path,
		PathHelper: helper,
		Root:       hex.EncodeToString(root),
	}, nil
}

func (k Keeper) AuditConfig(goCtx context.Context, req *types.QueryAuditConfigRequest) (*types.QueryAuditConfigResponse, error) {
	if req == nil {
		return nil, invalidQueryRequestErr()
	}

	ctx := sdk.UnwrapSDKContext(goCtx)
	pubKey := k.GetAuditMasterPubkey(ctx)

	return &types.QueryAuditConfigResponse{
		AuditMasterPubkeyHex: hex.EncodeToString(pubKey),
	}, nil
}

func (k Keeper) DisclosureConfig(goCtx context.Context, req *types.QueryDisclosureConfigRequest) (*types.QueryDisclosureConfigResponse, error) {
	if req == nil {
		return nil, invalidQueryRequestErr()
	}
	_ = sdk.UnwrapSDKContext(goCtx)

	return &types.QueryDisclosureConfigResponse{
		PayloadVersion:          types.DisclosurePayloadVersion,
		AuditDisclosureRequired: true,
		SupportedUserPolicies:   types.SupportedUserDisclosurePolicies(),
		SupportedUserModes:      normalizeUserModeNames(types.SupportedUserDisclosureModes()),
	}, nil
}

func (k Keeper) CircuitConfig(goCtx context.Context, req *types.QueryCircuitConfigRequest) (*types.QueryCircuitConfigResponse, error) {
	if req == nil {
		return nil, invalidQueryRequestErr()
	}
	_ = sdk.UnwrapSDKContext(goCtx)

	manifest, checksumSource, err := zk.ResolveRuntimeArtifactManifest()
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	response := &types.QueryCircuitConfigResponse{
		SchemaVersion:     manifest.SchemaVersion,
		ActiveSetId:       manifest.ActiveSetID,
		Curve:             manifest.Curve,
		ManifestFile:      zk.ArtifactManifestFile,
		ManifestAvailable: checksumSource == zk.ChecksumSourceManifest,
		ChecksumSource:    checksumSource,
		GeneratedAt:       manifest.GeneratedAt,
		Artifacts:         make([]*types.QueryCircuitArtifact, 0, len(manifest.Artifacts)),
	}
	for _, artifact := range manifest.Artifacts {
		response.Artifacts = append(response.Artifacts, &types.QueryCircuitArtifact{
			CircuitId:    artifact.CircuitID,
			ArtifactType: artifact.ArtifactType,
			Filename:     artifact.Filename,
			ChecksumEnv:  artifact.ChecksumEnv,
			Sha256:       artifact.SHA256,
		})
	}

	return response, nil
}

func canonicalZeroFieldHex() string {
	return strings.Repeat("0", fieldElementByteSize*2)
}

func normalizeUserModeNames(modes []string) []string {
	out := make([]string, len(modes))
	copy(out, modes)
	return out
}
