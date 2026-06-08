package keeper

import (
	"fmt"

	sdkmath "cosmossdk.io/math"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

type ReserveSnapshot struct {
	Denom                 string
	ModuleBalance         sdkmath.Int
	TotalDeposited        sdkmath.Int
	TotalWithdrawn        sdkmath.Int
	ExpectedModuleBalance sdkmath.Int
	InvariantHolds        bool
}

func (k Keeper) RecordReserveDeposit(ctx sdk.Context, coin sdk.Coin) error {
	if err := validateReserveCoin("reserve deposit", coin); err != nil {
		return err
	}

	return k.addReserveAmount(ctx, types.GetReserveDepositKey(coin.Denom), coin.Amount)
}

func (k Keeper) RecordReserveWithdraw(ctx sdk.Context, coin sdk.Coin) error {
	if err := validateReserveCoin("reserve withdraw", coin); err != nil {
		return err
	}

	return k.addReserveAmount(ctx, types.GetReserveWithdrawKey(coin.Denom), coin.Amount)
}

func (k Keeper) GetReserveSnapshot(ctx sdk.Context, denom string) (ReserveSnapshot, error) {
	if err := sdk.ValidateDenom(denom); err != nil {
		return ReserveSnapshot{}, fmt.Errorf("reserve denom is invalid: %w", err)
	}

	deposited, err := k.getReserveAmount(ctx, types.GetReserveDepositKey(denom))
	if err != nil {
		return ReserveSnapshot{}, fmt.Errorf("failed to load reserve deposits for %s: %w", denom, err)
	}
	withdrawn, err := k.getReserveAmount(ctx, types.GetReserveWithdrawKey(denom))
	if err != nil {
		return ReserveSnapshot{}, fmt.Errorf("failed to load reserve withdrawals for %s: %w", denom, err)
	}

	expected := deposited.Sub(withdrawn)
	moduleAddress := authtypes.NewModuleAddress(types.ModuleName)
	moduleBalance := k.bankKeeper.GetBalance(ctx, moduleAddress, denom).Amount
	invariantHolds := !expected.IsNegative() && moduleBalance.Equal(expected)

	return ReserveSnapshot{
		Denom:                 denom,
		ModuleBalance:         moduleBalance,
		TotalDeposited:        deposited,
		TotalWithdrawn:        withdrawn,
		ExpectedModuleBalance: expected,
		InvariantHolds:        invariantHolds,
	}, nil
}

func validateReserveCoin(name string, coin sdk.Coin) error {
	if err := coin.Validate(); err != nil {
		return fmt.Errorf("%s coin is invalid: %w", name, err)
	}
	if coin.Amount.IsNegative() {
		return fmt.Errorf("%s amount must be non-negative", name)
	}
	return nil
}

func (k Keeper) addReserveAmount(ctx sdk.Context, key []byte, amount sdkmath.Int) error {
	current, err := k.getReserveAmount(ctx, key)
	if err != nil {
		return err
	}

	return k.setReserveAmount(ctx, key, current.Add(amount))
}

func (k Keeper) getReserveAmount(ctx sdk.Context, key []byte) (sdkmath.Int, error) {
	store := k.storeService.OpenKVStore(ctx)
	bz, err := store.Get(key)
	if err != nil {
		return sdkmath.Int{}, err
	}
	if len(bz) == 0 {
		return sdkmath.ZeroInt(), nil
	}

	amount, ok := sdkmath.NewIntFromString(string(bz))
	if !ok {
		return sdkmath.Int{}, fmt.Errorf("stored reserve amount is invalid")
	}
	return amount, nil
}

func (k Keeper) setReserveAmount(ctx sdk.Context, key []byte, amount sdkmath.Int) error {
	store := k.storeService.OpenKVStore(ctx)
	return store.Set(key, []byte(amount.String()))
}
