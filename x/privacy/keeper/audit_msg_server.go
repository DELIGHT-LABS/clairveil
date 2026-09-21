package keeper

import (
	"context"
	"encoding/base64"
	"fmt"
	"math"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/auditfield"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	privacyv2 "github.com/DELIGHT-LABS/clairveil/x/privacy/types/v2"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/cosmos/gogoproto/proto"
)

type auditMsgServer struct{ Keeper }

func NewAuditMsgServerImpl(k Keeper) privacyv2.MsgServer { return auditMsgServer{k} }

// This value is local to a successful verifier call. It is never returned by
// an exported API and carries detached bytes, not a callback or apply token.
type verifiedAuditTransition struct {
	owner          *auditRuntime
	message        types.ValidatedAuditMessage
	record         *auditTransitionRecord
	projectedBound uint64
	delegated      *delegatedDepositExecution
	consumed       bool
}

type delegatedDepositExecution struct {
	funder  sdk.AccAddress
	payload []byte
}

func (d *delegatedDepositExecution) eventBytes() uint64 {
	if d == nil {
		return 0
	}
	return uint64(base64.StdEncoding.EncodedLen(len(d.payload)) + len(d.funder.String()) +
		len(types.AttributeKeyDelegatedDepositPayload) + len(types.AttributeKeyDelegatedFunder))
}

func (k auditMsgServer) executeAudit(ctx sdk.Context, raw sdk.Msg) (uint64, error) {
	return k.executeAuditWithDelegation(ctx, raw, nil)
}

func (k auditMsgServer) executeAuditWithDelegation(ctx sdk.Context, raw sdk.Msg, delegated *delegatedDepositExecution) (uint64, error) {
	if k.audit == nil {
		return 0, fmt.Errorf("audit-field execution is not configured")
	}
	if ctx.Value(auditApplyContextKey{}) != nil {
		return 0, fmt.Errorf("nested privacy transition is forbidden")
	}
	bound, err := prechargeAuditMessage(ctx, raw, delegated.eventBytes())
	if err != nil {
		return 0, err
	}
	m, err := types.ValidateAuditMessage(raw)
	if err != nil {
		return 0, err
	}
	// The four asset transactions must be tied to an original submitted tx so
	// successful events can always identify the source message. Key lifecycle
	// administration intentionally retains its block-origin path.
	if len(ctx.TxBytes()) == 0 {
		return 0, fmt.Errorf("privacy transaction requires original tx execution context")
	}
	// Proposal execution uses the governance module account as the sole
	// message signer. Keep this handler boundary even if a caller bypasses the
	// proposal submission decoder or invokes the registered route directly.
	if m.Creator() == authtypes.NewModuleAddress(govtypes.ModuleName).String() {
		return 0, fmt.Errorf("governance cannot execute privacy transactions")
	}
	record, err := k.buildAuditPublic(ctx, m)
	if err != nil {
		return 0, err
	}
	public, err := auditPublicWitness(record.PI)
	if err != nil {
		return 0, err
	}
	id, err := auditCircuitID(m.Kind())
	if err != nil {
		return 0, err
	}
	if err := k.audit.registry.VerifyProof(id, m.Proof(), public, k.audit.identity); err != nil {
		return 0, fmt.Errorf("audit proof verification failed: %w", err)
	}
	v := verifiedAuditTransition{owner: k.audit, message: m, record: record, projectedBound: bound, delegated: delegated}
	return k.applyPrivacyTransition(ctx, &v)
}

// DepositWithFunderV2 is a trusted in-process integration surface. It keeps
// msg.Creator bound to the proof/public provenance while debiting only the
// separately authenticated funder supplied by the caller. The caller must
// authenticate Creator, amount/value, and a fixed escrow funder, and must keep
// the EVM value movement plus all SDK state and events inside one outer
// rollback boundary.
func (k Keeper) DepositWithFunderV2(ctx sdk.Context, msg *privacyv2.MsgDeposit, funder sdk.AccAddress) (*privacyv2.MsgDepositResponse, error) {
	if err := sdk.VerifyAddressFormat(funder); err != nil || len(funder) == 0 {
		return nil, fmt.Errorf("invalid delegated deposit funder")
	}
	funder = append(sdk.AccAddress(nil), funder...)
	if funder.Equals(authtypes.NewModuleAddress(types.ModuleName)) || funder.Equals(authtypes.NewModuleAddress(govtypes.ModuleName)) {
		return nil, fmt.Errorf("invalid delegated deposit funder")
	}
	if msg == nil {
		return nil, fmt.Errorf("nil deposit")
	}
	if proto.Size(msg) > types.MaxDelegatedDepositPayloadBytes {
		return nil, fmt.Errorf("delegated deposit payload exceeds %d-byte cap", types.MaxDelegatedDepositPayloadBytes)
	}
	payload, err := proto.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("marshal delegated deposit: %w", err)
	}
	if len(payload) == 0 || len(payload) > types.MaxDelegatedDepositPayloadBytes {
		return nil, fmt.Errorf("delegated deposit payload exceeds %d-byte cap", types.MaxDelegatedDepositPayloadBytes)
	}
	server := auditMsgServer{Keeper: k}
	_, err = server.executeAuditWithDelegation(ctx, msg, &delegatedDepositExecution{funder: funder, payload: append([]byte(nil), payload...)})
	if err != nil {
		return nil, err
	}
	return &privacyv2.MsgDepositResponse{}, nil
}
func (k auditMsgServer) Deposit(ctx context.Context, m *privacyv2.MsgDeposit) (*privacyv2.MsgDepositResponse, error) {
	_, err := k.executeAudit(sdk.UnwrapSDKContext(ctx), m)
	if err != nil {
		return nil, err
	}
	return &privacyv2.MsgDepositResponse{}, nil
}
func (k auditMsgServer) Withdraw(ctx context.Context, m *privacyv2.MsgWithdraw) (*privacyv2.MsgWithdrawResponse, error) {
	_, err := k.executeAudit(sdk.UnwrapSDKContext(ctx), m)
	if err != nil {
		return nil, err
	}
	return &privacyv2.MsgWithdrawResponse{}, nil
}
func (k auditMsgServer) Transfer(ctx context.Context, m *privacyv2.MsgTransfer) (*privacyv2.MsgTransferResponse, error) {
	_, err := k.executeAudit(sdk.UnwrapSDKContext(ctx), m)
	if err != nil {
		return nil, err
	}
	return &privacyv2.MsgTransferResponse{}, nil
}
func (k auditMsgServer) BatchTransfer(ctx context.Context, m *privacyv2.MsgBatchTransfer) (*privacyv2.MsgBatchTransferResponse, error) {
	_, err := k.executeAudit(sdk.UnwrapSDKContext(ctx), m)
	if err != nil {
		return nil, err
	}
	return &privacyv2.MsgBatchTransferResponse{}, nil
}

// No point/hash/proof decoding occurs before this deterministic charge. The
// bound includes maximal varints and actual payload lengths; apply measures the
// scan state it writes and rejects any bound underestimate.
func prechargeAuditMessage(ctx sdk.Context, raw sdk.Msg, delegatedEventBytes ...uint64) (uint64, error) {
	var kind auditfield.Kind
	var ni int
	var outputs []*privacyv2.OutputEffect
	switch m := raw.(type) {
	case *privacyv2.MsgDeposit:
		if m == nil {
			return 0, fmt.Errorf("nil deposit")
		}
		kind = 1
		outputs = []*privacyv2.OutputEffect{m.Output}
	case *privacyv2.MsgWithdraw:
		if m == nil {
			return 0, fmt.Errorf("nil withdraw")
		}
		kind = 2
		ni = 1
	case *privacyv2.MsgTransfer:
		if m == nil {
			return 0, fmt.Errorf("nil transfer")
		}
		kind = 3
		ni = len(m.Nullifiers)
		outputs = m.Outputs
	case *privacyv2.MsgBatchTransfer:
		if m == nil {
			return 0, fmt.Errorf("nil batch")
		}
		kind = 4
		ni = len(m.Nullifiers)
		outputs = m.Outputs
	default:
		return 0, fmt.Errorf("unsupported audit message")
	}
	if ni > 16 || len(outputs) > 32 || proto.Size(raw) > types.MaxBatchTransferMessageBytesV1 {
		return 0, fmt.Errorf("audit message exceeds shape/byte cap")
	}
	aux := uint64(4)
	if len(delegatedEventBytes) > 1 {
		return 0, fmt.Errorf("invalid delegated event gas input")
	}
	if len(delegatedEventBytes) == 1 {
		if delegatedEventBytes[0] > types.MaxBatchTransferMessageBytesV1 || aux > math.MaxUint64-delegatedEventBytes[0] {
			return 0, fmt.Errorf("delegated event exceeds gas bound")
		}
		aux += delegatedEventBytes[0]
	}

	for _, o := range outputs {
		if o == nil {
			return 0, fmt.Errorf("nil output")
		}
		payload := uint64(len(o.Ciphertext) + len(o.ViewTag) + len(o.UserDisclosureTargetPubkey) + len(o.UserDisclosurePayload) + len(o.SelfViewDisclosurePayload))
		aux += 89 + payload

	} // per-output metadata and maximal cursor/leaf varints.
	// Reuse the scan serializers with maximal cursor/key metadata and the
	// actual user output bytes. Proofs and audit envelopes are intentionally
	// absent: neither is copied into scan state or an audit record/index.
	r := &auditTransitionRecord{
		Kind:    kind,
		SetID:   auditfield.CircuitSetID,
		Epoch:   math.MaxUint64,
		PK:      make([]byte, 64),
		Origin:  auditOrigin{Kind: 1, Height: math.MaxInt64},
		Inputs:  make([]auditfield.Field32, ni),
		Outputs: make([]auditOutputRef, len(outputs)),
	}
	for i := range r.Outputs {
		r.Outputs[i].LeafIndex = math.MaxUint32
	}
	summary, projectedOutputs := auditScanProjection(math.MaxUint64, math.MaxInt64, r, outputs, [32]byte{})
	projection := uint64(summary.Size() + 17 + 9 + 8)
	for _, output := range projectedOutputs {
		projection += uint64(output.Size() + 21)
	}
	if projection > math.MaxUint32 {
		return 0, fmt.Errorf("audit projection overflow")
	}
	gas, err := zk.ComputeAuditGasV1(zk.DefaultAuditGasModelV1(), zk.AuditResourceBoundsV1{MaxCanonicalAuxEnvelopeBytes: types.MaxBatchTransferMessageBytesV1, MaxProjectedStateBytes: 1 << 20}, zk.AuditResourceUsageV1{Kind: kind, InputCount: uint64(ni), OutputCount: uint64(len(outputs)), AuxBytes: aux, ProjectedStateBytes: projection})
	if err != nil {
		return 0, err
	}
	ctx.GasMeter().ConsumeGas(gas.Total, "audit transition precharge")
	return projection, nil
}
