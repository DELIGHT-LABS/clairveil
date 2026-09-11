package keeper

import (
	"fmt"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"reflect"
)

func (k Keeper) ValidateLoadedAuditIdentity(ctx sdk.Context) error {
	if k.audit == nil {
		return fmt.Errorf("audit runtime missing")
	}
	identity, found, err := k.GetCircuitSetIdentity(ctx)
	if err != nil {
		return err
	}
	if !found || !reflect.DeepEqual(identity, k.audit.identity) {
		return fmt.Errorf("loaded audit circuit identity mismatch")
	}
	// Initial key PoP binds the stored state to the configured chain/nonce and
	// original initial height even when no private transition has occurred.
	_, _, err = k.getAuditKey(ctx, 1)
	return err
}
