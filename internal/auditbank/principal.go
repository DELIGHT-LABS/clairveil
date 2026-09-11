package auditbank

import (
	"context"
	"fmt"
	"github.com/DELIGHT-LABS/clairveil/internal/auditinit"
	"sync"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
)

type auditPrincipalScope struct {
	mu               sync.Mutex
	from, to         sdk.AccAddress
	coin             sdk.Coin
	consumed, failed bool
}
type auditPrincipalContextKey struct{}

// Installed last in the explicit development composition. The app-wide bank
// capability facade (including burn and delegation) is a separate boundary.
type auditPrincipal struct {
	bank bankkeeper.Keeper
}

func NewPrincipal(bank bankkeeper.Keeper) *auditPrincipal {
	p := &auditPrincipal{bank: bank}
	bank.AppendSendRestriction(p.restrict)
	return p
}

func (p *auditPrincipal) restrict(ctx context.Context, from, to sdk.AccAddress, coins sdk.Coins) (sdk.AccAddress, error) {
	s, _ := ctx.Value(auditPrincipalContextKey{}).(*auditPrincipalScope)
	if s == nil {
		if from.Equals(authtypes.NewModuleAddress(privacytypes.ModuleName)) {
			return nil, fmt.Errorf("privacy principal requires an active scope")
		}
		return to, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.consumed || s.failed || !from.Equals(s.from) || !to.Equals(s.to) || len(coins) != 1 || !coins[0].Equal(s.coin) {
		s.failed = true
		return nil, fmt.Errorf("privacy principal scope mismatch or reentry")
	}
	s.consumed = true
	return to, nil
}

func (p *auditPrincipal) send(ctx sdk.Context, from, to sdk.AccAddress, coin sdk.Coin) error {
	if auditinit.Active(ctx) {
		return fmt.Errorf("privacy principal forbidden during initialization")
	}
	if from.Equals(to) || !coin.IsValid() || !coin.IsPositive() {
		return fmt.Errorf("invalid privacy principal tuple")
	}
	if previous, ok := ctx.Value(auditPrincipalContextKey{}).(*auditPrincipalScope); ok {
		previous.mu.Lock()
		previous.failed = true
		previous.mu.Unlock()
		return fmt.Errorf("reentrant privacy principal scope")
	}
	s := &auditPrincipalScope{from: append(sdk.AccAddress(nil), from...), to: append(sdk.AccAddress(nil), to...), coin: sdk.NewCoin(coin.Denom, coin.Amount.AddRaw(0))}
	err := p.bank.SendCoins(ctx.WithValue(auditPrincipalContextKey{}, s), from, to, sdk.NewCoins(coin))
	s.mu.Lock()
	valid := s.consumed && !s.failed
	s.mu.Unlock()
	if err != nil {
		return err
	}
	if !valid {
		return fmt.Errorf("privacy principal scope was not consumed exactly once")
	}
	return nil
}

func (p *auditPrincipal) Lock(ctx sdk.Context, funder sdk.AccAddress, coin sdk.Coin) error {
	return p.send(ctx, funder, authtypes.NewModuleAddress(privacytypes.ModuleName), coin)
}
func (p *auditPrincipal) Release(ctx sdk.Context, recipient sdk.AccAddress, coin sdk.Coin) error {
	return p.send(ctx, authtypes.NewModuleAddress(privacytypes.ModuleName), recipient, coin)
}
