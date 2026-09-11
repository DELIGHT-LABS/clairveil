package auditinit

import (
	"context"
	"fmt"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

type key struct{}

func Active(ctx context.Context) bool { v, _ := ctx.Value(key{}).(bool); return v }

// Run keeps the initialization marker inside the application-owned callback.
func Run(ctx sdk.Context, initialize func(sdk.Context) error) error {
	if Active(ctx) {
		return fmt.Errorf("nested audit initialization")
	}
	return initialize(ctx.WithValue(key{}, true))
}
