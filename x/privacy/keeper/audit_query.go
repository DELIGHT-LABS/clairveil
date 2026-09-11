package keeper

import (
	"context"

	v2 "github.com/DELIGHT-LABS/clairveil/x/privacy/types/v2"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// auditQueryServer intentionally exposes configuration and audit-key history
// only. Transaction records, creation/spend indexes, and replay queries are
// not consensus state in the simplified provenance design.
type auditQueryServer struct{ Keeper }

func NewAuditQueryServer(k Keeper) v2.QueryServer { return auditQueryServer{Keeper: k} }

func (q auditQueryServer) AuditKey(c context.Context, req *v2.QueryAuditKeyRequest) (*v2.QueryAuditRecordResponse, error) {
	if req == nil || req.Epoch == 0 {
		return nil, status.Error(codes.InvalidArgument, "epoch required")
	}
	ctx := sdk.UnwrapSDKContext(c)
	key := auditEpochKey(auditEpochPrefix, req.Epoch)
	found, err := q.storeService.OpenKVStore(ctx).Has(key)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if !found {
		return nil, status.Error(codes.NotFound, "audit epoch not found")
	}
	_, raw, err := q.getAuditKey(ctx, req.Epoch)
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	return &v2.QueryAuditRecordResponse{Record: raw, StoreKey: key, StateHeight: ctx.BlockHeight()}, nil
}

func (q auditQueryServer) AuditKeySchedule(c context.Context, req *v2.QueryAuditKeyScheduleRequest) (*v2.QueryAuditKeyScheduleResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request required")
	}
	ctx := sdk.UnwrapSDKContext(c)
	active, err := q.activeAuditKey(ctx)
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	epoch, height, err := q.pendingAuditEpoch(ctx)
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	halt, err := q.auditHalted(ctx)
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	return &v2.QueryAuditKeyScheduleResponse{ActiveEpoch: active.Epoch, NextActivationHeight: height, NextEpoch: epoch, Halted: halt, StateHeight: ctx.BlockHeight()}, nil
}

func (q auditQueryServer) AuditConfiguration(c context.Context, req *v2.QueryAuditConfigurationRequest) (*v2.QueryAuditConfigurationResponse, error) {
	if req == nil || q.audit == nil {
		return nil, status.Error(codes.FailedPrecondition, "audit configuration unavailable")
	}
	ctx := sdk.UnwrapSDKContext(c)
	identity, found, err := q.GetCircuitSetIdentity(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if !found {
		return nil, status.Error(codes.FailedPrecondition, "circuit identity unavailable")
	}
	encoded, err := identity.Marshal()
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	return &v2.QueryAuditConfigurationResponse{
		NetworkNonce:    append([]byte(nil), q.audit.nonce[:]...),
		InitialHeight:   q.audit.initialHeight,
		CircuitIdentity: encoded,
		StateHeight:     ctx.BlockHeight(),
	}, nil
}
