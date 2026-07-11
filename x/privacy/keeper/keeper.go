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
	cdc                             codec.BinaryCodec
	storeService                    store.KVStoreService
	paramstore                      paramtypes.Subspace
	bankKeeper                      BankKeeper
	historicalPathQueryRebuildSlots chan struct{}
	// batchTransitionHook is nil in production and is intentionally unexported.
	// Keeper-package integration tests use it to prove that the nested cache is
	// discarded when a deterministic mid-transition failure occurs.
	batchTransitionHook func(stage string) error
}

func NewKeeper(
	cdc codec.BinaryCodec,
	ss store.KVStoreService,
	ps paramtypes.Subspace,
	bk BankKeeper,
) *Keeper {
	return &Keeper{
		cdc:                             cdc,
		storeService:                    ss,
		paramstore:                      ps,
		bankKeeper:                      bk,
		historicalPathQueryRebuildSlots: make(chan struct{}, MaxConcurrentHistoricalPathQueryRebuilds),
	}
}

// Logger returns a module-specific logger.
func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", fmt.Sprintf("x/%s", types.ModuleName))
}
