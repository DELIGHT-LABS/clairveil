package keeper

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/auditfield"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	privacyv2 "github.com/DELIGHT-LABS/clairveil/x/privacy/types/v2"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k Keeper) getAuditKey(ctx sdk.Context, epoch uint64) (*auditKeyRecord, []byte, error) {
	if k.audit == nil {
		return nil, nil, fmt.Errorf("audit runtime is not configured")
	}
	raw, err := k.storeService.OpenKVStore(ctx).Get(auditEpochKey(auditEpochPrefix, epoch))
	if err != nil {
		return nil, nil, err
	}
	if raw == nil {
		return nil, nil, fmt.Errorf("audit epoch not found")
	}
	network, err := k.auditNetwork(ctx)
	if err != nil {
		return nil, nil, err
	}
	v, err := decodeAuditKey(raw, network, k.audit.initialHeight)
	if err != nil {
		return nil, nil, err
	}
	if v.Epoch != epoch {
		return nil, nil, fmt.Errorf("audit epoch key/value mismatch")
	}
	return v, raw, nil
}
func (k Keeper) activeAuditKey(ctx sdk.Context) (*auditKeyRecord, error) {
	raw, err := k.storeService.OpenKVStore(ctx).Get([]byte{auditActivePrefix})
	if err != nil {
		return nil, err
	}
	if len(raw) != 18 || binary.BigEndian.Uint16(raw) != 1 {
		return nil, fmt.Errorf("active audit key is not initialized")
	}
	v, _, err := k.getAuditKey(ctx, binary.BigEndian.Uint64(raw[2:10]))
	if err != nil {
		return nil, err
	}
	if v.ActivationHeight != binary.BigEndian.Uint64(raw[10:]) || v.ActivationHeight > uint64(ctx.BlockHeight()) {
		return nil, fmt.Errorf("invalid active audit epoch")
	}
	return v, nil
}
func (k Keeper) auditHalted(ctx sdk.Context) (bool, error) {
	raw, err := k.storeService.OpenKVStore(ctx).Get([]byte{auditHaltPrefix})
	if err != nil {
		return false, err
	}
	if raw == nil {
		return false, nil
	}
	if len(raw) != 1 || raw[0] > 1 {
		return false, fmt.Errorf("invalid privacy halt state")
	}
	return raw[0] == 1, nil
}
func (k Keeper) pendingAuditEpoch(ctx sdk.Context) (epoch, height uint64, returnErr error) {
	it, err := k.storeService.OpenKVStore(ctx).Iterator([]byte{auditActivationPrefix}, []byte{auditActivationPrefix + 1})
	if err != nil {
		return 0, 0, err
	}
	defer func() {
		if err := it.Close(); err != nil {
			returnErr = errors.Join(returnErr, err)
		}
	}()
	for ; it.Valid(); it.Next() {
		if epoch != 0 || len(it.Key()) != 9 || len(it.Value()) != 8 {
			return 0, 0, fmt.Errorf("invalid or multiple audit activation pointers")
		}
		height = binary.BigEndian.Uint64(it.Key()[1:])
		epoch = binary.BigEndian.Uint64(it.Value())
		if epoch == 0 || height > math.MaxInt64 {
			return 0, 0, fmt.Errorf("invalid audit activation")
		}
	}
	return epoch, height, nil
}
func (k Keeper) checkAuditAuthority(ctx sdk.Context, authority string) error {
	if k.audit == nil || authority != k.audit.authority {
		return fmt.Errorf("audit authority must equal gov module address")
	}
	if ctx.Value(auditApplyContextKey{}) != nil {
		return fmt.Errorf("nested audit mutation is forbidden")
	}
	_, err := k.auditExecutionOrigin(ctx)
	return err
}

func (k auditMsgServer) ScheduleAuditKeyEpoch(goCtx context.Context, msg *privacyv2.MsgScheduleAuditKeyEpoch) (*privacyv2.MsgScheduleAuditKeyEpochResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	if msg == nil {
		return nil, fmt.Errorf("nil audit key schedule")
	}
	if err := k.checkAuditAuthority(ctx, msg.Authority); err != nil {
		return nil, err
	}
	if msg.Suite != 2 || msg.Epoch == 0 || msg.ActivationHeight > math.MaxInt64 || msg.ActivationHeight <= uint64(ctx.BlockHeight()) {
		return nil, fmt.Errorf("audit epoch activation must be future with suite2")
	}
	ctx.GasMeter().ConsumeGas(400000+uint64(len(msg.PublicKey)+len(msg.PossessionProof))*4, "audit key validation")
	if msg.PublicKey == nil || len(msg.PublicKey) != 64 || len(msg.PossessionProof) != 96 {
		return nil, fmt.Errorf("audit key requires Point64 and PoP96")
	}
	key, err := auditfield.NewAuditKeyFromPoint64(msg.PublicKey)
	if err != nil {
		return nil, err
	}
	pending, _, err := k.pendingAuditEpoch(ctx)
	if err != nil {
		return nil, err
	}
	if pending != 0 {
		return nil, fmt.Errorf("an audit epoch is already pending")
	}
	maxEpoch, err := k.maxAuditEpoch(ctx, key.ID())
	if err != nil {
		return nil, err
	}
	if maxEpoch == math.MaxUint64 || msg.Epoch != maxEpoch+1 || maxEpoch == 0 {
		return nil, fmt.Errorf("audit epoch must be registered max+1 after initialization")
	}
	origin, err := k.auditExecutionOrigin(ctx)
	if err != nil {
		return nil, err
	}
	network, err := k.auditNetwork(ctx)
	if err != nil {
		return nil, err
	}
	record := &auditKeyRecord{Epoch: msg.Epoch, KeyID: key.ID(), Suite: 2, PK: append([]byte(nil), msg.PublicKey...), ActivationHeight: msg.ActivationHeight, RegisteredHeight: uint64(ctx.BlockHeight()), Origin: origin, PoP: append([]byte(nil), msg.PossessionProof...)}
	encoded, err := encodeAuditKey(record, network, k.audit.initialHeight)
	if err != nil {
		return nil, err
	}
	cache, publish := ctx.CacheContext()
	store := k.storeService.OpenKVStore(cache)
	if err := store.Set(auditEpochKey(auditEpochPrefix, msg.Epoch), encoded); err != nil {
		return nil, err
	}
	var epoch [8]byte
	binary.BigEndian.PutUint64(epoch[:], msg.Epoch)
	if err := store.Set(auditEpochKey(auditActivationPrefix, msg.ActivationHeight), epoch[:]); err != nil {
		return nil, err
	}
	cache.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeAuditKeyScheduled, sdk.NewAttribute("epoch", fmt.Sprint(record.Epoch)), sdk.NewAttribute("key_id", hex.EncodeToString(record.KeyID[:])), sdk.NewAttribute("activation_height", fmt.Sprint(record.ActivationHeight))))
	publish()
	return &privacyv2.MsgScheduleAuditKeyEpochResponse{}, nil
}
func (k Keeper) maxAuditEpoch(ctx sdk.Context, newID [32]byte) (max uint64, returnErr error) {
	it, err := k.storeService.OpenKVStore(ctx).Iterator([]byte{auditEpochPrefix}, []byte{auditEpochPrefix + 1})
	if err != nil {
		return 0, err
	}
	defer func() {
		if err := it.Close(); err != nil {
			returnErr = errors.Join(returnErr, err)
		}
	}()
	network, err := k.auditNetwork(ctx)
	if err != nil {
		return 0, err
	}
	for ; it.Valid(); it.Next() {
		if len(it.Key()) != 9 {
			return 0, fmt.Errorf("malformed audit epoch key")
		}
		v, err := decodeAuditKey(it.Value(), network, k.audit.initialHeight)
		if err != nil {
			return 0, err
		}
		if v.Epoch != binary.BigEndian.Uint64(it.Key()[1:]) || v.Epoch != max+1 {
			return 0, fmt.Errorf("noncontiguous audit epoch history")
		}
		if v.KeyID == newID {
			return 0, fmt.Errorf("audit key ID cannot be reused")
		}
		max = v.Epoch
	}
	return max, nil
}
func (k auditMsgServer) CancelPendingAuditEpoch(goCtx context.Context, msg *privacyv2.MsgCancelPendingAuditEpoch) (*privacyv2.MsgCancelPendingAuditEpochResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	if msg == nil {
		return nil, fmt.Errorf("nil audit cancel")
	}
	if err := k.checkAuditAuthority(ctx, msg.Authority); err != nil {
		return nil, err
	}
	epoch, height, err := k.pendingAuditEpoch(ctx)
	if err != nil {
		return nil, err
	}
	if epoch == 0 || epoch != msg.Epoch || height <= uint64(ctx.BlockHeight()) {
		return nil, fmt.Errorf("epoch is not a future pending activation")
	}
	origin, err := k.auditExecutionOrigin(ctx)
	if err != nil {
		return nil, err
	}
	cache, publish := ctx.CacheContext()
	store := k.storeService.OpenKVStore(cache)
	key := auditEpochKey(auditCancellationPrefix, epoch)
	exists, err := store.Has(key)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, fmt.Errorf("audit cancellation is immutable")
	}
	w := &auditEncoder{}
	w.u16(1)
	w.origin(origin)
	w.u64(uint64(ctx.BlockHeight()))
	if err := store.Set(key, w.Bytes()); err != nil {
		return nil, err
	}
	if err := store.Delete(auditEpochKey(auditActivationPrefix, height)); err != nil {
		return nil, err
	}
	cache.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeAuditKeyCancelled, sdk.NewAttribute("epoch", fmt.Sprint(epoch)), sdk.NewAttribute("activation_height", fmt.Sprint(height))))
	publish()
	return &privacyv2.MsgCancelPendingAuditEpochResponse{}, nil
}
func (k auditMsgServer) SetPrivacyHalt(goCtx context.Context, msg *privacyv2.MsgSetPrivacyHalt) (*privacyv2.MsgSetPrivacyHaltResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	if msg == nil {
		return nil, fmt.Errorf("nil privacy halt")
	}
	if err := k.checkAuditAuthority(ctx, msg.Authority); err != nil {
		return nil, err
	}
	if !msg.Halted {
		if _, err := k.activeAuditKey(ctx); err != nil {
			return nil, err
		}
		if _, err := k.maxAuditEpoch(ctx, [32]byte{}); err != nil {
			return nil, err
		}
	}
	b := byte(0)
	if msg.Halted {
		b = 1
	}
	cache, publish := ctx.CacheContext()
	if err := k.storeService.OpenKVStore(cache).Set([]byte{auditHaltPrefix}, []byte{b}); err != nil {
		return nil, err
	}
	cache.EventManager().EmitEvent(sdk.NewEvent(types.EventTypePrivacyHaltChanged, sdk.NewAttribute("halted", fmt.Sprint(msg.Halted))))
	publish()
	return &privacyv2.MsgSetPrivacyHaltResponse{}, nil
}

func (k Keeper) BeginAuditBlock(ctx sdk.Context) error {
	if k.audit == nil {
		return nil
	}
	epoch, height, err := k.pendingAuditEpoch(ctx)
	if err != nil {
		return err
	}
	if epoch == 0 || height > uint64(ctx.BlockHeight()) {
		return nil
	}
	if height != uint64(ctx.BlockHeight()) {
		return fmt.Errorf("missed audit key activation")
	}
	record, _, err := k.getAuditKey(ctx, epoch)
	if err != nil {
		return err
	}
	if record.ActivationHeight != height {
		return fmt.Errorf("audit activation index mismatch")
	}
	cancel, err := k.storeService.OpenKVStore(ctx).Has(auditEpochKey(auditCancellationPrefix, epoch))
	if err != nil {
		return err
	}
	if cancel {
		return fmt.Errorf("cancelled audit epoch cannot activate")
	}
	cache, publish := ctx.CacheContext()
	store := k.storeService.OpenKVStore(cache)
	w := &auditEncoder{}
	w.u16(1)
	w.u64(epoch)
	w.u64(height)
	if err := store.Set([]byte{auditActivePrefix}, w.Bytes()); err != nil {
		return err
	}
	if err := store.Delete(auditEpochKey(auditActivationPrefix, height)); err != nil {
		return err
	}
	cache.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeAuditKeyActivated, sdk.NewAttribute("epoch", fmt.Sprint(record.Epoch)), sdk.NewAttribute("key_id", hex.EncodeToString(record.KeyID[:])), sdk.NewAttribute("activation_height", fmt.Sprint(height))))
	publish()
	return nil
}
