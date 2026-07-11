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
)

var _ types.QueryServer = Keeper{}

const maxBatchNullifierQueryLimit = 1000

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

func (k Keeper) CheckNullifiers(goCtx context.Context, req *types.QueryCheckNullifiersRequest) (*types.QueryCheckNullifiersResponse, error) {
	if req == nil {
		return nil, invalidQueryRequestErr()
	}
	if len(req.Nullifiers) > maxBatchNullifierQueryLimit {
		return nil, status.Errorf(codes.InvalidArgument, "nullifier batch limit exceeded: got %d max %d", len(req.Nullifiers), maxBatchNullifierQueryLimit)
	}

	ctx := sdk.UnwrapSDKContext(goCtx)
	statuses := make([]*types.QueryNullifierStatus, 0, len(req.Nullifiers))
	for _, nullifierHex := range req.Nullifiers {
		nullifierBytes, err := decodeHexQueryArg(nullifierHex, "nullifier must be valid hex")
		if err != nil {
			return nil, err
		}
		canonicalNullifier, err := validateFieldElementBytesStrict(nullifierBytes)
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, "nullifier must be canonical 32-byte field bytes")
		}

		statuses = append(statuses, &types.QueryNullifierStatus{
			Nullifier: hex.EncodeToString(canonicalNullifier),
			Used:      k.HasNullifier(ctx, canonicalNullifier),
		})
	}

	return &types.QueryCheckNullifiersResponse{Statuses: statuses}, nil
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

	root := hex.EncodeToString(canonicalFieldBytesFromBigInt(types.EmptyNoteTreeRootV1(MerkleDepth)))
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

	leafIndex, found, err := k.GetCommitmentIndex(ctx, canonicalCommitment)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
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

func (k Keeper) ScanEvents(goCtx context.Context, req *types.QueryScanEventsRequest) (*types.QueryScanEventsResponse, error) {
	if req == nil {
		return nil, invalidQueryRequestErr()
	}
	if req.AfterHeight < 0 {
		return nil, status.Error(codes.InvalidArgument, "after_height must not be negative")
	}

	limit := req.Limit
	if limit == 0 {
		limit = defaultScanEventsLimit
	}
	if limit > maxScanEventsLimit {
		limit = maxScanEventsLimit
	}

	ctx := sdk.UnwrapSDKContext(goCtx)
	events, nextHeight, nextSequence, hasMore, err := k.GetScanEvents(ctx, req.AfterHeight, req.AfterSequence, limit, req.EventTypes)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryScanEventsResponse{
		Events:            events,
		NextHeight:        nextHeight,
		NextSequence:      nextSequence,
		Limit:             limit,
		HasMore:           hasMore,
		ScanFormatVersion: types.ScanFormatVersion,
		ViewTagVersion:    types.ViewTagVersion,
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
	ctx := sdk.UnwrapSDKContext(goCtx)
	identity, found, err := k.GetCircuitSetIdentity(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if !found {
		return nil, status.Error(codes.FailedPrecondition, "consensus circuit identity is not initialized")
	}

	response := &types.QueryCircuitConfigResponse{
		SchemaVersion:      identity.SchemaVersion,
		ActiveSetId:        identity.CircuitSetId,
		Curve:              identity.Curve,
		ManifestFile:       "",
		ManifestAvailable:  false,
		ChecksumSource:     "consensus",
		GeneratedAt:        "",
		Artifacts:          make([]*types.QueryCircuitArtifact, 0, len(identity.Circuits)),
		CircuitSetIdentity: types.CloneCircuitSetIdentity(identity),
	}
	for _, circuit := range identity.Circuits {
		response.Artifacts = append(response.Artifacts, &types.QueryCircuitArtifact{
			CircuitId:    circuit.CircuitId,
			ArtifactType: "verifying_key",
			Sha256:       circuit.VerifyingKeySha256,
		})
	}

	return response, nil
}

func (k Keeper) Reserve(goCtx context.Context, req *types.QueryReserveRequest) (*types.QueryReserveResponse, error) {
	if req == nil {
		return nil, invalidQueryRequestErr()
	}

	denom := strings.TrimSpace(req.Denom)
	if err := sdk.ValidateDenom(denom); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "reserve denom is invalid: %v", err)
	}

	ctx := sdk.UnwrapSDKContext(goCtx)
	snapshot, err := k.GetReserveSnapshot(ctx, denom)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryReserveResponse{
		Denom:                 snapshot.Denom,
		ModuleBalance:         snapshot.ModuleBalance.String(),
		TotalDeposited:        snapshot.TotalDeposited.String(),
		TotalWithdrawn:        snapshot.TotalWithdrawn.String(),
		ExpectedModuleBalance: snapshot.ExpectedModuleBalance.String(),
		InvariantHolds:        snapshot.InvariantHolds,
	}, nil
}

func (k Keeper) AssetByDenom(goCtx context.Context, req *types.QueryAssetByDenomRequest) (*types.QueryAssetByDenomResponse, error) {
	if req == nil {
		return nil, invalidQueryRequestErr()
	}
	if _, err := CanonicalAssetIDV1(req.CanonicalDenom); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	entry, found, err := k.GetAssetByDenomV1(ctx, req.CanonicalDenom)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if !found {
		return nil, status.Errorf(codes.NotFound, "asset denom %q is not registered", req.CanonicalDenom)
	}
	return &types.QueryAssetByDenomResponse{Asset: entry, MappingVersion: types.AssetRegistryVersionV1}, nil
}

func (k Keeper) AssetByID(goCtx context.Context, req *types.QueryAssetByIDRequest) (*types.QueryAssetByIDResponse, error) {
	if req == nil {
		return nil, invalidQueryRequestErr()
	}
	assetID, err := decodeHexQueryArg(req.AssetIdHex, "asset_id_hex must be valid hex")
	if err != nil {
		return nil, err
	}
	canonicalAssetID, err := validateFieldElementBytesStrict(assetID)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "asset_id_hex must be canonical 32-byte field bytes")
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	entry, found, err := k.GetAssetByIDV1(ctx, canonicalAssetID)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if !found {
		return nil, status.Error(codes.NotFound, "asset ID is not registered")
	}
	return &types.QueryAssetByIDResponse{Asset: entry, MappingVersion: types.AssetRegistryVersionV1}, nil
}

func (k Keeper) PrivacyScan(goCtx context.Context, req *types.QueryPrivacyScanRequest) (*types.QueryPrivacyScanResponse, error) {
	if req == nil {
		return nil, invalidQueryRequestErr()
	}
	if err := validatePrivacyScanCursor(req.After); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if _, _, _, err := normalizePrivacyScanLimits(req.OutputLimit, req.EventLimit, req.MaxEncodedBytes); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	response, err := k.GetPrivacyScanPageV2(ctx, req.After, req.OutputLimit, req.EventLimit, req.MaxEncodedBytes, req.EventTypes)
	if err != nil {
		if errors.Is(err, errPrivacyScanRecordExceedsBudget) {
			return nil, status.Error(codes.ResourceExhausted, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return response, nil
}

func (k Keeper) CommitmentPathsAtRoot(goCtx context.Context, req *types.QueryCommitmentPathsAtRootRequest) (*types.QueryCommitmentPathsAtRootResponse, error) {
	if req == nil {
		return nil, invalidQueryRequestErr()
	}
	if req.SnapshotHeight < 0 {
		return nil, status.Error(codes.InvalidArgument, "snapshot_height must not be negative")
	}
	if len(req.CommitmentHexes) == 0 || len(req.CommitmentHexes) > MaxCommitmentPathSnapshotQuery {
		return nil, status.Errorf(codes.InvalidArgument, "commitment path snapshot requires 1..%d commitments", MaxCommitmentPathSnapshotQuery)
	}
	commitments := make([][]byte, 0, len(req.CommitmentHexes))
	for _, commitmentHex := range req.CommitmentHexes {
		commitment, err := decodeHexQueryArg(commitmentHex, "commitment_hexes must contain valid hex")
		if err != nil {
			return nil, err
		}
		canonicalCommitment, err := validateFieldElementBytesStrict(commitment)
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, "commitment_hexes must contain canonical 32-byte field bytes")
		}
		commitments = append(commitments, canonicalCommitment)
	}
	if err := types.ValidateDistinctCanonicalFieldElements("path commitment", commitments); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	root, err := decodeHexQueryArg(req.RootHex, "root_hex must be valid hex")
	if err != nil {
		return nil, err
	}
	canonicalRoot, err := validateFieldElementBytesStrict(root)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "root_hex must be canonical 32-byte field bytes")
	}

	ctx := sdk.UnwrapSDKContext(goCtx)
	paths, snapshot, err := k.GetCommitmentPathsAtRootV1(ctx, commitments, canonicalRoot, req.SnapshotHeight)
	if err != nil {
		if errors.Is(err, errMerkleCommitmentNotFound) {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		if strings.Contains(err.Error(), "snapshot height") {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryCommitmentPathsAtRootResponse{
		RootHex:        hex.EncodeToString(snapshot.Root),
		SnapshotHeight: snapshot.Height,
		LeafCount:      snapshot.LeafCount,
		Paths:          paths,
	}, nil
}

func canonicalZeroFieldHex() string {
	return strings.Repeat("0", fieldElementByteSize*2)
}

func normalizeUserModeNames(modes []string) []string {
	out := make([]string, len(modes))
	copy(out, modes)
	return out
}
