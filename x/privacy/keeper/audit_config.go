package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func (k Keeper) GetAuditMasterPubkey(ctx sdk.Context) []byte {
	store := k.storeService.OpenKVStore(ctx)
	bz, err := store.Get(types.GetAuditConfigKey())
	if err != nil || len(bz) == 0 {
		return nil
	}

	return append([]byte(nil), bz...)
}

func (k Keeper) SetAuditMasterPubkey(ctx sdk.Context, pubKey []byte) {
	store := k.storeService.OpenKVStore(ctx)
	store.Set(types.GetAuditConfigKey(), append([]byte(nil), pubKey...))
}
