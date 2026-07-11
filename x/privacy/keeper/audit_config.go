package keeper

import (
	"encoding/binary"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

const auditKeyEpochByteSizeV1 = 8

// GetAuditMasterPubkey is the legacy JoinSplit2x2 compatibility accessor. New
// consensus paths must use GetAuditConfigV1 so malformed/partial identity
// state cannot be accepted as a configured audit target.
func (k Keeper) GetAuditMasterPubkey(ctx sdk.Context) []byte {
	config, configured, err := k.GetAuditConfigV1(ctx)
	if err != nil || !configured {
		return nil
	}
	return append([]byte(nil), config.AuditTargetPubkey...)
}

// SetAuditMasterPubkey preserves the legacy setter contract used by current
// JoinSplit2x2 tests while assigning the frozen default audit identity. It
// cannot report store failures; production initialization uses
// SetAuditConfigV1 instead.
func (k Keeper) SetAuditMasterPubkey(ctx sdk.Context, pubKey []byte) {
	if len(pubKey) == 0 {
		_ = k.SetAuditConfigV1(ctx, "", 0, nil)
		return
	}
	_ = k.SetAuditConfigV1(ctx, types.DefaultAuditKeyIDV1, types.DefaultAuditKeyEpochV1, pubKey)
}

// SetAuditConfigV1 stores an all-zero or fully populated exact audit identity.
// Values are validated before any write, and every store error is returned.
func (k Keeper) SetAuditConfigV1(ctx sdk.Context, auditKeyID string, auditKeyEpoch uint64, auditTargetPubkey []byte) error {
	config := types.AuditConfigV1{
		AuditKeyID:        auditKeyID,
		AuditKeyEpoch:     auditKeyEpoch,
		AuditTargetPubkey: auditTargetPubkey,
	}
	if err := types.ValidateAuditConfigV1(config); err != nil {
		return err
	}

	cacheCtx, writeCache := ctx.CacheContext()
	store := k.storeService.OpenKVStore(cacheCtx)
	if auditKeyID == "" {
		if err := store.Delete(types.GetAuditConfigKey()); err != nil {
			return fmt.Errorf("delete audit target pubkey: %w", err)
		}
		if err := store.Delete(types.GetAuditKeyIDKey()); err != nil {
			return fmt.Errorf("delete audit key id: %w", err)
		}
		if err := store.Delete(types.GetAuditKeyEpochKey()); err != nil {
			return fmt.Errorf("delete audit key epoch: %w", err)
		}
		writeCache()
		return nil
	}

	epochBytes := make([]byte, auditKeyEpochByteSizeV1)
	binary.BigEndian.PutUint64(epochBytes, auditKeyEpoch)
	if err := store.Set(types.GetAuditConfigKey(), append([]byte(nil), auditTargetPubkey...)); err != nil {
		return fmt.Errorf("store audit target pubkey: %w", err)
	}
	if err := store.Set(types.GetAuditKeyIDKey(), []byte(auditKeyID)); err != nil {
		return fmt.Errorf("store audit key id: %w", err)
	}
	if err := store.Set(types.GetAuditKeyEpochKey(), epochBytes); err != nil {
		return fmt.Errorf("store audit key epoch: %w", err)
	}
	writeCache()
	return nil
}

// GetAuditConfigV1 loads and validates the exact audit identity. All absent
// keys are the valid unconfigured state; any partial or malformed state is an
// error so consensus callers fail closed.
func (k Keeper) GetAuditConfigV1(ctx sdk.Context) (types.AuditConfigV1, bool, error) {
	store := k.storeService.OpenKVStore(ctx)
	target, err := store.Get(types.GetAuditConfigKey())
	if err != nil {
		return types.AuditConfigV1{}, false, fmt.Errorf("load audit target pubkey: %w", err)
	}
	idBytes, err := store.Get(types.GetAuditKeyIDKey())
	if err != nil {
		return types.AuditConfigV1{}, false, fmt.Errorf("load audit key id: %w", err)
	}
	epochBytes, err := store.Get(types.GetAuditKeyEpochKey())
	if err != nil {
		return types.AuditConfigV1{}, false, fmt.Errorf("load audit key epoch: %w", err)
	}

	hasTarget := len(target) != 0
	hasID := len(idBytes) != 0
	hasEpoch := len(epochBytes) != 0
	if !hasTarget && !hasID && !hasEpoch {
		return types.AuditConfigV1{}, false, nil
	}
	if !hasTarget || !hasID || !hasEpoch {
		return types.AuditConfigV1{}, false, fmt.Errorf("audit config state is partial: target=%t id=%t epoch=%t", hasTarget, hasID, hasEpoch)
	}
	if len(epochBytes) != auditKeyEpochByteSizeV1 {
		return types.AuditConfigV1{}, false, fmt.Errorf("audit key epoch state must be exactly %d bytes; got %d", auditKeyEpochByteSizeV1, len(epochBytes))
	}

	config := types.AuditConfigV1{
		AuditKeyID:        string(idBytes),
		AuditKeyEpoch:     binary.BigEndian.Uint64(epochBytes),
		AuditTargetPubkey: append([]byte(nil), target...),
	}
	if err := types.ValidateAuditConfigV1(config); err != nil {
		return types.AuditConfigV1{}, false, fmt.Errorf("invalid audit config state: %w", err)
	}
	return config, true, nil
}
