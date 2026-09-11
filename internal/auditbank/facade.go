package auditbank

import (
	"context"
	"fmt"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
)

// Facade exposes only the bank capabilities required by application modules.
// Audit mode reserves privacy debits for the private principal adapter.
type Facade struct {
	bank  bankkeeper.Keeper
	audit bool
}

func NewFacade(bank bankkeeper.Keeper, audit bool) Facade { return Facade{bank: bank, audit: audit} }
func (f Facade) deny(ctx context.Context, privacy bool) error {
	if !f.audit {
		return nil
	}
	if scope, ok := ctx.Value(auditPrincipalContextKey{}).(*auditPrincipalScope); ok {
		scope.mu.Lock()
		scope.failed = true
		scope.mu.Unlock()
		return fmt.Errorf("bank mutation during privacy principal scope")
	}
	if privacy {
		return fmt.Errorf("privacy bank capability denied")
	}
	return nil
}
func privacyAddress(a sdk.AccAddress) bool {
	return a.Equals(authtypes.NewModuleAddress(privacytypes.ModuleName))
}
func (f Facade) GetAllBalances(ctx context.Context, addr sdk.AccAddress) sdk.Coins {
	return f.bank.GetAllBalances(ctx, addr)
}
func (f Facade) GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin {
	return f.bank.GetBalance(ctx, addr, denom)
}
func (f Facade) LockedCoins(ctx context.Context, addr sdk.AccAddress) sdk.Coins {
	return f.bank.LockedCoins(ctx, addr)
}
func (f Facade) SpendableCoins(ctx context.Context, addr sdk.AccAddress) sdk.Coins {
	return f.bank.SpendableCoins(ctx, addr)
}
func (f Facade) GetSupply(ctx context.Context, denom string) sdk.Coin {
	return f.bank.GetSupply(ctx, denom)
}
func (f Facade) IsSendEnabledCoins(ctx context.Context, coins ...sdk.Coin) error {
	return f.bank.IsSendEnabledCoins(ctx, coins...)
}
func (f Facade) BlockedAddr(addr sdk.AccAddress) bool { return f.bank.BlockedAddr(addr) }
func (f Facade) SendCoins(ctx context.Context, from, to sdk.AccAddress, amt sdk.Coins) error {
	if err := f.deny(ctx, privacyAddress(from)); err != nil {
		return err
	}
	return f.bank.SendCoins(ctx, from, to, amt)
}
func (f Facade) SendCoinsFromAccountToModule(ctx context.Context, sender sdk.AccAddress, recipient string, amt sdk.Coins) error {
	if err := f.deny(ctx, privacyAddress(sender)); err != nil {
		return err
	}
	return f.bank.SendCoinsFromAccountToModule(ctx, sender, recipient, amt)
}
func (f Facade) SendCoinsFromModuleToAccount(ctx context.Context, sender string, recipient sdk.AccAddress, amt sdk.Coins) error {
	if err := f.deny(ctx, sender == privacytypes.ModuleName); err != nil {
		return err
	}
	return f.bank.SendCoinsFromModuleToAccount(ctx, sender, recipient, amt)
}
func (f Facade) SendCoinsFromModuleToModule(ctx context.Context, sender, recipient string, amt sdk.Coins) error {
	if err := f.deny(ctx, sender == privacytypes.ModuleName); err != nil {
		return err
	}
	return f.bank.SendCoinsFromModuleToModule(ctx, sender, recipient, amt)
}
func (f Facade) DelegateCoinsFromAccountToModule(ctx context.Context, sender sdk.AccAddress, recipient string, amt sdk.Coins) error {
	if err := f.deny(ctx, privacyAddress(sender) || recipient == privacytypes.ModuleName); err != nil {
		return err
	}
	return f.bank.DelegateCoinsFromAccountToModule(ctx, sender, recipient, amt)
}
func (f Facade) UndelegateCoinsFromModuleToAccount(ctx context.Context, sender string, recipient sdk.AccAddress, amt sdk.Coins) error {
	if err := f.deny(ctx, sender == privacytypes.ModuleName); err != nil {
		return err
	}
	return f.bank.UndelegateCoinsFromModuleToAccount(ctx, sender, recipient, amt)
}
func (f Facade) MintCoins(ctx context.Context, module string, amt sdk.Coins) error {
	if err := f.deny(ctx, module == privacytypes.ModuleName); err != nil {
		return err
	}
	return f.bank.MintCoins(ctx, module, amt)
}
func (f Facade) BurnCoins(ctx context.Context, module string, amt sdk.Coins) error {
	if err := f.deny(ctx, module == privacytypes.ModuleName); err != nil {
		return err
	}
	return f.bank.BurnCoins(ctx, module, amt)
}
