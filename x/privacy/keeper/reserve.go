package keeper

import (
	"errors"
	"fmt"
	"math/big"

	sdkmath "cosmossdk.io/math"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

var (
	ErrReserveStateInvalid        = errors.New("invalid reserve state")
	ErrReserveUnderCollateralized = errors.New("reserve is undercollateralized")
)

type ReserveSnapshot struct {
	Denom                 string
	ModuleBalance         sdkmath.Int
	TotalDeposited        sdkmath.Int
	TotalWithdrawn        sdkmath.Int
	ExpectedModuleBalance sdkmath.Int
	InvariantHolds        bool
	Liability             sdkmath.Int
	Collateralized        bool
	Surplus               sdkmath.Int
	Shortfall             sdkmath.Int
}

func (k Keeper) RecordReserveDeposit(ctx sdk.Context, coin sdk.Coin) error {
	if k.audit != nil {
		return fmt.Errorf("raw reserve mutation is disabled for audit-field")
	}
	return k.recordReserveDeposit(ctx, coin)
}

func (k Keeper) recordReserveDeposit(ctx sdk.Context, coin sdk.Coin) error {
	if err := validateReserveCoin("reserve deposit", coin); err != nil {
		return err
	}

	return k.addReserveAmount(ctx, types.GetReserveDepositKey(coin.Denom), coin.Amount)
}

func (k Keeper) RecordReserveWithdraw(ctx sdk.Context, coin sdk.Coin) error {
	if k.audit != nil {
		return fmt.Errorf("raw reserve mutation is disabled for audit-field")
	}
	return k.recordReserveWithdraw(ctx, coin)
}

func (k Keeper) recordReserveWithdraw(ctx sdk.Context, coin sdk.Coin) error {
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

	if deposited.LT(withdrawn) {
		return ReserveSnapshot{}, fmt.Errorf("%w: deposits below withdrawals", ErrReserveStateInvalid)
	}
	expected := deposited.Sub(withdrawn)
	moduleAddress := authtypes.NewModuleAddress(types.ModuleName)
	moduleBalance := k.bankKeeper.GetBalance(ctx, moduleAddress, denom).Amount
	if moduleBalance.IsNil() || moduleBalance.IsNegative() || moduleBalance.BigInt().BitLen() > 256 {
		return ReserveSnapshot{}, fmt.Errorf("%w: invalid module balance", ErrReserveStateInvalid)
	}
	invariantHolds := moduleBalance.Equal(expected)
	collateralized := moduleBalance.GTE(expected)
	surplus, shortfall := sdkmath.ZeroInt(), sdkmath.ZeroInt()
	if collateralized {
		surplus = moduleBalance.Sub(expected)
	} else {
		shortfall = expected.Sub(moduleBalance)
	}

	return ReserveSnapshot{
		Denom:                 denom,
		ModuleBalance:         moduleBalance,
		TotalDeposited:        deposited,
		TotalWithdrawn:        withdrawn,
		ExpectedModuleBalance: expected,
		InvariantHolds:        invariantHolds,
		Liability:             expected, Collateralized: collateralized, Surplus: surplus, Shortfall: shortfall,
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

	if amount.IsNil() || amount.IsNegative() {
		return fmt.Errorf("%w: invalid increment", ErrReserveStateInvalid)
	}
	sum := new(big.Int).Add(current.BigInt(), amount.BigInt())
	if sum.BitLen() > 256 {
		return fmt.Errorf("%w: counter overflow", ErrReserveStateInvalid)
	}
	return k.setReserveAmount(ctx, key, sdkmath.NewIntFromBigInt(sum))
}

func (k Keeper) getReserveAmount(ctx sdk.Context, key []byte) (sdkmath.Int, error) {
	store := k.storeService.OpenKVStore(ctx)
	bz, err := store.Get(key)
	if err != nil {
		return sdkmath.Int{}, err
	}
	if bz == nil {
		return sdkmath.ZeroInt(), nil
	}

	if len(bz) == 0 {
		return sdkmath.Int{}, fmt.Errorf("%w: empty counter", ErrReserveStateInvalid)
	}
	if string(bz) != "0" && (bz[0] < '1' || bz[0] > '9') {
		return sdkmath.Int{}, fmt.Errorf("%w: noncanonical counter", ErrReserveStateInvalid)
	}
	if len(bz) > 78 {
		return sdkmath.Int{}, fmt.Errorf("%w: counter overflow", ErrReserveStateInvalid)
	}
	for _, b := range bz {
		if b < '0' || b > '9' {
			return sdkmath.Int{}, fmt.Errorf("%w: noncanonical counter", ErrReserveStateInvalid)
		}
	}
	amount, ok := new(big.Int).SetString(string(bz), 10)
	if !ok || amount.BitLen() > 256 {
		return sdkmath.Int{}, fmt.Errorf("%w: counter overflow", ErrReserveStateInvalid)
	}
	return sdkmath.NewIntFromBigInt(amount), nil
}

func (k Keeper) setReserveAmount(ctx sdk.Context, key []byte, amount sdkmath.Int) error {
	if amount.IsNil() || amount.IsNegative() || amount.BigInt().BitLen() > 256 {
		return fmt.Errorf("%w: invalid counter", ErrReserveStateInvalid)
	}
	store := k.storeService.OpenKVStore(ctx)
	return store.Set(key, []byte(amount.String()))
}
