package keeper

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func (k Keeper) SetCircuitSetIdentity(ctx sdk.Context, identity *types.CircuitSetIdentity) error {
	if err := types.ValidateCircuitSetIdentity(identity); err != nil {
		return err
	}
	bz, err := identity.Marshal()
	if err != nil {
		return fmt.Errorf("marshal circuit set identity: %w", err)
	}
	return k.storeService.OpenKVStore(ctx).Set(types.GetCircuitIdentityKey(), bz)
}

func (k Keeper) GetCircuitSetIdentity(ctx sdk.Context) (*types.CircuitSetIdentity, bool, error) {
	bz, err := k.storeService.OpenKVStore(ctx).Get(types.GetCircuitIdentityKey())
	if err != nil {
		return nil, false, err
	}
	if len(bz) == 0 {
		return nil, false, nil
	}
	var identity types.CircuitSetIdentity
	if err := identity.Unmarshal(bz); err != nil {
		return nil, false, fmt.Errorf("unmarshal circuit set identity: %w", err)
	}
	if err := types.ValidateCircuitSetIdentity(&identity); err != nil {
		return nil, false, fmt.Errorf("stored circuit set identity is invalid: %w", err)
	}
	return types.CloneCircuitSetIdentity(&identity), true, nil
}
