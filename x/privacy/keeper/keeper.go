package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/core/store"
	"cosmossdk.io/log/v2"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

// BankKeeper captures the bank methods used by the privacy keeper.
type BankKeeper interface {
	GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin
	SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error
}

type Keeper struct {
	cdc          codec.BinaryCodec
	storeService store.KVStoreService
	paramstore   paramtypes.Subspace
	bankKeeper   BankKeeper
}

func NewKeeper(
	cdc codec.BinaryCodec,
	ss store.KVStoreService,
	ps paramtypes.Subspace,
	bk BankKeeper,
) *Keeper {
	return &Keeper{
		cdc:          cdc,
		storeService: ss,
		paramstore:   ps,
		bankKeeper:   bk,
	}
}

// Logger returns a module-specific logger.
func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", fmt.Sprintf("x/%s", types.ModuleName))
}
